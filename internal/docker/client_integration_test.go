//go:build integration

package docker

import (
	"context"
	"testing"
	"time"
)

// Integration tests for Docker client
// Run with: go test -tags=integration ./internal/docker/...

func TestIntegration_NewClient(t *testing.T) {
	client, err := NewClient("")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer client.Close()

	if client == nil {
		t.Fatal("NewClient() returned nil")
	}
}

func TestIntegration_EnsureImage(t *testing.T) {
	client, err := NewClient("")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Use a small, commonly available image
	err = client.EnsureImage(ctx, "alpine:latest")
	if err != nil {
		t.Errorf("EnsureImage() error = %v", err)
	}
}

func TestIntegration_ListRunners_Empty(t *testing.T) {
	client, err := NewClient("")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	runners, err := client.ListRunners(ctx)
	if err != nil {
		t.Errorf("ListRunners() error = %v", err)
	}
	// Just verify the call works, don't assert on count
	_ = runners
}

func TestIntegration_GetActiveRunnerCount(t *testing.T) {
	client, err := NewClient("")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	count, err := client.GetActiveRunnerCount(ctx)
	if err != nil {
		t.Errorf("GetActiveRunnerCount() error = %v", err)
	}
	if count < 0 {
		t.Errorf("GetActiveRunnerCount() = %d, want >= 0", count)
	}
}

func TestIntegration_CleanupExitedRunners(t *testing.T) {
	client, err := NewClient("")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	cleaned, err := client.CleanupExitedRunners(ctx)
	if err != nil {
		t.Errorf("CleanupExitedRunners() error = %v", err)
	}
	if cleaned < 0 {
		t.Errorf("CleanupExitedRunners() = %d, want >= 0", cleaned)
	}
}

func TestIntegration_Close(t *testing.T) {
	client, err := NewClient("")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	err = client.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}
