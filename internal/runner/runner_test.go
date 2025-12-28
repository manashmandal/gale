package runner

import (
	"context"
	"testing"
	"time"

	"github.com/manashmandal/gale/internal/docker"
	"github.com/manashmandal/gale/internal/native"
)

func TestRunner_Struct(t *testing.T) {
	now := time.Now()
	r := Runner{
		ID:          "runner-123",
		ContainerID: "container-abc",
		PID:         12345,
		Status:      "running",
		Repo:        "owner/repo",
		StartedAt:   now,
	}

	if r.ID != "runner-123" {
		t.Errorf("ID = %q, want %q", r.ID, "runner-123")
	}
	if r.ContainerID != "container-abc" {
		t.Errorf("ContainerID = %q, want %q", r.ContainerID, "container-abc")
	}
	if r.PID != 12345 {
		t.Errorf("PID = %d, want %d", r.PID, 12345)
	}
	if r.Status != "running" {
		t.Errorf("Status = %q, want %q", r.Status, "running")
	}
	if r.Repo != "owner/repo" {
		t.Errorf("Repo = %q, want %q", r.Repo, "owner/repo")
	}
	if !r.StartedAt.Equal(now) {
		t.Errorf("StartedAt = %v, want %v", r.StartedAt, now)
	}
}

func TestConfig_Struct(t *testing.T) {
	cfg := Config{
		Image:       "myimage:latest",
		Token:       "token123",
		RepoURL:     "https://github.com/owner/repo",
		OrgName:     "myorg",
		Scope:       "repo",
		Labels:      []string{"self-hosted", "linux"},
		Env:         map[string]string{"FOO": "bar"},
		NetworkMode: "host",
	}

	if cfg.Image != "myimage:latest" {
		t.Errorf("Image = %q, want %q", cfg.Image, "myimage:latest")
	}
	if cfg.Token != "token123" {
		t.Errorf("Token = %q, want %q", cfg.Token, "token123")
	}
	if cfg.RepoURL != "https://github.com/owner/repo" {
		t.Errorf("RepoURL = %q, want %q", cfg.RepoURL, "https://github.com/owner/repo")
	}
	if cfg.OrgName != "myorg" {
		t.Errorf("OrgName = %q, want %q", cfg.OrgName, "myorg")
	}
	if cfg.Scope != "repo" {
		t.Errorf("Scope = %q, want %q", cfg.Scope, "repo")
	}
	if len(cfg.Labels) != 2 {
		t.Errorf("len(Labels) = %d, want 2", len(cfg.Labels))
	}
	if cfg.Env["FOO"] != "bar" {
		t.Errorf("Env[FOO] = %q, want %q", cfg.Env["FOO"], "bar")
	}
	if cfg.NetworkMode != "host" {
		t.Errorf("NetworkMode = %q, want %q", cfg.NetworkMode, "host")
	}
}

func TestDockerAdapter_NewDockerAdapter(t *testing.T) {
	mockClient := docker.NewMockClient()
	adapter := NewDockerAdapter(mockClient)

	if adapter == nil {
		t.Fatal("NewDockerAdapter returned nil")
	}
	if adapter.client != mockClient {
		t.Error("adapter.client != mockClient")
	}
}

func TestDockerAdapter_Close(t *testing.T) {
	mockClient := docker.NewMockClient()
	adapter := NewDockerAdapter(mockClient)

	err := adapter.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
	if mockClient.CloseCalls != 1 {
		t.Errorf("CloseCalls = %d, want 1", mockClient.CloseCalls)
	}
}

func TestDockerAdapter_CreateRunner(t *testing.T) {
	mockClient := docker.NewMockClient()
	adapter := NewDockerAdapter(mockClient)

	cfg := Config{
		Image:       "test-image",
		Token:       "test-token",
		RepoURL:     "https://github.com/owner/repo",
		OrgName:     "owner",
		Scope:       "repo",
		Labels:      []string{"self-hosted"},
		Env:         map[string]string{"KEY": "value"},
		NetworkMode: "bridge",
	}

	runner, err := adapter.CreateRunner(context.Background(), cfg)
	if err != nil {
		t.Fatalf("CreateRunner() error = %v", err)
	}

	if runner == nil {
		t.Fatal("CreateRunner returned nil runner")
	}
	if len(mockClient.CreateRunnerCalls) != 1 {
		t.Errorf("CreateRunnerCalls = %d, want 1", len(mockClient.CreateRunnerCalls))
	}

	// Verify config was passed correctly
	call := mockClient.CreateRunnerCalls[0]
	if call.Image != cfg.Image {
		t.Errorf("Image = %q, want %q", call.Image, cfg.Image)
	}
	if call.Token != cfg.Token {
		t.Errorf("Token = %q, want %q", call.Token, cfg.Token)
	}
}

func TestDockerAdapter_ListRunners(t *testing.T) {
	mockClient := docker.NewMockClient()
	mockClient.SetRunners([]docker.Runner{
		{ID: "runner1", ContainerID: "c1", Status: "running"},
		{ID: "runner2", ContainerID: "c2", Status: "exited"},
	})
	adapter := NewDockerAdapter(mockClient)

	runners, err := adapter.ListRunners(context.Background())
	if err != nil {
		t.Fatalf("ListRunners() error = %v", err)
	}

	if len(runners) != 2 {
		t.Errorf("len(runners) = %d, want 2", len(runners))
	}
	if runners[0].ID != "runner1" {
		t.Errorf("runners[0].ID = %q, want %q", runners[0].ID, "runner1")
	}
	if runners[1].Status != "exited" {
		t.Errorf("runners[1].Status = %q, want %q", runners[1].Status, "exited")
	}
}

func TestDockerAdapter_GetActiveRunnerCount(t *testing.T) {
	mockClient := docker.NewMockClient()
	mockClient.SetRunners([]docker.Runner{
		{ID: "r1", Status: "running"},
		{ID: "r2", Status: "running"},
		{ID: "r3", Status: "exited"},
	})
	adapter := NewDockerAdapter(mockClient)

	count, err := adapter.GetActiveRunnerCount(context.Background())
	if err != nil {
		t.Fatalf("GetActiveRunnerCount() error = %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestDockerAdapter_RemoveRunner(t *testing.T) {
	mockClient := docker.NewMockClient()
	adapter := NewDockerAdapter(mockClient)

	err := adapter.RemoveRunner(context.Background(), "runner-123")
	if err != nil {
		t.Fatalf("RemoveRunner() error = %v", err)
	}
	if len(mockClient.RemoveRunnerCalls) != 1 {
		t.Errorf("RemoveRunnerCalls = %d, want 1", len(mockClient.RemoveRunnerCalls))
	}
	if mockClient.RemoveRunnerCalls[0] != "runner-123" {
		t.Errorf("RemoveRunnerCalls[0] = %q, want %q", mockClient.RemoveRunnerCalls[0], "runner-123")
	}
}

func TestDockerAdapter_CleanupExitedRunners(t *testing.T) {
	mockClient := docker.NewMockClient()
	mockClient.SetRunners([]docker.Runner{
		{ID: "r1", Status: "running"},
		{ID: "r2", Status: "exited"},
		{ID: "r3", Status: "exited"},
	})
	adapter := NewDockerAdapter(mockClient)

	count, err := adapter.CleanupExitedRunners(context.Background())
	if err != nil {
		t.Fatalf("CleanupExitedRunners() error = %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestDockerAdapter_StopRunner(t *testing.T) {
	mockClient := docker.NewMockClient()
	adapter := NewDockerAdapter(mockClient)

	err := adapter.StopRunner(context.Background(), "runner-123", 30)
	if err != nil {
		t.Fatalf("StopRunner() error = %v", err)
	}
	if len(mockClient.StopRunnerCalls) != 1 {
		t.Errorf("StopRunnerCalls = %d, want 1", len(mockClient.StopRunnerCalls))
	}
}

func TestDockerAdapter_IsRunnerExited(t *testing.T) {
	mockClient := docker.NewMockClient()
	mockClient.IsContainerExitedFunc = func(ctx context.Context, id string) (bool, error) {
		return true, nil
	}
	adapter := NewDockerAdapter(mockClient)

	exited, err := adapter.IsRunnerExited(context.Background(), "runner-123")
	if err != nil {
		t.Fatalf("IsRunnerExited() error = %v", err)
	}
	if !exited {
		t.Error("exited = false, want true")
	}
}

func TestDockerAdapter_ImplementsClient(t *testing.T) {
	mockClient := docker.NewMockClient()
	adapter := NewDockerAdapter(mockClient)

	var _ Client = adapter
}

func TestNativeAdapter_NewNativeAdapter(t *testing.T) {
	tmpDir := t.TempDir()
	nativeClient, err := native.NewClient(tmpDir)
	if err != nil {
		t.Fatalf("native.NewClient() error = %v", err)
	}

	adapter := NewNativeAdapter(nativeClient)
	if adapter == nil {
		t.Fatal("NewNativeAdapter returned nil")
	}
}

func TestNativeAdapter_Close(t *testing.T) {
	tmpDir := t.TempDir()
	nativeClient, _ := native.NewClient(tmpDir)
	adapter := NewNativeAdapter(nativeClient)

	err := adapter.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestNativeAdapter_ListRunners(t *testing.T) {
	tmpDir := t.TempDir()
	nativeClient, _ := native.NewClient(tmpDir)
	adapter := NewNativeAdapter(nativeClient)

	runners, err := adapter.ListRunners(context.Background())
	if err != nil {
		t.Fatalf("ListRunners() error = %v", err)
	}
	if runners == nil {
		t.Error("ListRunners() returned nil")
	}
}

func TestNativeAdapter_GetActiveRunnerCount(t *testing.T) {
	tmpDir := t.TempDir()
	nativeClient, _ := native.NewClient(tmpDir)
	adapter := NewNativeAdapter(nativeClient)

	count, err := adapter.GetActiveRunnerCount(context.Background())
	if err != nil {
		t.Fatalf("GetActiveRunnerCount() error = %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestNativeAdapter_RemoveRunner(t *testing.T) {
	tmpDir := t.TempDir()
	nativeClient, _ := native.NewClient(tmpDir)
	adapter := NewNativeAdapter(nativeClient)

	err := adapter.RemoveRunner(context.Background(), "nonexistent")
	if err != nil {
		t.Errorf("RemoveRunner() error = %v", err)
	}
}

func TestNativeAdapter_CleanupExitedRunners(t *testing.T) {
	tmpDir := t.TempDir()
	nativeClient, _ := native.NewClient(tmpDir)
	adapter := NewNativeAdapter(nativeClient)

	count, err := adapter.CleanupExitedRunners(context.Background())
	if err != nil {
		t.Fatalf("CleanupExitedRunners() error = %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestNativeAdapter_StopRunner(t *testing.T) {
	tmpDir := t.TempDir()
	nativeClient, _ := native.NewClient(tmpDir)
	adapter := NewNativeAdapter(nativeClient)

	err := adapter.StopRunner(context.Background(), "nonexistent", 10)
	if err != nil {
		t.Errorf("StopRunner() error = %v", err)
	}
}

func TestNativeAdapter_IsRunnerExited(t *testing.T) {
	tmpDir := t.TempDir()
	nativeClient, _ := native.NewClient(tmpDir)
	adapter := NewNativeAdapter(nativeClient)

	exited, err := adapter.IsRunnerExited(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("IsRunnerExited() error = %v", err)
	}
	if !exited {
		t.Error("exited = false, want true for nonexistent runner")
	}
}

func TestNativeAdapter_ImplementsClient(t *testing.T) {
	tmpDir := t.TempDir()
	nativeClient, _ := native.NewClient(tmpDir)
	adapter := NewNativeAdapter(nativeClient)

	var _ Client = adapter
}
