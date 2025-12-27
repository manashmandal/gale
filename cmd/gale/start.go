package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/manashmandal/gale/internal/config"
	"github.com/manashmandal/gale/internal/daemon"
	"github.com/manashmandal/gale/internal/scaler"
	"github.com/spf13/cobra"
)

var (
	forceMode      bool
	daemonMode     bool
	pidFile        string
	defaultPidFile = "/tmp/gale.pid"
)

func getGaleDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/gale"
	}
	return filepath.Join(homeDir, ".gale")
}

func GetLogFilePath() string {
	return filepath.Join(getGaleDir(), "gale.log")
}

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
Use --daemon to run in background mode.

Example:
  gale start
  gale start --daemon            # Run in background
  gale start --config /etc/gale/config.yaml
  gale start --log-level debug
  gale start --force             # Ignore rate limit threshold`,
	RunE: runStart,
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running gale daemon",
	Long:  `Stop the gale daemon if running in background mode.`,
	RunE:  runStop,
}

func init() {
	startCmd.Flags().BoolVarP(&forceMode, "force", "f", false, "ignore rate limit threshold (use with caution)")
	startCmd.Flags().BoolVarP(&daemonMode, "daemon", "d", false, "run in background (daemon mode)")
	startCmd.Flags().StringVar(&pidFile, "pid-file", defaultPidFile, "PID file path for daemon mode")
	stopCmd.Flags().StringVar(&pidFile, "pid-file", defaultPidFile, "PID file path")
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	d := daemon.New(pidFile)

	// Check if already running
	if pid, running := d.IsRunning(); running {
		return fmt.Errorf("gale is already running (PID: %d). Use 'gale stop' first", pid)
	}

	// If daemon mode, fork and exit
	if daemonMode {
		return startDaemon()
	}

	// Run in foreground
	return runScaler(d)
}

func startDaemon() error {
	// Build command with same args but without --daemon
	args := []string{"start"}
	if forceMode {
		args = append(args, "--force")
	}
	if cfgFile != "config.yaml" {
		args = append(args, "--config", cfgFile)
	}
	if logLevel != "" {
		args = append(args, "--log-level", logLevel)
	}
	args = append(args, "--pid-file", pidFile)

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

	fmt.Printf("Gale started in background (PID: %d)\n", cmd.Process.Pid)
	fmt.Printf("Use 'gale stop' to stop the daemon\n")
	fmt.Printf("Use 'gale status' to check status\n")
	return nil
}

func runScaler(d *daemon.Daemon) error {
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

	// Write PID file
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644); err != nil {
		logger.Warn("failed to write PID file", "error", err)
	}
	defer os.Remove(pidFile)

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

func runStop(cmd *cobra.Command, args []string) error {
	d := daemon.New(pidFile)

	pid, running := d.IsRunning()
	if !running {
		fmt.Println("Gale is not running")
		return nil
	}

	if err := d.Stop(); err != nil {
		return fmt.Errorf("stopping daemon: %w", err)
	}

	fmt.Printf("Gale stopped (was PID: %d)\n", pid)
	return nil
}

func setupLogger(level string) *slog.Logger {
	return setupLoggerWithOutput(level, os.Stdout)
}

func setupLoggerWithFile(level string) (*slog.Logger, *os.File, error) {
	logPath := GetLogFilePath()
	if err := os.MkdirAll(filepath.Dir(logPath), 0700); err != nil {
		return nil, nil, fmt.Errorf("creating log directory: %w", err)
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, nil, fmt.Errorf("opening log file: %w", err)
	}

	multiWriter := io.MultiWriter(os.Stdout, logFile)
	return setupLoggerWithOutput(level, multiWriter), logFile, nil
}

func setupDaemonLogger(level string) (*slog.Logger, *os.File, error) {
	logPath := GetLogFilePath()
	if err := os.MkdirAll(filepath.Dir(logPath), 0700); err != nil {
		return nil, nil, fmt.Errorf("creating log directory: %w", err)
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, nil, fmt.Errorf("opening log file: %w", err)
	}

	return setupLoggerWithOutput(level, logFile), logFile, nil
}

func setupLoggerWithOutput(level string, w io.Writer) *slog.Logger {
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

	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: logLevel,
	}))
}
