package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/manashmandal/gale/internal/config"
	"github.com/manashmandal/gale/internal/github"
	"github.com/manashmandal/gale/internal/secret"
	"github.com/spf13/cobra"
	"tailscale.com/client/tailscale"
)

var (
	registerOrg bool
	registerURL string
	noAddRepo   bool
)

var webhookRegisterCmd = &cobra.Command{
	Use:   "register [repo]",
	Short: "Register webhook on GitHub",
	Long: `Register a GitHub webhook to receive workflow job events.

For repository-level webhook:
  gale webhook register my-repo

For organization-level webhook (receives events from all repos):
  gale webhook register --org

You can specify a custom webhook URL:
  gale webhook register my-repo --url https://example.com/webhook

Examples:
  gale webhook register my-project
  gale webhook register owner/my-project
  gale webhook register --org
  gale webhook register my-repo --url https://my-server.com/webhook
  gale webhook register my-repo --no-add-repo`,
	RunE: runWebhookRegister,
}

var webhookListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered webhooks",
	Long:  `List all webhooks that have been registered through gale.`,
	RunE:  runWebhookList,
}

var webhookUnregisterCmd = &cobra.Command{
	Use:   "unregister [repo]",
	Short: "Remove a registered webhook",
	Long: `Remove a webhook that was previously registered.

Examples:
  gale webhook unregister my-repo
  gale webhook unregister --org`,
	RunE: runWebhookUnregister,
}

func init() {
	webhookRegisterCmd.Flags().BoolVar(&registerOrg, "org", false, "register organization-level webhook")
	webhookRegisterCmd.Flags().StringVar(&registerURL, "url", "", "webhook URL (required if not using --funnel)")
	webhookRegisterCmd.Flags().BoolVar(&noAddRepo, "no-add-repo", false, "don't automatically add repo to watchlist")

	webhookUnregisterCmd.Flags().BoolVar(&registerOrg, "org", false, "unregister organization-level webhook")

	webhookCmd.AddCommand(webhookRegisterCmd)
	webhookCmd.AddCommand(webhookListCmd)
	webhookCmd.AddCommand(webhookUnregisterCmd)
}

func runWebhookRegister(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if !registerOrg && len(args) == 0 {
		return fmt.Errorf("specify a repository or use --org for organization-level webhook")
	}

	if cfg.GitHub.Token == "" {
		return fmt.Errorf("GITHUB_TOKEN or github.token is required for webhook registration")
	}

	ctx := context.Background()

	webhookURL := registerURL
	if webhookURL == "" {
		dnsName, err := getTailscaleDNSName(ctx)
		if err != nil {
			return fmt.Errorf("detecting Tailscale URL: %w\n\nUse --url to specify the webhook URL manually", err)
		}
		webhookURL = fmt.Sprintf("https://%s/webhook", dnsName)
		fmt.Printf("Using Tailscale Funnel URL: %s\n", webhookURL)
	}

	if !strings.HasSuffix(webhookURL, "/webhook") {
		webhookURL = strings.TrimSuffix(webhookURL, "/") + "/webhook"
	}

	webhookSecret := cfg.GetWebhookSecret()
	if webhookSecret == "" {
		webhookSecret, err = secret.GenerateWebhookSecret()
		if err != nil {
			return fmt.Errorf("generating webhook secret: %w", err)
		}
		cfg.SetWebhookSecret(webhookSecret)
		fmt.Println("Generated new webhook secret")
	}

	ghClient := github.NewClient(cfg.GitHub.Token, cfg.GitHub.Owner, "", cfg.GitHub.Scope)

	if registerOrg {
		return registerOrgWebhook(ctx, cfg, ghClient, webhookURL, webhookSecret)
	}

	return registerRepoWebhook(ctx, cfg, ghClient, args[0], webhookURL, webhookSecret)
}

func registerRepoWebhook(ctx context.Context, cfg *config.Config, ghClient *github.Client, repo, webhookURL, webhookSecret string) error {
	owner := cfg.GitHub.Owner
	repoName := repo
	if strings.Contains(repo, "/") {
		parts := strings.Split(repo, "/")
		owner = parts[0]
		repoName = parts[1]
	}

	hooks, err := ghClient.ListRepoWebhooks(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("listing existing webhooks: %w", err)
	}
	for _, hook := range hooks {
		if hook.Config != nil && hook.Config.GetURL() == webhookURL {
			fmt.Printf("Webhook already exists for %s/%s (ID: %d)\n", owner, repoName, hook.GetID())
			return nil
		}
	}

	reg, err := ghClient.CreateRepoWebhook(ctx, owner, repoName, webhookURL, webhookSecret)
	if err != nil {
		if strings.Contains(err.Error(), "Hook already exists") {
			fmt.Printf("Webhook already registered on %s/%s\n", owner, repoName)
			return nil
		}
		return fmt.Errorf("creating webhook: %w", err)
	}

	target := fmt.Sprintf("%s/%s", owner, repoName)
	cfg.AddRegisteredHook(target, config.RegisteredHook{
		ID:        reg.ID,
		URL:       webhookURL,
		CreatedAt: reg.CreatedAt.Format(time.RFC3339),
		Type:      "repo",
	})

	if !noAddRepo && !cfg.IsRepoMonitored(repoName) {
		cfg.AddRepo(repoName)
		fmt.Printf("Added %s to monitored repos\n", repoName)
	}

	if err := cfg.Save(cfgFile); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("Webhook registered successfully!\n")
	fmt.Printf("  Repository: %s/%s\n", owner, repoName)
	fmt.Printf("  Webhook ID: %d\n", reg.ID)
	fmt.Printf("  URL:        %s\n", webhookURL)

	return nil
}

func registerOrgWebhook(ctx context.Context, cfg *config.Config, ghClient *github.Client, webhookURL, webhookSecret string) error {
	org := cfg.GitHub.Owner

	hooks, err := ghClient.ListOrgWebhooks(ctx, org)
	if err != nil {
		return fmt.Errorf("listing existing webhooks: %w", err)
	}
	for _, hook := range hooks {
		if hook.Config != nil && hook.Config.GetURL() == webhookURL {
			fmt.Printf("Webhook already exists for org %s (ID: %d)\n", org, hook.GetID())
			return nil
		}
	}

	reg, err := ghClient.CreateOrgWebhook(ctx, org, webhookURL, webhookSecret)
	if err != nil {
		if strings.Contains(err.Error(), "Hook already exists") {
			fmt.Printf("Webhook already registered on org %s\n", org)
			return nil
		}
		return fmt.Errorf("creating webhook: %w", err)
	}

	cfg.AddRegisteredHook(org, config.RegisteredHook{
		ID:        reg.ID,
		URL:       webhookURL,
		CreatedAt: reg.CreatedAt.Format(time.RFC3339),
		Type:      "org",
	})

	if err := cfg.Save(cfgFile); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("Organization webhook registered successfully!\n")
	fmt.Printf("  Organization: %s\n", org)
	fmt.Printf("  Webhook ID:   %d\n", reg.ID)
	fmt.Printf("  URL:          %s\n", webhookURL)

	return nil
}

func runWebhookList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	hooks := cfg.GetRegisteredHooks()
	if len(hooks) == 0 {
		fmt.Println("No webhooks registered")
		return nil
	}

	fmt.Printf("Registered webhooks (%d):\n", len(hooks))
	for target, hook := range hooks {
		fmt.Printf("\n  %s (%s)\n", target, hook.Type)
		fmt.Printf("    ID:      %d\n", hook.ID)
		fmt.Printf("    URL:     %s\n", hook.URL)
		fmt.Printf("    Created: %s\n", hook.CreatedAt)
	}

	return nil
}

func runWebhookUnregister(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if !registerOrg && len(args) == 0 {
		return fmt.Errorf("specify a repository or use --org for organization-level webhook")
	}

	if cfg.GitHub.Token == "" {
		return fmt.Errorf("GITHUB_TOKEN or github.token is required for webhook management")
	}

	ghClient := github.NewClient(cfg.GitHub.Token, cfg.GitHub.Owner, "", cfg.GitHub.Scope)
	ctx := context.Background()

	var target string
	if registerOrg {
		target = cfg.GitHub.Owner
	} else {
		repo := args[0]
		owner := cfg.GitHub.Owner
		repoName := repo
		if strings.Contains(repo, "/") {
			parts := strings.Split(repo, "/")
			owner = parts[0]
			repoName = parts[1]
		}
		target = fmt.Sprintf("%s/%s", owner, repoName)
	}

	hook, exists := cfg.GetRegisteredHook(target)
	if !exists {
		fmt.Printf("No registered webhook found for %s\n", target)
		return nil
	}

	var deleteErr error
	if hook.Type == "org" {
		deleteErr = ghClient.DeleteOrgWebhook(ctx, target, hook.ID)
	} else {
		parts := strings.Split(target, "/")
		if len(parts) == 2 {
			deleteErr = ghClient.DeleteRepoWebhook(ctx, parts[0], parts[1], hook.ID)
		} else {
			return fmt.Errorf("invalid target format: %s", target)
		}
	}

	if deleteErr != nil {
		if strings.Contains(deleteErr.Error(), "404") {
			fmt.Printf("Webhook not found on GitHub (may have been deleted manually)\n")
		} else {
			return fmt.Errorf("deleting webhook: %w", deleteErr)
		}
	}

	cfg.RemoveRegisteredHook(target)
	if err := cfg.Save(cfgFile); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("Webhook unregistered from %s (ID: %d)\n", target, hook.ID)
	return nil
}

func getTailscaleDNSName(ctx context.Context) (string, error) {
	lc := tailscale.LocalClient{}
	status, err := lc.Status(ctx)
	if err != nil {
		return "", fmt.Errorf("querying Tailscale status: %w (is Tailscale running?)", err)
	}

	if status.Self == nil {
		return "", fmt.Errorf("Tailscale not connected")
	}

	dnsName := status.Self.DNSName
	if dnsName == "" {
		return "", fmt.Errorf("no DNS name assigned - ensure Tailscale is properly configured")
	}

	// Remove trailing dot from DNS name
	dnsName = strings.TrimSuffix(dnsName, ".")

	return dnsName, nil
}
