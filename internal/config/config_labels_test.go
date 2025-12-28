package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfig_Labels(t *testing.T) {
	cfg := &Config{
		Runner: RunnerConfig{
			Labels: []string{"gale", "self-hosted"},
		},
	}

	if len(cfg.Runner.Labels) != 2 {
		t.Errorf("Expected 2 labels, got %d", len(cfg.Runner.Labels))
	}

	if cfg.Runner.Labels[0] != "gale" {
		t.Errorf("Expected first label 'gale', got %s", cfg.Runner.Labels[0])
	}
}

func TestConfig_LabelsDefault(t *testing.T) {
	cfg := &Config{}
	cfg.setDefaults()

	if len(cfg.Runner.Labels) == 0 {
		t.Error("Expected default labels to be set")
	}

	// Check default labels include expected values
	hasGale := false
	hasSelfHosted := false
	for _, label := range cfg.Runner.Labels {
		if label == "gale" {
			hasGale = true
		}
		if label == "self-hosted" {
			hasSelfHosted = true
		}
	}

	if !hasGale {
		t.Error("Expected default labels to include 'gale'")
	}
	if !hasSelfHosted {
		t.Error("Expected default labels to include 'self-hosted'")
	}
}

func TestConfig_LabelsPreserved(t *testing.T) {
	cfg := &Config{
		Runner: RunnerConfig{
			Labels: []string{"custom-label"},
		},
	}
	cfg.setDefaults()

	// Custom labels should be preserved, not overwritten with defaults
	if len(cfg.Runner.Labels) != 1 {
		t.Errorf("Expected 1 label (custom), got %d", len(cfg.Runner.Labels))
	}

	if cfg.Runner.Labels[0] != "custom-label" {
		t.Errorf("Expected 'custom-label', got %s", cfg.Runner.Labels[0])
	}
}

func TestConfig_Version(t *testing.T) {
	cfg := &Config{}
	cfg.setDefaults()

	if cfg.Version != CurrentConfigVersion {
		t.Errorf("Expected version %s, got %s", CurrentConfigVersion, cfg.Version)
	}
}

func TestConfig_Migrate_PreVersioned(t *testing.T) {
	cfg := &Config{
		Version: "",
	}

	err := cfg.migrate()
	if err != nil {
		t.Errorf("migrate() error = %v", err)
	}

	if cfg.Version != CurrentConfigVersion {
		t.Errorf("Expected version %s after migration, got %s", CurrentConfigVersion, cfg.Version)
	}
}

func TestConfig_Migrate_CurrentVersion(t *testing.T) {
	cfg := &Config{
		Version: CurrentConfigVersion,
	}

	err := cfg.migrate()
	if err != nil {
		t.Errorf("migrate() error = %v", err)
	}

	if cfg.Version != CurrentConfigVersion {
		t.Errorf("Expected version %s, got %s", CurrentConfigVersion, cfg.Version)
	}
}

func TestConfig_Migrate_FutureVersion(t *testing.T) {
	cfg := &Config{
		Version: "999",
	}

	err := cfg.migrate()
	if err == nil {
		t.Error("Expected error for future version")
	}
}

func TestConfig_SaveLoad_WithLabels(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create config with custom labels
	cfg := &Config{
		Version: CurrentConfigVersion,
		GitHub: GitHubConfig{
			Token: "test-token",
			Owner: "test-owner",
		},
		Runner: RunnerConfig{
			Labels: []string{"gale-linux", "docker", "gpu"},
		},
	}
	cfg.setDefaults()

	// Save
	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Load
	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verify labels preserved
	if len(loaded.Runner.Labels) != 3 {
		t.Errorf("Expected 3 labels, got %d", len(loaded.Runner.Labels))
	}

	expectedLabels := map[string]bool{"gale-linux": true, "docker": true, "gpu": true}
	for _, label := range loaded.Runner.Labels {
		if !expectedLabels[label] {
			t.Errorf("Unexpected label: %s", label)
		}
	}
}

func TestConfig_SaveLoad_WithVersion(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := &Config{
		Version: CurrentConfigVersion,
		GitHub: GitHubConfig{
			Token: "test-token",
			Owner: "test-owner",
		},
	}
	cfg.setDefaults()

	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify file contains version
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if !contains(string(data), "version:") {
		t.Error("Config file should contain version field")
	}

	// Load and verify
	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.Version != CurrentConfigVersion {
		t.Errorf("Expected version %s, got %s", CurrentConfigVersion, loaded.Version)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
