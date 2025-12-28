package main

import (
	"fmt"
	"strings"

	"github.com/manashmandal/gale/internal/config"
	"github.com/spf13/cobra"
)

var labelsCmd = &cobra.Command{
	Use:   "labels",
	Short: "Manage runner labels",
	Long: `Manage the runs-on labels that Gale will accept.

Jobs with matching labels in their 'runs-on' field will be picked up by Gale runners.

Examples:
  gale labels                    # List current labels
  gale labels add docker         # Add 'docker' label
  gale labels remove docker      # Remove 'docker' label`,
	RunE: runLabelsList,
}

var labelsAddCmd = &cobra.Command{
	Use:   "add <label>",
	Short: "Add a runner label",
	Long: `Add a new label that Gale runners will accept.

After adding, jobs with 'runs-on: <label>' will be picked up by Gale.

Examples:
  gale labels add docker
  gale labels add gpu-runner`,
	Args: cobra.ExactArgs(1),
	RunE: runLabelsAdd,
}

var labelsRemoveCmd = &cobra.Command{
	Use:   "remove <label>",
	Short: "Remove a runner label",
	Long: `Remove a label from the accepted list.

After removing, jobs with only this label will no longer be picked up by Gale.

Examples:
  gale labels remove docker
  gale labels remove gpu-runner`,
	Args: cobra.ExactArgs(1),
	RunE: runLabelsRemove,
}

func init() {
	labelsCmd.AddCommand(labelsAddCmd)
	labelsCmd.AddCommand(labelsRemoveCmd)
	rootCmd.AddCommand(labelsCmd)
}

func runLabelsList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if len(cfg.Runner.Labels) == 0 {
		fmt.Println("No runner labels configured.")
		fmt.Println("\nAdd labels with: gale labels add <label>")
		return nil
	}

	fmt.Println("Runner labels (runs-on):")
	for _, label := range cfg.Runner.Labels {
		fmt.Printf("  • %s\n", label)
	}
	fmt.Printf("\nJobs with any of these labels will be picked up by Gale.\n")

	return nil
}

func runLabelsAdd(cmd *cobra.Command, args []string) error {
	label := strings.TrimSpace(args[0])
	if label == "" {
		return fmt.Errorf("label cannot be empty")
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Check if already exists
	for _, existing := range cfg.Runner.Labels {
		if strings.EqualFold(existing, label) {
			fmt.Printf("Label '%s' already exists.\n", existing)
			return nil
		}
	}

	cfg.Runner.Labels = append(cfg.Runner.Labels, label)

	if err := cfg.Save(cfgFile); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("Added label '%s'.\n", label)
	fmt.Printf("\nJobs with 'runs-on: %s' will now be picked up by Gale.\n", label)
	fmt.Println("Restart gale for changes to take effect.")

	return nil
}

func runLabelsRemove(cmd *cobra.Command, args []string) error {
	label := strings.TrimSpace(args[0])
	if label == "" {
		return fmt.Errorf("label cannot be empty")
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Find and remove
	found := false
	newLabels := make([]string, 0, len(cfg.Runner.Labels))
	for _, existing := range cfg.Runner.Labels {
		if strings.EqualFold(existing, label) {
			found = true
			continue
		}
		newLabels = append(newLabels, existing)
	}

	if !found {
		fmt.Printf("Label '%s' not found.\n", label)
		fmt.Println("\nCurrent labels:")
		for _, l := range cfg.Runner.Labels {
			fmt.Printf("  • %s\n", l)
		}
		return nil
	}

	cfg.Runner.Labels = newLabels

	if err := cfg.Save(cfgFile); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("Removed label '%s'.\n", label)
	fmt.Println("Restart gale for changes to take effect.")

	return nil
}
