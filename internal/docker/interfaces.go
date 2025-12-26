package docker

import "context"

// DockerClient defines the interface for Docker operations
// This allows for mocking in tests
type DockerClient interface {
	Close() error
	EnsureImage(ctx context.Context, imageName string) error
	CreateRunner(ctx context.Context, cfg RunnerConfig) (*Runner, error)
	ListRunners(ctx context.Context) ([]Runner, error)
	GetActiveRunnerCount(ctx context.Context) (int, error)
	RemoveRunner(ctx context.Context, containerID string) error
	CleanupExitedRunners(ctx context.Context) (int, error)
}

// Ensure Client implements DockerClient
var _ DockerClient = (*Client)(nil)
