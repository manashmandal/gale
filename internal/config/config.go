package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	GitHub   GitHubConfig   `yaml:"github"`
	Docker   DockerConfig   `yaml:"docker"`
	Scaler   ScalerConfig   `yaml:"scaler"`
	Runner   RunnerConfig   `yaml:"runner"`
	Webhook  WebhookConfig  `yaml:"webhook"`
	LogLevel string         `yaml:"log_level"`
}

type WebhookConfig struct {
	Port   int    `yaml:"port"`   // Port to listen on (default 8080)
	Secret string `yaml:"secret"` // Webhook secret for signature verification
}

type GitHubConfig struct {
	Token string `yaml:"token"`
	Owner string `yaml:"owner"`
	Repo  string `yaml:"repo"`  // Optional: if empty, monitors all repos
	Scope string `yaml:"scope"` // "org" or "repo" (default: org if repo is empty)
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
	// Default to org scope if no specific repo is set
	if c.GitHub.Scope == "" {
		if c.GitHub.Repo == "" {
			c.GitHub.Scope = "org"
		} else {
			c.GitHub.Scope = "repo"
		}
	}
}

func (c *Config) IsOrgScope() bool {
	return c.GitHub.Scope == "org"
}
