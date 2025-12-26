package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/manashmandal/gale/internal/config"
	"github.com/manashmandal/gale/internal/docker"
	"github.com/spf13/cobra"
)

var runnersCmd = &cobra.Command{
	Use:   "runners",
	Short: "Manage runners",
	Long:  `List, inspect, and manage Gale runners.`,
}

var runnersListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all runners",
	Long: `List all Gale-managed runner containers.

Example:
  gale runners list
  gale runners list --all`,
	RunE: runRunnersList,
}

var runnersCleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean up exited runners",
	Long: `Remove all exited Gale runner containers.

Example:
  gale runners clean`,
	RunE: runRunnersClean,
}

var runnersStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop all runners",
	Long: `Stop and remove all Gale-managed runner containers.

Example:
  gale runners stop`,
	RunE: runRunnersStop,
}

var showAllRunners bool

func init() {
	runnersListCmd.Flags().BoolVarP(&showAllRunners, "all", "a", false, "show all runners including exited")

	runnersCmd.AddCommand(runnersListCmd)
	runnersCmd.AddCommand(runnersCleanCmd)
	runnersCmd.AddCommand(runnersStopCmd)
	rootCmd.AddCommand(runnersCmd)
}

func getDockerClient() (*docker.Client, error) {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		// Use default if config doesn't exist
		return docker.NewClient("")
	}
	return docker.NewClient(cfg.Docker.Host)
}

func runRunnersList(cmd *cobra.Command, args []string) error {
	client, err := getDockerClient()
	if err != nil {
		return fmt.Errorf("connecting to docker: %w", err)
	}
	defer client.Close()

	ctx := context.Background()
	runners, err := client.ListRunners(ctx)
	if err != nil {
		return fmt.Errorf("listing runners: %w", err)
	}

	if len(runners) == 0 {
		fmt.Println("No runners found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tCONTAINER\tSTATUS\tREPO")

	for _, r := range runners {
		if !showAllRunners && r.Status != "running" {
			continue
		}
		containerID := r.ContainerID
		if len(containerID) > 12 {
			containerID = containerID[:12]
		}
		repo := r.Repo
		if repo == "" {
			repo = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.ID, containerID, r.Status, repo)
	}
	w.Flush()

	return nil
}

func runRunnersClean(cmd *cobra.Command, args []string) error {
	client, err := getDockerClient()
	if err != nil {
		return fmt.Errorf("connecting to docker: %w", err)
	}
	defer client.Close()

	ctx := context.Background()
	cleaned, err := client.CleanupExitedRunners(ctx)
	if err != nil {
		return fmt.Errorf("cleaning up runners: %w", err)
	}

	if cleaned == 0 {
		fmt.Println("No exited runners to clean up.")
	} else {
		fmt.Printf("Cleaned up %d exited runner(s).\n", cleaned)
	}

	return nil
}

func runRunnersStop(cmd *cobra.Command, args []string) error {
	client, err := getDockerClient()
	if err != nil {
		return fmt.Errorf("connecting to docker: %w", err)
	}
	defer client.Close()

	ctx := context.Background()
	runners, err := client.ListRunners(ctx)
	if err != nil {
		return fmt.Errorf("listing runners: %w", err)
	}

	if len(runners) == 0 {
		fmt.Println("No runners to stop.")
		return nil
	}

	stopped := 0
	for _, r := range runners {
		if err := client.RemoveRunner(ctx, r.ContainerID); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove runner %s: %v\n", r.ID, err)
			continue
		}
		stopped++
	}

	fmt.Printf("Stopped %d runner(s).\n", stopped)
	return nil
}
