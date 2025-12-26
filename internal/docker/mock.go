package docker

import (
	"context"
	"sync"
)

type MockClient struct {
	mu sync.Mutex

	EnsureImageFunc          func(ctx context.Context, imageName string) error
	CreateRunnerFunc         func(ctx context.Context, cfg RunnerConfig) (*Runner, error)
	ListRunnersFunc          func(ctx context.Context) ([]Runner, error)
	GetActiveRunnerCountFunc func(ctx context.Context) (int, error)
	RemoveRunnerFunc         func(ctx context.Context, containerID string) error
	CleanupExitedRunnersFunc func(ctx context.Context) (int, error)
	CloseFunc                func() error

	EnsureImageCalls          []string
	CreateRunnerCalls         []RunnerConfig
	ListRunnersCalls          int
	GetActiveRunnerCountCalls int
	RemoveRunnerCalls         []string
	CleanupExitedRunnersCalls int
	CloseCalls                int

	runners []Runner
}

func NewMockClient() *MockClient {
	m := &MockClient{
		runners: []Runner{},
	}

	// Set up default implementations
	m.EnsureImageFunc = func(ctx context.Context, imageName string) error {
		return nil
	}

	m.CreateRunnerFunc = func(ctx context.Context, cfg RunnerConfig) (*Runner, error) {
		runner := &Runner{
			ID:          "mock-runner-id",
			ContainerID: "mock-container-id",
			Status:      "running",
			Repo:        cfg.RepoURL,
		}
		m.mu.Lock()
		m.runners = append(m.runners, *runner)
		m.mu.Unlock()
		return runner, nil
	}

	m.ListRunnersFunc = func(ctx context.Context) ([]Runner, error) {
		m.mu.Lock()
		result := make([]Runner, len(m.runners))
		copy(result, m.runners)
		m.mu.Unlock()
		return result, nil
	}

	m.GetActiveRunnerCountFunc = func(ctx context.Context) (int, error) {
		m.mu.Lock()
		count := 0
		for _, r := range m.runners {
			if r.Status == "running" {
				count++
			}
		}
		m.mu.Unlock()
		return count, nil
	}

	m.RemoveRunnerFunc = func(ctx context.Context, containerID string) error {
		m.mu.Lock()
		for i, r := range m.runners {
			if r.ContainerID == containerID {
				m.runners = append(m.runners[:i], m.runners[i+1:]...)
				break
			}
		}
		m.mu.Unlock()
		return nil
	}

	m.CleanupExitedRunnersFunc = func(ctx context.Context) (int, error) {
		m.mu.Lock()
		cleaned := 0
		remaining := []Runner{}
		for _, r := range m.runners {
			if r.Status == "exited" {
				cleaned++
			} else {
				remaining = append(remaining, r)
			}
		}
		m.runners = remaining
		m.mu.Unlock()
		return cleaned, nil
	}

	m.CloseFunc = func() error {
		return nil
	}

	return m
}

func (m *MockClient) Close() error {
	m.mu.Lock()
	m.CloseCalls++
	m.mu.Unlock()
	return m.CloseFunc()
}

func (m *MockClient) EnsureImage(ctx context.Context, imageName string) error {
	m.mu.Lock()
	m.EnsureImageCalls = append(m.EnsureImageCalls, imageName)
	m.mu.Unlock()
	return m.EnsureImageFunc(ctx, imageName)
}

func (m *MockClient) CreateRunner(ctx context.Context, cfg RunnerConfig) (*Runner, error) {
	m.mu.Lock()
	m.CreateRunnerCalls = append(m.CreateRunnerCalls, cfg)
	m.mu.Unlock()
	return m.CreateRunnerFunc(ctx, cfg)
}

func (m *MockClient) ListRunners(ctx context.Context) ([]Runner, error) {
	m.mu.Lock()
	m.ListRunnersCalls++
	m.mu.Unlock()
	return m.ListRunnersFunc(ctx)
}

func (m *MockClient) GetActiveRunnerCount(ctx context.Context) (int, error) {
	m.mu.Lock()
	m.GetActiveRunnerCountCalls++
	m.mu.Unlock()
	return m.GetActiveRunnerCountFunc(ctx)
}

func (m *MockClient) RemoveRunner(ctx context.Context, containerID string) error {
	m.mu.Lock()
	m.RemoveRunnerCalls = append(m.RemoveRunnerCalls, containerID)
	m.mu.Unlock()
	return m.RemoveRunnerFunc(ctx, containerID)
}

func (m *MockClient) CleanupExitedRunners(ctx context.Context) (int, error) {
	m.mu.Lock()
	m.CleanupExitedRunnersCalls++
	m.mu.Unlock()
	return m.CleanupExitedRunnersFunc(ctx)
}

func (m *MockClient) SetRunners(runners []Runner) {
	m.mu.Lock()
	m.runners = runners
	m.mu.Unlock()
}

func (m *MockClient) GetRunners() []Runner {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]Runner, len(m.runners))
	copy(result, m.runners)
	return result
}

func (m *MockClient) Reset() {
	m.mu.Lock()
	m.EnsureImageCalls = nil
	m.CreateRunnerCalls = nil
	m.ListRunnersCalls = 0
	m.GetActiveRunnerCountCalls = 0
	m.RemoveRunnerCalls = nil
	m.CleanupExitedRunnersCalls = 0
	m.CloseCalls = 0
	m.runners = []Runner{}
	m.mu.Unlock()
}

var _ DockerClient = (*MockClient)(nil)
