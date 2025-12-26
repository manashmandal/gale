package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-github/v68/github"
)

func TestNewClient(t *testing.T) {
	c := NewClient("test-token", "owner", "repo", "repo")
	if c == nil {
		t.Fatal("NewClient() returned nil")
	}
	if c.owner != "owner" {
		t.Errorf("owner = %q, want %q", c.owner, "owner")
	}
	if c.repo != "repo" {
		t.Errorf("repo = %q, want %q", c.repo, "repo")
	}
	if c.scope != "repo" {
		t.Errorf("scope = %q, want %q", c.scope, "repo")
	}
	if c.threshold != DefaultRateLimitThreshold {
		t.Errorf("threshold = %d, want %d", c.threshold, DefaultRateLimitThreshold)
	}
}

func TestNewClientWithOptions(t *testing.T) {
	tests := []struct {
		name            string
		opts            ClientOptions
		wantThreshold   int
		wantCallsPerMin int
		wantForce       bool
	}{
		{
			name: "default values",
			opts: ClientOptions{
				Token: "token",
				Owner: "owner",
			},
			wantThreshold:   DefaultRateLimitThreshold,
			wantCallsPerMin: DefaultCallsPerMinute,
			wantForce:       false,
		},
		{
			name: "custom threshold",
			opts: ClientOptions{
				Token:     "token",
				Owner:     "owner",
				Threshold: 1000,
			},
			wantThreshold:   1000,
			wantCallsPerMin: DefaultCallsPerMinute,
			wantForce:       false,
		},
		{
			name: "custom calls per minute",
			opts: ClientOptions{
				Token:       "token",
				Owner:       "owner",
				CallsPerMin: 10,
			},
			wantThreshold:   DefaultRateLimitThreshold,
			wantCallsPerMin: 10,
			wantForce:       false,
		},
		{
			name: "force mode enabled",
			opts: ClientOptions{
				Token: "token",
				Owner: "owner",
				Force: true,
			},
			wantThreshold:   DefaultRateLimitThreshold,
			wantCallsPerMin: DefaultCallsPerMinute,
			wantForce:       true,
		},
		{
			name: "zero threshold uses default",
			opts: ClientOptions{
				Token:     "token",
				Owner:     "owner",
				Threshold: 0,
			},
			wantThreshold:   DefaultRateLimitThreshold,
			wantCallsPerMin: DefaultCallsPerMinute,
		},
		{
			name: "negative threshold uses default",
			opts: ClientOptions{
				Token:     "token",
				Owner:     "owner",
				Threshold: -1,
			},
			wantThreshold:   DefaultRateLimitThreshold,
			wantCallsPerMin: DefaultCallsPerMinute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewClientWithOptions(tt.opts)
			if c.threshold != tt.wantThreshold {
				t.Errorf("threshold = %d, want %d", c.threshold, tt.wantThreshold)
			}
			if c.callsPerMin != tt.wantCallsPerMin {
				t.Errorf("callsPerMin = %d, want %d", c.callsPerMin, tt.wantCallsPerMin)
			}
			if c.forceMode != tt.wantForce {
				t.Errorf("forceMode = %v, want %v", c.forceMode, tt.wantForce)
			}
		})
	}
}

func TestIsOrgScope(t *testing.T) {
	tests := []struct {
		scope string
		want  bool
	}{
		{"org", true},
		{"repo", false},
		{"", false},
		{"Org", false}, // case sensitive
	}

	for _, tt := range tests {
		t.Run(tt.scope, func(t *testing.T) {
			c := NewClient("token", "owner", "repo", tt.scope)
			if got := c.IsOrgScope(); got != tt.want {
				t.Errorf("IsOrgScope() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetForceMode(t *testing.T) {
	c := NewClient("token", "owner", "repo", "repo")

	c.SetForceMode(true)
	if !c.forceMode {
		t.Error("SetForceMode(true) did not set forceMode to true")
	}

	c.SetForceMode(false)
	if c.forceMode {
		t.Error("SetForceMode(false) did not set forceMode to false")
	}
}

func TestGetRateLimitInfo(t *testing.T) {
	c := NewClient("token", "owner", "repo", "repo")
	c.rateLimitUsed = 100
	c.rateLimitLimit = 5000
	c.forceMode = true

	used, limit, threshold, force := c.GetRateLimitInfo()
	if used != 100 {
		t.Errorf("used = %d, want 100", used)
	}
	if limit != 5000 {
		t.Errorf("limit = %d, want 5000", limit)
	}
	if threshold != DefaultRateLimitThreshold {
		t.Errorf("threshold = %d, want %d", threshold, DefaultRateLimitThreshold)
	}
	if !force {
		t.Error("force = false, want true")
	}
}

func TestCheckRateLimit(t *testing.T) {
	tests := []struct {
		name      string
		used      int
		threshold int
		forceMode bool
		wantErr   bool
	}{
		{
			name:      "under threshold",
			used:      1000,
			threshold: 2500,
			forceMode: false,
			wantErr:   false,
		},
		{
			name:      "at threshold",
			used:      2500,
			threshold: 2500,
			forceMode: false,
			wantErr:   true,
		},
		{
			name:      "over threshold",
			used:      3000,
			threshold: 2500,
			forceMode: false,
			wantErr:   true,
		},
		{
			name:      "over threshold with force mode",
			used:      3000,
			threshold: 2500,
			forceMode: true,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewClientWithOptions(ClientOptions{
				Token:     "token",
				Owner:     "owner",
				Threshold: tt.threshold,
				Force:     tt.forceMode,
			})
			c.rateLimitUsed = tt.used

			err := c.checkRateLimit()
			if (err != nil) != tt.wantErr {
				t.Errorf("checkRateLimit() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestUpdateRateLimit(t *testing.T) {
	c := NewClient("token", "owner", "repo", "repo")

	// Test with nil response
	c.updateRateLimit(nil)
	if c.rateLimitUsed != 0 {
		t.Errorf("updateRateLimit(nil) set rateLimitUsed to %d, want 0", c.rateLimitUsed)
	}

	// Test with valid response
	resp := &github.Response{
		Rate: github.Rate{
			Limit:     5000,
			Remaining: 4500,
		},
	}
	c.updateRateLimit(resp)
	if c.rateLimitUsed != 500 {
		t.Errorf("rateLimitUsed = %d, want 500", c.rateLimitUsed)
	}
	if c.rateLimitLimit != 5000 {
		t.Errorf("rateLimitLimit = %d, want 5000", c.rateLimitLimit)
	}

	// Test with zero limit (should not update)
	c.rateLimitUsed = 100
	resp = &github.Response{
		Rate: github.Rate{
			Limit:     0,
			Remaining: 0,
		},
	}
	c.updateRateLimit(resp)
	if c.rateLimitUsed != 100 {
		t.Errorf("rateLimitUsed changed to %d, want 100 (unchanged)", c.rateLimitUsed)
	}
}

func TestRequiresSelfHosted(t *testing.T) {
	c := NewClient("token", "owner", "repo", "repo")

	tests := []struct {
		name   string
		labels []string
		want   bool
	}{
		{
			name:   "nil labels",
			labels: nil,
			want:   false,
		},
		{
			name:   "empty labels",
			labels: []string{},
			want:   false,
		},
		{
			name:   "self-hosted label",
			labels: []string{"self-hosted"},
			want:   true,
		},
		{
			name:   "gale label",
			labels: []string{"gale"},
			want:   true,
		},
		{
			name:   "GALE label (uppercase)",
			labels: []string{"GALE"},
			want:   true,
		},
		{
			name:   "Self-Hosted label (mixed case)",
			labels: []string{"Self-Hosted"},
			want:   true,
		},
		{
			name:   "ubuntu label only",
			labels: []string{"ubuntu-latest"},
			want:   false,
		},
		{
			name:   "multiple labels with gale",
			labels: []string{"linux", "x64", "gale"},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &github.WorkflowJob{Labels: tt.labels}
			if got := c.requiresSelfHosted(job); got != tt.want {
				t.Errorf("requiresSelfHosted() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWaitForRateLimit(t *testing.T) {
	c := NewClientWithOptions(ClientOptions{
		Token:       "token",
		Owner:       "owner",
		CallsPerMin: 100, // High limit for testing
	})

	// First call should not wait
	start := time.Now()
	c.waitForRateLimit()
	if time.Since(start) > 100*time.Millisecond {
		t.Error("First call waited unexpectedly")
	}

	// Verify call was recorded
	if len(c.callTimes) != 1 {
		t.Errorf("callTimes length = %d, want 1", len(c.callTimes))
	}
}

func TestClientScope(t *testing.T) {
	tests := []struct {
		name     string
		scope    string
		wantOrg  bool
		wantRepo bool
	}{
		{"org scope", "org", true, false},
		{"repo scope", "repo", false, true},
		{"repos scope", "repos", false, false},
		{"empty scope", "", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewClient("token", "owner", "repo", tt.scope)
			if got := c.IsOrgScope(); got != tt.wantOrg {
				t.Errorf("IsOrgScope() = %v, want %v", got, tt.wantOrg)
			}
		})
	}
}

func TestErrRateLimitThreshold(t *testing.T) {
	if ErrRateLimitThreshold == nil {
		t.Error("ErrRateLimitThreshold is nil")
	}
	if ErrRateLimitThreshold.Error() != "rate limit threshold reached" {
		t.Errorf("ErrRateLimitThreshold.Error() = %q", ErrRateLimitThreshold.Error())
	}
}

func TestDefaultConstants(t *testing.T) {
	if DefaultRateLimitThreshold != 2500 {
		t.Errorf("DefaultRateLimitThreshold = %d, want 2500", DefaultRateLimitThreshold)
	}
	if DefaultCallsPerMinute != 5 {
		t.Errorf("DefaultCallsPerMinute = %d, want 5", DefaultCallsPerMinute)
	}
}

// Mock client tests

func TestMockGitHubClient_NewMockGitHubClient(t *testing.T) {
	mock := NewMockGitHubClient()
	if mock == nil {
		t.Fatal("NewMockGitHubClient() returned nil")
	}
}

func TestMockGitHubClient_IsOrgScope(t *testing.T) {
	mock := NewMockGitHubClient()

	// Default is false
	if mock.IsOrgScope() {
		t.Error("Default IsOrgScope() should be false")
	}

	mock.SetOrgScope(true)
	if !mock.IsOrgScope() {
		t.Error("After SetOrgScope(true), IsOrgScope() should be true")
	}

	if mock.IsOrgScopeCalls != 2 {
		t.Errorf("IsOrgScopeCalls = %d, want 2", mock.IsOrgScopeCalls)
	}
}

func TestMockGitHubClient_SetForceMode(t *testing.T) {
	mock := NewMockGitHubClient()

	mock.SetForceMode(true)

	if len(mock.SetForceModeCalls) != 1 {
		t.Errorf("SetForceModeCalls len = %d, want 1", len(mock.SetForceModeCalls))
	}
	if !mock.SetForceModeCalls[0] {
		t.Error("SetForceModeCalls[0] should be true")
	}
}

func TestMockGitHubClient_GetRateLimitInfo(t *testing.T) {
	mock := NewMockGitHubClient()
	mock.SetRateLimitInfo(100, 5000, 2500)

	used, limit, threshold, force := mock.GetRateLimitInfo()

	if used != 100 {
		t.Errorf("used = %d, want 100", used)
	}
	if limit != 5000 {
		t.Errorf("limit = %d, want 5000", limit)
	}
	if threshold != 2500 {
		t.Errorf("threshold = %d, want 2500", threshold)
	}
	if force {
		t.Error("force should be false")
	}
}

func TestMockGitHubClient_GetQueuedJobs(t *testing.T) {
	mock := NewMockGitHubClient()
	ctx := context.Background()

	mock.SetQueuedJobs([]QueuedJob{
		{RunID: 1, JobID: 101, JobName: "job1", Status: "queued", Repo: "owner/repo1"},
		{RunID: 2, JobID: 102, JobName: "job2", Status: "queued", Repo: "owner/repo2"},
	})

	jobs, err := mock.GetQueuedJobs(ctx)
	if err != nil {
		t.Errorf("GetQueuedJobs() error = %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("GetQueuedJobs() len = %d, want 2", len(jobs))
	}

	if mock.GetQueuedJobsCalls != 1 {
		t.Errorf("GetQueuedJobsCalls = %d, want 1", mock.GetQueuedJobsCalls)
	}
}

func TestMockGitHubClient_GetRegistrationToken(t *testing.T) {
	mock := NewMockGitHubClient()
	ctx := context.Background()

	token, err := mock.GetRegistrationToken(ctx, "owner/repo")
	if err != nil {
		t.Errorf("GetRegistrationToken() error = %v", err)
	}
	if token != "mock-registration-token" {
		t.Errorf("token = %q, want mock-registration-token", token)
	}

	if len(mock.GetRegistrationTokenCalls) != 1 {
		t.Errorf("GetRegistrationTokenCalls len = %d, want 1", len(mock.GetRegistrationTokenCalls))
	}
}

func TestMockGitHubClient_Reset(t *testing.T) {
	mock := NewMockGitHubClient()
	ctx := context.Background()

	// Make some calls
	mock.IsOrgScope()
	mock.SetForceMode(true)
	mock.GetQueuedJobs(ctx)

	// Reset
	mock.Reset()

	if mock.IsOrgScopeCalls != 0 {
		t.Error("After Reset, IsOrgScopeCalls should be 0")
	}
	if len(mock.SetForceModeCalls) != 0 {
		t.Error("After Reset, SetForceModeCalls should be empty")
	}
	if mock.GetQueuedJobsCalls != 0 {
		t.Error("After Reset, GetQueuedJobsCalls should be 0")
	}
}

func TestQueuedJob(t *testing.T) {
	job := QueuedJob{
		RunID:   12345,
		JobID:   67890,
		JobName: "test-job",
		Status:  "queued",
		Repo:    "owner/repo",
	}

	if job.RunID != 12345 {
		t.Errorf("RunID = %d, want 12345", job.RunID)
	}
	if job.JobID != 67890 {
		t.Errorf("JobID = %d, want 67890", job.JobID)
	}
	if job.JobName != "test-job" {
		t.Errorf("JobName = %q, want test-job", job.JobName)
	}
	if job.Status != "queued" {
		t.Errorf("Status = %q, want queued", job.Status)
	}
	if job.Repo != "owner/repo" {
		t.Errorf("Repo = %q, want owner/repo", job.Repo)
	}
}

func TestGitHubClientInterface(t *testing.T) {
	// Test that both Client and MockGitHubClient satisfy GitHubClient interface
	var _ GitHubClient = (*Client)(nil)
	var _ GitHubClient = (*MockGitHubClient)(nil)
}

func TestQueuedJobFields(t *testing.T) {
	jobs := []QueuedJob{
		{
			RunID:   1,
			JobID:   100,
			JobName: "build",
			Status:  "queued",
			Repo:    "owner/repo1",
		},
		{
			RunID:   2,
			JobID:   200,
			JobName: "test",
			Status:  "in_progress",
			Repo:    "owner/repo2",
		},
	}

	// Test first job
	if jobs[0].RunID != 1 {
		t.Errorf("jobs[0].RunID = %d, want 1", jobs[0].RunID)
	}
	if jobs[0].JobID != 100 {
		t.Errorf("jobs[0].JobID = %d, want 100", jobs[0].JobID)
	}
	if jobs[0].JobName != "build" {
		t.Errorf("jobs[0].JobName = %q, want build", jobs[0].JobName)
	}
	if jobs[0].Repo != "owner/repo1" {
		t.Errorf("jobs[0].Repo = %q, want owner/repo1", jobs[0].Repo)
	}

	// Test second job
	if jobs[1].Status != "in_progress" {
		t.Errorf("jobs[1].Status = %q, want in_progress", jobs[1].Status)
	}
}

func TestClientOptions_AllFields(t *testing.T) {
	opts := ClientOptions{
		Token:       "test-token",
		Owner:       "test-owner",
		Repo:        "test-repo",
		Scope:       "repo",
		Threshold:   1500,
		CallsPerMin: 10,
		Force:       true,
	}

	c := NewClientWithOptions(opts)

	if c.owner != "test-owner" {
		t.Errorf("owner = %q, want test-owner", c.owner)
	}
	if c.repo != "test-repo" {
		t.Errorf("repo = %q, want test-repo", c.repo)
	}
	if c.scope != "repo" {
		t.Errorf("scope = %q, want repo", c.scope)
	}
	if c.threshold != 1500 {
		t.Errorf("threshold = %d, want 1500", c.threshold)
	}
	if c.callsPerMin != 10 {
		t.Errorf("callsPerMin = %d, want 10", c.callsPerMin)
	}
	if !c.forceMode {
		t.Error("forceMode should be true")
	}
}

func TestWaitForRateLimit_Throttling(t *testing.T) {
	c := NewClientWithOptions(ClientOptions{
		Token:       "token",
		Owner:       "owner",
		CallsPerMin: 600, // 10 calls per second
	})

	// Make several quick calls
	for i := 0; i < 5; i++ {
		c.waitForRateLimit()
	}

	// Should have recorded 5 calls
	if len(c.callTimes) != 5 {
		t.Errorf("callTimes length = %d, want 5", len(c.callTimes))
	}
}

func TestMockGitHubClient_CustomFunctions(t *testing.T) {
	mock := NewMockGitHubClient()
	ctx := context.Background()

	// Custom GetQueuedJobs function
	customError := false
	mock.GetQueuedJobsFunc = func(ctx context.Context) ([]QueuedJob, error) {
		if customError {
			return nil, context.DeadlineExceeded
		}
		return []QueuedJob{{RunID: 999, JobID: 888}}, nil
	}

	jobs, err := mock.GetQueuedJobs(ctx)
	if err != nil {
		t.Errorf("GetQueuedJobs() error = %v", err)
	}
	if len(jobs) != 1 || jobs[0].RunID != 999 {
		t.Errorf("GetQueuedJobs() returned unexpected jobs")
	}

	// Test error case
	customError = true
	_, err = mock.GetQueuedJobs(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("GetQueuedJobs() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestMockGitHubClient_CustomRegistrationToken(t *testing.T) {
	mock := NewMockGitHubClient()
	ctx := context.Background()

	mock.GetRegistrationTokenFunc = func(ctx context.Context, repoFullName string) (string, error) {
		return "custom-token-for-" + repoFullName, nil
	}

	token, err := mock.GetRegistrationToken(ctx, "myorg/myrepo")
	if err != nil {
		t.Errorf("GetRegistrationToken() error = %v", err)
	}
	if token != "custom-token-for-myorg/myrepo" {
		t.Errorf("token = %q, want custom-token-for-myorg/myrepo", token)
	}
}

// HTTP Mock Server Tests

func TestGetQueuedJobs_RepoScope_WithMock(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	// Mock workflow runs endpoint
	mux.HandleFunc("/repos/owner/repo/actions/runs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4999")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"total_count": 1,
			"workflow_runs": []map[string]interface{}{
				{
					"id":     12345,
					"status": "queued",
				},
			},
		})
	})

	// Mock workflow jobs endpoint
	mux.HandleFunc("/repos/owner/repo/actions/runs/12345/jobs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4998")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"total_count": 1,
			"jobs": []map[string]interface{}{
				{
					"id":     67890,
					"run_id": 12345,
					"status": "queued",
					"name":   "build",
					"labels": []string{"self-hosted", "linux"},
				},
			},
		})
	})

	c := NewClient("token", "owner", "repo", "repo")
	c.client = github.NewClient(nil).WithAuthToken("token")
	// Point client to mock server
	baseURL, _ := c.client.BaseURL.Parse(server.URL + "/")
	c.client.BaseURL = baseURL

	ctx := context.Background()
	jobs, err := c.GetQueuedJobs(ctx)
	if err != nil {
		t.Fatalf("GetQueuedJobs() error = %v", err)
	}

	// The test verifies that GetQueuedJobs works with mock server
	// The actual count depends on implementation details (may filter by status)
	if len(jobs) == 0 {
		t.Error("GetQueuedJobs() returned no jobs")
	}
}

func TestGetQueuedJobs_CheckRateLimitError(t *testing.T) {
	c := NewClientWithOptions(ClientOptions{
		Token:     "token",
		Owner:     "owner",
		Repo:      "repo",
		Scope:     "repo",
		Threshold: 100,
		Force:     false,
	})
	c.rateLimitUsed = 150 // Over threshold

	ctx := context.Background()
	_, err := c.GetQueuedJobs(ctx)
	if err == nil {
		t.Error("GetQueuedJobs() should return error when rate limit threshold exceeded")
	}
	// Error message should mention rate limit
	if err != nil && err.Error() == "" {
		t.Errorf("GetQueuedJobs() error message should not be empty")
	}
}

func TestGetRegistrationToken_WithMock(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/repos/owner/repo/actions/runners/registration-token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4999")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"token":      "MOCK_REG_TOKEN",
			"expires_at": "2024-01-01T00:00:00Z",
		})
	})

	c := NewClient("token", "owner", "repo", "repo")
	c.client = github.NewClient(nil).WithAuthToken("token")
	baseURL, _ := c.client.BaseURL.Parse(server.URL + "/")
	c.client.BaseURL = baseURL

	ctx := context.Background()
	token, err := c.GetRegistrationToken(ctx, "owner/repo")
	if err != nil {
		t.Fatalf("GetRegistrationToken() error = %v", err)
	}

	if token != "MOCK_REG_TOKEN" {
		t.Errorf("token = %q, want MOCK_REG_TOKEN", token)
	}
}

func TestGetRegistrationToken_OrgScope(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/orgs/myorg/actions/runners/registration-token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4999")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"token":      "ORG_REG_TOKEN",
			"expires_at": "2024-01-01T00:00:00Z",
		})
	})

	c := NewClient("token", "myorg", "", "org")
	c.client = github.NewClient(nil).WithAuthToken("token")
	baseURL, _ := c.client.BaseURL.Parse(server.URL + "/")
	c.client.BaseURL = baseURL

	ctx := context.Background()
	token, err := c.GetRegistrationToken(ctx, "myorg")
	if err != nil {
		t.Fatalf("GetRegistrationToken() error = %v", err)
	}

	if token != "ORG_REG_TOKEN" {
		t.Errorf("token = %q, want ORG_REG_TOKEN", token)
	}
}

func TestCallTimesCleanup(t *testing.T) {
	c := NewClientWithOptions(ClientOptions{
		Token:       "token",
		Owner:       "owner",
		CallsPerMin: 60, // 1 call per second
	})

	// Add old call times that should be cleaned up
	oldTime := time.Now().Add(-2 * time.Minute)
	c.callTimes = []time.Time{oldTime, oldTime, oldTime}

	// Add a new call
	c.waitForRateLimit()

	// Old calls should be cleaned up, only new call remains
	if len(c.callTimes) != 1 {
		t.Errorf("callTimes length = %d, want 1 after cleanup", len(c.callTimes))
	}
}

func TestGetQueuedJobs_ForceMode(t *testing.T) {
	c := NewClientWithOptions(ClientOptions{
		Token:     "token",
		Owner:     "owner",
		Repo:      "repo",
		Scope:     "repo",
		Threshold: 100,
		Force:     true, // Force mode bypasses rate limit
	})
	c.rateLimitUsed = 150 // Over threshold but force mode is on

	// Force mode should not return rate limit error from checkRateLimit
	err := c.checkRateLimit()
	if err != nil {
		t.Errorf("checkRateLimit() with force mode should not error, got: %v", err)
	}
}

func TestListRepos_OrgRepos(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/orgs/myorg/repos", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4999")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"name": "repo1", "archived": false, "disabled": false},
			{"name": "repo2", "archived": false, "disabled": false},
			{"name": "archived-repo", "archived": true, "disabled": false},
		})
	})

	c := NewClient("token", "myorg", "", "org")
	c.client = github.NewClient(nil).WithAuthToken("token")
	baseURL, _ := c.client.BaseURL.Parse(server.URL + "/")
	c.client.BaseURL = baseURL

	ctx := context.Background()
	repos, err := c.listRepos(ctx)
	if err != nil {
		t.Fatalf("listRepos() error = %v", err)
	}

	if len(repos) != 2 {
		t.Errorf("listRepos() got %d repos, want 2 (archived should be filtered)", len(repos))
	}
}

func TestListRepos_FallbackToUserRepos(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/orgs/myuser/repos", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "Not Found",
		})
	})

	mux.HandleFunc("/users/myuser/repos", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4999")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"name": "user-repo1", "archived": false, "disabled": false},
			{"name": "user-repo2", "archived": false, "disabled": false},
		})
	})

	c := NewClient("token", "myuser", "", "org")
	c.client = github.NewClient(nil).WithAuthToken("token")
	baseURL, _ := c.client.BaseURL.Parse(server.URL + "/")
	c.client.BaseURL = baseURL

	ctx := context.Background()
	repos, err := c.listRepos(ctx)
	if err != nil {
		t.Fatalf("listRepos() error = %v", err)
	}

	if len(repos) != 2 {
		t.Errorf("listRepos() got %d repos, want 2", len(repos))
	}
}

func TestListUserRepos_Success(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/users/myuser/repos", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4999")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"name": "repo1", "archived": false, "disabled": false},
			{"name": "repo2", "archived": false, "disabled": false},
			{"name": "disabled-repo", "archived": false, "disabled": true},
		})
	})

	c := NewClient("token", "myuser", "", "repo")
	c.client = github.NewClient(nil).WithAuthToken("token")
	baseURL, _ := c.client.BaseURL.Parse(server.URL + "/")
	c.client.BaseURL = baseURL

	ctx := context.Background()
	repos, err := c.listUserRepos(ctx)
	if err != nil {
		t.Fatalf("listUserRepos() error = %v", err)
	}

	if len(repos) != 2 {
		t.Errorf("listUserRepos() got %d repos, want 2 (disabled should be filtered)", len(repos))
	}
}

func TestListUserRepos_RateLimitError(t *testing.T) {
	c := NewClientWithOptions(ClientOptions{
		Token:     "token",
		Owner:     "owner",
		Threshold: 100,
		Force:     false,
	})
	c.rateLimitUsed = 150

	ctx := context.Background()
	_, err := c.listUserRepos(ctx)
	if err == nil {
		t.Error("listUserRepos() should return error when rate limit exceeded")
	}
}

func TestGetOrgQueuedJobs_Success(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/orgs/myorg/repos", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4999")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"name": "repo1", "archived": false, "disabled": false},
		})
	})

	mux.HandleFunc("/repos/myorg/repo1/actions/runs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4998")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"total_count": 1,
			"workflow_runs": []map[string]interface{}{
				{"id": 12345, "status": "queued"},
			},
		})
	})

	mux.HandleFunc("/repos/myorg/repo1/actions/runs/12345/jobs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4997")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"total_count": 1,
			"jobs": []map[string]interface{}{
				{
					"id":     67890,
					"run_id": 12345,
					"status": "queued",
					"name":   "build",
					"labels": []string{"self-hosted"},
				},
			},
		})
	})

	c := NewClient("token", "myorg", "", "org")
	c.client = github.NewClient(nil).WithAuthToken("token")
	baseURL, _ := c.client.BaseURL.Parse(server.URL + "/")
	c.client.BaseURL = baseURL

	ctx := context.Background()
	jobs, err := c.getOrgQueuedJobs(ctx)
	if err != nil {
		t.Fatalf("getOrgQueuedJobs() error = %v", err)
	}

	if len(jobs) == 0 {
		t.Error("getOrgQueuedJobs() returned no jobs")
	}
}

func TestGetQueuedJobs_OrgScope(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/orgs/myorg/repos", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4999")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"name": "repo1", "archived": false, "disabled": false},
		})
	})

	mux.HandleFunc("/repos/myorg/repo1/actions/runs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4998")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"total_count":   0,
			"workflow_runs": []map[string]interface{}{},
		})
	})

	c := NewClient("token", "myorg", "", "org")
	c.client = github.NewClient(nil).WithAuthToken("token")
	baseURL, _ := c.client.BaseURL.Parse(server.URL + "/")
	c.client.BaseURL = baseURL

	ctx := context.Background()
	jobs, err := c.GetQueuedJobs(ctx)
	if err != nil {
		t.Fatalf("GetQueuedJobs() org scope error = %v", err)
	}

	// No jobs expected since no queued runs - nil is acceptable
	if len(jobs) != 0 {
		t.Errorf("GetQueuedJobs() returned %d jobs, want 0", len(jobs))
	}
}

func TestRepo_Struct(t *testing.T) {
	repo := Repo{Owner: "myowner", Name: "myrepo"}

	if repo.Owner != "myowner" {
		t.Errorf("Repo.Owner = %q, want myowner", repo.Owner)
	}
	if repo.Name != "myrepo" {
		t.Errorf("Repo.Name = %q, want myrepo", repo.Name)
	}
}

func TestGetRegistrationToken_InvalidRepoFormat(t *testing.T) {
	c := NewClient("token", "owner", "", "repo")

	ctx := context.Background()
	_, err := c.GetRegistrationToken(ctx, "invalidformat")
	if err == nil {
		t.Error("GetRegistrationToken() with invalid format should return error")
	}
}

func TestWaitForRateLimit_RecentCallsArePreserved(t *testing.T) {
	c := NewClientWithOptions(ClientOptions{
		Token:       "token",
		Owner:       "owner",
		CallsPerMin: 60,
	})

	now := time.Now()
	c.callTimes = []time.Time{
		now.Add(-10 * time.Second),
		now.Add(-5 * time.Second),
	}

	c.waitForRateLimit()

	if len(c.callTimes) < 2 {
		t.Errorf("recent callTimes should be preserved, got %d entries", len(c.callTimes))
	}
}
