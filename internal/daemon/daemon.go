package daemon

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
)

func DefaultPidFile() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/gale.pid"
	}
	return filepath.Join(homeDir, ".gale", "gale.pid")
}

type Daemon struct {
	pidFile string
	cancel  context.CancelFunc
}

func New(pidFile string) *Daemon {
	if pidFile == "" {
		pidFile = DefaultPidFile()
	}
	return &Daemon{pidFile: pidFile}
}

// Start runs the given function in a controlled context
// Returns a cancel function to stop the daemon
func (d *Daemon) Start(ctx context.Context, fn func(context.Context) error) (context.CancelFunc, error) {
	// Check if already running
	if pid, running := d.IsRunning(); running {
		return nil, fmt.Errorf("gale is already running (PID: %d)", pid)
	}

	// Write PID file
	if err := d.writePID(); err != nil {
		return nil, fmt.Errorf("writing PID file: %w", err)
	}

	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	d.cancel = cancel

	// Setup signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Run in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- fn(ctx)
	}()

	// Handle signals in goroutine
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
		d.cleanup()
	}()

	return cancel, nil
}

// Stop stops the running daemon by sending SIGTERM
func (d *Daemon) Stop() error {
	pid, running := d.IsRunning()
	if !running {
		return fmt.Errorf("gale is not running")
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("finding process: %w", err)
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("sending signal: %w", err)
	}

	// Remove PID file
	d.cleanup()
	return nil
}

// IsRunning checks if daemon is running, returns PID if running
func (d *Daemon) IsRunning() (int, bool) {
	data, err := os.ReadFile(d.pidFile)
	if err != nil {
		return 0, false
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return 0, false
	}

	// Check if process exists
	process, err := os.FindProcess(pid)
	if err != nil {
		return 0, false
	}

	// On Unix, FindProcess always succeeds, so we need to send signal 0
	if err := process.Signal(syscall.Signal(0)); err != nil {
		// Process doesn't exist, clean up stale PID file
		os.Remove(d.pidFile)
		return 0, false
	}

	return pid, true
}

// GetPID returns the PID of running daemon
func (d *Daemon) GetPID() (int, error) {
	pid, running := d.IsRunning()
	if !running {
		return 0, fmt.Errorf("gale is not running")
	}
	return pid, nil
}

func (d *Daemon) writePID() error {
	dir := filepath.Dir(d.pidFile)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	// Use O_EXCL to prevent symlink attacks - fail if file exists
	_ = os.Remove(d.pidFile)
	f, err := os.OpenFile(d.pidFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(strconv.Itoa(os.Getpid()))
	return err
}

func (d *Daemon) cleanup() {
	os.Remove(d.pidFile)
}
