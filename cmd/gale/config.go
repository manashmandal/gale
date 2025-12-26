package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/manashmandal/gale/internal/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration",
	Long:  `View and modify Gale configuration.`,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current configuration",
	Long: `Display the current Gale configuration.

Example:
  gale config show
  gale config show --config /etc/gale/config.yaml`,
	RunE: runConfigShow,
}

var configSetCmd = &cobra.Command{
	Use:   "set KEY VALUE",
	Short: "Set a configuration value",
	Long: `Set a configuration value.

Available keys:
  github.owner       - GitHub username or organization
  github.repo        - Repository name (empty for org mode)
  github.scope       - Scope: org or repo
  scaler.max_runners - Maximum concurrent runners
  scaler.min_runners - Minimum runners to keep alive
  scaler.poll_interval - How often to check for jobs
  runner.image       - Docker image for runners
  log_level          - Log level: debug, info, warn, error

Example:
  gale config set scaler.max_runners 20
  gale config set github.scope org
  gale config set log_level debug`,
	Args: cobra.ExactArgs(2),
	RunE: runConfigSet,
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Show config file path",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(cfgFile)
		return nil
	},
}

var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration",
	Long: `Validate the configuration file for errors.

Example:
  gale config validate`,
	RunE: runConfigValidate,
}

func init() {
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configPathCmd)
	configCmd.AddCommand(configValidateCmd)
	rootCmd.AddCommand(configCmd)
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	// Parse and re-marshal to show expanded values
	cfg, err := config.Load(cfgFile)
	if err != nil {
		// If parsing fails, just show raw file
		fmt.Println(string(data))
		return nil
	}

	// Show with masked token
	output := struct {
		GitHub struct {
			Token string `yaml:"token"`
			Owner string `yaml:"owner"`
			Repo  string `yaml:"repo,omitempty"`
			Scope string `yaml:"scope"`
		} `yaml:"github"`
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
	}{}

	// Mask token
	if len(cfg.GitHub.Token) > 8 {
		output.GitHub.Token = cfg.GitHub.Token[:4] + "..." + cfg.GitHub.Token[len(cfg.GitHub.Token)-4:]
	} else {
		output.GitHub.Token = "***"
	}
	output.GitHub.Owner = cfg.GitHub.Owner
	output.GitHub.Repo = cfg.GitHub.Repo
	output.GitHub.Scope = cfg.GitHub.Scope
	output.Scaler.MinRunners = cfg.Scaler.MinRunners
	output.Scaler.MaxRunners = cfg.Scaler.MaxRunners
	output.Scaler.PollInterval = cfg.Scaler.PollInterval.String()
	output.Scaler.ScaleUpDelay = cfg.Scaler.ScaleUpDelay.String()
	output.Runner.Image = cfg.Runner.Image
	output.Runner.Labels = cfg.Runner.Labels
	output.LogLevel = cfg.LogLevel

	out, _ := yaml.Marshal(output)
	fmt.Println(string(out))
	return nil
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	key := args[0]
	value := args[1]

	// Read current config
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}

	// Set value based on key path
	parts := strings.Split(key, ".")
	if err := setNestedValue(cfg, parts, value); err != nil {
		return err
	}

	// Write back
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(cfgFile, out, 0644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	fmt.Printf("Set %s = %s\n", key, value)
	return nil
}

func setNestedValue(m map[string]interface{}, path []string, value string) error {
	if len(path) == 0 {
		return fmt.Errorf("empty path")
	}

	if len(path) == 1 {
		m[path[0]] = convertValue(value)
		return nil
	}

	// Navigate to nested map
	key := path[0]
	nested, ok := m[key].(map[string]interface{})
	if !ok {
		nested = make(map[string]interface{})
		m[key] = nested
	}

	return setNestedValue(nested, path[1:], value)
}

func convertValue(s string) interface{} {
	// Try int
	if i, err := strconv.Atoi(s); err == nil {
		return i
	}
	// Try bool
	if b, err := strconv.ParseBool(s); err == nil {
		return b
	}
	// Return as string
	return s
}

func runConfigValidate(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("❌ Invalid configuration: %w", err)
	}

	errors := []string{}

	if cfg.GitHub.Token == "" || cfg.GitHub.Token == "${GITHUB_TOKEN}" {
		errors = append(errors, "github.token is not set (set GITHUB_TOKEN env var)")
	}
	if cfg.GitHub.Owner == "" {
		errors = append(errors, "github.owner is required")
	}
	if cfg.Scaler.MaxRunners < 1 {
		errors = append(errors, "scaler.max_runners must be at least 1")
	}
	if cfg.Runner.Image == "" {
		errors = append(errors, "runner.image is required")
	}

	if len(errors) > 0 {
		fmt.Println("❌ Configuration has errors:")
		for _, e := range errors {
			fmt.Printf("   - %s\n", e)
		}
		return fmt.Errorf("validation failed")
	}

	fmt.Println("✅ Configuration is valid")
	return nil
}
