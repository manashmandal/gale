package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/manashmandal/gale/internal/config"
	"github.com/manashmandal/gale/internal/webhook"
	"github.com/spf13/cobra"
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

Setup:
  1. Configure webhook in GitHub repo/org settings:
     - Payload URL: http://your-server:8080/webhook
     - Content type: application/json
     - Secret: (optional, for signature verification)
     - Events: Workflow jobs

  2. Start gale:
     gale webhook

Example:
  gale webhook
  gale webhook --port 9000
  gale webhook --log-level debug`,
	RunE: runWebhook,
}

var webhookPort int

func init() {
	webhookCmd.Flags().IntVarP(&webhookPort, "port", "p", 0, "port to listen on (default 8080)")
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

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Webhook.Port),
		Handler: mux,
	}

	// Graceful shutdown
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

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

	if cfg.Webhook.Secret != "" {
		logger.Info("webhook signature verification enabled")
	} else {
		logger.Warn("webhook signature verification disabled (no secret configured)")
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}
