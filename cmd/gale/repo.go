package main

import (
	"fmt"
	"strings"

	"github.com/manashmandal/gale/internal/config"
	"github.com/spf13/cobra"
)

var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Manage monitored repositories",
	Long: `Manage which repositories Gale monitors for workflow jobs.

By default, Gale monitors all repositories under the configured owner.
Use these commands to specify individual repositories instead.

Examples:
  gale repo list              # List monitored repos
  gale repo add my-repo       # Add a repo to monitor
  gale repo add repo1 repo2   # Add multiple repos
  gale repo remove my-repo    # Stop monitoring a repo
  gale repo clear             # Monitor all repos (clear list)`,
}

var repoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List monitored repositories",
	RunE:  runRepoList,
}

var repoAddCmd = &cobra.Command{
	Use:   "add <repo> [repo...]",
	Short: "Add repositories to monitor",
	Long: `Add one or more repositories to the monitored list.

When specific repos are configured, Gale only responds to webhook
events from those repos and ignores events from other repos.

Examples:
  gale repo add my-project
  gale repo add frontend backend api`,
	Args: cobra.MinimumNArgs(1),
	RunE: runRepoAdd,
}

var repoRemoveCmd = &cobra.Command{
	Use:   "remove <repo> [repo...]",
	Short: "Remove repositories from monitoring",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runRepoRemove,
}

var repoClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear repo list (monitor all repos)",
	Long: `Clear the list of specific repos to monitor.

After clearing, Gale will respond to webhook events from all
repositories where the GitHub App is installed.`,
	RunE: runRepoClear,
}

var repoSetCmd = &cobra.Command{
	Use:   "set <repo> [repo...]",
	Short: "Set the exact list of repos to monitor",
	Long: `Replace the current repo list with the specified repos.

This clears any existing repos and sets only the specified ones.

Examples:
  gale repo set my-project
  gale repo set frontend backend api`,
	Args: cobra.MinimumNArgs(1),
	RunE: runRepoSet,
}

func init() {
	rootCmd.AddCommand(repoCmd)
	repoCmd.AddCommand(repoListCmd)
	repoCmd.AddCommand(repoAddCmd)
	repoCmd.AddCommand(repoRemoveCmd)
	repoCmd.AddCommand(repoClearCmd)
	repoCmd.AddCommand(repoSetCmd)
}

func runRepoList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	repos := cfg.GetRepos()
	if len(repos) == 0 {
		fmt.Println("Monitoring: ALL repositories")
		fmt.Println()
		fmt.Println("To monitor specific repos, use:")
		fmt.Println("  gale repo add <repo-name>")
		fmt.Println("  gale repo set <repo1> <repo2> ...")
		return nil
	}

	fmt.Printf("Monitoring %d specific repo(s):\n", len(repos))
	for _, repo := range repos {
		fmt.Printf("  - %s\n", repo)
	}
	fmt.Println()
	fmt.Println("To monitor all repos: gale repo clear")

	return nil
}

func runRepoAdd(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	added := []string{}
	for _, repo := range args {
		repo = strings.TrimSpace(repo)
		if repo == "" {
			continue
		}
		// Remove owner prefix if provided (e.g., "owner/repo" -> "repo")
		if strings.Contains(repo, "/") {
			parts := strings.Split(repo, "/")
			repo = parts[len(parts)-1]
		}
		if !cfg.IsRepoMonitored(repo) {
			cfg.AddRepo(repo)
			added = append(added, repo)
		}
	}

	if len(added) == 0 {
		fmt.Println("No new repos added (already in list)")
		return nil
	}

	if err := cfg.Save(cfgFile); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("Added %d repo(s):\n", len(added))
	for _, repo := range added {
		fmt.Printf("  + %s\n", repo)
	}
	fmt.Println()
	fmt.Printf("Now monitoring %d repo(s)\n", len(cfg.GetRepos()))

	return nil
}

func runRepoRemove(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if len(cfg.GetRepos()) == 0 {
		fmt.Println("No specific repos configured (monitoring all)")
		return nil
	}

	removed := []string{}
	for _, repo := range args {
		repo = strings.TrimSpace(repo)
		if repo == "" {
			continue
		}
		// Remove owner prefix if provided
		if strings.Contains(repo, "/") {
			parts := strings.Split(repo, "/")
			repo = parts[len(parts)-1]
		}
		if cfg.RemoveRepo(repo) {
			removed = append(removed, repo)
		}
	}

	if len(removed) == 0 {
		fmt.Println("No repos removed (not in list)")
		return nil
	}

	if err := cfg.Save(cfgFile); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("Removed %d repo(s):\n", len(removed))
	for _, repo := range removed {
		fmt.Printf("  - %s\n", repo)
	}

	remaining := cfg.GetRepos()
	if len(remaining) == 0 {
		fmt.Println("\nNow monitoring ALL repos")
	} else {
		fmt.Printf("\nNow monitoring %d repo(s)\n", len(remaining))
	}

	return nil
}

func runRepoClear(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if len(cfg.GetRepos()) == 0 {
		fmt.Println("Already monitoring all repos")
		return nil
	}

	cfg.GitHub.Repos = []string{}
	cfg.GitHub.Repo = ""
	cfg.GitHub.Scope = "org"

	if err := cfg.Save(cfgFile); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Println("Cleared repo list - now monitoring ALL repos")
	return nil
}

func runRepoSet(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Clear existing
	cfg.GitHub.Repos = []string{}
	cfg.GitHub.Repo = ""

	// Add specified repos
	for _, repo := range args {
		repo = strings.TrimSpace(repo)
		if repo == "" {
			continue
		}
		// Remove owner prefix if provided
		if strings.Contains(repo, "/") {
			parts := strings.Split(repo, "/")
			repo = parts[len(parts)-1]
		}
		cfg.AddRepo(repo)
	}

	// Update scope
	if len(cfg.GitHub.Repos) == 1 {
		cfg.GitHub.Scope = "repo"
	} else {
		cfg.GitHub.Scope = "repos"
	}

	if err := cfg.Save(cfgFile); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("Set %d repo(s) to monitor:\n", len(cfg.GetRepos()))
	for _, repo := range cfg.GetRepos() {
		fmt.Printf("  - %s\n", repo)
	}

	return nil
}
