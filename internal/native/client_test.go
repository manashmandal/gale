package native

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestRunner_Struct(t *testing.T) {
	now := time.Now()
	r := Runner{
		ID:        "runner-123",
		PID:       12345,
		Dir:       "/tmp/runner",
		Status:    "running",
		Repo:      "https://github.com/owner/repo",
		StartedAt: now,
	}

	if r.ID != "runner-123" {
		t.Errorf("ID = %q, want %q", r.ID, "runner-123")
	}
	if r.PID != 12345 {
		t.Errorf("PID = %d, want %d", r.PID, 12345)
	}
	if r.Dir != "/tmp/runner" {
		t.Errorf("Dir = %q, want %q", r.Dir, "/tmp/runner")
	}
	if r.Status != "running" {
		t.Errorf("Status = %q, want %q", r.Status, "running")
	}
	if r.Repo != "https://github.com/owner/repo" {
		t.Errorf("Repo = %q, want %q", r.Repo, "https://github.com/owner/repo")
	}
	if !r.StartedAt.Equal(now) {
		t.Errorf("StartedAt = %v, want %v", r.StartedAt, now)
	}
}

func TestRunnerConfig_Struct(t *testing.T) {
	cfg := RunnerConfig{
		Token:      "test-token",
		RepoURL:    "https://github.com/owner/repo",
		OrgName:    "myorg",
		Scope:      "repo",
		Labels:     []string{"self-hosted", "linux"},
		Env:        map[string]string{"KEY": "value"},
		RunnerName: "my-runner",
	}

	if cfg.Token != "test-token" {
		t.Errorf("Token = %q, want %q", cfg.Token, "test-token")
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
	if cfg.Env["KEY"] != "value" {
		t.Errorf("Env[KEY] = %q, want %q", cfg.Env["KEY"], "value")
	}
	if cfg.RunnerName != "my-runner" {
		t.Errorf("RunnerName = %q, want %q", cfg.RunnerName, "my-runner")
	}
}

func TestNewClient(t *testing.T) {
	tmpDir := t.TempDir()

	client, err := NewClient(tmpDir)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewClient() returned nil")
	}
	if client.baseDir != tmpDir {
		t.Errorf("baseDir = %q, want %q", client.baseDir, tmpDir)
	}
	if client.runners == nil {
		t.Error("runners map is nil")
	}
}

func TestNewClient_DefaultDir(t *testing.T) {
	client, err := NewClient("")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewClient() returned nil")
	}

	homeDir, _ := os.UserHomeDir()
	expectedDir := filepath.Join(homeDir, ".gale", "native-runners")
	if client.baseDir != expectedDir {
		t.Errorf("baseDir = %q, want %q", client.baseDir, expectedDir)
	}

	// Cleanup
	os.RemoveAll(expectedDir)
}

func TestClient_Close(t *testing.T) {
	tmpDir := t.TempDir()
	client, _ := NewClient(tmpDir)

	err := client.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestClient_ListRunners_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	client, _ := NewClient(tmpDir)

	runners, err := client.ListRunners(context.Background())
	if err != nil {
		t.Fatalf("ListRunners() error = %v", err)
	}
	if len(runners) != 0 {
		t.Errorf("len(runners) = %d, want 0", len(runners))
	}
}

func TestClient_ListRunners_WithRunners(t *testing.T) {
	tmpDir := t.TempDir()
	client, _ := NewClient(tmpDir)

	client.runners["r1"] = &Runner{ID: "r1", Status: "running"}
	client.runners["r2"] = &Runner{ID: "r2", Status: "exited"}

	runners, err := client.ListRunners(context.Background())
	if err != nil {
		t.Fatalf("ListRunners() error = %v", err)
	}
	if len(runners) != 2 {
		t.Errorf("len(runners) = %d, want 2", len(runners))
	}
}

func TestClient_GetActiveRunnerCount(t *testing.T) {
	tmpDir := t.TempDir()
	client, _ := NewClient(tmpDir)

	client.runners["r1"] = &Runner{ID: "r1", Status: "running"}
	client.runners["r2"] = &Runner{ID: "r2", Status: "running"}
	client.runners["r3"] = &Runner{ID: "r3", Status: "exited"}

	count, err := client.GetActiveRunnerCount(context.Background())
	if err != nil {
		t.Fatalf("GetActiveRunnerCount() error = %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestClient_RemoveRunner_NotExists(t *testing.T) {
	tmpDir := t.TempDir()
	client, _ := NewClient(tmpDir)

	err := client.RemoveRunner(context.Background(), "nonexistent")
	if err != nil {
		t.Errorf("RemoveRunner() error = %v", err)
	}
}

func TestClient_RemoveRunner_Exists(t *testing.T) {
	tmpDir := t.TempDir()
	client, _ := NewClient(tmpDir)

	runnerDir := filepath.Join(tmpDir, "work", "r1")
	os.MkdirAll(runnerDir, 0755)

	client.runners["r1"] = &Runner{ID: "r1", Dir: runnerDir, Status: "exited"}

	err := client.RemoveRunner(context.Background(), "r1")
	if err != nil {
		t.Errorf("RemoveRunner() error = %v", err)
	}

	if _, exists := client.runners["r1"]; exists {
		t.Error("runner still exists in map after removal")
	}

	if _, err := os.Stat(runnerDir); !os.IsNotExist(err) {
		t.Error("runner directory still exists after removal")
	}
}

func TestClient_CleanupExitedRunners(t *testing.T) {
	tmpDir := t.TempDir()
	client, _ := NewClient(tmpDir)

	r1Dir := filepath.Join(tmpDir, "work", "r1")
	r2Dir := filepath.Join(tmpDir, "work", "r2")
	os.MkdirAll(r1Dir, 0755)
	os.MkdirAll(r2Dir, 0755)

	client.runners["r1"] = &Runner{ID: "r1", Dir: r1Dir, Status: "exited"}
	client.runners["r2"] = &Runner{ID: "r2", Dir: r2Dir, Status: "exited"}
	client.runners["r3"] = &Runner{ID: "r3", Dir: "", Status: "running"}

	count, err := client.CleanupExitedRunners(context.Background())
	if err != nil {
		t.Fatalf("CleanupExitedRunners() error = %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}

	if len(client.runners) != 1 {
		t.Errorf("len(runners) = %d, want 1", len(client.runners))
	}
}

func TestClient_IsRunnerExited_NotExists(t *testing.T) {
	tmpDir := t.TempDir()
	client, _ := NewClient(tmpDir)

	exited, err := client.IsRunnerExited(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("IsRunnerExited() error = %v", err)
	}
	if !exited {
		t.Error("exited = false, want true for nonexistent runner")
	}
}

func TestClient_IsRunnerExited_Running(t *testing.T) {
	tmpDir := t.TempDir()
	client, _ := NewClient(tmpDir)

	client.runners["r1"] = &Runner{ID: "r1", Status: "running"}

	exited, err := client.IsRunnerExited(context.Background(), "r1")
	if err != nil {
		t.Fatalf("IsRunnerExited() error = %v", err)
	}
	if exited {
		t.Error("exited = true, want false for running runner")
	}
}

func TestClient_IsRunnerExited_Exited(t *testing.T) {
	tmpDir := t.TempDir()
	client, _ := NewClient(tmpDir)

	client.runners["r1"] = &Runner{ID: "r1", Status: "exited"}

	exited, err := client.IsRunnerExited(context.Background(), "r1")
	if err != nil {
		t.Fatalf("IsRunnerExited() error = %v", err)
	}
	if !exited {
		t.Error("exited = false, want true for exited runner")
	}
}

func TestClient_StopRunner_NotExists(t *testing.T) {
	tmpDir := t.TempDir()
	client, _ := NewClient(tmpDir)

	err := client.StopRunner(context.Background(), "nonexistent", 10)
	if err != nil {
		t.Errorf("StopRunner() error = %v", err)
	}
}

func TestGetRunnerArch(t *testing.T) {
	arch := getRunnerArch()

	switch runtime.GOOS {
	case "darwin":
		if runtime.GOARCH == "arm64" {
			if arch != "osx-arm64" {
				t.Errorf("arch = %q, want osx-arm64", arch)
			}
		} else {
			if arch != "osx-x64" {
				t.Errorf("arch = %q, want osx-x64", arch)
			}
		}
	case "linux":
		if runtime.GOARCH == "amd64" {
			if arch != "linux-x64" {
				t.Errorf("arch = %q, want linux-x64", arch)
			}
		} else if runtime.GOARCH == "arm64" {
			if arch != "linux-arm64" {
				t.Errorf("arch = %q, want linux-arm64", arch)
			}
		}
	}
}

func TestVerifyChecksum_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	content := []byte("test content")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	// SHA256 of "test content"
	expectedHash := "6ae8a75555209fd6c44157c0aed8016e763ff435a19cf186f76863140143ff72"

	err := verifyChecksum(testFile, expectedHash)
	if err != nil {
		t.Errorf("verifyChecksum() error = %v", err)
	}
}

func TestVerifyChecksum_Invalid(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatal(err)
	}

	err := verifyChecksum(testFile, "invalidhash")
	if err == nil {
		t.Error("verifyChecksum() expected error for invalid hash")
	}
}

func TestVerifyChecksum_FileNotFound(t *testing.T) {
	err := verifyChecksum("/nonexistent/file", "somehash")
	if err == nil {
		t.Error("verifyChecksum() expected error for missing file")
	}
}

func TestConstants(t *testing.T) {
	if runnerVersion == "" {
		t.Error("runnerVersion is empty")
	}
	if runnerBaseURL == "" {
		t.Error("runnerBaseURL is empty")
	}
	if LabelManagedBy != "gale-native" {
		t.Errorf("LabelManagedBy = %q, want %q", LabelManagedBy, "gale-native")
	}
}

func TestRunnerChecksums(t *testing.T) {
	if len(runnerChecksums) == 0 {
		t.Error("runnerChecksums is empty")
	}

	for arch, checksum := range runnerChecksums {
		if len(checksum) != 64 {
			t.Errorf("checksum for %s has invalid length %d, want 64", arch, len(checksum))
		}
	}
}

func TestExtractTarGz(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a test tar.gz file
	tarPath := filepath.Join(tmpDir, "test.tar.gz")
	destDir := filepath.Join(tmpDir, "dest")

	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create test tar.gz with a simple file
	createTestTarGz(t, tarPath, map[string]string{
		"test.txt":     "hello world",
		"dir/file.txt": "nested content",
	})

	err := extractTarGz(tarPath, destDir)
	if err != nil {
		t.Fatalf("extractTarGz() error = %v", err)
	}

	// Verify extracted files
	content, err := os.ReadFile(filepath.Join(destDir, "test.txt"))
	if err != nil {
		t.Fatalf("reading extracted file: %v", err)
	}
	if string(content) != "hello world" {
		t.Errorf("content = %q, want %q", string(content), "hello world")
	}

	content, err = os.ReadFile(filepath.Join(destDir, "dir", "file.txt"))
	if err != nil {
		t.Fatalf("reading nested file: %v", err)
	}
	if string(content) != "nested content" {
		t.Errorf("nested content = %q, want %q", string(content), "nested content")
	}
}

func TestExtractTarGz_FileNotFound(t *testing.T) {
	err := extractTarGz("/nonexistent/file.tar.gz", t.TempDir())
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestCopyDir(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")

	// Create source directory structure
	if err := os.MkdirAll(filepath.Join(srcDir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "file1.txt"), []byte("content1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "subdir", "file2.txt"), []byte("content2"), 0644); err != nil {
		t.Fatal(err)
	}

	err := copyDir(srcDir, destDir)
	if err != nil {
		t.Fatalf("copyDir() error = %v", err)
	}

	// Verify copied files
	content, err := os.ReadFile(filepath.Join(destDir, "file1.txt"))
	if err != nil {
		t.Fatalf("reading copied file: %v", err)
	}
	if string(content) != "content1" {
		t.Errorf("content = %q, want %q", string(content), "content1")
	}

	content, err = os.ReadFile(filepath.Join(destDir, "subdir", "file2.txt"))
	if err != nil {
		t.Fatalf("reading nested copied file: %v", err)
	}
	if string(content) != "content2" {
		t.Errorf("nested content = %q, want %q", string(content), "content2")
	}
}

func TestCopyDir_Symlink(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")

	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a file and symlink
	if err := os.WriteFile(filepath.Join(srcDir, "target.txt"), []byte("target"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.txt", filepath.Join(srcDir, "link.txt")); err != nil {
		t.Fatal(err)
	}

	err := copyDir(srcDir, destDir)
	if err != nil {
		t.Fatalf("copyDir() error = %v", err)
	}

	// Verify symlink was copied
	linkDest, err := os.Readlink(filepath.Join(destDir, "link.txt"))
	if err != nil {
		t.Fatalf("reading symlink: %v", err)
	}
	if linkDest != "target.txt" {
		t.Errorf("symlink target = %q, want %q", linkDest, "target.txt")
	}
}

func TestClient_StopRunner_WithRunningProcess(t *testing.T) {
	tmpDir := t.TempDir()
	client, _ := NewClient(tmpDir)

	// Create a mock runner with a nil cmd (simulates a runner without process)
	client.runners["r1"] = &Runner{ID: "r1", Status: "running", cmd: nil}

	err := client.StopRunner(context.Background(), "r1", 1)
	if err != nil {
		t.Errorf("StopRunner() error = %v", err)
	}
}

// Helper to create a test tar.gz file
func createTestTarGz(t *testing.T, path string, files map[string]string) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gzw := gzip.NewWriter(f)
	defer gzw.Close()

	tw := tar.NewWriter(gzw)
	defer tw.Close()

	for name, content := range files {
		// Create directory entries if needed
		dir := filepath.Dir(name)
		if dir != "." {
			hdr := &tar.Header{
				Name:     dir + "/",
				Mode:     0755,
				Typeflag: tar.TypeDir,
			}
			if err := tw.WriteHeader(hdr); err != nil {
				t.Fatal(err)
			}
		}

		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
}
