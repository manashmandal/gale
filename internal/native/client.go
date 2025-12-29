package native

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
)

const (
	runnerVersion  = "2.330.0"
	runnerBaseURL  = "https://github.com/actions/runner/releases/download"
	LabelManagedBy = "gale-native"
)

var runnerChecksums = map[string]string{
	"osx-arm64":   "e7515e45f6de15e37e6f1667bb2f962fb535a86689af1f9b219860300d06de1b",
	"osx-x64":     "40a32b7b87e25b76b595e201e0af376fcb1c3b7838fe21452909756090473ea9",
	"linux-x64":   "af5c33fa94f3cc33b8e97937939136a6b04197e6dadfcfb3b6e33ae1bf41e79a",
	"linux-arm64": "9cb43527912086c7c8fb4119cb06409fcbcbd6f93a2d8507f30b07c495620f5c",
}

type Runner struct {
	ID        string
	PID       int
	Dir       string
	Status    string
	Repo      string
	StartedAt time.Time
	ExitedAt  time.Time
	cmd       *exec.Cmd
	logFile   *os.File
}

const cleanupGracePeriod = 2 * time.Minute

type RunnerConfig struct {
	Token      string
	RepoURL    string
	OrgName    string
	Scope      string
	Labels     []string
	Env        map[string]string
	RunnerName string
}

type Client struct {
	baseDir string
	mu      sync.Mutex
	runners map[string]*Runner
}

func NewClient(baseDir string) (*Client, error) {
	if baseDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("getting home dir: %w", err)
		}
		baseDir = filepath.Join(homeDir, ".gale", "native-runners")
	}

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("creating base dir: %w", err)
	}

	return &Client{
		baseDir: baseDir,
		runners: make(map[string]*Runner),
	}, nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, r := range c.runners {
		if r.cmd != nil && r.cmd.Process != nil {
			_ = r.cmd.Process.Signal(syscall.SIGTERM)
		}
		if r.logFile != nil {
			r.logFile.Close()
		}
	}
	return nil
}

func (c *Client) EnsureRunnerBinary(ctx context.Context) (string, error) {
	arch := getRunnerArch()
	if arch == "" {
		return "", fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	cacheDir := filepath.Join(c.baseDir, "cache", runnerVersion)
	binaryPath := filepath.Join(cacheDir, "bin", "Runner.Listener")

	if _, err := os.Stat(binaryPath); err == nil {
		return cacheDir, nil
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("creating cache dir: %w", err)
	}

	tarballName := fmt.Sprintf("actions-runner-%s-%s.tar.gz", arch, runnerVersion)
	downloadURL := fmt.Sprintf("%s/v%s/%s", runnerBaseURL, runnerVersion, tarballName)

	tarballPath := filepath.Join(cacheDir, tarballName)
	if err := downloadFile(ctx, downloadURL, tarballPath); err != nil {
		return "", fmt.Errorf("downloading runner: %w", err)
	}

	expectedChecksum := runnerChecksums[arch]
	if err := verifyChecksum(tarballPath, expectedChecksum); err != nil {
		os.Remove(tarballPath)
		return "", fmt.Errorf("checksum verification failed: %w", err)
	}

	if err := extractTarGz(tarballPath, cacheDir); err != nil {
		return "", fmt.Errorf("extracting runner: %w", err)
	}

	os.Remove(tarballPath)
	return cacheDir, nil
}

func (c *Client) CreateRunner(ctx context.Context, cfg RunnerConfig) (*Runner, error) {
	runnerBinaryDir, err := c.EnsureRunnerBinary(ctx)
	if err != nil {
		return nil, fmt.Errorf("ensuring runner binary: %w", err)
	}

	// Get runner registration token from GitHub API
	registrationToken, err := getRegistrationToken(ctx, cfg.Token, cfg.RepoURL, cfg.OrgName, cfg.Scope)
	if err != nil {
		return nil, fmt.Errorf("getting registration token: %w", err)
	}

	runnerID := uuid.New().String()[:8]
	runnerName := cfg.RunnerName
	if runnerName == "" {
		runnerName = fmt.Sprintf("gale-native-%s", runnerID)
	}

	runnerDir := filepath.Join(c.baseDir, "work", runnerID)
	if err := os.MkdirAll(runnerDir, 0755); err != nil {
		return nil, fmt.Errorf("creating runner dir: %w", err)
	}

	if err := copyDir(runnerBinaryDir, runnerDir); err != nil {
		os.RemoveAll(runnerDir)
		return nil, fmt.Errorf("copying runner files: %w", err)
	}

	configArgs := []string{
		"--unattended",
		"--ephemeral",
		"--name", runnerName,
		"--token", registrationToken,
		"--labels", strings.Join(cfg.Labels, ","),
		"--work", "_work",
		"--replace",
	}

	if cfg.Scope == "org" && cfg.OrgName != "" {
		configArgs = append(configArgs, "--url", fmt.Sprintf("https://github.com/%s", cfg.OrgName))
	} else {
		configArgs = append(configArgs, "--url", cfg.RepoURL)
	}

	configScript := filepath.Join(runnerDir, "config.sh")
	configCmd := exec.CommandContext(ctx, configScript, configArgs...)
	configCmd.Dir = runnerDir
	configCmd.Env = os.Environ()
	for k, v := range cfg.Env {
		configCmd.Env = append(configCmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	if output, err := configCmd.CombinedOutput(); err != nil {
		os.RemoveAll(runnerDir)
		return nil, fmt.Errorf("configuring runner: %w\nOutput: %s", err, string(output))
	}

	runScript := filepath.Join(runnerDir, "run.sh")
	runCmd := exec.Command(runScript)
	runCmd.Dir = runnerDir
	runCmd.Env = os.Environ()
	for k, v := range cfg.Env {
		runCmd.Env = append(runCmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Capture runner output to log file for debugging
	logFile, err := os.OpenFile(filepath.Join(runnerDir, "runner.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		runCmd.Stdout = logFile
		runCmd.Stderr = logFile
	}

	// Run in its own process group to prevent signal interference from parent
	runCmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := runCmd.Start(); err != nil {
		if logFile != nil {
			logFile.Close()
		}
		os.RemoveAll(runnerDir)
		return nil, fmt.Errorf("starting runner: %w", err)
	}

	runner := &Runner{
		ID:        runnerID,
		PID:       runCmd.Process.Pid,
		Dir:       runnerDir,
		Status:    "running",
		Repo:      cfg.RepoURL,
		StartedAt: time.Now(),
		cmd:       runCmd,
		logFile:   logFile,
	}

	c.mu.Lock()
	c.runners[runnerID] = runner
	c.mu.Unlock()

	go c.waitForCompletion(runner)

	return runner, nil
}

func (c *Client) waitForCompletion(runner *Runner) {
	var exitCode int
	var exitErr error

	if runner.cmd != nil {
		exitErr = runner.cmd.Wait()
		if runner.cmd.ProcessState != nil {
			exitCode = runner.cmd.ProcessState.ExitCode()
		}
	}

	listenerExitTime := time.Now()

	// Check if process was signaled
	signaled := false
	var signal syscall.Signal
	if runner.cmd != nil && runner.cmd.ProcessState != nil {
		if ws, ok := runner.cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
			signaled = ws.Signaled()
			if signaled {
				signal = ws.Signal()
			}
		}
	}

	fmt.Fprintf(os.Stderr, "[GALE DEBUG] Runner.Listener %s exited at %v (code=%d, err=%v, signaled=%v, signal=%v)\n",
		runner.ID, listenerExitTime, exitCode, exitErr, signaled, signal)

	// Log exit information to the runner's log file before closing
	if runner.logFile != nil {
		if exitErr != nil {
			fmt.Fprintf(runner.logFile, "\n[GALE] Runner.Listener exited with error: %v (exit code: %d)\n", exitErr, exitCode)
		} else {
			fmt.Fprintf(runner.logFile, "\n[GALE] Runner.Listener exited normally (exit code: %d)\n", exitCode)
		}
		runner.logFile.Close()
		runner.logFile = nil
	}

	// Wait for all child processes (Runner.Worker, Post steps, etc.) to complete
	// This is critical because Runner.Listener may exit before Post steps finish
	fmt.Fprintf(os.Stderr, "[GALE DEBUG] Waiting for child processes in %s to complete...\n", runner.Dir)
	c.waitForAllProcesses(runner, 5*time.Minute)

	actualExitTime := time.Now()
	fmt.Fprintf(os.Stderr, "[GALE DEBUG] All processes in %s completed at %v (waited %v)\n",
		runner.Dir, actualExitTime, actualExitTime.Sub(listenerExitTime))

	c.mu.Lock()
	if r, exists := c.runners[runner.ID]; exists {
		r.Status = "exited"
		r.ExitedAt = actualExitTime // Use time when ALL processes exited
	}
	c.mu.Unlock()
}

// waitForAllProcesses waits for all processes running in the runner directory to exit
func (c *Client) waitForAllProcesses(runner *Runner, maxWait time.Duration) {
	deadline := time.Now().Add(maxWait)
	checkInterval := 2 * time.Second
	startTime := time.Now()

	// Always wait at least 5 seconds initially - Runner.Worker may not have
	// opened files yet when Runner.Listener exits
	fmt.Fprintf(os.Stderr, "[GALE DEBUG] Initial wait (5s) for Runner.Worker to start...\n")
	time.Sleep(5 * time.Second)

	// Log what detection methods find initially
	c.logProcessDetectionDetails(runner.Dir)

	iteration := 0
	for time.Now().Before(deadline) {
		pids := c.getProcessesInDir(runner.Dir)
		iteration++

		if len(pids) == 0 {
			// Double-check after a short delay to avoid race condition
			time.Sleep(1 * time.Second)
			pids = c.getProcessesInDir(runner.Dir)
			if len(pids) == 0 {
				fmt.Fprintf(os.Stderr, "[GALE DEBUG] All processes completed after %v (iterations=%d)\n",
					time.Since(startTime), iteration)
				return
			}
		}

		fmt.Fprintf(os.Stderr, "[GALE DEBUG] [iter=%d] Still waiting for %d processes in %s (elapsed=%v)\n",
			iteration, len(pids), runner.Dir, time.Since(startTime))

		// Every 10 iterations, log detailed detection info
		if iteration%10 == 0 {
			c.logProcessDetectionDetails(runner.Dir)
		}

		time.Sleep(checkInterval)
	}

	// Timeout - log warning but continue
	pids := c.getProcessesInDir(runner.Dir)
	if len(pids) > 0 {
		fmt.Fprintf(os.Stderr, "[GALE WARN] Timeout waiting for processes in %s after %v, still running: %v\n",
			runner.Dir, time.Since(startTime), pids)
	}
}

// logProcessDetectionDetails logs what each detection method finds
func (c *Client) logProcessDetectionDetails(dir string) {
	lsofPids := c.getProcessesViaLsof(dir)
	pgrepPids := c.getProcessesViaPgrep(dir)
	workerPids := c.getRunnerWorkerProcesses()

	fmt.Fprintf(os.Stderr, "[GALE DEBUG] Process detection details for %s:\n", dir)
	fmt.Fprintf(os.Stderr, "[GALE DEBUG]   lsof +D: %v\n", lsofPids)
	fmt.Fprintf(os.Stderr, "[GALE DEBUG]   pgrep -f dir: %v\n", pgrepPids)
	fmt.Fprintf(os.Stderr, "[GALE DEBUG]   pgrep Runner.Worker: %v\n", workerPids)
}

// getProcessesInDir returns PIDs of processes related to the runner directory
// Uses multiple detection methods for robustness on macOS:
// 1. lsof - finds processes with open files in directory
// 2. pgrep - finds processes with directory in command line
// 3. ps - finds Runner.Worker/Runner.Listener processes
func (c *Client) getProcessesInDir(dir string) []string {
	pidSet := make(map[string]bool)

	// Method 1: Use lsof to find processes with open files in the directory
	// This is the most reliable method on macOS as it catches orphaned processes
	lsofPids := c.getProcessesViaLsof(dir)
	for _, pid := range lsofPids {
		pidSet[pid] = true
	}

	// Method 2: Use pgrep to find processes with directory in command line
	pgrepPids := c.getProcessesViaPgrep(dir)
	for _, pid := range pgrepPids {
		pidSet[pid] = true
	}

	// Method 3: Look for Runner.Worker processes specifically
	// These may be running without the directory in their command line
	workerPids := c.getRunnerWorkerProcesses()
	for _, pid := range workerPids {
		pidSet[pid] = true
	}

	var pids []string
	for pid := range pidSet {
		pids = append(pids, pid)
	}

	if len(pids) > 0 {
		fmt.Fprintf(os.Stderr, "[GALE DEBUG] getProcessesInDir found %d processes: %v (lsof=%d, pgrep=%d, worker=%d)\n",
			len(pids), pids, len(lsofPids), len(pgrepPids), len(workerPids))
	}

	return pids
}

// getProcessesViaLsof uses lsof to find processes with open files in directory
func (c *Client) getProcessesViaLsof(dir string) []string {
	// lsof +D recursively searches directory for open files
	// Use -t for terse output (just PIDs)
	cmd := exec.Command("lsof", "-t", "+D", dir)
	output, err := cmd.Output()
	if err != nil || len(output) == 0 {
		return nil
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var pids []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			pids = append(pids, line)
		}
	}
	return pids
}

// getProcessesViaPgrep uses pgrep to find processes with directory in command line
func (c *Client) getProcessesViaPgrep(dir string) []string {
	cmd := exec.Command("pgrep", "-f", dir)
	output, err := cmd.Output()
	if err != nil || len(output) == 0 {
		return nil
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var pids []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			pids = append(pids, line)
		}
	}
	return pids
}

// getRunnerWorkerProcesses finds GitHub Actions Runner.Worker processes
func (c *Client) getRunnerWorkerProcesses() []string {
	// Look for Runner.Worker processes that may not have the directory in their command line
	cmd := exec.Command("pgrep", "-f", "Runner.Worker")
	output, err := cmd.Output()
	if err != nil || len(output) == 0 {
		return nil
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var pids []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			pids = append(pids, line)
		}
	}
	return pids
}

func (c *Client) ListRunners(ctx context.Context) ([]Runner, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	runners := make([]Runner, 0, len(c.runners))
	for _, r := range c.runners {
		runners = append(runners, *r)
	}
	return runners, nil
}

func (c *Client) GetActiveRunnerCount(ctx context.Context) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := 0
	for _, r := range c.runners {
		if r.Status == "running" {
			count++
		}
	}
	return count, nil
}

func (c *Client) RemoveRunner(ctx context.Context, runnerID string) error {
	c.mu.Lock()
	runner, exists := c.runners[runnerID]
	if exists {
		delete(c.runners, runnerID)
	}
	c.mu.Unlock()

	if !exists {
		return nil
	}

	if runner.cmd != nil && runner.cmd.Process != nil {
		_ = runner.cmd.Process.Kill()
	}

	if runner.logFile != nil {
		runner.logFile.Close()
	}

	fmt.Fprintf(os.Stderr, "[GALE DEBUG] RemoveRunner called for %s, dir=%s\n", runnerID, runner.Dir)

	// Kill any remaining processes in the runner directory before removing
	pids := c.getProcessesInDir(runner.Dir)
	if len(pids) > 0 {
		fmt.Fprintf(os.Stderr, "[GALE DEBUG] Killing remaining processes before removal: %v\n", pids)
		killCmd := exec.Command("pkill", "-9", "-f", runner.Dir)
		_ = killCmd.Run()
		time.Sleep(500 * time.Millisecond)
	}

	return os.RemoveAll(runner.Dir)
}

func (c *Client) CleanupExitedRunners(ctx context.Context) (int, error) {
	c.mu.Lock()
	var toRemove []string
	now := time.Now()
	for id, r := range c.runners {
		if r.Status == "exited" && !r.ExitedAt.IsZero() {
			elapsed := now.Sub(r.ExitedAt)
			fmt.Fprintf(os.Stderr, "[GALE DEBUG] CleanupExitedRunners: runner %s status=%s elapsed=%v gracePeriod=%v willRemove=%v\n",
				id, r.Status, elapsed, cleanupGracePeriod, elapsed >= cleanupGracePeriod)
			if elapsed >= cleanupGracePeriod {
				toRemove = append(toRemove, id)
			}
		} else if r.Status == "exited" {
			fmt.Fprintf(os.Stderr, "[GALE DEBUG] CleanupExitedRunners: runner %s status=%s ExitedAt.IsZero=%v (skipping)\n",
				id, r.Status, r.ExitedAt.IsZero())
		}
	}
	c.mu.Unlock()

	cleaned := 0
	for _, id := range toRemove {
		fmt.Fprintf(os.Stderr, "[GALE DEBUG] CleanupExitedRunners: removing runner %s\n", id)
		if err := c.RemoveRunner(ctx, id); err == nil {
			cleaned++
		}
	}
	return cleaned, nil
}

func (c *Client) StopRunner(ctx context.Context, runnerID string, timeout int) error {
	c.mu.Lock()
	runner, exists := c.runners[runnerID]
	c.mu.Unlock()

	if !exists || runner.cmd == nil || runner.cmd.Process == nil {
		return nil
	}

	_ = runner.cmd.Process.Signal(syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		runner.cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(time.Duration(timeout) * time.Second):
		return runner.cmd.Process.Kill()
	}
}

func (c *Client) IsRunnerExited(ctx context.Context, runnerID string) (bool, error) {
	c.mu.Lock()
	runner, exists := c.runners[runnerID]
	c.mu.Unlock()

	if !exists {
		return true, nil
	}
	return runner.Status == "exited", nil
}

func getRunnerArch() string {
	switch runtime.GOOS {
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return "osx-arm64"
		}
		return "osx-x64"
	case "linux":
		if runtime.GOARCH == "amd64" {
			return "linux-x64"
		}
		if runtime.GOARCH == "arm64" {
			return "linux-arm64"
		}
	}
	return ""
}

func downloadFile(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func verifyChecksum(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}

	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch: got %s, want %s", actual, expected)
	}
	return nil
}

func extractTarGz(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(dest, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			// Remove existing file if it exists (handles partial extractions)
			os.Remove(target)
			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
		case tar.TypeSymlink:
			// Remove existing file/symlink if it exists
			os.Remove(target)
			if err := os.Symlink(header.Linkname, target); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyDir(src, dest string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		destPath := filepath.Join(dest, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}

		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			os.Remove(destPath) // Remove existing if present
			return os.Symlink(link, destPath)
		}

		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		destFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY, info.Mode())
		if err != nil {
			return err
		}
		defer destFile.Close()

		_, err = io.Copy(destFile, srcFile)
		return err
	})
}

type registrationTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

func getRegistrationToken(ctx context.Context, token, repoURL, orgName, scope string) (string, error) {
	var apiURL string

	if scope == "org" && orgName != "" {
		apiURL = fmt.Sprintf("https://api.github.com/orgs/%s/actions/runners/registration-token", orgName)
	} else {
		// Extract owner/repo from URL like https://github.com/owner/repo
		repoURL = strings.TrimSuffix(repoURL, "/")
		repoURL = strings.TrimSuffix(repoURL, ".git")
		parts := strings.Split(repoURL, "/")
		if len(parts) < 2 {
			return "", fmt.Errorf("invalid repo URL: %s", repoURL)
		}
		owner := parts[len(parts)-2]
		repo := parts[len(parts)-1]
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/runners/registration-token", owner, repo)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader([]byte{}))
	if err != nil {
		return "", err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to get registration token: %s (status %d)", string(body), resp.StatusCode)
	}

	var tokenResp registrationTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}

	return tokenResp.Token, nil
}
