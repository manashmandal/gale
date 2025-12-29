package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/manashmandal/gale/internal/config"
	"github.com/manashmandal/gale/internal/docker"
	"github.com/manashmandal/gale/internal/github"
	"github.com/manashmandal/gale/internal/runner"
)

// WorkflowJobEvent represents GitHub's workflow_job webhook payload
type WorkflowJobEvent struct {
	Action      string `json:"action"` // queued, in_progress, completed
	WorkflowJob struct {
		ID         int64    `json:"id"`
		RunID      int64    `json:"run_id"`
		Name       string   `json:"name"`
		Status     string   `json:"status"`
		Labels     []string `json:"labels"`
		RunnerName string   `json:"runner_name"`
		RunnerID   int64    `json:"runner_id"`
	} `json:"workflow_job"`
	Repository struct {
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
	} `json:"repository"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

type Handler struct {
	cfg       *config.Config
	runner    runner.Client
	docker    docker.DockerClient // Deprecated: for backward compatibility with tests
	appClient *github.AppClient   // nil if using PAT mode
	logger    *slog.Logger
	secret    string

	mu            sync.Mutex
	activeRunners map[int64]string // jobID -> runnerID (containerID or native runner ID)

	cleanupDone chan struct{}
}

type HandlerOptions struct {
	DockerClient docker.DockerClient // Deprecated: use RunnerClient
	RunnerClient runner.Client
}

func NewHandler(cfg *config.Config, logger *slog.Logger) (*Handler, error) {
	return NewHandlerWithOptions(cfg, logger, HandlerOptions{})
}

func NewHandlerWithOptions(cfg *config.Config, logger *slog.Logger, opts HandlerOptions) (*Handler, error) {
	var runnerClient runner.Client
	var err error

	if opts.RunnerClient != nil {
		runnerClient = opts.RunnerClient
	} else if opts.DockerClient != nil {
		// Backward compatibility: wrap docker client
		runnerClient = runner.NewDockerAdapter(opts.DockerClient)
	} else {
		// Create runner client based on config mode
		runnerClient, err = runner.NewClient(cfg, logger)
		if err != nil {
			return nil, fmt.Errorf("creating runner client: %w", err)
		}
	}

	return NewHandlerWithRunner(cfg, logger, runnerClient)
}

// NewHandlerWithDocker creates a handler with a specific Docker client (for backward compatibility)
func NewHandlerWithDocker(cfg *config.Config, logger *slog.Logger, dockerClient docker.DockerClient) (*Handler, error) {
	runnerClient := runner.NewDockerAdapter(dockerClient)
	return NewHandlerWithRunner(cfg, logger, runnerClient)
}

func NewHandlerWithRunner(cfg *config.Config, logger *slog.Logger, runnerClient runner.Client) (*Handler, error) {
	h := &Handler{
		cfg:           cfg,
		runner:        runnerClient,
		logger:        logger,
		secret:        cfg.GetWebhookSecret(),
		activeRunners: make(map[int64]string),
	}

	// Initialize GitHub App client if in app mode
	if cfg.IsAppMode() {
		privateKey, err := cfg.GetPrivateKey()
		if err != nil {
			return nil, fmt.Errorf("getting private key: %w", err)
		}

		appClient, err := github.NewAppClient(cfg.GitHub.App.AppID, privateKey)
		if err != nil {
			return nil, fmt.Errorf("creating app client: %w", err)
		}
		h.appClient = appClient
		logger.Info("using GitHub App authentication", "app_id", cfg.GitHub.App.AppID)
	} else {
		logger.Info("using PAT authentication")
	}

	return h, nil
}

func (h *Handler) Close() error {
	// Stop cleanup goroutine if running
	if h.cleanupDone != nil {
		close(h.cleanupDone)
	}
	return h.runner.Close()
}

func (h *Handler) StartCleanup(ctx context.Context) {
	h.cleanupDone = make(chan struct{})

	// Cleanup orphaned containers from previous runs on startup
	h.cleanupExitedContainers()

	// Start periodic cleanup goroutine
	go h.cleanupLoop(ctx)
}

func (h *Handler) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-h.cleanupDone:
			return
		case <-ticker.C:
			h.cleanupExitedContainers()
		}
	}
}

func (h *Handler) cleanupExitedContainers() {
	h.logger.Debug("[DEBUG] cleanupExitedContainers running")
	cleaned, err := h.runner.CleanupExitedRunners(context.Background())
	if err != nil {
		h.logger.Debug("cleanup error", "error", err)
		return
	}
	if cleaned > 0 {
		h.logger.Warn("[DEBUG] cleaned up exited runners", "count", cleaned)
	}

	// Also cleanup phantom GitHub runners (offline but marked busy)
	h.cleanupPhantomGitHubRunners()
}

func (h *Handler) cleanupPhantomGitHubRunners() {
	token := h.cfg.GitHub.Token
	if token == "" {
		return
	}

	owner := h.cfg.GitHub.Owner
	if owner == "" {
		return
	}

	// Get repos from config
	repos := h.cfg.GetRepos()
	if len(repos) == 0 {
		return
	}

	for _, repo := range repos {
		// Construct full repo name (owner/repo)
		fullRepoName := fmt.Sprintf("%s/%s", owner, repo)
		h.cleanupPhantomRunnersForRepo(token, fullRepoName)
	}
}

func (h *Handler) cleanupPhantomRunnersForRepo(token, repoFullName string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// List runners from GitHub
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/actions/runners?per_page=100", repoFullName)
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	var result struct {
		Runners []struct {
			ID     int64  `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
			Busy   bool   `json:"busy"`
		} `json:"runners"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return
	}

	// Find and delete phantom runners (offline + busy + gale-managed)
	for _, r := range result.Runners {
		if r.Status == "offline" && r.Busy && strings.HasPrefix(r.Name, "gale-native-") {
			h.logger.Info("cleaning up phantom GitHub runner",
				"runner_name", r.Name,
				"runner_id", r.ID,
				"repo", repoFullName,
			)

			// Try to delete - will fail if still actually busy, which is fine
			deleteURL := fmt.Sprintf("https://api.github.com/repos/%s/actions/runners/%d", repoFullName, r.ID)
			delReq, err := http.NewRequestWithContext(ctx, "DELETE", deleteURL, nil)
			if err != nil {
				continue
			}
			delReq.Header.Set("Accept", "application/vnd.github+json")
			delReq.Header.Set("Authorization", "Bearer "+token)
			delReq.Header.Set("X-GitHub-Api-Version", "2022-11-28")

			delResp, err := http.DefaultClient.Do(delReq)
			if err != nil {
				continue
			}
			delResp.Body.Close()

			if delResp.StatusCode == http.StatusNoContent {
				h.logger.Info("successfully removed phantom runner",
					"runner_name", r.Name,
					"runner_id", r.ID,
				)
			}
		}
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read body with size limit (1MB max)
	const maxBodySize = 1 << 20 // 1MB
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize))
	if err != nil {
		h.logger.Error("failed to read body", "error", err)
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// Verify signature if secret is configured
	if h.secret != "" {
		signature := r.Header.Get("X-Hub-Signature-256")
		if !h.verifySignature(body, signature) {
			h.logger.Warn("invalid webhook signature")
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}
	}

	// Check event type
	eventType := r.Header.Get("X-GitHub-Event")
	if eventType != "workflow_job" {
		h.logger.Debug("ignoring event", "type", eventType)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Parse payload
	var event WorkflowJobEvent
	if err := json.Unmarshal(body, &event); err != nil {
		h.logger.Error("failed to parse payload", "error", err)
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	h.logger.Info("received workflow_job event",
		"action", event.Action,
		"job_id", event.WorkflowJob.ID,
		"job_name", event.WorkflowJob.Name,
		"repo", event.Repository.FullName,
		"labels", event.WorkflowJob.Labels,
	)

	// Handle event
	ctx := r.Context()
	switch event.Action {
	case "queued":
		h.handleQueued(ctx, &event)
	case "completed":
		h.handleCompleted(ctx, &event)
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleQueued(ctx context.Context, event *WorkflowJobEvent) {
	// Check if this repo is monitored
	repoName := extractRepoName(event.Repository.FullName)
	if !h.cfg.IsRepoMonitored(repoName) {
		h.logger.Warn("received webhook for unregistered repo, ignoring",
			"repo", event.Repository.FullName,
			"hint", "use 'gale webhook register' or 'gale repo add' to monitor this repo",
		)
		return
	}

	// Check if job requires our runner
	if !h.requiresGaleRunner(event.WorkflowJob.Labels) {
		h.logger.Debug("job doesn't require gale runner", "labels", event.WorkflowJob.Labels)
		return
	}

	// Check runner limit and reserve slot atomically
	h.mu.Lock()
	// Prevent duplicate runners for the same job (duplicate webhooks from GitHub retries)
	if _, exists := h.activeRunners[event.WorkflowJob.ID]; exists {
		h.mu.Unlock()
		h.logger.Debug("runner already spawned for job", "job_id", event.WorkflowJob.ID)
		return
	}
	activeCount := len(h.activeRunners)
	if activeCount >= h.cfg.Scaler.MaxRunners {
		h.mu.Unlock()
		h.logger.Warn("at max runners, cannot spawn more",
			"active", activeCount,
			"max", h.cfg.Scaler.MaxRunners,
		)
		return
	}
	// Reserve slot by adding placeholder (prevents race condition)
	h.activeRunners[event.WorkflowJob.ID] = ""
	h.mu.Unlock()

	// Get token - either from GitHub App or PAT
	var token string
	if h.appClient != nil {
		// GitHub App mode - get installation token
		if event.Installation.ID == 0 {
			h.logger.Error("no installation ID in webhook payload")
			h.releaseSlot(event.WorkflowJob.ID)
			return
		}

		var err error
		// Use background context for API calls - don't tie to HTTP request lifecycle
		token, err = h.appClient.GetInstallationToken(context.Background(), event.Installation.ID)
		if err != nil {
			h.logger.Error("failed to get installation token", "error", err, "installation_id", event.Installation.ID)
			h.releaseSlot(event.WorkflowJob.ID)
			return
		}
		h.logger.Debug("got installation token", "installation_id", event.Installation.ID)
	} else {
		// PAT mode
		token = h.cfg.GitHub.Token
	}

	// Spawn runner - use background context to avoid cancellation from HTTP timeout
	runnerCfg := runner.Config{
		Image:       h.cfg.Runner.Image,
		Token:       token,
		RepoURL:     event.Repository.HTMLURL,
		Labels:      h.cfg.Runner.Labels,
		Env:         h.cfg.Runner.Env,
		NetworkMode: h.cfg.Runner.NetworkMode,
		Scope:       "repo",
	}

	r, err := h.runner.CreateRunner(context.Background(), runnerCfg)
	if err != nil {
		h.logger.Error("failed to create runner", "error", err)
		h.releaseSlot(event.WorkflowJob.ID)
		return
	}

	// Update slot with actual runner ID (containerID for Docker, runnerID for native)
	runnerID := r.ContainerID
	if runnerID == "" {
		runnerID = r.ID
	}
	h.mu.Lock()
	h.activeRunners[event.WorkflowJob.ID] = runnerID
	h.mu.Unlock()

	h.logger.Info("spawned runner for job",
		"job_id", event.WorkflowJob.ID,
		"job_name", event.WorkflowJob.Name,
		"runner_id", r.ID,
		"repo", event.Repository.FullName,
	)

	// Monitor for early runner exit (indicates registration failure)
	go h.monitorRunnerStartup(runnerID, event.WorkflowJob.ID, event.WorkflowJob.Name)
}

func (h *Handler) releaseSlot(jobID int64) {
	h.mu.Lock()
	delete(h.activeRunners, jobID)
	h.mu.Unlock()
}

func (h *Handler) monitorRunnerStartup(runnerID string, jobID int64, jobName string) {
	// Wait a few seconds for runner to register
	time.Sleep(5 * time.Second)

	if h.runner == nil {
		return // Handler was closed
	}

	exited, err := h.runner.IsRunnerExited(context.Background(), runnerID)
	if err != nil {
		return // Runner may have been removed already
	}

	if exited {
		h.logger.Error("runner exited immediately - likely token/permission issue",
			"job_id", jobID,
			"job_name", jobName,
			"runner_id", runnerID,
			"common_causes", "GitHub App needs 'Administration: Read & Write' permission, or PAT needs 'repo' and 'admin:org' scopes",
		)
	}
}

func (h *Handler) handleCompleted(ctx context.Context, event *WorkflowJobEvent) {
	// The webhook payload includes the actual runner that ran the job
	// Use that instead of our spawn-time mapping (which may be wrong due to GitHub's job assignment)
	actualRunnerName := event.WorkflowJob.RunnerName
	actualRunnerID := ""

	// Our runners are named "gale-native-<uuid>" - extract the UUID
	if strings.HasPrefix(actualRunnerName, "gale-native-") {
		actualRunnerID = strings.TrimPrefix(actualRunnerName, "gale-native-")
	}

	// Clean up our mapping (for bookkeeping, even if the mapping was wrong)
	h.mu.Lock()
	mappedRunnerID, exists := h.activeRunners[event.WorkflowJob.ID]
	if exists {
		delete(h.activeRunners, event.WorkflowJob.ID)
	}
	h.mu.Unlock()

	// Use the actual runner ID from webhook if available, otherwise fall back to mapped
	runnerID := actualRunnerID
	if runnerID == "" {
		runnerID = mappedRunnerID
	}

	h.logger.Info("[DEBUG] handleCompleted called",
		"job_id", event.WorkflowJob.ID,
		"actual_runner_name", actualRunnerName,
		"actual_runner_id", actualRunnerID,
		"mapped_runner_id", mappedRunnerID,
		"using_runner_id", runnerID,
		"exists", exists,
		"status", event.WorkflowJob.Status,
	)

	if runnerID != "" {
		h.logger.Info("job completed, initiating graceful shutdown",
			"job_id", event.WorkflowJob.ID,
			"runner_id", runnerID,
		)
		// Run graceful shutdown in background to not block webhook response
		go h.gracefulShutdown(runnerID, event.WorkflowJob.ID)
	}
}

func (h *Handler) gracefulShutdown(runnerID string, jobID int64) {
	ctx := context.Background()

	h.logger.Info("[DEBUG] gracefulShutdown started",
		"job_id", jobID,
		"runner_id", runnerID,
	)

	if h.runner == nil {
		h.logger.Info("[DEBUG] gracefulShutdown: runner is nil, returning")
		return // Handler was closed
	}

	// First check if runner already exited (e.g., ephemeral mode on Linux, or --once mode)
	exited, err := h.runner.IsRunnerExited(ctx, runnerID)
	if err == nil && exited {
		h.logger.Info("runner already exited",
			"job_id", jobID,
			"runner_id", runnerID,
		)
		return
	}

	// With --once flag on macOS, the runner should exit on its own after completing all steps.
	// This timeout is a safety net in case something goes wrong.
	// 2 minutes should be sufficient for Post steps to complete.
	postStepWait := 120 * time.Second
	h.logger.Info("[DEBUG] waiting for runner to exit (with --once flag, should exit after Post steps)",
		"job_id", jobID,
		"runner_id", runnerID,
		"timeout", postStepWait,
	)

	// Poll while waiting - runner might exit on its own (ephemeral/--once mode)
	deadline := time.Now().Add(postStepWait)
	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Second)
		if h.runner == nil {
			return // Handler was closed
		}
		exited, err := h.runner.IsRunnerExited(ctx, runnerID)
		if err != nil {
			h.logger.Debug("error checking runner status", "error", err)
			break
		}
		if exited {
			h.logger.Info("runner exited gracefully during Post step wait",
				"job_id", jobID,
				"runner_id", runnerID,
			)
			return
		}
	}

	if h.runner == nil {
		return // Handler was closed
	}

	// Post step wait complete - now stop the runner gracefully
	h.logger.Info("stopping runner after Post step wait",
		"job_id", jobID,
		"runner_id", runnerID,
	)
	if err := h.runner.StopRunner(ctx, runnerID, 30); err != nil {
		h.logger.Warn("failed to stop runner", "error", err)
	}
}

func (h *Handler) requiresGaleRunner(jobLabels []string) bool {
	configuredLabels := h.cfg.Runner.Labels
	for _, jobLabel := range jobLabels {
		for _, configuredLabel := range configuredLabels {
			if strings.EqualFold(jobLabel, configuredLabel) {
				return true
			}
		}
		// Also match "self-hosted" as a fallback for standard self-hosted runners
		if strings.EqualFold(jobLabel, "self-hosted") {
			return true
		}
	}
	return false
}

func (h *Handler) verifySignature(payload []byte, signature string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	sig := strings.TrimPrefix(signature, "sha256=")

	mac := hmac.New(sha256.New, []byte(h.secret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(sig), []byte(expected))
}

// GetActiveRunnerCount returns the number of active runners
func (h *Handler) GetActiveRunnerCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.activeRunners)
}

// extractRepoName extracts the repo name from "owner/repo" format
func extractRepoName(fullName string) string {
	parts := strings.Split(fullName, "/")
	if len(parts) == 2 {
		return parts[1]
	}
	return fullName
}
