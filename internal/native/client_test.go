package native

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

	// ExitedAt must be older than cleanupGracePeriod (2 minutes) to be cleaned up
	oldExitTime := time.Now().Add(-3 * time.Minute)
	client.runners["r1"] = &Runner{ID: "r1", Dir: r1Dir, Status: "exited", ExitedAt: oldExitTime}
	client.runners["r2"] = &Runner{ID: "r2", Dir: r2Dir, Status: "exited", ExitedAt: oldExitTime}
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

func TestClient_CleanupExitedRunners_GracePeriod(t *testing.T) {
	tmpDir := t.TempDir()
	client, _ := NewClient(tmpDir)

	r1Dir := filepath.Join(tmpDir, "work", "r1")
	os.MkdirAll(r1Dir, 0755)

	// Recent exit - should NOT be cleaned up (within grace period)
	recentExitTime := time.Now().Add(-30 * time.Second)
	client.runners["r1"] = &Runner{ID: "r1", Dir: r1Dir, Status: "exited", ExitedAt: recentExitTime}

	count, err := client.CleanupExitedRunners(context.Background())
	if err != nil {
		t.Fatalf("CleanupExitedRunners() error = %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 (runner should not be cleaned up within grace period)", count)
	}

	if len(client.runners) != 1 {
		t.Errorf("len(runners) = %d, want 1 (runner should remain)", len(client.runners))
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

func TestGetRegistrationToken_InvalidURL(t *testing.T) {
	_, err := getRegistrationToken(context.Background(), "token", "invalid-url", "", "repo")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestGetRegistrationToken_ExtractsOwnerRepo(t *testing.T) {
	// This will fail with auth error but tests URL parsing
	_, err := getRegistrationToken(context.Background(), "invalid-token", "https://github.com/owner/repo", "", "repo")
	if err == nil {
		t.Error("expected error with invalid token")
	}
	// Should not error on URL parsing, only on API call
	if strings.Contains(err.Error(), "invalid repo URL") {
		t.Errorf("URL parsing should succeed, got: %v", err)
	}
}

func TestProcessGroupAlive(t *testing.T) {
	// Test with invalid PGID
	alive, err := processGroupAlive(0)
	if alive || err != nil {
		t.Errorf("processGroupAlive(0) = %v, %v; want false, nil", alive, err)
	}

	alive, err = processGroupAlive(-1)
	if alive || err != nil {
		t.Errorf("processGroupAlive(-1) = %v, %v; want false, nil", alive, err)
	}
}

func TestProcessAlive(t *testing.T) {
	// Test with invalid PID
	alive, err := processAlive(0)
	if alive || err != nil {
		t.Errorf("processAlive(0) = %v, %v; want false, nil", alive, err)
	}

	alive, err = processAlive(-1)
	if alive || err != nil {
		t.Errorf("processAlive(-1) = %v, %v; want false, nil", alive, err)
	}

	// Test with current process (should be alive)
	alive, err = processAlive(os.Getpid())
	if !alive {
		t.Errorf("processAlive(current) = %v, %v; want true, nil", alive, err)
	}
}

func TestReadLocalRunnerID(t *testing.T) {
	tmpDir := t.TempDir()

	// Test with valid file
	validPath := filepath.Join(tmpDir, ".runner")
	validContent := `{"agentId": 12345, "agentName": "test-runner"}`
	if err := os.WriteFile(validPath, []byte(validContent), 0644); err != nil {
		t.Fatal(err)
	}

	id, err := readLocalRunnerID(validPath)
	if err != nil {
		t.Errorf("readLocalRunnerID() error = %v", err)
	}
	if id != 12345 {
		t.Errorf("readLocalRunnerID() = %d, want 12345", id)
	}

	// Test with missing file
	_, err = readLocalRunnerID("/nonexistent/file")
	if err == nil {
		t.Error("readLocalRunnerID() expected error for missing file")
	}

	// Test with invalid JSON
	invalidPath := filepath.Join(tmpDir, ".runner-invalid")
	if err := os.WriteFile(invalidPath, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err = readLocalRunnerID(invalidPath)
	if err == nil {
		t.Error("readLocalRunnerID() expected error for invalid JSON")
	}

	// Test with missing agentId
	noIdPath := filepath.Join(tmpDir, ".runner-noid")
	if err := os.WriteFile(noIdPath, []byte(`{"agentName": "test"}`), 0644); err != nil {
		t.Fatal(err)
	}
	_, err = readLocalRunnerID(noIdPath)
	if err == nil {
		t.Error("readLocalRunnerID() expected error for missing agentId")
	}
}

func TestParseRepoOwnerRepo(t *testing.T) {
	tests := []struct {
		url       string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{"https://github.com/owner/repo", "owner", "repo", false},
		{"https://github.com/owner/repo/", "owner", "repo", false},
		{"https://github.com/owner/repo.git", "owner", "repo", false},
		{"invalid", "", "", true},
		{"https://github.com/single", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			owner, repo, err := parseRepoOwnerRepo(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseRepoOwnerRepo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if owner != tt.wantOwner {
				t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}

func TestGetProcessesByCwd(t *testing.T) {
	// Test with non-existent directory
	pids := getProcessesByCwd("/nonexistent/directory/path")
	if len(pids) != 0 {
		t.Errorf("getProcessesByCwd() for nonexistent dir = %v, want empty", pids)
	}
}

func TestGetProcessesViaLsof(t *testing.T) {
	// Test with non-existent directory
	pids := getProcessesViaLsof("/nonexistent/directory/path")
	if len(pids) != 0 {
		t.Errorf("getProcessesViaLsof() for nonexistent dir = %v, want empty", pids)
	}
}

func TestHasRecentTempScripts(t *testing.T) {
	tmpDir := t.TempDir()

	// Test with non-existent directory
	result := hasRecentTempScripts("/nonexistent/directory")
	if result {
		t.Error("hasRecentTempScripts() for nonexistent dir = true, want false")
	}

	// Test with empty temp directory
	tempDir := filepath.Join(tmpDir, "_work", "_temp")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatal(err)
	}
	result = hasRecentTempScripts(tmpDir)
	if result {
		t.Error("hasRecentTempScripts() for empty dir = true, want false")
	}

	// Test with old script (should return false)
	oldScript := filepath.Join(tempDir, "old.sh")
	if err := os.WriteFile(oldScript, []byte("#!/bin/bash"), 0755); err != nil {
		t.Fatal(err)
	}
	// Set modification time to 5 minutes ago
	oldTime := time.Now().Add(-5 * time.Minute)
	os.Chtimes(oldScript, oldTime, oldTime)

	result = hasRecentTempScripts(tmpDir)
	if result {
		t.Error("hasRecentTempScripts() for old script = true, want false")
	}
}

func TestDownloadFile_InvalidURL(t *testing.T) {
	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "test.file")

	err := downloadFile(context.Background(), "http://invalid.localhost.test/file", destPath)
	if err == nil {
		t.Error("downloadFile() expected error for invalid URL")
	}
}

func TestClient_CleanupExitedRunners_ZeroExitTime(t *testing.T) {
	tmpDir := t.TempDir()
	client, _ := NewClient(tmpDir)

	// Runner with zero exit time should NOT be cleaned up
	client.runners["r1"] = &Runner{ID: "r1", Dir: "", Status: "exited"}

	count, err := client.CleanupExitedRunners(context.Background())
	if err != nil {
		t.Fatalf("CleanupExitedRunners() error = %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 (runner with zero exit time should not be cleaned)", count)
	}
}

func TestRunner_AllFields(t *testing.T) {
	now := time.Now()
	exitTime := now.Add(time.Minute)

	r := Runner{
		ID:              "test-id",
		Name:            "test-name",
		PID:             1234,
		PGID:            5678,
		Dir:             "/test/dir",
		Status:          "running",
		Repo:            "https://github.com/owner/repo",
		Scope:           "repo",
		OrgName:         "myorg",
		GitHubToken:     "token",
		GitHubRunnerID:  99999,
		GitHubEphemeral: true,
		StartedAt:       now,
		ExitedAt:        exitTime,
	}

	if r.Name != "test-name" {
		t.Errorf("Name = %q, want test-name", r.Name)
	}
	if r.PGID != 5678 {
		t.Errorf("PGID = %d, want 5678", r.PGID)
	}
	if r.Scope != "repo" {
		t.Errorf("Scope = %q, want repo", r.Scope)
	}
	if r.OrgName != "myorg" {
		t.Errorf("OrgName = %q, want myorg", r.OrgName)
	}
	if r.GitHubToken != "token" {
		t.Errorf("GitHubToken = %q, want token", r.GitHubToken)
	}
	if r.GitHubRunnerID != 99999 {
		t.Errorf("GitHubRunnerID = %d, want 99999", r.GitHubRunnerID)
	}
	if !r.GitHubEphemeral {
		t.Error("GitHubEphemeral = false, want true")
	}
	if !r.ExitedAt.Equal(exitTime) {
		t.Errorf("ExitedAt = %v, want %v", r.ExitedAt, exitTime)
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
