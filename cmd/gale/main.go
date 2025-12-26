package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/manashmandal/gale/internal/config"
	"github.com/manashmandal/gale/internal/scaler"
)

var (
	version = "dev"
	commit  = "none"
)

func main() {
	var (
		configPath  = flag.String("config", "config.yaml", "path to config file")
		showVersion = flag.Bool("version", false, "show version")
		logLevel    = flag.String("log-level", "", "log level (debug, info, warn, error)")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("gale %s (%s)\n", version, commit)
		os.Exit(0)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Override log level from flag
	if *logLevel != "" {
		cfg.LogLevel = *logLevel
	}

	logger := setupLogger(cfg.LogLevel)

	s, err := scaler.New(cfg, logger)
	if err != nil {
		logger.Error("failed to create scaler", "error", err)
		os.Exit(1)
	}
	defer s.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		logger.Info("received shutdown signal")
		cancel()
	}()

	if err := s.Run(ctx); err != nil && err != context.Canceled {
		logger.Error("scaler error", "error", err)
		os.Exit(1)
	}
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
