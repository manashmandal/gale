package native

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	ID              string
	Name            string
	PID             int
	PGID            int
	Dir             string
	Status          string
	Repo            string
	Scope           string
	OrgName         string
	GitHubToken     string
	GitHubRunnerID  int64
	GitHubEphemeral bool
	StartedAt       time.Time
	ExitedAt        time.Time
	cmd             *exec.Cmd
	logFile         *os.File
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

// downloadAndExtractRunner downloads and extracts the runner binary directly to the target directory.
// Each runner gets its own copy - no shared cache to avoid race conditions.
func (c *Client) downloadAndExtractRunner(ctx context.Context, targetDir string) error {
	arch := getRunnerArch()
	if arch == "" {
		return fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	tarballName := fmt.Sprintf("actions-runner-%s-%s.tar.gz", arch, runnerVersion)
	downloadURL := fmt.Sprintf("%s/v%s/%s", runnerBaseURL, runnerVersion, tarballName)

	tarballPath := filepath.Join(targetDir, tarballName)
	if err := downloadFile(ctx, downloadURL, tarballPath); err != nil {
		return fmt.Errorf("downloading runner: %w", err)
	}

	expectedChecksum := runnerChecksums[arch]
	if err := verifyChecksum(tarballPath, expectedChecksum); err != nil {
		os.Remove(tarballPath)
		return fmt.Errorf("checksum verification failed: %w", err)
	}

	if err := extractTarGz(tarballPath, targetDir); err != nil {
		return fmt.Errorf("extracting runner: %w", err)
	}

	os.Remove(tarballPath)
	return nil
}

func (c *Client) CreateRunner(ctx context.Context, cfg RunnerConfig) (*Runner, error) {
	// Generate unique runner ID first - this ensures complete isolation
	runnerID := uuid.New().String()[:8]
	runnerName := cfg.RunnerName
	if runnerName == "" {
		runnerName = fmt.Sprintf("gale-native-%s", runnerID)
	}

	// Each runner gets its own fully isolated directory - no shared cache
	runnerDir := filepath.Join(c.baseDir, "work", runnerID)

	if err := os.MkdirAll(runnerDir, 0755); err != nil {
		return nil, fmt.Errorf("creating runner dir: %w", err)
	}

	// Download and extract runner directly to this runner's directory
	// No shared cache - each runner is completely independent
	if err := c.downloadAndExtractRunner(ctx, runnerDir); err != nil {
		os.RemoveAll(runnerDir)
		return nil, fmt.Errorf("setting up runner binary: %w", err)
	}

	// Get runner registration token from GitHub API
	registrationToken, err := getRegistrationToken(ctx, cfg.Token, cfg.RepoURL, cfg.OrgName, cfg.Scope)
	if err != nil {
		os.RemoveAll(runnerDir)
		return nil, fmt.Errorf("getting registration token: %w", err)
	}

	// Pre-create the _work directory structure for this runner instance
	workDir := filepath.Join(runnerDir, "_work")
	tempDir := filepath.Join(workDir, "_temp")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		os.RemoveAll(runnerDir)
		return nil, fmt.Errorf("creating work temp dir: %w", err)
	}

	// On macOS, the upstream runner's built-in `--ephemeral` mode can exit before Post steps complete.
	// Work around this by registering as non-ephemeral and emulating ephemeral semantics with `run.sh --once`
	// plus GitHub API de-registration after exit.
	useGitHubEphemeral := runtime.GOOS != "darwin"

	configArgs := []string{
		"--unattended",
		"--name", runnerName,
		"--token", registrationToken,
		"--labels", strings.Join(cfg.Labels, ","),
		"--work", "_work",
		"--replace",
	}
	if useGitHubEphemeral {
		configArgs = append(configArgs, "--ephemeral")
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

	ghRunnerID, _ := readLocalRunnerID(filepath.Join(runnerDir, ".runner"))

	runScript := filepath.Join(runnerDir, "run.sh")
	runArgs := []string{}
	if !useGitHubEphemeral {
		runArgs = append(runArgs, "--once")
	}
	runCmd := exec.Command(runScript, runArgs...)
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
		if !useGitHubEphemeral && cfg.Token != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			id := ghRunnerID
			if id == 0 {
				if foundID, findErr := findGitHubRunnerID(ctx, cfg.Token, cfg.RepoURL, cfg.OrgName, cfg.Scope, runnerName); findErr == nil {
					id = foundID
				}
			}
			if id != 0 {
				_ = deleteGitHubRunner(ctx, cfg.Token, cfg.RepoURL, cfg.OrgName, cfg.Scope, id)
			}
		}
		os.RemoveAll(runnerDir)
		return nil, fmt.Errorf("starting runner: %w", err)
	}

	pgid, _ := syscall.Getpgid(runCmd.Process.Pid)

	runner := &Runner{
		ID:              runnerID,
		Name:            runnerName,
		PID:             runCmd.Process.Pid,
		PGID:            pgid,
		Dir:             runnerDir,
		Status:          "running",
		Repo:            cfg.RepoURL,
		Scope:           cfg.Scope,
		OrgName:         cfg.OrgName,
		GitHubToken:     cfg.Token,
		GitHubRunnerID:  ghRunnerID,
		GitHubEphemeral: useGitHubEphemeral,
		StartedAt:       time.Now(),
		cmd:             runCmd,
		logFile:         logFile,
	}

	// Only lock for map registration - runner creation is fully parallel
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

	if !runner.GitHubEphemeral && runner.GitHubToken != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		ghID := runner.GitHubRunnerID
		if ghID == 0 && runner.Name != "" {
			if foundID, err := findGitHubRunnerID(ctx, runner.GitHubToken, runner.Repo, runner.OrgName, runner.Scope, runner.Name); err == nil {
				ghID = foundID
			}
		}
		if ghID != 0 {
			if err := deleteGitHubRunner(ctx, runner.GitHubToken, runner.Repo, runner.OrgName, runner.Scope, ghID); err != nil {
				fmt.Fprintf(os.Stderr, "[GALE WARN] Failed to de-register runner %s (id=%d): %v\n", runner.Name, ghID, err)
			}
		} else {
			fmt.Fprintf(os.Stderr, "[GALE WARN] Unable to determine GitHub runner ID for %s; skipping de-registration\n", runner.Name)
		}
	}

	c.mu.Lock()
	if r, exists := c.runners[runner.ID]; exists {
		r.Status = "exited"
		r.ExitedAt = actualExitTime // Use time when ALL processes exited
	}
	c.mu.Unlock()
}

// waitForAllProcesses waits for all processes using the runner directory to exit.
// This includes both the runner's process group AND any job scripts that may run
// in their own process group.
func (c *Client) waitForAllProcesses(runner *Runner, maxWait time.Duration) {
	deadline := time.Now().Add(maxWait)
	checkInterval := 2 * time.Second
	startTime := time.Now()
	iteration := 0

	for time.Now().Before(deadline) {
		iteration++

		// Check both process group AND processes with open files in directory
		pgAlive := false
		if runner.PGID > 0 {
			pgAlive, _ = processGroupAlive(runner.PGID)
		}

		// Use multiple methods to find processes using the runner directory:
		// 1. pgrep -f: Fast, finds processes with directory in command line
		// 2. lsof +D: Slower but catches processes with open file handles
		pgrepPids := getProcessesViaPgrep(runner.Dir)
		lsofPids := getProcessesViaLsof(runner.Dir)

		allDone := !pgAlive && len(pgrepPids) == 0 && len(lsofPids) == 0
		if allDone {
			// Double-check after a short delay to avoid race condition
			// (brief gap between commands in shell script)
			time.Sleep(1 * time.Second)
			pgrepPids = getProcessesViaPgrep(runner.Dir)
			lsofPids = getProcessesViaLsof(runner.Dir)
			if len(pgrepPids) == 0 && len(lsofPids) == 0 {
				fmt.Fprintf(os.Stderr, "[GALE DEBUG] All processes completed after %v (iterations=%d)\n",
					time.Since(startTime), iteration)
				return
			}
		}

		if iteration%10 == 0 {
			fmt.Fprintf(os.Stderr, "[GALE DEBUG] [iter=%d] Still waiting: pgAlive=%v, pgrepPids=%v, lsofPids=%v (elapsed=%v)\n",
				iteration, pgAlive, pgrepPids, lsofPids, time.Since(startTime))
		}
		time.Sleep(checkInterval)
	}

	lsofPids := getProcessesViaLsof(runner.Dir)
	if len(lsofPids) > 0 {
		fmt.Fprintf(os.Stderr, "[GALE WARN] Timeout waiting for process group %d after %v\n",
			runner.PGID, time.Since(startTime))
	}
}

func processGroupAlive(pgid int) (bool, error) {
	if pgid <= 0 {
		return false, nil
	}
	err := syscall.Kill(-pgid, 0)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	if errors.Is(err, syscall.EPERM) {
		// Process group exists, but we don't have permissions (shouldn't happen for our own runners).
		return true, nil
	}
	return false, err
}

func processAlive(pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	if errors.Is(err, syscall.EPERM) {
		return true, nil
	}
	return false, err
}

// getProcessesViaPgrep uses pgrep to find processes with directory in command line.
// This is fast and catches bash scripts running in the runner directory.
func getProcessesViaPgrep(dir string) []string {
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

// getProcessesViaLsof uses lsof to find processes with open files in directory.
// This catches job scripts that run in their own process group.
func getProcessesViaLsof(dir string) []string {
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

	if runner.PGID > 0 && runner.PGID != syscall.Getpgrp() {
		// Best-effort kill of the entire process group (Runner.Listener + any children).
		_ = syscall.Kill(-runner.PGID, syscall.SIGKILL)
		time.Sleep(500 * time.Millisecond)
	} else if runner.cmd != nil && runner.cmd.Process != nil {
		_ = runner.cmd.Process.Kill()
		time.Sleep(500 * time.Millisecond)
	}

	if runner.logFile != nil {
		runner.logFile.Close()
	}

	fmt.Fprintf(os.Stderr, "[GALE DEBUG] RemoveRunner called for %s, dir=%s\n", runnerID, runner.Dir)

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

	// Prefer signaling the runner's process group so children (Runner.Worker, etc.) are stopped too.
	if runner.PGID > 0 && runner.PGID != syscall.Getpgrp() {
		_ = syscall.Kill(-runner.PGID, syscall.SIGTERM)
	} else {
		_ = runner.cmd.Process.Signal(syscall.SIGTERM)
	}

	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	for time.Now().Before(deadline) {
		if runner.PGID > 0 {
			alive, err := processGroupAlive(runner.PGID)
			if err == nil && !alive {
				return nil
			}
		} else {
			alive, err := processAlive(runner.PID)
			if err == nil && !alive {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}

	if runner.PGID > 0 && runner.PGID != syscall.Getpgrp() {
		if err := syscall.Kill(-runner.PGID, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		return nil
	}
	if err := runner.cmd.Process.Kill(); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
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

type localRunnerFile struct {
	AgentID   int64  `json:"agentId"`
	AgentName string `json:"agentName"`
}

func readLocalRunnerID(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var f localRunnerFile
	if err := json.Unmarshal(data, &f); err != nil {
		return 0, err
	}
	if f.AgentID == 0 {
		return 0, fmt.Errorf("missing agentId in %s", path)
	}
	return f.AgentID, nil
}

type listRunnersResponse struct {
	Runners []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"runners"`
}

func parseRepoOwnerRepo(repoURL string) (string, string, error) {
	repoURL = strings.TrimSuffix(repoURL, "/")
	repoURL = strings.TrimSuffix(repoURL, ".git")
	parts := strings.Split(repoURL, "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid repo URL: %s", repoURL)
	}
	owner := parts[len(parts)-2]
	repo := parts[len(parts)-1]
	return owner, repo, nil
}

func findGitHubRunnerID(ctx context.Context, token, repoURL, orgName, scope, runnerName string) (int64, error) {
	var apiURL string
	if scope == "org" && orgName != "" {
		apiURL = fmt.Sprintf("https://api.github.com/orgs/%s/actions/runners?per_page=100", orgName)
	} else {
		owner, repo, err := parseRepoOwnerRepo(repoURL)
		if err != nil {
			return 0, err
		}
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/runners?per_page=100", owner, repo)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("listing runners failed: %s (status %d)", string(body), resp.StatusCode)
	}

	var listResp listRunnersResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return 0, fmt.Errorf("decoding response: %w", err)
	}

	for _, r := range listResp.Runners {
		if r.Name == runnerName {
			return r.ID, nil
		}
	}
	return 0, fmt.Errorf("runner %q not found", runnerName)
}

func deleteGitHubRunner(ctx context.Context, token, repoURL, orgName, scope string, runnerID int64) error {
	var apiURL string
	if scope == "org" && orgName != "" {
		apiURL = fmt.Sprintf("https://api.github.com/orgs/%s/actions/runners/%d", orgName, runnerID)
	} else {
		owner, repo, err := parseRepoOwnerRepo(repoURL)
		if err != nil {
			return err
		}
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/runners/%d", owner, repo, runnerID)
	}

	req, err := http.NewRequestWithContext(ctx, "DELETE", apiURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("failed to delete runner: %s (status %d)", string(body), resp.StatusCode)
}
