package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Interactive setup wizard",
	Long: `Initialize Gale with an interactive setup wizard.

This will guide you through:
  - GitHub authentication
  - Repository/Organization selection
  - Runner configuration
  - Scaler settings

Example:
  gale init
  gale init --config /etc/gale/config.yaml`,
	RunE: runInit,
}

var forceInit bool

func init() {
	initCmd.Flags().BoolVarP(&forceInit, "force", "f", false, "overwrite existing config")
	rootCmd.AddCommand(initCmd)
}

type initConfig struct {
	GitHub struct {
		Token string `yaml:"token"`
		Owner string `yaml:"owner"`
		Repo  string `yaml:"repo,omitempty"`
		Scope string `yaml:"scope"`
	} `yaml:"github"`
	Docker struct {
		Host string `yaml:"host,omitempty"`
	} `yaml:"docker"`
	Scaler struct {
		MinRunners   int    `yaml:"min_runners"`
		MaxRunners   int    `yaml:"max_runners"`
		PollInterval string `yaml:"poll_interval"`
		ScaleUpDelay string `yaml:"scale_up_delay"`
	} `yaml:"scaler"`
	Runner struct {
		Image  string   `yaml:"image"`
		Labels []string `yaml:"labels"`
	} `yaml:"runner"`
	LogLevel string `yaml:"log_level"`
}

func runInit(cmd *cobra.Command, args []string) error {
	fmt.Println("🚀 Welcome to Gale Setup!")
	fmt.Println()

	// Check if config exists
	if _, err := os.Stat(cfgFile); err == nil && !forceInit {
		var overwrite bool
		survey.AskOne(&survey.Confirm{
			Message: fmt.Sprintf("Config file %s already exists. Overwrite?", cfgFile),
			Default: false,
		}, &overwrite)
		if !overwrite {
			fmt.Println("Setup cancelled.")
			return nil
		}
	}

	cfg := initConfig{}

	// GitHub Configuration
	fmt.Println("\n📦 GitHub Configuration")
	fmt.Println("------------------------")

	// Token
	tokenHelp := "Create a token at https://github.com/settings/tokens with 'repo' scope"
	var token string
	survey.AskOne(&survey.Password{
		Message: "GitHub Personal Access Token:",
		Help:    tokenHelp,
	}, &token, survey.WithValidator(survey.Required))

	// Check if token should be stored directly or as env var reference
	var tokenStorage string
	survey.AskOne(&survey.Select{
		Message: "How should the token be stored?",
		Options: []string{
			"Environment variable (recommended)",
			"Direct in config file",
		},
		Default: "Environment variable (recommended)",
	}, &tokenStorage)

	if strings.Contains(tokenStorage, "Environment") {
		cfg.GitHub.Token = "${GITHUB_TOKEN}"
		fmt.Printf("\n💡 Set the environment variable: export GITHUB_TOKEN=<your-token>\n")
	} else {
		cfg.GitHub.Token = token
	}

	// Owner
	survey.AskOne(&survey.Input{
		Message: "GitHub username or organization:",
		Help:    "Your GitHub username or the organization name",
	}, &cfg.GitHub.Owner, survey.WithValidator(survey.Required))

	// Scope
	var scope string
	survey.AskOne(&survey.Select{
		Message: "Runner scope:",
		Options: []string{
			"All repositories (org mode)",
			"Single repository",
		},
		Default: "All repositories (org mode)",
		Help:    "Org mode monitors all repos, single repo mode monitors one specific repo",
	}, &scope)

	if strings.Contains(scope, "Single") {
		cfg.GitHub.Scope = "repo"
		survey.AskOne(&survey.Input{
			Message: "Repository name:",
		}, &cfg.GitHub.Repo, survey.WithValidator(survey.Required))
	} else {
		cfg.GitHub.Scope = "org"
	}

	// Scaler Configuration
	fmt.Println("\n⚙️  Scaler Configuration")
	fmt.Println("------------------------")

	survey.AskOne(&survey.Input{
		Message: "Maximum runners:",
		Default: "10",
		Help:    "Maximum number of concurrent runners",
	}, &cfg.Scaler.MaxRunners)

	survey.AskOne(&survey.Input{
		Message: "Minimum runners:",
		Default: "0",
		Help:    "Minimum runners to keep alive (0 = scale to zero)",
	}, &cfg.Scaler.MinRunners)

	survey.AskOne(&survey.Input{
		Message: "Poll interval:",
		Default: "10s",
		Help:    "How often to check for queued jobs",
	}, &cfg.Scaler.PollInterval)

	cfg.Scaler.ScaleUpDelay = "5s"

	// Runner Configuration
	fmt.Println("\n🏃 Runner Configuration")
	fmt.Println("-----------------------")

	survey.AskOne(&survey.Input{
		Message: "Runner image:",
		Default: "myoung34/github-runner:latest",
		Help:    "Docker image for runners",
	}, &cfg.Runner.Image)

	var labelsStr string
	survey.AskOne(&survey.Input{
		Message: "Runner labels (comma-separated):",
		Default: "gale,self-hosted,linux,x64,docker",
		Help:    "Labels for job matching (use 'gale' for runs-on: gale)",
	}, &labelsStr)
	cfg.Runner.Labels = strings.Split(labelsStr, ",")
	for i := range cfg.Runner.Labels {
		cfg.Runner.Labels[i] = strings.TrimSpace(cfg.Runner.Labels[i])
	}

	// Log Level
	survey.AskOne(&survey.Select{
		Message: "Log level:",
		Options: []string{"info", "debug", "warn", "error"},
		Default: "info",
	}, &cfg.LogLevel)

	// Write config
	if err := os.MkdirAll(filepath.Dir(cfgFile), 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	// Add header comment
	header := `# Gale Configuration
# Generated by 'gale init'
# Documentation: https://github.com/manashmandal/gale

`
	if err := os.WriteFile(cfgFile, []byte(header+string(data)), 0600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	fmt.Println("\n✅ Configuration saved to", cfgFile)
	fmt.Println()
	fmt.Println("Next steps:")
	if strings.Contains(tokenStorage, "Environment") {
		fmt.Println("  1. Export token: export GITHUB_TOKEN=<your-token>")
		fmt.Println("  2. Start gale:   gale start")
	} else {
		fmt.Println("  1. Start gale:   gale start")
	}
	fmt.Println()

	return nil
}
