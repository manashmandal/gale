package runner

import (
	"context"
	"time"
)

type Runner struct {
	ID          string
	ContainerID string // Docker container ID or empty for native
	PID         int    // Native process ID or 0 for Docker
	Status      string
	Repo        string
	StartedAt   time.Time
}

type Config struct {
	Image       string // Docker image (docker mode only)
	Token       string
	RepoURL     string
	OrgName     string
	Scope       string
	Labels      []string
	Env         map[string]string
	NetworkMode string // Docker network mode (docker mode only)
}

type Client interface {
	Close() error
	CreateRunner(ctx context.Context, cfg Config) (*Runner, error)
	ListRunners(ctx context.Context) ([]Runner, error)
	GetActiveRunnerCount(ctx context.Context) (int, error)
	RemoveRunner(ctx context.Context, id string) error
	CleanupExitedRunners(ctx context.Context) (int, error)
	StopRunner(ctx context.Context, id string, timeout int) error
	IsRunnerExited(ctx context.Context, id string) (bool, error)
}
