package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/manashmandal/gale/internal/config"
	"github.com/manashmandal/gale/internal/docker"
	"github.com/manashmandal/gale/internal/github"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current status",
	Long: `Show the current status of Gale including:
  - Configuration summary
  - Active runners
  - Queued jobs

Example:
  gale status`,
	RunE: runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	ctx := context.Background()

	// Print configuration
	fmt.Println("📋 Configuration")
	fmt.Println("----------------")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Config file:\t%s\n", cfgFile)
	fmt.Fprintf(w, "Owner:\t%s\n", cfg.GitHub.Owner)
	repos := cfg.GetRepos()
	if len(repos) == 0 {
		fmt.Fprintf(w, "Repositories:\tALL\n")
	} else if len(repos) == 1 {
		fmt.Fprintf(w, "Repository:\t%s\n", repos[0])
	} else {
		fmt.Fprintf(w, "Repositories:\t%s\n", fmt.Sprintf("%v", repos))
	}
	fmt.Fprintf(w, "Scope:\t%s\n", cfg.GitHub.Scope)
	fmt.Fprintf(w, "Max runners:\t%d\n", cfg.Scaler.MaxRunners)
	fmt.Fprintf(w, "Min runners:\t%d\n", cfg.Scaler.MinRunners)
	fmt.Fprintf(w, "Poll interval:\t%s\n", cfg.Scaler.PollInterval)
	w.Flush()

	// Docker status
	fmt.Println("\n🐳 Docker Runners")
	fmt.Println("-----------------")

	dockerClient, err := docker.NewClient(cfg.Docker.Host)
	if err != nil {
		fmt.Printf("Error connecting to Docker: %v\n", err)
	} else {
		defer dockerClient.Close()

		runners, err := dockerClient.ListRunners(ctx)
		if err != nil {
			fmt.Printf("Error listing runners: %v\n", err)
		} else {
			running := 0
			exited := 0
			for _, r := range runners {
				if r.Status == "running" {
					running++
				} else {
					exited++
				}
			}
			fmt.Printf("Running: %d\n", running)
			fmt.Printf("Exited:  %d\n", exited)
		}
	}

	// GitHub status
	fmt.Println("\n🐙 GitHub Jobs")
	fmt.Println("--------------")

	if cfg.GitHub.Token == "" || cfg.GitHub.Token == "${GITHUB_TOKEN}" {
		fmt.Println("GitHub token not configured")
		return nil
	}

	// Multi-repo mode - query each repo individually
	repos = cfg.GetRepos()
	if len(repos) == 0 {
		// Org mode - use original behavior
		ghClient := github.NewClient(cfg.GitHub.Token, cfg.GitHub.Owner, "", cfg.GitHub.Scope)
		jobs, err := ghClient.GetQueuedJobs(ctx)
		if err != nil {
			fmt.Printf("Error fetching jobs: %v\n", err)
			return nil
		}
		printJobs(jobs)
	} else {
		// Query each repo
		var allJobs []github.QueuedJob
		for _, repo := range repos {
			ghClient := github.NewClient(cfg.GitHub.Token, cfg.GitHub.Owner, repo, "repo")
			jobs, err := ghClient.GetQueuedJobs(ctx)
			if err != nil {
				fmt.Printf("Error fetching jobs for %s: %v\n", repo, err)
				continue
			}
			allJobs = append(allJobs, jobs...)
		}
		printJobs(allJobs)
	}

	return nil
}

func printJobs(jobs []github.QueuedJob) {
	if len(jobs) == 0 {
		fmt.Println("No queued jobs")
		return
	}

	fmt.Printf("Queued jobs: %d\n\n", len(jobs))

	// Group by repo
	byRepo := make(map[string][]github.QueuedJob)
	for _, j := range jobs {
		byRepo[j.Repo] = append(byRepo[j.Repo], j)
	}

	for repo, repoJobs := range byRepo {
		fmt.Printf("  %s: %d job(s)\n", repo, len(repoJobs))
		for _, j := range repoJobs {
			fmt.Printf("    - %s (%s)\n", j.JobName, j.Status)
		}
	}
}
