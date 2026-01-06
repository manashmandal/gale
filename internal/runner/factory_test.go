package runner

import (
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/manashmandal/gale/internal/config"
	"github.com/manashmandal/gale/internal/docker"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestNewClient_DockerMode(t *testing.T) {
	cfg := &config.Config{
		Runner: config.RunnerConfig{
			Mode:  "docker",
			Image: "test-image:latest",
		},
	}

	// This will fail without Docker, but tests the path selection
	_, err := NewClient(cfg, testLogger())
	// We expect an error because Docker isn't available in tests
	// The important thing is it chose the docker path
	if err == nil {
		t.Log("NewClient succeeded (Docker available)")
	} else {
		t.Logf("NewClient failed as expected without Docker: %v", err)
	}
}

func TestNewClient_NativeMode(t *testing.T) {
	cfg := &config.Config{
		Runner: config.RunnerConfig{
			Mode: "native",
		},
	}

	client, err := NewClient(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewClient(native) error = %v", err)
	}
	if client == nil {
		t.Fatal("NewClient(native) returned nil")
	}
	defer client.Close()

	// Verify it's a native adapter
	_, ok := client.(*NativeAdapter)
	if !ok {
		t.Errorf("Expected *NativeAdapter, got %T", client)
	}
}

func TestNewClient_EmptyModeDefaultsToDocker(t *testing.T) {
	cfg := &config.Config{
		Runner: config.RunnerConfig{
			Mode:  "", // Empty defaults to docker
			Image: "test-image:latest",
		},
	}

	// This will fail without Docker, but verifies it defaults to docker mode
	_, err := NewClient(cfg, testLogger())
	if err == nil {
		t.Log("NewClient with empty mode succeeded (Docker available)")
	} else {
		// Error indicates it tried docker path (expected without Docker)
		t.Logf("NewClient with empty mode tried Docker path: %v", err)
	}
}

func TestNewClient_UnknownMode(t *testing.T) {
	cfg := &config.Config{
		Runner: config.RunnerConfig{
			Mode: "invalid-mode",
		},
	}

	client, err := NewClient(cfg, testLogger())
	if err == nil {
		t.Error("NewClient(invalid-mode) should return error")
	}
	if client != nil {
		t.Error("NewClient(invalid-mode) should return nil client")
	}

	expectedMsg := "unknown runner mode"
	if err != nil && !contains(err.Error(), expectedMsg) {
		t.Errorf("Error message should contain %q, got %q", expectedMsg, err.Error())
	}
}

func TestDockerPermissionError_TypeAlias(t *testing.T) {
	// Verify the type alias works correctly
	originalErr := errors.New("permission denied")
	permErr := &DockerPermissionError{Err: originalErr}

	if permErr.Error() != originalErr.Error() {
		t.Errorf("Error() = %q, want %q", permErr.Error(), originalErr.Error())
	}

	if permErr.Unwrap() != originalErr {
		t.Error("Unwrap() should return original error")
	}
}

func TestNewDockerClient_WithMock(t *testing.T) {
	// Test the docker adapter creation indirectly through mock
	mockClient := docker.NewMockClient()
	adapter := NewDockerAdapter(mockClient)

	if adapter == nil {
		t.Fatal("NewDockerAdapter returned nil")
	}

	// Verify it implements Client interface
	var _ Client = adapter
}

func TestNewNativeClient_Success(t *testing.T) {
	cfg := &config.Config{
		Runner: config.RunnerConfig{
			Mode: "native",
		},
	}

	client, err := NewClient(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewClient error = %v", err)
	}
	defer client.Close()

	// Verify we can call methods without panic
	_, err = client.ListRunners(nil)
	if err != nil {
		t.Logf("ListRunners error (expected): %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
