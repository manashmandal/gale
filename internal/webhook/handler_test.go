package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/manashmandal/gale/internal/config"
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
	h := &Handler{}

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
