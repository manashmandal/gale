package scaler

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/manashmandal/gale/internal/config"
	"github.com/manashmandal/gale/internal/docker"
	"github.com/manashmandal/gale/internal/github"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func testConfig() *config.Config {
	return &config.Config{
		GitHub: config.GitHubConfig{
			Owner: "testowner",
			Repo:  "testrepo",
			Token: "test-token",
			Scope: "repo",
		},
		Scaler: config.ScalerConfig{
			MaxRunners:         10,
			MinRunners:         1,
			PollInterval:       30 * time.Second,
			ScaleUpDelay:       0, // No delay for tests
			RateLimitThreshold: 2500,
		},
		Runner: config.RunnerConfig{
			Image:  "ghcr.io/actions/actions-runner:latest",
			Labels: []string{"gale", "self-hosted"},
		},
	}
}

func TestStats(t *testing.T) {
	stats := Stats{
		QueuedJobs:      5,
		ActiveRunners:   3,
		MaxRunners:      10,
		MinRunners:      1,
		RateLimitUsed:   100,
		RateLimitLimit:  5000,
		RateLimitThresh: 2500,
	}

	if stats.QueuedJobs != 5 {
		t.Errorf("QueuedJobs = %d, want 5", stats.QueuedJobs)
	}
	if stats.ActiveRunners != 3 {
		t.Errorf("ActiveRunners = %d, want 3", stats.ActiveRunners)
	}
	if stats.MaxRunners != 10 {
		t.Errorf("MaxRunners = %d, want 10", stats.MaxRunners)
	}
	if stats.MinRunners != 1 {
		t.Errorf("MinRunners = %d, want 1", stats.MinRunners)
	}
	if stats.RateLimitUsed != 100 {
		t.Errorf("RateLimitUsed = %d, want 100", stats.RateLimitUsed)
	}
	if stats.RateLimitLimit != 5000 {
		t.Errorf("RateLimitLimit = %d, want 5000", stats.RateLimitLimit)
	}
	if stats.RateLimitThresh != 2500 {
		t.Errorf("RateLimitThresh = %d, want 2500", stats.RateLimitThresh)
	}
}

func TestOptions(t *testing.T) {
	tests := []struct {
		name      string
		opts      Options
		wantForce bool
	}{
		{
			name:      "default options",
			opts:      Options{},
			wantForce: false,
		},
		{
			name:      "force mode enabled",
			opts:      Options{Force: true},
			wantForce: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.opts.Force != tt.wantForce {
				t.Errorf("Force = %v, want %v", tt.opts.Force, tt.wantForce)
			}
		})
	}
}

func TestStats_ZeroValues(t *testing.T) {
	stats := Stats{}

	if stats.QueuedJobs != 0 {
		t.Errorf("Zero QueuedJobs = %d, want 0", stats.QueuedJobs)
	}
	if stats.ActiveRunners != 0 {
		t.Errorf("Zero ActiveRunners = %d, want 0", stats.ActiveRunners)
	}
	if stats.MaxRunners != 0 {
		t.Errorf("Zero MaxRunners = %d, want 0", stats.MaxRunners)
	}
	if stats.MinRunners != 0 {
		t.Errorf("Zero MinRunners = %d, want 0", stats.MinRunners)
	}
}

func TestNewWithClients(t *testing.T) {
	cfg := testConfig()
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()

	scaler, err := NewWithClients(cfg, logger, mockGH, mockDocker, Options{})
	if err != nil {
		t.Fatalf("NewWithClients() error = %v", err)
	}

	if scaler == nil {
		t.Fatal("NewWithClients() returned nil scaler")
	}

	if scaler.cfg != cfg {
		t.Error("Scaler config not set correctly")
	}

	if scaler.forceMode != false {
		t.Error("Force mode should be false by default")
	}
}

func TestNewWithClients_ForceMode(t *testing.T) {
	cfg := testConfig()
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()

	scaler, err := NewWithClients(cfg, logger, mockGH, mockDocker, Options{Force: true})
	if err != nil {
		t.Fatalf("NewWithClients() error = %v", err)
	}

	if scaler.forceMode != true {
		t.Error("Force mode should be true when set")
	}
}

func TestScaler_Close(t *testing.T) {
	cfg := testConfig()
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()

	scaler, _ := NewWithClients(cfg, logger, mockGH, mockDocker, Options{})

	err := scaler.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	if mockDocker.CloseCalls != 1 {
		t.Errorf("Close() calls = %d, want 1", mockDocker.CloseCalls)
	}
}

func TestScaler_Close_Error(t *testing.T) {
	cfg := testConfig()
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()
	mockDocker.CloseFunc = func() error {
		return errors.New("close error")
	}

	scaler, _ := NewWithClients(cfg, logger, mockGH, mockDocker, Options{})

	err := scaler.Close()
	if err == nil {
		t.Error("Close() should return error")
	}
}

func TestScaler_GetStats(t *testing.T) {
	cfg := testConfig()
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()

	// Set up mock data
	mockGH.SetQueuedJobs([]github.QueuedJob{
		{RunID: 1, JobID: 100, JobName: "build", Status: "queued", Repo: "testowner/testrepo"},
		{RunID: 1, JobID: 101, JobName: "test", Status: "queued", Repo: "testowner/testrepo"},
	})
	mockGH.SetRateLimitInfo(100, 5000, 2500)
	mockDocker.SetRunners([]docker.Runner{
		{ID: "runner-1", ContainerID: "container-1", Status: "running"},
		{ID: "runner-2", ContainerID: "container-2", Status: "running"},
		{ID: "runner-3", ContainerID: "container-3", Status: "running"},
	})

	scaler, _ := NewWithClients(cfg, logger, mockGH, mockDocker, Options{})

	ctx := context.Background()
	stats, err := scaler.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats() error = %v", err)
	}

	if stats.QueuedJobs != 2 {
		t.Errorf("QueuedJobs = %d, want 2", stats.QueuedJobs)
	}
	if stats.ActiveRunners != 3 {
		t.Errorf("ActiveRunners = %d, want 3", stats.ActiveRunners)
	}
	if stats.MaxRunners != 10 {
		t.Errorf("MaxRunners = %d, want 10", stats.MaxRunners)
	}
	if stats.MinRunners != 1 {
		t.Errorf("MinRunners = %d, want 1", stats.MinRunners)
	}
	if stats.RateLimitUsed != 100 {
		t.Errorf("RateLimitUsed = %d, want 100", stats.RateLimitUsed)
	}
	if stats.RateLimitLimit != 5000 {
		t.Errorf("RateLimitLimit = %d, want 5000", stats.RateLimitLimit)
	}
}

func TestScaler_GetStats_GitHubError(t *testing.T) {
	cfg := testConfig()
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()

	mockGH.GetQueuedJobsFunc = func(ctx context.Context) ([]github.QueuedJob, error) {
		return nil, errors.New("GitHub API error")
	}

	scaler, _ := NewWithClients(cfg, logger, mockGH, mockDocker, Options{})

	ctx := context.Background()
	_, err := scaler.GetStats(ctx)
	if err == nil {
		t.Error("GetStats() should return error when GitHub fails")
	}
}

func TestScaler_GetStats_DockerError(t *testing.T) {
	cfg := testConfig()
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()

	mockDocker.GetActiveRunnerCountFunc = func(ctx context.Context) (int, error) {
		return 0, errors.New("Docker error")
	}

	scaler, _ := NewWithClients(cfg, logger, mockGH, mockDocker, Options{})

	ctx := context.Background()
	_, err := scaler.GetStats(ctx)
	if err == nil {
		t.Error("GetStats() should return error when Docker fails")
	}
}

func TestScaler_Reconcile_NoJobsMinRunners(t *testing.T) {
	cfg := testConfig()
	cfg.Scaler.MinRunners = 2
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()

	// No queued jobs, no active runners
	mockGH.SetQueuedJobs([]github.QueuedJob{})
	mockDocker.SetRunners([]docker.Runner{})

	scaler, _ := NewWithClients(cfg, logger, mockGH, mockDocker, Options{})

	ctx := context.Background()
	scaler.reconcile(ctx)

	// Should create 2 runners to maintain minimum
	if len(mockDocker.CreateRunnerCalls) != 2 {
		t.Errorf("CreateRunner calls = %d, want 2", len(mockDocker.CreateRunnerCalls))
	}
}

func TestScaler_Reconcile_WithQueuedJobs(t *testing.T) {
	cfg := testConfig()
	cfg.Scaler.MinRunners = 0
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()

	// 3 queued jobs, no active runners
	mockGH.SetQueuedJobs([]github.QueuedJob{
		{RunID: 1, JobID: 100, JobName: "job1", Status: "queued", Repo: "testowner/testrepo"},
		{RunID: 1, JobID: 101, JobName: "job2", Status: "queued", Repo: "testowner/testrepo"},
		{RunID: 1, JobID: 102, JobName: "job3", Status: "queued", Repo: "testowner/testrepo"},
	})
	mockDocker.SetRunners([]docker.Runner{})

	scaler, _ := NewWithClients(cfg, logger, mockGH, mockDocker, Options{})

	ctx := context.Background()
	scaler.reconcile(ctx)

	// Should create 3 runners for the 3 jobs
	if len(mockDocker.CreateRunnerCalls) != 3 {
		t.Errorf("CreateRunner calls = %d, want 3", len(mockDocker.CreateRunnerCalls))
	}
}

func TestScaler_Reconcile_MaxRunnersLimit(t *testing.T) {
	cfg := testConfig()
	cfg.Scaler.MaxRunners = 2
	cfg.Scaler.MinRunners = 0
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()

	// 5 queued jobs, but max is 2
	mockGH.SetQueuedJobs([]github.QueuedJob{
		{RunID: 1, JobID: 100, JobName: "job1", Status: "queued", Repo: "testowner/testrepo"},
		{RunID: 1, JobID: 101, JobName: "job2", Status: "queued", Repo: "testowner/testrepo"},
		{RunID: 1, JobID: 102, JobName: "job3", Status: "queued", Repo: "testowner/testrepo"},
		{RunID: 1, JobID: 103, JobName: "job4", Status: "queued", Repo: "testowner/testrepo"},
		{RunID: 1, JobID: 104, JobName: "job5", Status: "queued", Repo: "testowner/testrepo"},
	})
	mockDocker.SetRunners([]docker.Runner{})

	scaler, _ := NewWithClients(cfg, logger, mockGH, mockDocker, Options{})

	ctx := context.Background()
	scaler.reconcile(ctx)

	// Should only create 2 runners (max limit)
	if len(mockDocker.CreateRunnerCalls) != 2 {
		t.Errorf("CreateRunner calls = %d, want 2 (max)", len(mockDocker.CreateRunnerCalls))
	}
}

func TestScaler_Reconcile_AlreadyHaveEnoughRunners(t *testing.T) {
	cfg := testConfig()
	cfg.Scaler.MinRunners = 1
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()

	// 2 queued jobs, 2 active runners - no need to scale
	mockGH.SetQueuedJobs([]github.QueuedJob{
		{RunID: 1, JobID: 100, JobName: "job1", Status: "queued", Repo: "testowner/testrepo"},
		{RunID: 1, JobID: 101, JobName: "job2", Status: "queued", Repo: "testowner/testrepo"},
	})
	mockDocker.SetRunners([]docker.Runner{
		{ID: "runner-1", ContainerID: "container-1", Status: "running"},
		{ID: "runner-2", ContainerID: "container-2", Status: "running"},
	})

	scaler, _ := NewWithClients(cfg, logger, mockGH, mockDocker, Options{})

	ctx := context.Background()
	scaler.reconcile(ctx)

	// Should not create any runners
	if len(mockDocker.CreateRunnerCalls) != 0 {
		t.Errorf("CreateRunner calls = %d, want 0", len(mockDocker.CreateRunnerCalls))
	}
}

func TestScaler_Reconcile_CleansUpExitedRunners(t *testing.T) {
	cfg := testConfig()
	cfg.Scaler.MinRunners = 0
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()

	// Set some exited runners
	mockDocker.SetRunners([]docker.Runner{
		{ID: "runner-1", ContainerID: "container-1", Status: "exited"},
		{ID: "runner-2", ContainerID: "container-2", Status: "running"},
	})
	mockGH.SetQueuedJobs([]github.QueuedJob{})

	scaler, _ := NewWithClients(cfg, logger, mockGH, mockDocker, Options{})

	ctx := context.Background()
	scaler.reconcile(ctx)

	// Should call cleanup
	if mockDocker.CleanupExitedRunnersCalls != 1 {
		t.Errorf("CleanupExitedRunners calls = %d, want 1", mockDocker.CleanupExitedRunnersCalls)
	}
}

func TestScaler_Reconcile_GitHubError_MaintainsMinRunners(t *testing.T) {
	cfg := testConfig()
	cfg.Scaler.MinRunners = 2
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()

	// GitHub returns error
	mockGH.GetQueuedJobsFunc = func(ctx context.Context) ([]github.QueuedJob, error) {
		return nil, errors.New("GitHub API error")
	}
	mockDocker.SetRunners([]docker.Runner{})

	scaler, _ := NewWithClients(cfg, logger, mockGH, mockDocker, Options{})

	ctx := context.Background()
	scaler.reconcile(ctx)

	// Should still try to maintain min runners
	if len(mockDocker.CreateRunnerCalls) != 2 {
		t.Errorf("CreateRunner calls = %d, want 2 (min runners)", len(mockDocker.CreateRunnerCalls))
	}
}

func TestScaler_ScaleUpDelay(t *testing.T) {
	cfg := testConfig()
	cfg.Scaler.ScaleUpDelay = 1 * time.Hour // Long delay
	cfg.Scaler.MinRunners = 0
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()

	mockGH.SetQueuedJobs([]github.QueuedJob{
		{RunID: 1, JobID: 100, JobName: "job1", Status: "queued", Repo: "testowner/testrepo"},
	})
	mockDocker.SetRunners([]docker.Runner{})

	scaler, _ := NewWithClients(cfg, logger, mockGH, mockDocker, Options{})

	ctx := context.Background()

	// First reconcile should create runners
	scaler.reconcile(ctx)
	firstCalls := len(mockDocker.CreateRunnerCalls)

	// Second reconcile immediately should be throttled
	mockDocker.Reset()
	mockGH.SetQueuedJobs([]github.QueuedJob{
		{RunID: 1, JobID: 101, JobName: "job2", Status: "queued", Repo: "testowner/testrepo"},
	})
	scaler.reconcile(ctx)

	if len(mockDocker.CreateRunnerCalls) != 0 {
		t.Errorf("Second reconcile should be throttled, got %d calls", len(mockDocker.CreateRunnerCalls))
	}
	if firstCalls != 1 {
		t.Errorf("First reconcile should have created 1 runner, got %d", firstCalls)
	}
}

func TestScaler_EnsureMinRunners(t *testing.T) {
	cfg := testConfig()
	cfg.Scaler.MinRunners = 3
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()

	// 1 active runner, need 2 more
	mockDocker.SetRunners([]docker.Runner{
		{ID: "runner-1", ContainerID: "container-1", Status: "running"},
	})

	scaler, _ := NewWithClients(cfg, logger, mockGH, mockDocker, Options{})

	ctx := context.Background()
	scaler.ensureMinRunners(ctx)

	// Should create 2 runners
	if len(mockDocker.CreateRunnerCalls) != 2 {
		t.Errorf("CreateRunner calls = %d, want 2", len(mockDocker.CreateRunnerCalls))
	}
}

func TestScaler_EnsureMinRunners_ZeroMin(t *testing.T) {
	cfg := testConfig()
	cfg.Scaler.MinRunners = 0
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()

	scaler, _ := NewWithClients(cfg, logger, mockGH, mockDocker, Options{})

	ctx := context.Background()
	scaler.ensureMinRunners(ctx)

	// Should not create any runners
	if len(mockDocker.CreateRunnerCalls) != 0 {
		t.Errorf("CreateRunner calls = %d, want 0", len(mockDocker.CreateRunnerCalls))
	}
}

func TestScaler_EnsureMinRunners_AlreadyMet(t *testing.T) {
	cfg := testConfig()
	cfg.Scaler.MinRunners = 2
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()

	// 3 active runners, min is 2 - already met
	mockDocker.SetRunners([]docker.Runner{
		{ID: "runner-1", ContainerID: "container-1", Status: "running"},
		{ID: "runner-2", ContainerID: "container-2", Status: "running"},
		{ID: "runner-3", ContainerID: "container-3", Status: "running"},
	})

	scaler, _ := NewWithClients(cfg, logger, mockGH, mockDocker, Options{})

	ctx := context.Background()
	scaler.ensureMinRunners(ctx)

	// Should not create any runners
	if len(mockDocker.CreateRunnerCalls) != 0 {
		t.Errorf("CreateRunner calls = %d, want 0", len(mockDocker.CreateRunnerCalls))
	}
}

func TestScaler_Run_ContextCancel(t *testing.T) {
	cfg := testConfig()
	cfg.Scaler.PollInterval = 100 * time.Millisecond
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()

	scaler, _ := NewWithClients(cfg, logger, mockGH, mockDocker, Options{})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- scaler.Run(ctx)
	}()

	// Let it run for a bit then cancel
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("Run() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Run() did not exit after context cancel")
	}
}

func TestScaler_OrgScope(t *testing.T) {
	cfg := testConfig()
	cfg.GitHub.Repo = ""   // No specific repo
	cfg.GitHub.Scope = "org"
	cfg.Scaler.MinRunners = 1
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()

	mockGH.SetOrgScope(true)
	mockGH.SetQueuedJobs([]github.QueuedJob{
		{RunID: 1, JobID: 100, JobName: "job1", Status: "queued", Repo: "testowner/repo1"},
	})

	scaler, _ := NewWithClients(cfg, logger, mockGH, mockDocker, Options{})

	ctx := context.Background()
	scaler.reconcile(ctx)

	// Should have created a runner
	if len(mockDocker.CreateRunnerCalls) == 0 {
		t.Error("Should have created runners for org scope")
	}

	// Verify org name is set in config
	if len(mockDocker.CreateRunnerCalls) > 0 {
		runnerCfg := mockDocker.CreateRunnerCalls[0]
		if runnerCfg.OrgName != cfg.GitHub.Owner {
			t.Errorf("OrgName = %s, want %s", runnerCfg.OrgName, cfg.GitHub.Owner)
		}
	}
}

func TestScaler_MultiRepo(t *testing.T) {
	cfg := testConfig()
	cfg.GitHub.Repo = ""
	cfg.GitHub.Repos = []string{"repo1", "repo2", "repo3"}
	cfg.Scaler.MinRunners = 0
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()

	// Jobs from different repos
	mockGH.SetQueuedJobs([]github.QueuedJob{
		{RunID: 1, JobID: 100, JobName: "job1", Status: "queued", Repo: "testowner/repo1"},
		{RunID: 2, JobID: 101, JobName: "job2", Status: "queued", Repo: "testowner/repo2"},
	})

	scaler, _ := NewWithClients(cfg, logger, mockGH, mockDocker, Options{})

	ctx := context.Background()
	scaler.reconcile(ctx)

	// Should create 2 runners
	if len(mockDocker.CreateRunnerCalls) != 2 {
		t.Errorf("CreateRunner calls = %d, want 2", len(mockDocker.CreateRunnerCalls))
	}
}

func TestScaler_CreateRunnerError(t *testing.T) {
	cfg := testConfig()
	cfg.Scaler.MinRunners = 0
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()

	// Make CreateRunner fail
	mockDocker.CreateRunnerFunc = func(ctx context.Context, cfg docker.RunnerConfig) (*docker.Runner, error) {
		return nil, errors.New("failed to create runner")
	}

	mockGH.SetQueuedJobs([]github.QueuedJob{
		{RunID: 1, JobID: 100, JobName: "job1", Status: "queued", Repo: "testowner/testrepo"},
	})

	scaler, _ := NewWithClients(cfg, logger, mockGH, mockDocker, Options{})

	ctx := context.Background()
	// Should not panic even if CreateRunner fails
	scaler.reconcile(ctx)

	if len(mockDocker.CreateRunnerCalls) != 1 {
		t.Errorf("CreateRunner should have been called once, got %d", len(mockDocker.CreateRunnerCalls))
	}
}

func TestNew_WithMockClients(t *testing.T) {
	cfg := testConfig()
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()

	// Test NewWithOptions with injected clients
	scaler, err := NewWithOptions(cfg, logger, Options{
		GitHubClient: mockGH,
		DockerClient: mockDocker,
	})
	if err != nil {
		t.Fatalf("NewWithOptions() error = %v", err)
	}
	if scaler == nil {
		t.Fatal("NewWithOptions() returned nil")
	}

	// Verify the injected clients are used
	mockGH.SetQueuedJobs([]github.QueuedJob{
		{RunID: 1, JobID: 100, JobName: "test", Status: "queued", Repo: "testowner/testrepo"},
	})

	ctx := context.Background()
	stats, err := scaler.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats() error = %v", err)
	}

	if stats.QueuedJobs != 1 {
		t.Errorf("QueuedJobs = %d, want 1", stats.QueuedJobs)
	}

	// Verify mock was called
	if mockGH.GetQueuedJobsCalls != 1 {
		t.Errorf("GetQueuedJobsCalls = %d, want 1", mockGH.GetQueuedJobsCalls)
	}
}

func TestNewWithOptions_ForceMode(t *testing.T) {
	cfg := testConfig()
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()

	scaler, err := NewWithOptions(cfg, logger, Options{
		Force:        true,
		GitHubClient: mockGH,
		DockerClient: mockDocker,
	})
	if err != nil {
		t.Fatalf("NewWithOptions() error = %v", err)
	}

	if !scaler.forceMode {
		t.Error("forceMode should be true")
	}
}

func TestNewWithOptions_MultiRepoConfig(t *testing.T) {
	cfg := testConfig()
	cfg.GitHub.Repo = ""
	cfg.GitHub.Repos = []string{"repo1", "repo2"}
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()

	scaler, err := NewWithOptions(cfg, logger, Options{
		GitHubClient: mockGH,
		DockerClient: mockDocker,
	})
	if err != nil {
		t.Fatalf("NewWithOptions() error = %v", err)
	}

	if scaler == nil {
		t.Fatal("NewWithOptions() returned nil")
	}
}

func TestNew_WithMock(t *testing.T) {
	cfg := testConfig()
	logger := testLogger()
	mockGH := github.NewMockGitHubClient()
	mockDocker := docker.NewMockClient()

	// Test that New() works with Options containing mock clients
	scaler, err := NewWithOptions(cfg, logger, Options{
		GitHubClient: mockGH,
		DockerClient: mockDocker,
	})
	if err != nil {
		t.Fatalf("New() with mocks error = %v", err)
	}

	// Verify scaler works
	err = scaler.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	if mockDocker.CloseCalls != 1 {
		t.Errorf("CloseCalls = %d, want 1", mockDocker.CloseCalls)
	}
}
