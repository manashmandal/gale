package docker

import (
	"context"
	"sync"
	"time"
)

type MockClient struct {
	mu sync.Mutex

	EnsureImageFunc          func(ctx context.Context, imageName string) error
	CreateRunnerFunc         func(ctx context.Context, cfg RunnerConfig) (*Runner, error)
	ListRunnersFunc          func(ctx context.Context) ([]Runner, error)
	GetActiveRunnerCountFunc func(ctx context.Context) (int, error)
	RemoveRunnerFunc         func(ctx context.Context, containerID string) error
	CleanupExitedRunnersFunc func(ctx context.Context) (int, error)
	KillTimedOutRunnersFunc  func(ctx context.Context, timeout time.Duration) ([]Runner, error)
	IsContainerExitedFunc    func(ctx context.Context, containerID string) (bool, error)
	StopRunnerFunc           func(ctx context.Context, containerID string, timeout int) error
	CloseFunc                func() error

	EnsureImageCalls          []string
	CreateRunnerCalls         []RunnerConfig
	ListRunnersCalls          int
	GetActiveRunnerCountCalls int
	RemoveRunnerCalls         []string
	CleanupExitedRunnersCalls int
	KillTimedOutRunnersCalls  int
	IsContainerExitedCalls    []string
	StopRunnerCalls           []string
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

	m.KillTimedOutRunnersFunc = func(ctx context.Context, timeout time.Duration) ([]Runner, error) {
		if timeout <= 0 {
			return nil, nil
		}
		m.mu.Lock()
		var killed []Runner
		cutoff := time.Now().Add(-timeout)
		remaining := []Runner{}
		for _, r := range m.runners {
			if r.Status == "running" && r.StartedAt.Before(cutoff) {
				killed = append(killed, r)
			} else {
				remaining = append(remaining, r)
			}
		}
		m.runners = remaining
		m.mu.Unlock()
		return killed, nil
	}

	m.CloseFunc = func() error {
		return nil
	}

	m.IsContainerExitedFunc = func(ctx context.Context, containerID string) (bool, error) {
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, r := range m.runners {
			if r.ContainerID == containerID {
				return r.Status == "exited", nil
			}
		}
		return true, nil
	}

	m.StopRunnerFunc = func(ctx context.Context, containerID string, timeout int) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		for i, r := range m.runners {
			if r.ContainerID == containerID {
				m.runners[i].Status = "exited"
				break
			}
		}
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

func (m *MockClient) KillTimedOutRunners(ctx context.Context, timeout time.Duration) ([]Runner, error) {
	m.mu.Lock()
	m.KillTimedOutRunnersCalls++
	m.mu.Unlock()
	return m.KillTimedOutRunnersFunc(ctx, timeout)
}

func (m *MockClient) IsContainerExited(ctx context.Context, containerID string) (bool, error) {
	m.mu.Lock()
	m.IsContainerExitedCalls = append(m.IsContainerExitedCalls, containerID)
	m.mu.Unlock()
	return m.IsContainerExitedFunc(ctx, containerID)
}

func (m *MockClient) StopRunner(ctx context.Context, containerID string, timeout int) error {
	m.mu.Lock()
	m.StopRunnerCalls = append(m.StopRunnerCalls, containerID)
	m.mu.Unlock()
	return m.StopRunnerFunc(ctx, containerID, timeout)
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
	m.KillTimedOutRunnersCalls = 0
	m.IsContainerExitedCalls = nil
	m.StopRunnerCalls = nil
	m.CloseCalls = 0
	m.runners = []Runner{}
	m.mu.Unlock()
}

var _ DockerClient = (*MockClient)(nil)
