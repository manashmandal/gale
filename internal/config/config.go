package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	GitHub   GitHubConfig  `yaml:"github"`
	Docker   DockerConfig  `yaml:"docker"`
	Scaler   ScalerConfig  `yaml:"scaler"`
	Runner   RunnerConfig  `yaml:"runner"`
	Webhook  WebhookConfig `yaml:"webhook"`
	LogLevel string        `yaml:"log_level"`
}

type WebhookConfig struct {
	Port   int    `yaml:"port"`   // Port to listen on (default 8080)
	Secret string `yaml:"secret"` // Webhook secret for signature verification
}

type GitHubConfig struct {
	Token string   `yaml:"token"`
	Owner string   `yaml:"owner"`
	Repo  string   `yaml:"repo"`  // Deprecated: use repos instead
	Repos []string `yaml:"repos"` // List of specific repos to monitor (empty = all repos)
	Scope string   `yaml:"scope"` // "org", "repo", or "repos" (auto-detected)

	// GitHub App configuration (alternative to PAT)
	App GitHubAppConfig `yaml:"app"`
}

type GitHubAppConfig struct {
	AppID          int64  `yaml:"app_id"`
	PrivateKeyPath string `yaml:"private_key_path"` // Path to .pem file
	PrivateKey     string `yaml:"private_key"`      // Or inline PEM content
	WebhookSecret  string `yaml:"webhook_secret"`
}

type DockerConfig struct {
	Host       string `yaml:"host"`
	APIVersion string `yaml:"api_version"`
}

type ScalerConfig struct {
	MinRunners         int           `yaml:"min_runners"`
	MaxRunners         int           `yaml:"max_runners"`
	PollInterval       time.Duration `yaml:"poll_interval"`
	ScaleUpDelay       time.Duration `yaml:"scale_up_delay"`
	RateLimitThreshold int           `yaml:"rate_limit_threshold"` // Stop at this many API calls (default 2500)
}

type RunnerConfig struct {
	Image       string            `yaml:"image"`
	Labels      []string          `yaml:"labels"`
	Env         map[string]string `yaml:"env"`
	NetworkMode string            `yaml:"network_mode"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	// Expand environment variables
	expanded := os.ExpandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	cfg.setDefaults()
	return &cfg, nil
}

// Save writes the config to the specified file path
func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	header := `# Gale Configuration
# Documentation: https://github.com/manashmandal/gale

`
	if err := os.WriteFile(path, []byte(header+string(data)), 0644); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}
	return nil
}

func (c *Config) setDefaults() {
	if c.Docker.Host == "" {
		c.Docker.Host = "unix:///var/run/docker.sock"
	}
	if c.Scaler.MaxRunners == 0 {
		c.Scaler.MaxRunners = 10
	}
	if c.Scaler.PollInterval == 0 {
		c.Scaler.PollInterval = 10 * time.Second
	}
	if c.Scaler.RateLimitThreshold == 0 {
		c.Scaler.RateLimitThreshold = 2500 // Default: stop at 2500 of 5000 calls
	}
	if c.Webhook.Port == 0 {
		c.Webhook.Port = 8080
	}
	if c.Runner.Image == "" {
		c.Runner.Image = "myoung34/github-runner:latest"
	}
	if len(c.Runner.Labels) == 0 {
		c.Runner.Labels = []string{"gale", "self-hosted", "linux", "x64"}
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
	// Migrate single repo to repos list for backward compatibility
	if c.GitHub.Repo != "" && len(c.GitHub.Repos) == 0 {
		c.GitHub.Repos = []string{c.GitHub.Repo}
	}

	// Default scope based on repos configuration
	if c.GitHub.Scope == "" {
		if len(c.GitHub.Repos) == 0 {
			c.GitHub.Scope = "org"
		} else if len(c.GitHub.Repos) == 1 {
			c.GitHub.Scope = "repo"
		} else {
			c.GitHub.Scope = "repos"
		}
	}
}

func (c *Config) IsOrgScope() bool {
	return c.GitHub.Scope == "org"
}

// HasSpecificRepos returns true if monitoring specific repos (not all)
func (c *Config) HasSpecificRepos() bool {
	return len(c.GitHub.Repos) > 0
}

// IsRepoMonitored checks if a given repo should be monitored
func (c *Config) IsRepoMonitored(repo string) bool {
	// If no specific repos configured, monitor all
	if len(c.GitHub.Repos) == 0 {
		return true
	}
	// Check if repo is in the list
	for _, r := range c.GitHub.Repos {
		if r == repo {
			return true
		}
	}
	return false
}

// GetRepos returns the list of repos to monitor
func (c *Config) GetRepos() []string {
	return c.GitHub.Repos
}

// AddRepo adds a repository to the monitored list
func (c *Config) AddRepo(repo string) {
	// Check if already exists
	for _, r := range c.GitHub.Repos {
		if r == repo {
			return
		}
	}
	c.GitHub.Repos = append(c.GitHub.Repos, repo)
}

// RemoveRepo removes a repository from the monitored list
func (c *Config) RemoveRepo(repo string) bool {
	for i, r := range c.GitHub.Repos {
		if r == repo {
			c.GitHub.Repos = append(c.GitHub.Repos[:i], c.GitHub.Repos[i+1:]...)
			return true
		}
	}
	return false
}

// IsAppMode returns true if using GitHub App authentication
func (c *Config) IsAppMode() bool {
	return c.GitHub.App.AppID > 0
}

// GetPrivateKey returns the private key PEM data from file, env var, or inline config
func (c *Config) GetPrivateKey() ([]byte, error) {
	// Try inline first
	if c.GitHub.App.PrivateKey != "" {
		return []byte(os.ExpandEnv(c.GitHub.App.PrivateKey)), nil
	}

	// Try environment variable
	if envKey := os.Getenv("GALE_PRIVATE_KEY"); envKey != "" {
		return []byte(envKey), nil
	}

	// Try file path
	if c.GitHub.App.PrivateKeyPath != "" {
		path := os.ExpandEnv(c.GitHub.App.PrivateKeyPath)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading private key file %s: %w", path, err)
		}
		return data, nil
	}

	return nil, fmt.Errorf("no private key configured (set private_key_path, private_key, or GALE_PRIVATE_KEY env var)")
}

// GetWebhookSecret returns the webhook secret, preferring app config over webhook config
func (c *Config) GetWebhookSecret() string {
	if c.GitHub.App.WebhookSecret != "" {
		return os.ExpandEnv(c.GitHub.App.WebhookSecret)
	}
	return c.Webhook.Secret
}
