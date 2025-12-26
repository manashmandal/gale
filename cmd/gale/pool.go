package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/manashmandal/gale/internal/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var poolCmd = &cobra.Command{
	Use:   "pool [size]",
	Short: "Set warm pool size (min_runners)",
	Long: `Set the number of runners to keep always running (warm pool).

A warm pool reduces cold start time by keeping runners ready to pick up jobs.
Set to 0 to scale to zero when idle.

Examples:
  gale pool 3      # Keep 3 runners always running
  gale pool 0      # Scale to zero when idle
  gale pool        # Show current pool size`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPool,
}

func init() {
	rootCmd.AddCommand(poolCmd)
}

func runPool(cmd *cobra.Command, args []string) error {
	// Load current config
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// If no argument, show current value
	if len(args) == 0 {
		fmt.Printf("Current warm pool size: %d runners\n", cfg.Scaler.MinRunners)
		if cfg.Scaler.MinRunners == 0 {
			fmt.Println("(scales to zero when idle)")
		} else {
			fmt.Printf("(always keeps %d runner(s) ready)\n", cfg.Scaler.MinRunners)
		}
		return nil
	}

	// Parse new value
	size, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid pool size: %s", args[0])
	}

	if size < 0 {
		return fmt.Errorf("pool size cannot be negative")
	}

	if size > cfg.Scaler.MaxRunners {
		return fmt.Errorf("pool size (%d) cannot exceed max_runners (%d)", size, cfg.Scaler.MaxRunners)
	}

	// Update config file
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	var cfgMap map[string]interface{}
	if err := yaml.Unmarshal(data, &cfgMap); err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}

	// Set min_runners
	if scaler, ok := cfgMap["scaler"].(map[string]interface{}); ok {
		scaler["min_runners"] = size
	} else {
		cfgMap["scaler"] = map[string]interface{}{"min_runners": size}
	}

	out, err := yaml.Marshal(cfgMap)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(cfgFile, out, 0644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	if size == 0 {
		fmt.Println("✅ Warm pool disabled (will scale to zero when idle)")
	} else {
		fmt.Printf("✅ Warm pool set to %d runner(s)\n", size)
		fmt.Println("   Runners will be kept ready even when there are no jobs")
	}

	return nil
}
