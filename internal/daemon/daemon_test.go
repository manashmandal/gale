package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name     string
		pidFile  string
		expected string
	}{
		{
			name:     "custom pid file",
			pidFile:  "/tmp/test-gale.pid",
			expected: "/tmp/test-gale.pid",
		},
		{
			name:     "empty pid file uses default",
			pidFile:  "",
			expected: DefaultPidFile(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := New(tt.pidFile)
			if d.pidFile != tt.expected {
				t.Errorf("New(%q).pidFile = %q, want %q", tt.pidFile, d.pidFile, tt.expected)
			}
		})
	}
}

func TestIsRunning_NoPidFile(t *testing.T) {
	d := New("/tmp/nonexistent-gale-test.pid")
	pid, running := d.IsRunning()
	if running {
		t.Error("IsRunning() = true, want false when PID file doesn't exist")
	}
	if pid != 0 {
		t.Errorf("IsRunning() pid = %d, want 0", pid)
	}
}

func TestIsRunning_InvalidPidFile(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "gale.pid")

	// Write invalid content
	if err := os.WriteFile(pidFile, []byte("not-a-number"), 0644); err != nil {
		t.Fatalf("Failed to write PID file: %v", err)
	}

	d := New(pidFile)
	pid, running := d.IsRunning()
	if running {
		t.Error("IsRunning() = true, want false for invalid PID file")
	}
	if pid != 0 {
		t.Errorf("IsRunning() pid = %d, want 0", pid)
	}
}

func TestIsRunning_StalePidFile(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "gale.pid")

	// Write a PID that definitely doesn't exist (very high number)
	if err := os.WriteFile(pidFile, []byte("999999999"), 0644); err != nil {
		t.Fatalf("Failed to write PID file: %v", err)
	}

	d := New(pidFile)
	pid, running := d.IsRunning()
	if running {
		t.Error("IsRunning() = true, want false for stale PID file")
	}
	if pid != 0 {
		t.Errorf("IsRunning() pid = %d, want 0", pid)
	}

	// Verify stale PID file was cleaned up
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Error("Stale PID file was not cleaned up")
	}
}

func TestIsRunning_CurrentProcess(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "gale.pid")

	// Write current process PID
	currentPID := os.Getpid()
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(currentPID)), 0644); err != nil {
		t.Fatalf("Failed to write PID file: %v", err)
	}

	d := New(pidFile)
	pid, running := d.IsRunning()
	if !running {
		t.Error("IsRunning() = false, want true for current process PID")
	}
	if pid != currentPID {
		t.Errorf("IsRunning() pid = %d, want %d", pid, currentPID)
	}

	// Cleanup
	os.Remove(pidFile)
}

func TestGetPID_NotRunning(t *testing.T) {
	d := New("/tmp/nonexistent-gale-test.pid")
	pid, err := d.GetPID()
	if err == nil {
		t.Error("GetPID() error = nil, want error when not running")
	}
	if pid != 0 {
		t.Errorf("GetPID() = %d, want 0", pid)
	}
}

func TestGetPID_Running(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "gale.pid")

	currentPID := os.Getpid()
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(currentPID)), 0644); err != nil {
		t.Fatalf("Failed to write PID file: %v", err)
	}

	d := New(pidFile)
	pid, err := d.GetPID()
	if err != nil {
		t.Errorf("GetPID() error = %v, want nil", err)
	}
	if pid != currentPID {
		t.Errorf("GetPID() = %d, want %d", pid, currentPID)
	}

	// Cleanup
	os.Remove(pidFile)
}

func TestWritePID(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "subdir", "gale.pid")

	d := New(pidFile)
	err := d.writePID()
	if err != nil {
		t.Errorf("writePID() error = %v, want nil", err)
	}

	// Verify PID file was created
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("Failed to read PID file: %v", err)
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatalf("Failed to parse PID: %v", err)
	}

	if pid != os.Getpid() {
		t.Errorf("PID file contains %d, want %d", pid, os.Getpid())
	}

	// Cleanup
	os.RemoveAll(filepath.Dir(pidFile))
}

func TestCleanup(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "gale.pid")

	// Create PID file
	if err := os.WriteFile(pidFile, []byte("12345"), 0644); err != nil {
		t.Fatalf("Failed to write PID file: %v", err)
	}

	d := New(pidFile)
	d.cleanup()

	// Verify PID file was removed
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Error("PID file was not removed")
	}
}

func TestStop_NotRunning(t *testing.T) {
	d := New("/tmp/nonexistent-gale-test.pid")
	err := d.Stop()
	if err == nil {
		t.Error("Stop() error = nil, want error when not running")
	}
}

func TestStart_AlreadyRunning(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "gale.pid")

	// Write current process PID to simulate already running
	currentPID := os.Getpid()
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(currentPID)), 0644); err != nil {
		t.Fatalf("Failed to write PID file: %v", err)
	}

	d := New(pidFile)
	_, err := d.Start(context.Background(), func(ctx context.Context) error {
		return nil
	})
	if err == nil {
		t.Error("Start() error = nil, want error when already running")
	}

	// Cleanup
	os.Remove(pidFile)
}

func TestStart_Success(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "gale.pid")

	d := New(pidFile)
	started := make(chan bool, 1)

	cancel, err := d.Start(context.Background(), func(ctx context.Context) error {
		started <- true
		<-ctx.Done()
		return nil
	})
	if err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}

	// Wait for function to start
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("Function did not start within timeout")
	}

	// Verify PID file was created
	if _, err := os.Stat(pidFile); os.IsNotExist(err) {
		t.Error("PID file was not created")
	}

	// Cancel and cleanup
	cancel()
	time.Sleep(100 * time.Millisecond) // Give cleanup goroutine time to run
}

func TestDefaultPidFile(t *testing.T) {
	pidFile := DefaultPidFile()
	homeDir, err := os.UserHomeDir()
	if err != nil {
		if pidFile != "/tmp/gale.pid" {
			t.Errorf("DefaultPidFile() = %q, want %q when no home dir", pidFile, "/tmp/gale.pid")
		}
		return
	}
	expected := filepath.Join(homeDir, ".gale", "gale.pid")
	if pidFile != expected {
		t.Errorf("DefaultPidFile() = %q, want %q", pidFile, expected)
	}
}
