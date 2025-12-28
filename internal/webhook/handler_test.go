package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/manashmandal/gale/internal/config"
	"github.com/manashmandal/gale/internal/docker"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestExtractRepoName(t *testing.T) {
	tests := []struct {
		name     string
		fullName string
		expected string
	}{
		{
			name:     "owner/repo format",
			fullName: "manashmandal/gale",
			expected: "gale",
		},
		{
			name:     "just repo name",
			fullName: "gale",
			expected: "gale",
		},
		{
			name:     "org/repo format",
			fullName: "my-org/my-repo",
			expected: "my-repo",
		},
		{
			name:     "empty string",
			fullName: "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractRepoName(tt.fullName)
			if got != tt.expected {
				t.Errorf("extractRepoName(%q) = %q, want %q", tt.fullName, got, tt.expected)
			}
		})
	}
}

func TestVerifySignature(t *testing.T) {
	secret := "test-secret"
	payload := []byte(`{"action":"queued"}`)

	// Generate valid signature
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	h := &Handler{
		secret: secret,
	}

	tests := []struct {
		name      string
		payload   []byte
		signature string
		expected  bool
	}{
		{
			name:      "valid signature",
			payload:   payload,
			signature: validSig,
			expected:  true,
		},
		{
			name:      "invalid signature",
			payload:   payload,
			signature: "sha256=invalid",
			expected:  false,
		},
		{
			name:      "missing prefix",
			payload:   payload,
			signature: hex.EncodeToString(mac.Sum(nil)),
			expected:  false,
		},
		{
			name:      "wrong payload",
			payload:   []byte(`{"action":"completed"}`),
			signature: validSig,
			expected:  false,
		},
		{
			name:      "empty signature",
			payload:   payload,
			signature: "",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := h.verifySignature(tt.payload, tt.signature)
			if got != tt.expected {
				t.Errorf("verifySignature() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestRequiresGaleRunner(t *testing.T) {
	cfg := &config.Config{
		Runner: config.RunnerConfig{
			Labels: []string{"gale", "gale-linux"},
		},
	}
	h := &Handler{cfg: cfg}

	tests := []struct {
		name     string
		labels   []string
		expected bool
	}{
		{
			name:     "has gale label",
			labels:   []string{"gale", "linux"},
			expected: true,
		},
		{
			name:     "has self-hosted label",
			labels:   []string{"self-hosted", "x64"},
			expected: true,
		},
		{
			name:     "case insensitive gale",
			labels:   []string{"GALE", "docker"},
			expected: true,
		},
		{
			name:     "case insensitive self-hosted",
			labels:   []string{"Self-Hosted"},
			expected: true,
		},
		{
			name:     "has gale-linux label",
			labels:   []string{"gale-linux"},
			expected: true,
		},
		{
			name:     "no matching labels",
			labels:   []string{"ubuntu-latest", "linux"},
			expected: false,
		},
		{
			name:     "empty labels",
			labels:   []string{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := h.requiresGaleRunner(tt.labels)
			if got != tt.expected {
				t.Errorf("requiresGaleRunner(%v) = %v, want %v", tt.labels, got, tt.expected)
			}
		})
	}
}

func TestServeHTTP_MethodNotAllowed(t *testing.T) {
	h := &Handler{}

	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET request status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestServeHTTP_IgnoreNonWorkflowJob(t *testing.T) {
	cfg := &config.Config{}
	cfg.GitHub.Repos = []string{}

	h := &Handler{
		cfg:           cfg,
		logger:        testLogger(),
		activeRunners: make(map[int64]string),
	}

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader("{}"))
	req.Header.Set("X-GitHub-Event", "push")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("non-workflow_job event status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestServeHTTP_InvalidSignature(t *testing.T) {
	cfg := &config.Config{}

	h := &Handler{
		cfg:           cfg,
		logger:        testLogger(),
		secret:        "test-secret",
		activeRunners: make(map[int64]string),
	}

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader("{}"))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	req.Header.Set("X-Hub-Signature-256", "sha256=invalid")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("invalid signature status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestServeHTTP_ValidWorkflowJob(t *testing.T) {
	cfg := &config.Config{}
	cfg.GitHub.Repos = []string{}

	h := &Handler{
		cfg:           cfg,
		logger:        testLogger(),
		activeRunners: make(map[int64]string),
	}

	event := WorkflowJobEvent{
		Action: "queued",
	}
	event.WorkflowJob.ID = 123
	event.WorkflowJob.Name = "test-job"
	event.WorkflowJob.Labels = []string{"ubuntu-latest"} // Not gale, so won't trigger runner
	event.Repository.FullName = "owner/repo"

	body, _ := json.Marshal(event)

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("valid workflow_job status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestGetActiveRunnerCount(t *testing.T) {
	h := &Handler{
		activeRunners: map[int64]string{
			1: "container1",
			2: "container2",
			3: "container3",
		},
	}

	if got := h.GetActiveRunnerCount(); got != 3 {
		t.Errorf("GetActiveRunnerCount() = %d, want 3", got)
	}
}

func TestServeHTTP_InvalidJSON(t *testing.T) {
	cfg := &config.Config{}
	cfg.GitHub.Repos = []string{}

	h := &Handler{
		cfg:           cfg,
		logger:        testLogger(),
		activeRunners: make(map[int64]string),
	}

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader("invalid json{"))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid JSON status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestServeHTTP_CompletedAction_NotTracked(t *testing.T) {
	cfg := &config.Config{}
	cfg.GitHub.Repos = []string{}

	h := &Handler{
		cfg:           cfg,
		logger:        testLogger(),
		activeRunners: make(map[int64]string), // No tracked runners
	}

	event := WorkflowJobEvent{
		Action: "completed",
	}
	event.WorkflowJob.ID = 123
	event.WorkflowJob.Name = "test-job"
	event.Repository.FullName = "owner/repo"

	body, _ := json.Marshal(event)

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	// Should return OK even when runner is not tracked (no cleanup needed)
	if w.Code != http.StatusOK {
		t.Errorf("completed event status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestServeHTTP_QueuedWithoutGaleLabel(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Token: "test-token",
			Repos: []string{}, // monitor all repos
		},
		Scaler: config.ScalerConfig{
			MaxRunners: 10,
		},
	}

	h := &Handler{
		cfg:           cfg,
		logger:        testLogger(),
		activeRunners: make(map[int64]string),
	}

	event := WorkflowJobEvent{
		Action: "queued",
	}
	event.WorkflowJob.ID = 456
	event.WorkflowJob.Name = "test-job"
	event.WorkflowJob.Labels = []string{"ubuntu-latest"} // No gale label
	event.Repository.FullName = "owner/repo"
	event.Repository.HTMLURL = "https://github.com/owner/repo"

	body, _ := json.Marshal(event)

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	// Should return 200 because job doesn't require gale runner
	if w.Code != http.StatusOK {
		t.Errorf("queued without gale label status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestServeHTTP_RepoNotMonitored(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Repos: []string{"other-repo"}, // only monitor other-repo
		},
	}

	h := &Handler{
		cfg:           cfg,
		logger:        testLogger(),
		activeRunners: make(map[int64]string),
	}

	event := WorkflowJobEvent{
		Action: "queued",
	}
	event.WorkflowJob.ID = 789
	event.WorkflowJob.Labels = []string{"gale"}
	event.Repository.FullName = "owner/repo" // not in monitored list

	body, _ := json.Marshal(event)

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("unmonitored repo status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestServeHTTP_MaxRunnersReached(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Repos: []string{}, // monitor all
		},
		Scaler: config.ScalerConfig{
			MaxRunners: 2,
		},
	}

	h := &Handler{
		cfg:    cfg,
		logger: testLogger(),
		activeRunners: map[int64]string{
			1: "container1",
			2: "container2",
		},
	}

	event := WorkflowJobEvent{
		Action: "queued",
	}
	event.WorkflowJob.ID = 999
	event.WorkflowJob.Labels = []string{"gale"}
	event.Repository.FullName = "owner/repo"

	body, _ := json.Marshal(event)

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("max runners reached status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestServeHTTP_ValidSignature(t *testing.T) {
	secret := "webhook-secret"
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Repos: []string{},
		},
	}

	h := &Handler{
		cfg:           cfg,
		logger:        testLogger(),
		secret:        secret,
		activeRunners: make(map[int64]string),
	}

	payload := `{"action":"queued","workflow_job":{"id":1,"labels":["ubuntu"]},"repository":{"full_name":"o/r"}}`
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	req.Header.Set("X-Hub-Signature-256", validSig)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("valid signature status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleCompleted_RunnerNotTracked(t *testing.T) {
	h := &Handler{
		logger:        testLogger(),
		activeRunners: make(map[int64]string),
	}

	event := &WorkflowJobEvent{
		Action: "completed",
	}
	event.WorkflowJob.ID = 999 // not in activeRunners

	// Should not panic
	h.handleCompleted(nil, event)

	if len(h.activeRunners) != 0 {
		t.Error("activeRunners should remain empty")
	}
}

func TestWorkflowJobEventParsing(t *testing.T) {
	payload := `{
		"action": "queued",
		"workflow_job": {
			"id": 12345,
			"run_id": 67890,
			"name": "build",
			"status": "queued",
			"labels": ["gale", "linux"],
			"runner_name": "",
			"runner_id": 0
		},
		"repository": {
			"full_name": "owner/repo",
			"html_url": "https://github.com/owner/repo"
		},
		"installation": {
			"id": 99999
		}
	}`

	var event WorkflowJobEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		t.Fatalf("Failed to parse WorkflowJobEvent: %v", err)
	}

	if event.Action != "queued" {
		t.Errorf("Action = %q, want queued", event.Action)
	}
	if event.WorkflowJob.ID != 12345 {
		t.Errorf("WorkflowJob.ID = %d, want 12345", event.WorkflowJob.ID)
	}
	if event.WorkflowJob.Name != "build" {
		t.Errorf("WorkflowJob.Name = %q, want build", event.WorkflowJob.Name)
	}
	if len(event.WorkflowJob.Labels) != 2 {
		t.Errorf("WorkflowJob.Labels len = %d, want 2", len(event.WorkflowJob.Labels))
	}
	if event.Repository.FullName != "owner/repo" {
		t.Errorf("Repository.FullName = %q, want owner/repo", event.Repository.FullName)
	}
	if event.Installation.ID != 99999 {
		t.Errorf("Installation.ID = %d, want 99999", event.Installation.ID)
	}
}

// Tests with mock docker client

func TestNewHandlerWithDocker(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Token: "test-token",
			Repos: []string{},
		},
		Scaler: config.ScalerConfig{
			MaxRunners: 10,
		},
	}

	mockDocker := docker.NewMockClient()
	h, err := NewHandlerWithDocker(cfg, testLogger(), mockDocker)
	if err != nil {
		t.Fatalf("NewHandlerWithDocker() error = %v", err)
	}
	if h == nil {
		t.Fatal("NewHandlerWithDocker() returned nil")
	}
}

func TestServeHTTP_QueuedWithGaleLabel_WithMock(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Token: "test-token",
			Repos: []string{}, // monitor all repos
		},
		Scaler: config.ScalerConfig{
			MaxRunners: 10,
		},
		Runner: config.RunnerConfig{
			Image:  "test-image",
			Labels: []string{"gale"},
		},
	}

	mockDocker := docker.NewMockClient()
	h, err := NewHandlerWithDocker(cfg, testLogger(), mockDocker)
	if err != nil {
		t.Fatalf("NewHandlerWithDocker() error = %v", err)
	}

	event := WorkflowJobEvent{
		Action: "queued",
	}
	event.WorkflowJob.ID = 456
	event.WorkflowJob.Name = "test-job"
	event.WorkflowJob.Labels = []string{"gale", "linux"}
	event.Repository.FullName = "owner/repo"
	event.Repository.HTMLURL = "https://github.com/owner/repo"

	body, _ := json.Marshal(event)

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("queued with gale label status = %d, want %d", w.Code, http.StatusOK)
	}

	// Verify runner was created
	if len(mockDocker.CreateRunnerCalls) != 1 {
		t.Errorf("CreateRunnerCalls = %d, want 1", len(mockDocker.CreateRunnerCalls))
	}

	// Verify active runner count
	if h.GetActiveRunnerCount() != 1 {
		t.Errorf("GetActiveRunnerCount() = %d, want 1", h.GetActiveRunnerCount())
	}
}

func TestServeHTTP_CompletedWithRunner_WithMock(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Token: "test-token",
			Repos: []string{},
		},
		Scaler: config.ScalerConfig{
			MaxRunners: 10,
		},
	}

	mockDocker := docker.NewMockClient()
	h, err := NewHandlerWithDocker(cfg, testLogger(), mockDocker)
	if err != nil {
		t.Fatalf("NewHandlerWithDocker() error = %v", err)
	}

	// Pre-populate active runner
	h.activeRunners[123] = "container-abc123def456"

	event := WorkflowJobEvent{
		Action: "completed",
	}
	event.WorkflowJob.ID = 123
	event.WorkflowJob.Name = "test-job"
	event.Repository.FullName = "owner/repo"

	body, _ := json.Marshal(event)

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("completed status = %d, want %d", w.Code, http.StatusOK)
	}

	// Verify runner was removed from tracking (graceful shutdown runs in background)
	if h.GetActiveRunnerCount() != 0 {
		t.Errorf("GetActiveRunnerCount() = %d, want 0", h.GetActiveRunnerCount())
	}
}

func TestServeHTTP_MaxRunnersReached_WithMock(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Token: "test-token",
			Repos: []string{}, // monitor all
		},
		Scaler: config.ScalerConfig{
			MaxRunners: 2,
		},
	}

	mockDocker := docker.NewMockClient()
	h, err := NewHandlerWithDocker(cfg, testLogger(), mockDocker)
	if err != nil {
		t.Fatalf("NewHandlerWithDocker() error = %v", err)
	}

	// Pre-populate at max capacity
	h.activeRunners[1] = "container1"
	h.activeRunners[2] = "container2"

	event := WorkflowJobEvent{
		Action: "queued",
	}
	event.WorkflowJob.ID = 999
	event.WorkflowJob.Labels = []string{"gale"}
	event.Repository.FullName = "owner/repo"
	event.Repository.HTMLURL = "https://github.com/owner/repo"

	body, _ := json.Marshal(event)

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("max runners status = %d, want %d", w.Code, http.StatusOK)
	}

	// Should NOT create new runner
	if len(mockDocker.CreateRunnerCalls) != 0 {
		t.Errorf("CreateRunnerCalls = %d, want 0 (at max capacity)", len(mockDocker.CreateRunnerCalls))
	}
}

func TestHandlerClose_WithMock(t *testing.T) {
	cfg := &config.Config{}
	mockDocker := docker.NewMockClient()
	h, err := NewHandlerWithDocker(cfg, testLogger(), mockDocker)
	if err != nil {
		t.Fatalf("NewHandlerWithDocker() error = %v", err)
	}

	err = h.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	if mockDocker.CloseCalls != 1 {
		t.Errorf("CloseCalls = %d, want 1", mockDocker.CloseCalls)
	}
}

func TestServeHTTP_InProgressAction(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Repos: []string{},
		},
	}

	h := &Handler{
		cfg:           cfg,
		logger:        testLogger(),
		activeRunners: make(map[int64]string),
	}

	event := WorkflowJobEvent{
		Action: "in_progress",
	}
	event.WorkflowJob.ID = 123
	event.WorkflowJob.Name = "test-job"
	event.WorkflowJob.Labels = []string{"gale"}
	event.Repository.FullName = "owner/repo"

	body, _ := json.Marshal(event)

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	// in_progress is not handled, should return OK
	if w.Code != http.StatusOK {
		t.Errorf("in_progress event status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestServeHTTP_MonitoredRepo(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Token: "test-token",
			Repos: []string{"repo"}, // Only monitor "repo"
		},
		Scaler: config.ScalerConfig{
			MaxRunners: 10,
		},
		Runner: config.RunnerConfig{
			Image:  "test-image",
			Labels: []string{"gale"},
		},
	}

	mockDocker := docker.NewMockClient()
	h, err := NewHandlerWithDocker(cfg, testLogger(), mockDocker)
	if err != nil {
		t.Fatalf("NewHandlerWithDocker() error = %v", err)
	}

	event := WorkflowJobEvent{
		Action: "queued",
	}
	event.WorkflowJob.ID = 456
	event.WorkflowJob.Name = "test-job"
	event.WorkflowJob.Labels = []string{"gale"}
	event.Repository.FullName = "owner/repo" // Should match
	event.Repository.HTMLURL = "https://github.com/owner/repo"

	body, _ := json.Marshal(event)

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("monitored repo status = %d, want %d", w.Code, http.StatusOK)
	}

	// Should have created a runner
	if len(mockDocker.CreateRunnerCalls) != 1 {
		t.Errorf("CreateRunnerCalls = %d, want 1", len(mockDocker.CreateRunnerCalls))
	}
}

func TestServeHTTP_CreateRunnerError(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Token: "test-token",
			Repos: []string{},
		},
		Scaler: config.ScalerConfig{
			MaxRunners: 10,
		},
		Runner: config.RunnerConfig{
			Image:  "test-image",
			Labels: []string{"gale"},
		},
	}

	mockDocker := docker.NewMockClient()
	mockDocker.CreateRunnerFunc = func(ctx context.Context, cfg docker.RunnerConfig) (*docker.Runner, error) {
		return nil, context.DeadlineExceeded
	}

	h, err := NewHandlerWithDocker(cfg, testLogger(), mockDocker)
	if err != nil {
		t.Fatalf("NewHandlerWithDocker() error = %v", err)
	}

	event := WorkflowJobEvent{
		Action: "queued",
	}
	event.WorkflowJob.ID = 789
	event.WorkflowJob.Name = "test-job"
	event.WorkflowJob.Labels = []string{"gale"}
	event.Repository.FullName = "owner/repo"
	event.Repository.HTMLURL = "https://github.com/owner/repo"

	body, _ := json.Marshal(event)

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	// Should still return OK (error is logged but doesn't fail the webhook)
	if w.Code != http.StatusOK {
		t.Errorf("create runner error status = %d, want %d", w.Code, http.StatusOK)
	}

	// Runner should NOT be in activeRunners
	if h.GetActiveRunnerCount() != 0 {
		t.Errorf("GetActiveRunnerCount() = %d, want 0", h.GetActiveRunnerCount())
	}
}

func TestHandleQueued_SelfHostedLabel(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Token: "test-token",
			Repos: []string{},
		},
		Scaler: config.ScalerConfig{
			MaxRunners: 10,
		},
		Runner: config.RunnerConfig{
			Image:  "test-image",
			Labels: []string{"self-hosted"},
		},
	}

	mockDocker := docker.NewMockClient()
	h, err := NewHandlerWithDocker(cfg, testLogger(), mockDocker)
	if err != nil {
		t.Fatalf("NewHandlerWithDocker() error = %v", err)
	}

	event := WorkflowJobEvent{
		Action: "queued",
	}
	event.WorkflowJob.ID = 321
	event.WorkflowJob.Name = "test-job"
	event.WorkflowJob.Labels = []string{"self-hosted", "linux"} // self-hosted label
	event.Repository.FullName = "owner/repo"
	event.Repository.HTMLURL = "https://github.com/owner/repo"

	body, _ := json.Marshal(event)

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("self-hosted label status = %d, want %d", w.Code, http.StatusOK)
	}

	// Should have created a runner for self-hosted label
	if len(mockDocker.CreateRunnerCalls) != 1 {
		t.Errorf("CreateRunnerCalls = %d, want 1", len(mockDocker.CreateRunnerCalls))
	}
}

func TestHandler_ConcurrentActiveRunnerAccess(t *testing.T) {
	h := &Handler{
		logger:        testLogger(),
		activeRunners: make(map[int64]string),
	}

	// Simulate concurrent access
	done := make(chan bool)
	go func() {
		for i := int64(0); i < 100; i++ {
			h.mu.Lock()
			h.activeRunners[i] = "container"
			h.mu.Unlock()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_ = h.GetActiveRunnerCount()
		}
		done <- true
	}()

	<-done
	<-done

	// Should have 100 runners
	if h.GetActiveRunnerCount() != 100 {
		t.Errorf("GetActiveRunnerCount() = %d, want 100", h.GetActiveRunnerCount())
	}
}

func TestWorkflowJobEvent_AllFields(t *testing.T) {
	event := WorkflowJobEvent{
		Action: "queued",
	}
	event.WorkflowJob.ID = 12345
	event.WorkflowJob.RunID = 67890
	event.WorkflowJob.Name = "test-job"
	event.WorkflowJob.Status = "queued"
	event.WorkflowJob.Labels = []string{"gale", "linux", "x64"}
	event.WorkflowJob.RunnerName = "runner-1"
	event.WorkflowJob.RunnerID = 99
	event.Repository.FullName = "owner/repo"
	event.Repository.HTMLURL = "https://github.com/owner/repo"
	event.Installation.ID = 55555

	if event.Action != "queued" {
		t.Errorf("Action = %q, want queued", event.Action)
	}
	if event.WorkflowJob.ID != 12345 {
		t.Errorf("WorkflowJob.ID = %d, want 12345", event.WorkflowJob.ID)
	}
	if event.WorkflowJob.RunID != 67890 {
		t.Errorf("WorkflowJob.RunID = %d, want 67890", event.WorkflowJob.RunID)
	}
	if event.WorkflowJob.RunnerName != "runner-1" {
		t.Errorf("WorkflowJob.RunnerName = %q, want runner-1", event.WorkflowJob.RunnerName)
	}
	if event.WorkflowJob.RunnerID != 99 {
		t.Errorf("WorkflowJob.RunnerID = %d, want 99", event.WorkflowJob.RunnerID)
	}
	if event.Installation.ID != 55555 {
		t.Errorf("Installation.ID = %d, want 55555", event.Installation.ID)
	}
}

func TestNewHandlerWithOptions(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Token: "test-token",
			Repos: []string{},
		},
		Scaler: config.ScalerConfig{
			MaxRunners: 10,
		},
	}

	mockDocker := docker.NewMockClient()

	h, err := NewHandlerWithOptions(cfg, testLogger(), HandlerOptions{
		DockerClient: mockDocker,
	})
	if err != nil {
		t.Fatalf("NewHandlerWithOptions() error = %v", err)
	}
	if h == nil {
		t.Fatal("NewHandlerWithOptions() returned nil")
	}

	// Verify the mock client is used
	err = h.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	if mockDocker.CloseCalls != 1 {
		t.Errorf("CloseCalls = %d, want 1", mockDocker.CloseCalls)
	}
}

func TestNewHandlerWithOptions_FullWorkflow(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Token: "test-token",
			Repos: []string{},
		},
		Scaler: config.ScalerConfig{
			MaxRunners: 10,
		},
		Runner: config.RunnerConfig{
			Image:  "test-image",
			Labels: []string{"gale"},
		},
	}

	mockDocker := docker.NewMockClient()

	h, err := NewHandlerWithOptions(cfg, testLogger(), HandlerOptions{
		DockerClient: mockDocker,
	})
	if err != nil {
		t.Fatalf("NewHandlerWithOptions() error = %v", err)
	}

	// Test full workflow: queue job -> create runner -> complete job -> remove runner
	event := WorkflowJobEvent{
		Action: "queued",
	}
	event.WorkflowJob.ID = 123
	event.WorkflowJob.Name = "test-job"
	event.WorkflowJob.Labels = []string{"gale"}
	event.Repository.FullName = "owner/repo"
	event.Repository.HTMLURL = "https://github.com/owner/repo"

	body, _ := json.Marshal(event)

	// Queue event
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("queued status = %d, want %d", w.Code, http.StatusOK)
	}

	// Verify runner was created
	if len(mockDocker.CreateRunnerCalls) != 1 {
		t.Errorf("CreateRunnerCalls = %d, want 1", len(mockDocker.CreateRunnerCalls))
	}

	// Complete event
	event.Action = "completed"
	body, _ = json.Marshal(event)

	req = httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("completed status = %d, want %d", w.Code, http.StatusOK)
	}

	// Verify runner was removed from tracking (graceful shutdown runs in background)
	if h.GetActiveRunnerCount() != 0 {
		t.Errorf("GetActiveRunnerCount() = %d, want 0 after completion", h.GetActiveRunnerCount())
	}
}

func TestHandlerOptions_Empty(t *testing.T) {
	opts := HandlerOptions{}

	if opts.DockerClient != nil {
		t.Error("Empty HandlerOptions should have nil DockerClient")
	}
}

func TestHandlerWithAppMode(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Token: "test-token",
			Repos: []string{},
		},
		Scaler: config.ScalerConfig{
			MaxRunners: 10,
		},
	}

	mockDocker := docker.NewMockClient()
	h, err := NewHandlerWithDocker(cfg, testLogger(), mockDocker)
	if err != nil {
		t.Fatalf("NewHandlerWithDocker() error = %v", err)
	}

	if h.appClient != nil {
		t.Error("appClient should be nil when not in app mode")
	}
}

func TestHandleQueued_NoInstallationIDInAppMode(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Repos: []string{},
		},
		Scaler: config.ScalerConfig{
			MaxRunners: 10,
		},
		Runner: config.RunnerConfig{
			Image:  "test-image",
			Labels: []string{"gale"},
		},
	}

	mockDocker := docker.NewMockClient()
	h := &Handler{
		cfg:           cfg,
		docker:        mockDocker,
		logger:        testLogger(),
		activeRunners: make(map[int64]string),
	}

	event := &WorkflowJobEvent{
		Action: "queued",
	}
	event.WorkflowJob.ID = 123
	event.WorkflowJob.Labels = []string{"gale"}
	event.Repository.FullName = "owner/repo"
	event.Repository.HTMLURL = "https://github.com/owner/repo"
	event.Installation.ID = 0

	h.handleQueued(context.Background(), event)

	if len(mockDocker.CreateRunnerCalls) != 1 {
		t.Errorf("CreateRunnerCalls = %d, want 1", len(mockDocker.CreateRunnerCalls))
	}
}

func TestHandleQueued_RunnerConfigValues(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Token: "test-token",
			Repos: []string{},
		},
		Scaler: config.ScalerConfig{
			MaxRunners: 10,
		},
		Runner: config.RunnerConfig{
			Image:       "custom-image:latest",
			Labels:      []string{"gale", "custom"},
			Env:         map[string]string{"FOO": "bar"},
			NetworkMode: "bridge",
		},
	}

	mockDocker := docker.NewMockClient()
	h, err := NewHandlerWithDocker(cfg, testLogger(), mockDocker)
	if err != nil {
		t.Fatalf("NewHandlerWithDocker() error = %v", err)
	}

	event := WorkflowJobEvent{
		Action: "queued",
	}
	event.WorkflowJob.ID = 456
	event.WorkflowJob.Labels = []string{"gale"}
	event.Repository.FullName = "owner/repo"
	event.Repository.HTMLURL = "https://github.com/owner/repo"

	body, _ := json.Marshal(event)

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if len(mockDocker.CreateRunnerCalls) != 1 {
		t.Fatalf("CreateRunnerCalls = %d, want 1", len(mockDocker.CreateRunnerCalls))
	}

	runnerCfg := mockDocker.CreateRunnerCalls[0]
	if runnerCfg.Image != "custom-image:latest" {
		t.Errorf("Image = %q, want custom-image:latest", runnerCfg.Image)
	}
	if runnerCfg.Token != "test-token" {
		t.Errorf("Token = %q, want test-token", runnerCfg.Token)
	}
	if runnerCfg.NetworkMode != "bridge" {
		t.Errorf("NetworkMode = %q, want bridge", runnerCfg.NetworkMode)
	}
	if runnerCfg.Scope != "repo" {
		t.Errorf("Scope = %q, want repo", runnerCfg.Scope)
	}
}

func TestHandleCompleted_ShortContainerID(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Repos: []string{},
		},
	}

	mockDocker := docker.NewMockClient()
	h := &Handler{
		cfg:    cfg,
		docker: mockDocker,
		logger: testLogger(),
		activeRunners: map[int64]string{
			123: "abcdefghijklmnop",
		},
	}

	event := &WorkflowJobEvent{
		Action: "completed",
	}
	event.WorkflowJob.ID = 123
	event.Repository.FullName = "owner/repo"

	h.handleCompleted(context.Background(), event)

	// Graceful shutdown runs in background goroutine, just verify runner was removed from tracking
	if len(h.activeRunners) != 0 {
		t.Errorf("activeRunners should be empty after completion")
	}
}

func TestServeHTTP_NoSecret(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Repos: []string{},
		},
	}

	h := &Handler{
		cfg:           cfg,
		logger:        testLogger(),
		secret:        "",
		activeRunners: make(map[int64]string),
	}

	payload := `{"action":"queued","workflow_job":{"id":1,"labels":[]},"repository":{"full_name":"o/r"}}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("no secret status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestMonitorRunnerStartup_ContainerExited(t *testing.T) {
	mockDocker := docker.NewMockClient()
	mockDocker.IsContainerExitedFunc = func(ctx context.Context, containerID string) (bool, error) {
		return true, nil // Container exited
	}

	h := &Handler{
		docker: mockDocker,
		logger: testLogger(),
	}

	// Run in goroutine since it has a sleep
	done := make(chan struct{})
	go func() {
		h.monitorRunnerStartup("abcdefghijklmnop", 123, "test-job")
		close(done)
	}()

	// Wait for completion (5s sleep + buffer)
	select {
	case <-done:
		// Success
	case <-time.After(10 * time.Second):
		t.Fatal("monitorRunnerStartup timed out")
	}

	// Verify IsContainerExited was called
	if len(mockDocker.IsContainerExitedCalls) != 1 {
		t.Errorf("IsContainerExitedCalls = %d, want 1", len(mockDocker.IsContainerExitedCalls))
	}
}

func TestMonitorRunnerStartup_ContainerRunning(t *testing.T) {
	mockDocker := docker.NewMockClient()
	mockDocker.IsContainerExitedFunc = func(ctx context.Context, containerID string) (bool, error) {
		return false, nil // Container still running
	}

	h := &Handler{
		docker: mockDocker,
		logger: testLogger(),
	}

	done := make(chan struct{})
	go func() {
		h.monitorRunnerStartup("abcdefghijklmnop", 123, "test-job")
		close(done)
	}()

	select {
	case <-done:
		// Success - no error logged for running container
	case <-time.After(10 * time.Second):
		t.Fatal("monitorRunnerStartup timed out")
	}

	if len(mockDocker.IsContainerExitedCalls) != 1 {
		t.Errorf("IsContainerExitedCalls = %d, want 1", len(mockDocker.IsContainerExitedCalls))
	}
}

func TestMonitorRunnerStartup_CheckError(t *testing.T) {
	mockDocker := docker.NewMockClient()
	mockDocker.IsContainerExitedFunc = func(ctx context.Context, containerID string) (bool, error) {
		return false, fmt.Errorf("container not found")
	}

	h := &Handler{
		docker: mockDocker,
		logger: testLogger(),
	}

	done := make(chan struct{})
	go func() {
		h.monitorRunnerStartup("abcdefghijklmnop", 123, "test-job")
		close(done)
	}()

	select {
	case <-done:
		// Success - returns silently on error
	case <-time.After(10 * time.Second):
		t.Fatal("monitorRunnerStartup timed out")
	}
}
