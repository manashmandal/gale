package runner

import (
	"context"

	"github.com/manashmandal/gale/internal/native"
)

type NativeAdapter struct {
	client *native.Client
}

func NewNativeAdapter(client *native.Client) *NativeAdapter {
	return &NativeAdapter{client: client}
}

func (a *NativeAdapter) Close() error {
	return a.client.Close()
}

func (a *NativeAdapter) CreateRunner(ctx context.Context, cfg Config) (*Runner, error) {
	nativeCfg := native.RunnerConfig{
		Token:   cfg.Token,
		RepoURL: cfg.RepoURL,
		OrgName: cfg.OrgName,
		Scope:   cfg.Scope,
		Labels:  cfg.Labels,
		Env:     cfg.Env,
	}

	r, err := a.client.CreateRunner(ctx, nativeCfg)
	if err != nil {
		return nil, err
	}

	return &Runner{
		ID:        r.ID,
		PID:       r.PID,
		Status:    r.Status,
		Repo:      r.Repo,
		StartedAt: r.StartedAt,
	}, nil
}

func (a *NativeAdapter) ListRunners(ctx context.Context) ([]Runner, error) {
	nativeRunners, err := a.client.ListRunners(ctx)
	if err != nil {
		return nil, err
	}

	runners := make([]Runner, len(nativeRunners))
	for i, r := range nativeRunners {
		runners[i] = Runner{
			ID:        r.ID,
			PID:       r.PID,
			Status:    r.Status,
			Repo:      r.Repo,
			StartedAt: r.StartedAt,
		}
	}
	return runners, nil
}

func (a *NativeAdapter) GetActiveRunnerCount(ctx context.Context) (int, error) {
	return a.client.GetActiveRunnerCount(ctx)
}

func (a *NativeAdapter) RemoveRunner(ctx context.Context, id string) error {
	return a.client.RemoveRunner(ctx, id)
}

func (a *NativeAdapter) CleanupExitedRunners(ctx context.Context) (int, error) {
	return a.client.CleanupExitedRunners(ctx)
}

func (a *NativeAdapter) StopRunner(ctx context.Context, id string, timeout int) error {
	return a.client.StopRunner(ctx, id, timeout)
}

func (a *NativeAdapter) IsRunnerExited(ctx context.Context, id string) (bool, error) {
	return a.client.IsRunnerExited(ctx, id)
}

var _ Client = (*NativeAdapter)(nil)
