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

	"github.com/manashmandal/gale/internal/config"
	"github.com/manashmandal/gale/internal/docker"
	"github.com/manashmandal/gale/internal/github"
)

// WorkflowJobEvent represents GitHub's workflow_job webhook payload
type WorkflowJobEvent struct {
	Action      string `json:"action"` // queued, in_progress, completed
	WorkflowJob struct {
		ID          int64    `json:"id"`
		RunID       int64    `json:"run_id"`
		Name        string   `json:"name"`
		Status      string   `json:"status"`
		Labels      []string `json:"labels"`
		RunnerName  string   `json:"runner_name"`
		RunnerID    int64    `json:"runner_id"`
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
	docker    *docker.Client
	appClient *github.AppClient // nil if using PAT mode
	logger    *slog.Logger
	secret    string

	mu            sync.Mutex
	activeRunners map[int64]string // jobID -> containerID
}

func NewHandler(cfg *config.Config, logger *slog.Logger) (*Handler, error) {
	dockerClient, err := docker.NewClient(cfg.Docker.Host)
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}

	h := &Handler{
		cfg:           cfg,
		docker:        dockerClient,
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
	return h.docker.Close()
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read body
	body, err := io.ReadAll(r.Body)
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
		h.logger.Debug("repo not in monitored list", "repo", repoName, "monitored", h.cfg.GetRepos())
		return
	}

	// Check if job requires our runner
	if !h.requiresGaleRunner(event.WorkflowJob.Labels) {
		h.logger.Debug("job doesn't require gale runner", "labels", event.WorkflowJob.Labels)
		return
	}

	// Check runner limit
	h.mu.Lock()
	activeCount := len(h.activeRunners)
	h.mu.Unlock()

	if activeCount >= h.cfg.Scaler.MaxRunners {
		h.logger.Warn("at max runners, cannot spawn more",
			"active", activeCount,
			"max", h.cfg.Scaler.MaxRunners,
		)
		return
	}

	// Get token - either from GitHub App or PAT
	var token string
	if h.appClient != nil {
		// GitHub App mode - get installation token
		if event.Installation.ID == 0 {
			h.logger.Error("no installation ID in webhook payload")
			return
		}

		var err error
		token, err = h.appClient.GetInstallationToken(ctx, event.Installation.ID)
		if err != nil {
			h.logger.Error("failed to get installation token", "error", err, "installation_id", event.Installation.ID)
			return
		}
		h.logger.Debug("got installation token", "installation_id", event.Installation.ID)
	} else {
		// PAT mode
		token = h.cfg.GitHub.Token
	}

	// Spawn runner
	runnerCfg := docker.RunnerConfig{
		Image:       h.cfg.Runner.Image,
		Token:       token,
		RepoURL:     event.Repository.HTMLURL,
		Labels:      h.cfg.Runner.Labels,
		Env:         h.cfg.Runner.Env,
		NetworkMode: h.cfg.Runner.NetworkMode,
		Scope:       "repo",
	}

	runner, err := h.docker.CreateRunner(ctx, runnerCfg)
	if err != nil {
		h.logger.Error("failed to create runner", "error", err)
		return
	}

	h.mu.Lock()
	h.activeRunners[event.WorkflowJob.ID] = runner.ContainerID
	h.mu.Unlock()

	h.logger.Info("spawned runner for job",
		"job_id", event.WorkflowJob.ID,
		"job_name", event.WorkflowJob.Name,
		"runner_id", runner.ID,
		"container_id", runner.ContainerID[:12],
		"repo", event.Repository.FullName,
	)
}

func (h *Handler) handleCompleted(ctx context.Context, event *WorkflowJobEvent) {
	h.mu.Lock()
	containerID, exists := h.activeRunners[event.WorkflowJob.ID]
	if exists {
		delete(h.activeRunners, event.WorkflowJob.ID)
	}
	h.mu.Unlock()

	if exists {
		h.logger.Info("job completed, cleaning up runner",
			"job_id", event.WorkflowJob.ID,
			"container_id", containerID[:12],
		)
		// Runner should auto-exit (ephemeral), but cleanup just in case
		_ = h.docker.RemoveRunner(ctx, containerID)
	}
}

func (h *Handler) requiresGaleRunner(labels []string) bool {
	for _, label := range labels {
		if strings.EqualFold(label, "gale") || strings.EqualFold(label, "self-hosted") {
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
