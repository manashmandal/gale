package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/manashmandal/gale/internal/config"
	"github.com/spf13/cobra"
)

var webhookStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show webhook daemon status",
	Long: `Show the current status of the webhook daemon.

Displays:
  - Whether the daemon is running
  - Active runner count (from health endpoint)
  - Funnel URL (if configured)
  - Number of registered webhooks
  - Configuration summary

Example:
  gale webhook status`,
	RunE: runWebhookStatus,
}

func init() {
	webhookCmd.AddCommand(webhookStatusCmd)
}

func runWebhookStatus(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	fmt.Println()
	fmt.Println("Webhook Status")
	fmt.Println("==============")
	fmt.Println()

	// Check daemon status via PID file
	pid, running := getWebhookPid()
	if running {
		fmt.Printf("  Daemon:        Running (PID: %d)\n", pid)
	} else {
		fmt.Printf("  Daemon:        Not running\n")
	}

	// Try to get health endpoint response
	var activeRunners string
	if running {
		activeRunners = getHealthStatus(cfg)
	} else {
		activeRunners = "-"
	}
	fmt.Printf("  Active Runners: %s\n", activeRunners)

	// Show Funnel URL if configured
	if cfg.Webhook.FunnelDNSName != "" {
		funnelURL := fmt.Sprintf("https://%s/webhook", cfg.Webhook.FunnelDNSName)
		fmt.Printf("  Funnel URL:    %s\n", funnelURL)
	} else {
		fmt.Printf("  Funnel URL:    (not configured)\n")
	}

	// Show registered webhooks count
	hooks := cfg.GetRegisteredHooks()
	fmt.Printf("  Webhooks:      %d registered\n", len(hooks))

	fmt.Println()
	fmt.Println("Configuration")
	fmt.Println("-------------")
	fmt.Printf("  Port:          %d\n", cfg.Webhook.Port)
	fmt.Printf("  Runner Mode:   %s\n", cfg.Runner.Mode)
	fmt.Printf("  Max Runners:   %d\n", cfg.Scaler.MaxRunners)
	if cfg.GetWebhookSecret() != "" {
		fmt.Printf("  Signature:     enabled\n")
	} else {
		fmt.Printf("  Signature:     disabled (not recommended)\n")
	}

	// Show registered webhooks details
	if len(hooks) > 0 {
		fmt.Println()
		fmt.Println("Registered Webhooks")
		fmt.Println("-------------------")
		for target, hook := range hooks {
			fmt.Printf("  %s (%s)\n", target, hook.Type)
			fmt.Printf("    URL: %s\n", hook.URL)

			// Check for URL mismatch
			if cfg.Webhook.FunnelDNSName != "" {
				expectedURL := fmt.Sprintf("https://%s/webhook", cfg.Webhook.FunnelDNSName)
				if hook.URL != expectedURL {
					fmt.Printf("    WARNING: URL mismatch! Expected: %s\n", expectedURL)
				}
			}
		}
	}

	fmt.Println()

	// Show helpful commands
	if !running {
		fmt.Println("To start the webhook daemon:")
		fmt.Println("  gale webhook --funnel --daemon")
		fmt.Println()
	} else {
		fmt.Println("Commands:")
		fmt.Println("  gale logs              - View daemon logs")
		fmt.Println("  gale webhook stop      - Stop the daemon")
		fmt.Println("  gale webhook restart   - Restart the daemon")
		fmt.Println("  gale webhook runners   - List active runners")
		fmt.Println()
	}

	return nil
}

func getHealthStatus(cfg *config.Config) string {
	// Try localhost first (for local server mode)
	urls := []string{
		fmt.Sprintf("http://127.0.0.1:%d/health", cfg.Webhook.Port),
	}

	// Also try funnel URL if configured
	if cfg.Webhook.FunnelDNSName != "" {
		urls = append(urls, fmt.Sprintf("https://%s/health", cfg.Webhook.FunnelDNSName))
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	for _, url := range urls {
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return "running (error reading response)"
			}
			// Response is "OK - N active runners\n"
			bodyStr := strings.TrimSpace(string(body))
			if strings.HasPrefix(bodyStr, "OK - ") {
				return strings.TrimPrefix(bodyStr, "OK - ")
			}
			return bodyStr
		}
	}

	return "unknown (health endpoint unreachable)"
}
