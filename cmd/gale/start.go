package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/manashmandal/gale/internal/config"
	"github.com/manashmandal/gale/internal/scaler"
	"github.com/spf13/cobra"
)

var (
	forceMode bool
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the autoscaler",
	Long: `Start the Gale autoscaler to monitor GitHub Actions and spawn runners.

The autoscaler will:
  - Poll GitHub for queued self-hosted jobs
  - Spawn Docker-based runners on demand
  - Clean up exited runners automatically
  - Stop when API rate limit threshold (2500 calls) is reached

Use --force to ignore the rate limit threshold (not recommended).

Example:
  gale start
  gale start --config /etc/gale/config.yaml
  gale start --log-level debug
  gale start --force  # Ignore rate limit threshold`,
	RunE: runStart,
}

func init() {
	startCmd.Flags().BoolVarP(&forceMode, "force", "f", false, "ignore rate limit threshold (use with caution)")
	rootCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if logLevel != "" {
		cfg.LogLevel = logLevel
	}

	logger := setupLogger(cfg.LogLevel)

	opts := scaler.Options{
		Force: forceMode,
	}

	if forceMode {
		logger.Warn("running in force mode - rate limit threshold will be ignored")
	} else {
		logger.Info("rate limit protection enabled", "threshold", cfg.Scaler.RateLimitThreshold)
	}

	s, err := scaler.NewWithOptions(cfg, logger, opts)
	if err != nil {
		return fmt.Errorf("creating scaler: %w", err)
	}
	defer s.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		logger.Info("received shutdown signal")
		cancel()
	}()

	if err := s.Run(ctx); err != nil && err != context.Canceled {
		return fmt.Errorf("scaler error: %w", err)
	}

	return nil
}

func setupLogger(level string) *slog.Logger {
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
}
