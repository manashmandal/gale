package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/manashmandal/gale/internal/config"
	"github.com/manashmandal/gale/internal/github"
	"github.com/manashmandal/gale/internal/secret"
	"github.com/manashmandal/gale/internal/webhook"
	"github.com/spf13/cobra"
	"tailscale.com/ipn/ipnstate"
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
	registerOnStart   string
)

func getWebhookPidFile() string {
	return filepath.Join(getGaleDir(), "webhook.pid")
}

var webhookStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running webhook daemon",
	RunE:  runWebhookStop,
}

var webhookRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the webhook daemon",
	Long: `Restart the webhook daemon with the same configuration.

This stops the currently running webhook daemon (if any) and starts a new one.
All flags from the original 'webhook' command are supported.

Example:
  gale webhook restart
  gale webhook restart --funnel`,
	RunE: runWebhookRestart,
}

func init() {
	webhookCmd.Flags().IntVarP(&webhookPort, "port", "p", 0, "port to listen on (default 8080)")
	webhookCmd.Flags().BoolVar(&useFunnel, "funnel", false, "use Tailscale Funnel for public HTTPS endpoint")
	webhookCmd.Flags().StringVar(&funnelHostname, "hostname", "", "Tailscale hostname (default: gale-<machine-hostname>)")
	webhookCmd.Flags().BoolVarP(&webhookDaemonMode, "daemon", "d", false, "run in background (daemon mode)")
	webhookCmd.Flags().BoolVar(&requireSignature, "require-signature", false, "require webhook signature verification (recommended for production)")
	webhookCmd.Flags().StringVar(&registerOnStart, "register", "", "register webhook on repo before starting (requires --funnel)")

	webhookRestartCmd.Flags().IntVarP(&webhookPort, "port", "p", 0, "port to listen on (default 8080)")
	webhookRestartCmd.Flags().BoolVar(&useFunnel, "funnel", false, "use Tailscale Funnel for public HTTPS endpoint")
	webhookRestartCmd.Flags().StringVar(&funnelHostname, "hostname", "", "Tailscale hostname (default: gale-<machine-hostname>)")
	webhookRestartCmd.Flags().BoolVar(&requireSignature, "require-signature", false, "require webhook signature verification (recommended for production)")
	webhookRestartCmd.Flags().StringVar(&registerOnStart, "register", "", "register webhook on repo before starting (requires --funnel)")

	webhookCmd.AddCommand(webhookStopCmd)
	webhookCmd.AddCommand(webhookRestartCmd)
	rootCmd.AddCommand(webhookCmd)
}

func getDefaultFunnelHostname() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		return "gale"
	}
	return fmt.Sprintf("gale-%s", hostname)
}

func writeWebhookPid(pid int) error {
	pidFile := getWebhookPidFile()
	if err := os.MkdirAll(filepath.Dir(pidFile), 0700); err != nil {
		return err
	}
	return os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", pid)), 0600)
}

func removeWebhookPid() {
	os.Remove(getWebhookPidFile())
}

func getWebhookPid() (int, bool) {
	data, err := os.ReadFile(getWebhookPidFile())
	if err != nil {
		return 0, false
	}

	var pid int
	if _, err := fmt.Sscanf(string(data), "%d", &pid); err != nil {
		return 0, false
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return 0, false
	}

	if err := process.Signal(syscall.Signal(0)); err != nil {
		removeWebhookPid()
		return 0, false
	}

	return pid, true
}

func stopWebhookProcess() (int, error) {
	pid, running := getWebhookPid()
	if !running {
		return 0, nil
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return pid, fmt.Errorf("finding process: %w", err)
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		return pid, fmt.Errorf("sending signal: %w", err)
	}

	removeWebhookPid()
	return pid, nil
}

func runWebhookStop(cmd *cobra.Command, args []string) error {
	pid, running := getWebhookPid()
	if !running {
		fmt.Println("Webhook is not running")
		return nil
	}

	if _, err := stopWebhookProcess(); err != nil {
		return fmt.Errorf("stopping webhook: %w", err)
	}

	fmt.Printf("Webhook stopped (was PID: %d)\n", pid)
	return nil
}

func runWebhookRestart(cmd *cobra.Command, args []string) error {
	pid, running := getWebhookPid()
	if running {
		fmt.Printf("Stopping existing webhook (PID: %d)...\n", pid)
		if _, err := stopWebhookProcess(); err != nil {
			return fmt.Errorf("stopping existing webhook: %w", err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	webhookDaemonMode = true
	return startWebhookDaemon()
}

func startWebhookDaemon() error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if logLevel != "" {
		cfg.LogLevel = logLevel
	}

	port := cfg.Webhook.Port
	if webhookPort > 0 {
		port = webhookPort
	}

	hostname := funnelHostname
	if hostname == "" {
		hostname = getDefaultFunnelHostname()
	}

	fmt.Println()
	fmt.Println("┌─────────────────────────────────────────────────────────────┐")
	fmt.Println("│  Gale Webhook Server - Starting in Daemon Mode             │")
	fmt.Println("├─────────────────────────────────────────────────────────────┤")
	fmt.Printf("│  Config:      %-46s │\n", cfgFile)
	fmt.Printf("│  Log Level:   %-46s │\n", cfg.LogLevel)
	if useFunnel {
		fmt.Printf("│  Mode:        %-46s │\n", "Tailscale Funnel")
		fmt.Printf("│  Hostname:    %-46s │\n", hostname)
	} else {
		fmt.Printf("│  Mode:        %-46s │\n", "Local Server")
		fmt.Printf("│  Port:        %-46d │\n", port)
	}
	fmt.Printf("│  Log File:    %-46s │\n", GetLogFilePath())
	fmt.Println("└─────────────────────────────────────────────────────────────┘")
	fmt.Println()

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

	// Open log file for daemon output
	logPath := GetLogFilePath()
	if err := os.MkdirAll(filepath.Dir(logPath), 0700); err != nil {
		return fmt.Errorf("creating log directory: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}

	cmd := exec.Command(exe, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil

	// Detach from parent
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("starting daemon: %w", err)
	}

	if err := writeWebhookPid(cmd.Process.Pid); err != nil {
		fmt.Printf("Warning: failed to write PID file: %v\n", err)
	}

	fmt.Printf("Gale webhook started in background (PID: %d)\n", cmd.Process.Pid)
	fmt.Printf("Use 'gale logs' to view logs\n")
	fmt.Printf("Use 'gale webhook stop' to stop\n")
	fmt.Printf("Use 'gale webhook restart' to restart\n")
	return nil
}

func runWebhook(cmd *cobra.Command, args []string) error {
	// Validate --register flag
	if registerOnStart != "" && !useFunnel {
		return fmt.Errorf("--register requires --funnel to determine the webhook URL")
	}

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
	defer removeWebhookPid()

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
	// ANSI color codes
	const (
		colorReset  = "\033[0m"
		colorRed    = "\033[31m"
		colorGreen  = "\033[32m"
		colorYellow = "\033[33m"
	)

	// Create tsnet server
	stateDir := filepath.Join(os.TempDir(), "gale-tsnet")
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return fmt.Errorf("creating state dir: %w", err)
	}

	fmt.Printf("  • Creating Tailscale node (hostname: %s)...\n", funnelHostname)

	// Suppress tsnet's verbose internal logging
	origLogOutput := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(origLogOutput)

	srv := &tsnet.Server{
		Hostname: funnelHostname,
		Dir:      stateDir,
		Logf:     func(format string, args ...any) {}, // Suppress verbose logs
	}

	fmt.Print("  • Connecting to Tailscale network...")

	// Start the server in a goroutine to handle auth URL
	upDone := make(chan struct{})
	var status *ipnstate.Status
	var upErr error

	go func() {
		status, upErr = srv.Up(ctx)
		close(upDone)
	}()

	// Check for auth URL while waiting for Up() to complete
	authShown := false
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

waitLoop:
	for {
		select {
		case <-upDone:
			break waitLoop
		case <-ticker.C:
			if !authShown {
				lc, err := srv.LocalClient()
				if err == nil {
					st, err := lc.Status(ctx)
					if err == nil && st.AuthURL != "" {
						fmt.Printf("\n\n%s  ✖ Authentication required!%s\n", colorRed, colorReset)
						fmt.Printf("%s    Please visit: %s%s\n\n", colorRed, st.AuthURL, colorReset)
						authShown = true
					}
				}
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if upErr != nil {
		fmt.Printf(" %s✖%s\n", colorRed, colorReset)
		return fmt.Errorf("connecting to Tailscale: %w", upErr)
	}
	fmt.Printf(" %s✓%s\n", colorGreen, colorReset)

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

	fmt.Print("  • Creating Funnel listener...")

	// Get funnel listener with FunnelOnly() - accepts only public internet traffic
	ln, err := srv.ListenFunnel("tcp", ":443", tsnet.FunnelOnly())
	if err != nil {
		fmt.Printf(" %s✖%s\n", colorRed, colorReset)
		return fmt.Errorf("creating funnel listener: %w", err)
	}
	fmt.Printf(" %s✓%s\n", colorGreen, colorReset)
	defer ln.Close()

	server := &http.Server{
		Handler:     mux,
		ReadTimeout: 10 * time.Second,
		IdleTimeout: 120 * time.Second,
	}

	go func() {
		<-sigCh
		fmt.Println("\nReceived shutdown signal, stopping...")
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		server.Shutdown(shutdownCtx)
	}()

	// Start serving in background for validation
	serverErrCh := make(chan error, 1)
	go func() {
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			serverErrCh <- err
		}
	}()

	// Validate funnel is publicly accessible
	fmt.Print("  • Validating public accessibility...")

	healthURL := fmt.Sprintf("https://%s/health", dnsName)
	if err := validateFunnelAccess(ctx, dnsName, healthURL); err != nil {
		fmt.Printf(" %s✖%s\n", colorRed, colorReset)
		fmt.Printf("    %sWarning: Could not verify public access: %v%s\n", colorYellow, err, colorReset)
		fmt.Printf("    The funnel may still work - first request triggers certificate issuance.\n")
	} else {
		fmt.Printf(" %s✓%s\n", colorGreen, colorReset)
	}

	// Register webhook if --register flag was provided
	if registerOnStart != "" {
		fmt.Print("  • Registering webhook on GitHub...")
		if err := registerWebhookOnStart(ctx, cfg, funnelURL); err != nil {
			fmt.Printf(" %s✖%s\n", colorRed, colorReset)
			fmt.Printf("    %sWarning: %v%s\n", colorYellow, err, colorReset)
		} else {
			fmt.Printf(" %s✓%s\n", colorGreen, colorReset)
		}
	}

	fmt.Printf("  • Funnel ready! %s✓%s\n", colorGreen, colorReset)
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
		fmt.Printf("%s✓%s Webhook signature verification enabled\n", colorGreen, colorReset)
	} else {
		fmt.Printf("%s⚠ WARNING:%s Webhook signature verification disabled\n", colorYellow, colorReset)
		fmt.Printf("  Anyone can send forged webhook events. Configure webhook.secret for production.\n")
	}
	fmt.Println()

	// Wait for server error or context cancellation
	select {
	case err := <-serverErrCh:
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
		return nil
	}
}

func registerWebhookOnStart(ctx context.Context, cfg *config.Config, webhookURL string) error {
	if cfg.GitHub.Token == "" {
		return fmt.Errorf("GITHUB_TOKEN or github.token is required")
	}

	webhookSecret := cfg.GetWebhookSecret()
	if webhookSecret == "" {
		var err error
		webhookSecret, err = secret.GenerateWebhookSecret()
		if err != nil {
			return fmt.Errorf("generating secret: %w", err)
		}
		cfg.SetWebhookSecret(webhookSecret)
	}

	ghClient := github.NewClient(cfg.GitHub.Token, cfg.GitHub.Owner, "", cfg.GitHub.Scope)

	owner := cfg.GitHub.Owner
	repoName := registerOnStart
	if strings.Contains(registerOnStart, "/") {
		parts := strings.Split(registerOnStart, "/")
		owner = parts[0]
		repoName = parts[1]
	}

	hooks, err := ghClient.ListRepoWebhooks(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("checking existing webhooks: %w", err)
	}
	for _, hook := range hooks {
		if hook.Config != nil && hook.Config.GetURL() == webhookURL {
			return nil
		}
	}

	reg, err := ghClient.CreateRepoWebhook(ctx, owner, repoName, webhookURL, webhookSecret)
	if err != nil {
		if strings.Contains(err.Error(), "Hook already exists") {
			return nil
		}
		return err
	}

	target := fmt.Sprintf("%s/%s", owner, repoName)
	cfg.AddRegisteredHook(target, config.RegisteredHook{
		ID:        reg.ID,
		URL:       webhookURL,
		CreatedAt: reg.CreatedAt.Format(time.RFC3339),
		Type:      "repo",
	})

	if !cfg.IsRepoMonitored(repoName) {
		cfg.AddRepo(repoName)
	}

	if err := cfg.Save(cfgFile); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	return nil
}

func validateFunnelAccess(ctx context.Context, hostname, healthURL string) error {
	// Resolve public IP via Google DNS to bypass local Tailscale resolution
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, "udp", "8.8.8.8:53")
		},
	}

	ips, err := resolver.LookupIP(ctx, "ip4", hostname)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("DNS resolution failed: %w", err)
	}
	publicIP := ips[0].String()

	// Create HTTP client that uses the public IP
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				// Replace hostname with public IP
				_, port, _ := net.SplitHostPort(addr)
				return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(publicIP, port))
			},
			TLSClientConfig: &tls.Config{
				ServerName: hostname,
			},
		},
	}

	// Retry a few times as certificate might be provisioned on first request
	var lastErr error
	for i := 0; i < 3; i++ {
		req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
		if err != nil {
			return err
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(2 * time.Second)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			return nil
		}
		lastErr = fmt.Errorf("unexpected status: %d", resp.StatusCode)
		time.Sleep(2 * time.Second)
	}

	return lastErr
}
