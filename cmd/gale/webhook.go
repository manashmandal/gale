package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/manashmandal/gale/internal/config"
	"github.com/manashmandal/gale/internal/webhook"
	"github.com/spf13/cobra"
	"tailscale.com/tsnet"
)

var webhookCmd = &cobra.Command{
	Use:   "webhook",
	Short: "Start webhook server (recommended)",
	Long: `Start the Gale webhook server to receive GitHub events.

This is the recommended mode - instead of polling the GitHub API,
gale receives webhook events when jobs are queued/completed.

Benefits:
  - No API rate limiting issues
  - Instant response to new jobs
  - Lower resource usage

Setup (local):
  1. Configure webhook in GitHub repo/org settings:
     - Payload URL: http://your-server:8080/webhook
     - Content type: application/json
     - Secret: (optional, for signature verification)
     - Events: Workflow jobs

  2. Start gale:
     gale webhook

Setup (Tailscale Funnel - recommended):
  1. Start gale with funnel:
     gale webhook --funnel

  2. Use the provided URL in GitHub webhook settings

Example:
  gale webhook
  gale webhook --port 9000
  gale webhook --funnel              # Use Tailscale Funnel
  gale webhook --funnel --hostname gale  # Custom hostname
  gale webhook --log-level debug`,
	RunE: runWebhook,
}

var (
	webhookPort    int
	useFunnel      bool
	funnelHostname string
)

func init() {
	webhookCmd.Flags().IntVarP(&webhookPort, "port", "p", 0, "port to listen on (default 8080)")
	webhookCmd.Flags().BoolVar(&useFunnel, "funnel", false, "use Tailscale Funnel for public HTTPS endpoint")
	webhookCmd.Flags().StringVar(&funnelHostname, "hostname", "gale", "Tailscale hostname (used with --funnel)")
	rootCmd.AddCommand(webhookCmd)
}

func runWebhook(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if logLevel != "" {
		cfg.LogLevel = logLevel
	}

	if webhookPort > 0 {
		cfg.Webhook.Port = webhookPort
	}

	logger := setupLogger(cfg.LogLevel)

	handler, err := webhook.NewHandler(cfg, logger)
	if err != nil {
		return fmt.Errorf("creating webhook handler: %w", err)
	}
	defer handler.Close()

	mux := http.NewServeMux()
	mux.Handle("/webhook", handler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK - %d active runners\n", handler.GetActiveRunnerCount())
	})

	// Graceful shutdown context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	if useFunnel {
		return runWithFunnel(ctx, cancel, sigCh, mux, logger, cfg)
	}

	return runLocalServer(ctx, cancel, sigCh, mux, logger, cfg)
}

func runLocalServer(ctx context.Context, cancel context.CancelFunc, sigCh chan os.Signal, mux *http.ServeMux, logger interface{ Info(string, ...any) }, cfg *config.Config) error {
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Webhook.Port),
		Handler: mux,
	}

	go func() {
		<-sigCh
		logger.Info("received shutdown signal")
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		server.Shutdown(shutdownCtx)
	}()

	logger.Info("starting webhook server",
		"port", cfg.Webhook.Port,
		"endpoint", fmt.Sprintf("http://0.0.0.0:%d/webhook", cfg.Webhook.Port),
	)

	if cfg.GetWebhookSecret() != "" {
		logger.Info("webhook signature verification enabled")
	} else {
		logger.Info("webhook signature verification disabled (no secret configured)", "warning", true)
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

func runWithFunnel(ctx context.Context, cancel context.CancelFunc, sigCh chan os.Signal, mux *http.ServeMux, logger interface{ Info(string, ...any) }, cfg *config.Config) error {
	// Create tsnet server
	stateDir := filepath.Join(os.TempDir(), "gale-tsnet")
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return fmt.Errorf("creating state dir: %w", err)
	}

	srv := &tsnet.Server{
		Hostname: funnelHostname,
		Dir:      stateDir,
		Logf:     log.Printf,
	}

	logger.Info("starting Tailscale node", "hostname", funnelHostname)

	// Start the tsnet server
	if err := srv.Start(); err != nil {
		return fmt.Errorf("starting tsnet: %w", err)
	}
	defer srv.Close()

	// Wait for Tailscale to be ready
	lc, err := srv.LocalClient()
	if err != nil {
		return fmt.Errorf("getting local client: %w", err)
	}

	// Get status to find our funnel URL
	status, err := lc.Status(ctx)
	if err != nil {
		return fmt.Errorf("getting status: %w", err)
	}

	// Get the DNS name for the funnel URL
	dnsName := status.Self.DNSName
	if dnsName == "" {
		return fmt.Errorf("no DNS name assigned yet - check Tailscale status")
	}

	// Remove trailing dot from DNS name
	if dnsName[len(dnsName)-1] == '.' {
		dnsName = dnsName[:len(dnsName)-1]
	}

	funnelURL := fmt.Sprintf("https://%s/webhook", dnsName)

	// Get funnel listener (HTTPS with auto TLS)
	ln, err := srv.ListenFunnel("tcp", ":443")
	if err != nil {
		return fmt.Errorf("creating funnel listener: %w", err)
	}
	defer ln.Close()

	server := &http.Server{
		Handler: mux,
		TLSConfig: &tls.Config{
			GetCertificate: func(hi *tls.ClientHelloInfo) (*tls.Certificate, error) {
				return lc.GetCertificate(hi)
			},
		},
	}

	go func() {
		<-sigCh
		logger.Info("received shutdown signal")
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		server.Shutdown(shutdownCtx)
	}()

	logger.Info("Tailscale Funnel ready",
		"url", funnelURL,
		"hostname", funnelHostname,
	)
	fmt.Println()
	fmt.Println("┌─────────────────────────────────────────────────────────────┐")
	fmt.Println("│  Gale Webhook Server (Tailscale Funnel)                     │")
	fmt.Println("├─────────────────────────────────────────────────────────────┤")
	fmt.Printf("│  Webhook URL: %-46s │\n", funnelURL)
	fmt.Println("│                                                             │")
	fmt.Println("│  Configure this URL in your GitHub webhook settings:       │")
	fmt.Println("│    - Payload URL: (above URL)                              │")
	fmt.Println("│    - Content type: application/json                        │")
	fmt.Println("│    - Events: Workflow jobs                                 │")
	fmt.Println("└─────────────────────────────────────────────────────────────┘")
	fmt.Println()

	if cfg.GetWebhookSecret() != "" {
		logger.Info("webhook signature verification enabled")
	} else {
		logger.Info("webhook signature verification disabled (no secret configured)", "warning", true)
	}

	// Serve with TLS
	if err := server.ServeTLS(ln, "", ""); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}
