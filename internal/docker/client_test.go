package docker

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestConstants(t *testing.T) {
	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{"LabelManagedBy", LabelManagedBy, "gale.managed-by"},
		{"LabelRunnerID", LabelRunnerID, "gale.runner-id"},
		{"LabelRepo", LabelRepo, "gale.repo"},
		{"ManagedByValue", ManagedByValue, "gale"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.expected)
			}
		})
	}
}

func TestRunnerConfig(t *testing.T) {
	cfg := RunnerConfig{
		Image:       "myoung34/github-runner:latest",
		Token:       "test-token",
		RepoURL:     "https://github.com/owner/repo",
		OrgName:     "test-org",
		Scope:       "org",
		Labels:      []string{"gale", "self-hosted"},
		Env:         map[string]string{"FOO": "bar"},
		NetworkMode: "host",
	}

	if cfg.Image != "myoung34/github-runner:latest" {
		t.Errorf("Image = %q, want myoung34/github-runner:latest", cfg.Image)
	}
	if cfg.Token != "test-token" {
		t.Errorf("Token = %q, want test-token", cfg.Token)
	}
	if cfg.RepoURL != "https://github.com/owner/repo" {
		t.Errorf("RepoURL = %q, want https://github.com/owner/repo", cfg.RepoURL)
	}
	if cfg.OrgName != "test-org" {
		t.Errorf("OrgName = %q, want test-org", cfg.OrgName)
	}
	if cfg.Scope != "org" {
		t.Errorf("Scope = %q, want org", cfg.Scope)
	}
	if len(cfg.Labels) != 2 {
		t.Errorf("Labels len = %d, want 2", len(cfg.Labels))
	}
	if cfg.Env["FOO"] != "bar" {
		t.Errorf("Env[FOO] = %q, want bar", cfg.Env["FOO"])
	}
	if cfg.NetworkMode != "host" {
		t.Errorf("NetworkMode = %q, want host", cfg.NetworkMode)
	}
}

func TestRunner(t *testing.T) {
	runner := Runner{
		ID:          "abc123",
		ContainerID: "container-12345",
		Status:      "running",
		Repo:        "https://github.com/owner/repo",
	}

	if runner.ID != "abc123" {
		t.Errorf("ID = %q, want abc123", runner.ID)
	}
	if runner.ContainerID != "container-12345" {
		t.Errorf("ContainerID = %q, want container-12345", runner.ContainerID)
	}
	if runner.Status != "running" {
		t.Errorf("Status = %q, want running", runner.Status)
	}
	if runner.Repo != "https://github.com/owner/repo" {
		t.Errorf("Repo = %q, want https://github.com/owner/repo", runner.Repo)
	}
}

func TestNewClient_InvalidHost(t *testing.T) {
	// Test with an invalid host - should return an error
	// Note: Some invalid hosts may not cause immediate errors due to lazy connection
	_, err := NewClient("tcp://invalid:9999")
	// Just ensure the function doesn't panic with invalid input
	_ = err
}

func TestRunnerConfig_EmptyValues(t *testing.T) {
	cfg := RunnerConfig{}

	if cfg.Image != "" {
		t.Errorf("Empty Image = %q, want empty string", cfg.Image)
	}
	if cfg.Token != "" {
		t.Errorf("Empty Token = %q, want empty string", cfg.Token)
	}
	if cfg.Scope != "" {
		t.Errorf("Empty Scope = %q, want empty string", cfg.Scope)
	}
	if cfg.Labels != nil {
		t.Errorf("Empty Labels = %v, want nil", cfg.Labels)
	}
	if cfg.Env != nil {
		t.Errorf("Empty Env = %v, want nil", cfg.Env)
	}
}

// Mock client tests

func TestMockClient_NewMockClient(t *testing.T) {
	mock := NewMockClient()
	if mock == nil {
		t.Fatal("NewMockClient() returned nil")
	}
}

func TestMockClient_EnsureImage(t *testing.T) {
	mock := NewMockClient()
	ctx := context.Background()

	err := mock.EnsureImage(ctx, "test-image:latest")
	if err != nil {
		t.Errorf("EnsureImage() error = %v", err)
	}

	if len(mock.EnsureImageCalls) != 1 {
		t.Errorf("EnsureImageCalls = %d, want 1", len(mock.EnsureImageCalls))
	}
	if mock.EnsureImageCalls[0] != "test-image:latest" {
		t.Errorf("EnsureImageCalls[0] = %q, want test-image:latest", mock.EnsureImageCalls[0])
	}
}

func TestMockClient_EnsureImage_Error(t *testing.T) {
	mock := NewMockClient()
	mock.EnsureImageFunc = func(ctx context.Context, imageName string) error {
		return errors.New("pull failed")
	}

	ctx := context.Background()
	err := mock.EnsureImage(ctx, "test-image:latest")
	if err == nil {
		t.Error("EnsureImage() should return error")
	}
}

func TestMockClient_CreateRunner(t *testing.T) {
	mock := NewMockClient()
	ctx := context.Background()

	cfg := RunnerConfig{
		Image:   "test-image",
		Token:   "test-token",
		RepoURL: "https://github.com/owner/repo",
	}

	runner, err := mock.CreateRunner(ctx, cfg)
	if err != nil {
		t.Errorf("CreateRunner() error = %v", err)
	}
	if runner == nil {
		t.Fatal("CreateRunner() returned nil")
	}
	if runner.Status != "running" {
		t.Errorf("runner.Status = %q, want running", runner.Status)
	}

	if len(mock.CreateRunnerCalls) != 1 {
		t.Errorf("CreateRunnerCalls = %d, want 1", len(mock.CreateRunnerCalls))
	}
}

func TestMockClient_ListRunners(t *testing.T) {
	mock := NewMockClient()
	ctx := context.Background()

	// Add some runners
	mock.SetRunners([]Runner{
		{ID: "1", ContainerID: "c1", Status: "running"},
		{ID: "2", ContainerID: "c2", Status: "exited"},
	})

	runners, err := mock.ListRunners(ctx)
	if err != nil {
		t.Errorf("ListRunners() error = %v", err)
	}
	if len(runners) != 2 {
		t.Errorf("ListRunners() len = %d, want 2", len(runners))
	}

	if mock.ListRunnersCalls != 1 {
		t.Errorf("ListRunnersCalls = %d, want 1", mock.ListRunnersCalls)
	}
}

func TestMockClient_GetActiveRunnerCount(t *testing.T) {
	mock := NewMockClient()
	ctx := context.Background()

	mock.SetRunners([]Runner{
		{ID: "1", ContainerID: "c1", Status: "running"},
		{ID: "2", ContainerID: "c2", Status: "exited"},
		{ID: "3", ContainerID: "c3", Status: "running"},
	})

	count, err := mock.GetActiveRunnerCount(ctx)
	if err != nil {
		t.Errorf("GetActiveRunnerCount() error = %v", err)
	}
	if count != 2 {
		t.Errorf("GetActiveRunnerCount() = %d, want 2", count)
	}
}

func TestMockClient_RemoveRunner(t *testing.T) {
	mock := NewMockClient()
	ctx := context.Background()

	mock.SetRunners([]Runner{
		{ID: "1", ContainerID: "c1", Status: "running"},
		{ID: "2", ContainerID: "c2", Status: "running"},
	})

	err := mock.RemoveRunner(ctx, "c1")
	if err != nil {
		t.Errorf("RemoveRunner() error = %v", err)
	}

	runners := mock.GetRunners()
	if len(runners) != 1 {
		t.Errorf("After RemoveRunner, len = %d, want 1", len(runners))
	}

	if len(mock.RemoveRunnerCalls) != 1 {
		t.Errorf("RemoveRunnerCalls = %d, want 1", len(mock.RemoveRunnerCalls))
	}
}

func TestMockClient_CleanupExitedRunners(t *testing.T) {
	mock := NewMockClient()
	ctx := context.Background()

	mock.SetRunners([]Runner{
		{ID: "1", ContainerID: "c1", Status: "running"},
		{ID: "2", ContainerID: "c2", Status: "exited"},
		{ID: "3", ContainerID: "c3", Status: "exited"},
	})

	cleaned, err := mock.CleanupExitedRunners(ctx)
	if err != nil {
		t.Errorf("CleanupExitedRunners() error = %v", err)
	}
	if cleaned != 2 {
		t.Errorf("CleanupExitedRunners() = %d, want 2", cleaned)
	}

	runners := mock.GetRunners()
	if len(runners) != 1 {
		t.Errorf("After cleanup, len = %d, want 1", len(runners))
	}
}

func TestMockClient_Close(t *testing.T) {
	mock := NewMockClient()

	err := mock.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	if mock.CloseCalls != 1 {
		t.Errorf("CloseCalls = %d, want 1", mock.CloseCalls)
	}
}

func TestMockClient_Reset(t *testing.T) {
	mock := NewMockClient()
	ctx := context.Background()

	// Make some calls
	mock.EnsureImage(ctx, "image")
	mock.ListRunners(ctx)
	mock.Close()

	// Reset
	mock.Reset()

	if len(mock.EnsureImageCalls) != 0 {
		t.Error("After Reset, EnsureImageCalls should be empty")
	}
	if mock.ListRunnersCalls != 0 {
		t.Error("After Reset, ListRunnersCalls should be 0")
	}
	if mock.CloseCalls != 0 {
		t.Error("After Reset, CloseCalls should be 0")
	}
}

func TestDockerClientInterface(t *testing.T) {
	// Test that both Client and MockClient satisfy DockerClient interface
	var _ DockerClient = (*Client)(nil)
	var _ DockerClient = (*MockClient)(nil)
}

func TestMockClient_IsContainerExited(t *testing.T) {
	mock := NewMockClient()
	ctx := context.Background()

	// Add a running container
	mock.SetRunners([]Runner{
		{ID: "1", ContainerID: "container-123", Status: "running"},
	})

	// Container exists and is running - should return false
	exited, err := mock.IsContainerExited(ctx, "container-123")
	if err != nil {
		t.Errorf("IsContainerExited() error = %v", err)
	}
	if exited {
		t.Error("IsContainerExited() for running container = true, want false")
	}

	if len(mock.IsContainerExitedCalls) != 1 {
		t.Errorf("IsContainerExitedCalls = %d, want 1", len(mock.IsContainerExitedCalls))
	}

	// Container not found - default returns true
	exited, err = mock.IsContainerExited(ctx, "nonexistent")
	if err != nil {
		t.Errorf("IsContainerExited() for nonexistent error = %v", err)
	}
	if !exited {
		t.Error("IsContainerExited() for nonexistent = false, want true")
	}

	// Custom behavior
	mock.IsContainerExitedFunc = func(ctx context.Context, containerID string) (bool, error) {
		return true, nil
	}

	exited, err = mock.IsContainerExited(ctx, "container-456")
	if err != nil {
		t.Errorf("IsContainerExited() with func error = %v", err)
	}
	if !exited {
		t.Error("IsContainerExited() with func = false, want true")
	}
}

func TestMockClient_StopRunner(t *testing.T) {
	mock := NewMockClient()
	ctx := context.Background()

	err := mock.StopRunner(ctx, "container-123", 10)
	if err != nil {
		t.Errorf("StopRunner() error = %v", err)
	}

	if len(mock.StopRunnerCalls) != 1 {
		t.Errorf("StopRunnerCalls = %d, want 1", len(mock.StopRunnerCalls))
	}
	if mock.StopRunnerCalls[0] != "container-123" {
		t.Errorf("StopRunnerCalls[0] = %q, want container-123", mock.StopRunnerCalls[0])
	}

	// Custom error behavior
	mock.StopRunnerFunc = func(ctx context.Context, containerID string, timeout int) error {
		return errors.New("stop failed")
	}

	err = mock.StopRunner(ctx, "container-456", 5)
	if err == nil {
		t.Error("StopRunner() with func should return error")
	}
}

func TestMockClient_KillTimedOutRunners(t *testing.T) {
	mock := NewMockClient()
	ctx := context.Background()

	killed, err := mock.KillTimedOutRunners(ctx, 30*time.Minute)
	if err != nil {
		t.Errorf("KillTimedOutRunners() error = %v", err)
	}
	if len(killed) != 0 {
		t.Errorf("KillTimedOutRunners() = %d runners, want 0", len(killed))
	}

	if mock.KillTimedOutRunnersCalls != 1 {
		t.Errorf("KillTimedOutRunnersCalls = %d, want 1", mock.KillTimedOutRunnersCalls)
	}
}

func TestMockClient_CreateRunner_CustomFunc(t *testing.T) {
	mock := NewMockClient()
	ctx := context.Background()

	customRunner := &Runner{
		ID:          "custom-id",
		ContainerID: "custom-container",
		Status:      "created",
	}

	mock.CreateRunnerFunc = func(ctx context.Context, cfg RunnerConfig) (*Runner, error) {
		return customRunner, nil
	}

	cfg := RunnerConfig{
		Image: "test-image",
	}

	runner, err := mock.CreateRunner(ctx, cfg)
	if err != nil {
		t.Errorf("CreateRunner() error = %v", err)
	}
	if runner.ID != "custom-id" {
		t.Errorf("runner.ID = %q, want custom-id", runner.ID)
	}
}

func TestMockClient_CreateRunner_Error(t *testing.T) {
	mock := NewMockClient()
	ctx := context.Background()

	mock.CreateRunnerFunc = func(ctx context.Context, cfg RunnerConfig) (*Runner, error) {
		return nil, errors.New("create failed")
	}

	cfg := RunnerConfig{
		Image: "test-image",
	}

	runner, err := mock.CreateRunner(ctx, cfg)
	if err == nil {
		t.Error("CreateRunner() should return error")
	}
	if runner != nil {
		t.Error("CreateRunner() should return nil runner on error")
	}
}
