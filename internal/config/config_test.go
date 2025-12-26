package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	content := `
github:
  token: test-token
  owner: test-owner
  repos:
    - repo1
    - repo2
scaler:
  max_runners: 5
  min_runners: 1
runner:
  image: test-image:latest
  labels:
    - test
    - label
log_level: debug
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verify values
	if cfg.GitHub.Token != "test-token" {
		t.Errorf("GitHub.Token = %q, want %q", cfg.GitHub.Token, "test-token")
	}
	if cfg.GitHub.Owner != "test-owner" {
		t.Errorf("GitHub.Owner = %q, want %q", cfg.GitHub.Owner, "test-owner")
	}
	if len(cfg.GitHub.Repos) != 2 {
		t.Errorf("GitHub.Repos len = %d, want 2", len(cfg.GitHub.Repos))
	}
	if cfg.Scaler.MaxRunners != 5 {
		t.Errorf("Scaler.MaxRunners = %d, want 5", cfg.Scaler.MaxRunners)
	}
	if cfg.Scaler.MinRunners != 1 {
		t.Errorf("Scaler.MinRunners = %d, want 1", cfg.Scaler.MinRunners)
	}
	if cfg.Runner.Image != "test-image:latest" {
		t.Errorf("Runner.Image = %q, want %q", cfg.Runner.Image, "test-image:latest")
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
}

func TestLoadWithEnvExpansion(t *testing.T) {
	os.Setenv("TEST_TOKEN", "expanded-token")
	defer os.Unsetenv("TEST_TOKEN")

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	content := `
github:
  token: ${TEST_TOKEN}
  owner: test-owner
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.GitHub.Token != "expanded-token" {
		t.Errorf("GitHub.Token = %q, want %q", cfg.GitHub.Token, "expanded-token")
	}
}

func TestSetDefaults(t *testing.T) {
	cfg := &Config{}
	cfg.setDefaults()

	if cfg.Docker.Host != "unix:///var/run/docker.sock" {
		t.Errorf("Docker.Host = %q, want default", cfg.Docker.Host)
	}
	if cfg.Scaler.MaxRunners != 10 {
		t.Errorf("Scaler.MaxRunners = %d, want 10", cfg.Scaler.MaxRunners)
	}
	if cfg.Webhook.Port != 8080 {
		t.Errorf("Webhook.Port = %d, want 8080", cfg.Webhook.Port)
	}
	if cfg.Runner.Image != "myoung34/github-runner:latest" {
		t.Errorf("Runner.Image = %q, want default", cfg.Runner.Image)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", cfg.LogLevel)
	}
	if cfg.GitHub.Scope != "org" {
		t.Errorf("GitHub.Scope = %q, want org", cfg.GitHub.Scope)
	}
}

func TestSetDefaultsWithSingleRepo(t *testing.T) {
	cfg := &Config{
		GitHub: GitHubConfig{
			Repo: "single-repo",
		},
	}
	cfg.setDefaults()

	// Should migrate single repo to repos list
	if len(cfg.GitHub.Repos) != 1 || cfg.GitHub.Repos[0] != "single-repo" {
		t.Errorf("GitHub.Repos = %v, want [single-repo]", cfg.GitHub.Repos)
	}
	if cfg.GitHub.Scope != "repo" {
		t.Errorf("GitHub.Scope = %q, want repo", cfg.GitHub.Scope)
	}
}

func TestSetDefaultsWithMultipleRepos(t *testing.T) {
	cfg := &Config{
		GitHub: GitHubConfig{
			Repos: []string{"repo1", "repo2"},
		},
	}
	cfg.setDefaults()

	if cfg.GitHub.Scope != "repos" {
		t.Errorf("GitHub.Scope = %q, want repos", cfg.GitHub.Scope)
	}
}

func TestIsRepoMonitored(t *testing.T) {
	tests := []struct {
		name     string
		repos    []string
		check    string
		expected bool
	}{
		{
			name:     "empty repos monitors all",
			repos:    []string{},
			check:    "any-repo",
			expected: true,
		},
		{
			name:     "repo in list",
			repos:    []string{"repo1", "repo2"},
			check:    "repo1",
			expected: true,
		},
		{
			name:     "repo not in list",
			repos:    []string{"repo1", "repo2"},
			check:    "repo3",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				GitHub: GitHubConfig{
					Repos: tt.repos,
				},
			}
			if got := cfg.IsRepoMonitored(tt.check); got != tt.expected {
				t.Errorf("IsRepoMonitored(%q) = %v, want %v", tt.check, got, tt.expected)
			}
		})
	}
}

func TestAddRepo(t *testing.T) {
	cfg := &Config{}

	cfg.AddRepo("repo1")
	if len(cfg.GitHub.Repos) != 1 || cfg.GitHub.Repos[0] != "repo1" {
		t.Errorf("AddRepo() repos = %v, want [repo1]", cfg.GitHub.Repos)
	}

	// Adding same repo should not duplicate
	cfg.AddRepo("repo1")
	if len(cfg.GitHub.Repos) != 1 {
		t.Errorf("AddRepo() duplicate, repos = %v, want [repo1]", cfg.GitHub.Repos)
	}

	cfg.AddRepo("repo2")
	if len(cfg.GitHub.Repos) != 2 {
		t.Errorf("AddRepo() repos = %v, want [repo1 repo2]", cfg.GitHub.Repos)
	}
}

func TestRemoveRepo(t *testing.T) {
	cfg := &Config{
		GitHub: GitHubConfig{
			Repos: []string{"repo1", "repo2", "repo3"},
		},
	}

	if !cfg.RemoveRepo("repo2") {
		t.Error("RemoveRepo(repo2) = false, want true")
	}
	if len(cfg.GitHub.Repos) != 2 {
		t.Errorf("After RemoveRepo(), repos = %v, want [repo1 repo3]", cfg.GitHub.Repos)
	}

	if cfg.RemoveRepo("nonexistent") {
		t.Error("RemoveRepo(nonexistent) = true, want false")
	}
}

func TestHasSpecificRepos(t *testing.T) {
	tests := []struct {
		name     string
		repos    []string
		expected bool
	}{
		{
			name:     "empty repos",
			repos:    []string{},
			expected: false,
		},
		{
			name:     "has repos",
			repos:    []string{"repo1"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				GitHub: GitHubConfig{
					Repos: tt.repos,
				},
			}
			if got := cfg.HasSpecificRepos(); got != tt.expected {
				t.Errorf("HasSpecificRepos() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestIsAppMode(t *testing.T) {
	tests := []struct {
		name     string
		appID    int64
		expected bool
	}{
		{
			name:     "no app id",
			appID:    0,
			expected: false,
		},
		{
			name:     "with app id",
			appID:    12345,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				GitHub: GitHubConfig{
					App: GitHubAppConfig{
						AppID: tt.appID,
					},
				},
			}
			if got := cfg.IsAppMode(); got != tt.expected {
				t.Errorf("IsAppMode() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSave(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg := &Config{
		GitHub: GitHubConfig{
			Owner: "test-owner",
			Repos: []string{"repo1", "repo2"},
		},
		Scaler: ScalerConfig{
			MaxRunners: 5,
		},
	}

	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Load it back
	loaded, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() after Save() error = %v", err)
	}

	if loaded.GitHub.Owner != "test-owner" {
		t.Errorf("Loaded Owner = %q, want test-owner", loaded.GitHub.Owner)
	}
	if len(loaded.GitHub.Repos) != 2 {
		t.Errorf("Loaded Repos len = %d, want 2", len(loaded.GitHub.Repos))
	}
	if loaded.Scaler.MaxRunners != 5 {
		t.Errorf("Loaded MaxRunners = %d, want 5", loaded.Scaler.MaxRunners)
	}
}

func TestIsOrgScope(t *testing.T) {
	tests := []struct {
		name     string
		scope    string
		expected bool
	}{
		{"org scope", "org", true},
		{"repo scope", "repo", false},
		{"repos scope", "repos", false},
		{"empty scope", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				GitHub: GitHubConfig{
					Scope: tt.scope,
				},
			}
			if got := cfg.IsOrgScope(); got != tt.expected {
				t.Errorf("IsOrgScope() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGetRepos(t *testing.T) {
	tests := []struct {
		name     string
		repos    []string
		repo     string
		expected []string
	}{
		{
			name:     "repos list",
			repos:    []string{"repo1", "repo2"},
			expected: []string{"repo1", "repo2"},
		},
		{
			name:     "empty repos list",
			repos:    []string{},
			expected: []string{},
		},
		{
			name:     "nil repos list",
			repos:    nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				GitHub: GitHubConfig{
					Repos: tt.repos,
					Repo:  tt.repo,
				},
			}
			got := cfg.GetRepos()
			if len(got) != len(tt.expected) {
				t.Errorf("GetRepos() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGetWebhookSecret(t *testing.T) {
	tests := []struct {
		name      string
		secret    string
		appSecret string
		expected  string
	}{
		{
			name:     "webhook secret",
			secret:   "webhook-secret",
			expected: "webhook-secret",
		},
		{
			name:      "app webhook secret",
			appSecret: "app-secret",
			expected:  "app-secret",
		},
		{
			name:     "no secret",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Webhook: WebhookConfig{
					Secret: tt.secret,
				},
				GitHub: GitHubConfig{
					App: GitHubAppConfig{
						WebhookSecret: tt.appSecret,
					},
				},
			}
			if got := cfg.GetWebhookSecret(); got != tt.expected {
				t.Errorf("GetWebhookSecret() = %q, want %q", got, tt.expected)
			}
		})
	}

	// Test env var expansion in app secret
	t.Run("app secret with env expansion", func(t *testing.T) {
		os.Setenv("TEST_WEBHOOK_SECRET", "expanded-secret")
		defer os.Unsetenv("TEST_WEBHOOK_SECRET")

		cfg := &Config{
			GitHub: GitHubConfig{
				App: GitHubAppConfig{
					WebhookSecret: "${TEST_WEBHOOK_SECRET}",
				},
			},
		}
		if got := cfg.GetWebhookSecret(); got != "expanded-secret" {
			t.Errorf("GetWebhookSecret() = %q, want expanded-secret", got)
		}
	})
}

func TestGetPrivateKey(t *testing.T) {
	t.Run("from inline", func(t *testing.T) {
		cfg := &Config{
			GitHub: GitHubConfig{
				App: GitHubAppConfig{
					PrivateKey: "inline-key",
				},
			},
		}
		key, err := cfg.GetPrivateKey()
		if err != nil {
			t.Fatalf("GetPrivateKey() error = %v", err)
		}
		if string(key) != "inline-key" {
			t.Errorf("GetPrivateKey() = %q, want inline-key", string(key))
		}
	})

	t.Run("from env var", func(t *testing.T) {
		os.Setenv("GALE_PRIVATE_KEY", "env-key")
		defer os.Unsetenv("GALE_PRIVATE_KEY")

		cfg := &Config{}
		key, err := cfg.GetPrivateKey()
		if err != nil {
			t.Fatalf("GetPrivateKey() error = %v", err)
		}
		if string(key) != "env-key" {
			t.Errorf("GetPrivateKey() = %q, want env-key", string(key))
		}
	})

	t.Run("from file", func(t *testing.T) {
		tmpDir := t.TempDir()
		keyPath := filepath.Join(tmpDir, "key.pem")
		if err := os.WriteFile(keyPath, []byte("file-key"), 0644); err != nil {
			t.Fatalf("failed to write key file: %v", err)
		}

		cfg := &Config{
			GitHub: GitHubConfig{
				App: GitHubAppConfig{
					PrivateKeyPath: keyPath,
				},
			},
		}
		key, err := cfg.GetPrivateKey()
		if err != nil {
			t.Fatalf("GetPrivateKey() error = %v", err)
		}
		if string(key) != "file-key" {
			t.Errorf("GetPrivateKey() = %q, want file-key", string(key))
		}
	})

	t.Run("not configured", func(t *testing.T) {
		cfg := &Config{}
		_, err := cfg.GetPrivateKey()
		if err == nil {
			t.Error("GetPrivateKey() expected error, got nil")
		}
	})
}
