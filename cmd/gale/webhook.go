package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
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
  gale webhook --funnel              # Use Tailscale Funnel (hostname: gale-<machine>)
  gale webhook --funnel --hostname my-gale  # Custom hostname
  gale webhook --funnel --daemon     # Run in background
  gale webhook --log-level debug`,
	RunE: runWebhook,
}

var (
	webhookPort       int
	useFunnel         bool
	funnelHostname    string
	webhookDaemonMode bool
	requireSignature  bool
)

func init() {
	webhookCmd.Flags().IntVarP(&webhookPort, "port", "p", 0, "port to listen on (default 8080)")
	webhookCmd.Flags().BoolVar(&useFunnel, "funnel", false, "use Tailscale Funnel for public HTTPS endpoint")
	webhookCmd.Flags().StringVar(&funnelHostname, "hostname", "", "Tailscale hostname (default: gale-<machine-hostname>)")
	webhookCmd.Flags().BoolVarP(&webhookDaemonMode, "daemon", "d", false, "run in background (daemon mode)")
	webhookCmd.Flags().BoolVar(&requireSignature, "require-signature", false, "require webhook signature verification (recommended for production)")
	rootCmd.AddCommand(webhookCmd)
}

func getDefaultFunnelHostname() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		return "gale"
	}
	return fmt.Sprintf("gale-%s", hostname)
}

func startWebhookDaemon() error {
	// Build command with same args but without --daemon
	args := []string{"webhook"}
	if useFunnel {
		args = append(args, "--funnel")
	}
	if funnelHostname != "" {
		args = append(args, "--hostname", funnelHostname)
	}
	if webhookPort > 0 {
		args = append(args, "--port", fmt.Sprintf("%d", webhookPort))
	}
	if cfgFile != "config.yaml" {
		args = append(args, "--config", cfgFile)
	}
	if logLevel != "" {
		args = append(args, "--log-level", logLevel)
	}

	// Get the executable path
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("getting executable: %w", err)
	}

	cmd := exec.Command(exe, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil

	// Detach from parent
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting daemon: %w", err)
	}

	fmt.Printf("Gale webhook started in background (PID: %d)\n", cmd.Process.Pid)
	if useFunnel {
		fmt.Printf("Note: Check logs for Tailscale Funnel URL\n")
	}
	fmt.Printf("Use 'gale stop' or 'kill %d' to stop\n", cmd.Process.Pid)
	return nil
}

func runWebhook(cmd *cobra.Command, args []string) error {
	// If daemon mode, fork and exit
	if webhookDaemonMode {
		return startWebhookDaemon()
	}

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

	// Check signature requirement
	if requireSignature && cfg.GetWebhookSecret() == "" {
		return fmt.Errorf("--require-signature is set but no webhook secret is configured; set webhook.secret in config or github.app.webhook_secret for GitHub App mode")
	}

	// Set default funnel hostname if not provided
	if funnelHostname == "" {
		funnelHostname = getDefaultFunnelHostname()
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
		Addr:         fmt.Sprintf(":%d", cfg.Webhook.Port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
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
		logger.Info("⚠️  SECURITY WARNING: webhook signature verification disabled")
		logger.Info("⚠️  Anyone can send forged webhook events. Configure webhook.secret for production use.")
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
	logger.Info("waiting for Tailscale connection (check browser if prompted to authenticate)...")

	// Use Up() which waits for Tailscale to be fully connected
	// This will prompt for authentication on first run
	status, err := srv.Up(ctx)
	if err != nil {
		return fmt.Errorf("connecting to Tailscale: %w", err)
	}
	defer srv.Close()

	// Get the DNS name for the funnel URL
	dnsName := status.Self.DNSName
	if dnsName == "" {
		return fmt.Errorf("no DNS name assigned - ensure Tailscale is properly configured")
	}

	// Remove trailing dot from DNS name
	if dnsName[len(dnsName)-1] == '.' {
		dnsName = dnsName[:len(dnsName)-1]
	}

	funnelURL := fmt.Sprintf("https://%s/webhook", dnsName)

	// Get funnel listener with FunnelOnly() - accepts only public internet traffic
	// Tailscale handles TLS termination at their edge, so we serve plain HTTP
	ln, err := srv.ListenFunnel("tcp", ":443", tsnet.FunnelOnly())
	if err != nil {
		return fmt.Errorf("creating funnel listener: %w", err)
	}
	defer ln.Close()

	server := &http.Server{
		Handler:     mux,
		ReadTimeout: 10 * time.Second,
		IdleTimeout: 120 * time.Second,
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
		logger.Info("⚠️  SECURITY WARNING: webhook signature verification disabled")
		logger.Info("⚠️  Anyone can send forged webhook events. Configure webhook.secret for production use.")
	}

	// Serve plain HTTP - TLS is terminated by Tailscale Funnel
	if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}
