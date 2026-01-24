package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/manashmandal/gale/internal/config"
	"github.com/manashmandal/gale/internal/docker"
	"github.com/manashmandal/gale/internal/native"
	"github.com/spf13/cobra"
)

var (
	webhookShowAllRunners bool
)

var webhookRunnersCmd = &cobra.Command{
	Use:   "runners",
	Short: "List active runners",
	Long: `List all active runners managed by Gale.

For Docker mode: Lists containers with Gale labels.
For Native mode: Lists in-memory runners and optionally scans the filesystem.

Examples:
  gale webhook runners
  gale webhook runners --all    # Include filesystem runners for native mode`,
	RunE: runWebhookRunners,
}

func init() {
	webhookRunnersCmd.Flags().BoolVarP(&webhookShowAllRunners, "all", "a", false, "include filesystem runners (native mode only)")
	webhookCmd.AddCommand(webhookRunnersCmd)
}

func runWebhookRunners(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	ctx := context.Background()

	switch cfg.Runner.Mode {
	case "native":
		return listNativeRunners(ctx, cfg)
	case "docker", "":
		return listDockerRunners(ctx, cfg)
	default:
		return fmt.Errorf("unknown runner mode: %s", cfg.Runner.Mode)
	}
}

func listDockerRunners(ctx context.Context, cfg *config.Config) error {
	dockerClient, err := docker.NewClient(cfg.Docker.Host)
	if err != nil {
		return fmt.Errorf("connecting to Docker: %w", err)
	}
	defer dockerClient.Close()

	runners, err := dockerClient.ListRunners(ctx)
	if err != nil {
		return fmt.Errorf("listing runners: %w", err)
	}

	if len(runners) == 0 {
		fmt.Println("No active runners")
		return nil
	}

	fmt.Printf("Active Docker Runners (%d)\n", len(runners))
	fmt.Println()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSTATUS\tREPO\tSTARTED\tCONTAINER")
	fmt.Fprintln(w, "--\t------\t----\t-------\t---------")

	for _, r := range runners {
		containerID := r.ContainerID
		if len(containerID) > 12 {
			containerID = containerID[:12]
		}

		started := formatDuration(time.Since(r.StartedAt))

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			r.ID,
			r.Status,
			shortenRepo(r.Repo),
			started,
			containerID,
		)
	}
	w.Flush()

	return nil
}

func listNativeRunners(ctx context.Context, cfg *config.Config) error {
	nativeClient, err := native.NewClient("")
	if err != nil {
		return fmt.Errorf("creating native client: %w", err)
	}
	defer nativeClient.Close()

	runners, err := nativeClient.ListRunners(ctx)
	if err != nil {
		return fmt.Errorf("listing runners: %w", err)
	}

	// Also scan filesystem for additional runners if --all flag is provided
	var fsRunners []filesystemRunner
	if webhookShowAllRunners {
		fsRunners = scanFilesystemRunners()
	}

	if len(runners) == 0 && len(fsRunners) == 0 {
		fmt.Println("No active runners")
		return nil
	}

	if len(runners) > 0 {
		fmt.Printf("Active Native Runners (%d)\n", len(runners))
		fmt.Println()

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tSTATUS\tREPO\tSTARTED\tPID")
		fmt.Fprintln(w, "--\t------\t----\t-------\t---")

		for _, r := range runners {
			started := formatDuration(time.Since(r.StartedAt))

			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\n",
				r.ID,
				r.Status,
				shortenRepo(r.Repo),
				started,
				r.PID,
			)
		}
		w.Flush()
	}

	if len(fsRunners) > 0 {
		if len(runners) > 0 {
			fmt.Println()
		}
		fmt.Printf("Filesystem Runners (%d)\n", len(fsRunners))
		fmt.Println()

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tDIR\tMODIFIED")
		fmt.Fprintln(w, "--\t---\t--------")

		for _, r := range fsRunners {
			modified := formatDuration(time.Since(r.ModTime))
			fmt.Fprintf(w, "%s\t%s\t%s ago\n",
				r.ID,
				shortenPath(r.Dir),
				modified,
			)
		}
		w.Flush()

		fmt.Println()
		fmt.Println("Note: Filesystem runners are directories in ~/.gale/native-runners/work/")
		fmt.Println("      They may be orphaned or from previous sessions.")
	}

	return nil
}

type filesystemRunner struct {
	ID      string
	Dir     string
	ModTime time.Time
}

func scanFilesystemRunners() []filesystemRunner {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	workDir := filepath.Join(homeDir, ".gale", "native-runners", "work")
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return nil
	}

	var runners []filesystemRunner
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		runners = append(runners, filesystemRunner{
			ID:      entry.Name(),
			Dir:     filepath.Join(workDir, entry.Name()),
			ModTime: info.ModTime(),
		})
	}

	return runners
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func shortenRepo(repo string) string {
	if repo == "" {
		return "-"
	}
	// Remove https://github.com/ prefix
	repo = strings.TrimPrefix(repo, "https://github.com/")
	repo = strings.TrimPrefix(repo, "http://github.com/")
	if len(repo) > 30 {
		return repo[:27] + "..."
	}
	return repo
}

func shortenPath(path string) string {
	homeDir, _ := os.UserHomeDir()
	if homeDir != "" && strings.HasPrefix(path, homeDir) {
		path = "~" + path[len(homeDir):]
	}
	if len(path) > 40 {
		return "..." + path[len(path)-37:]
	}
	return path
}
