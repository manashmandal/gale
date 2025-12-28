package runner

import (
	"context"

	"github.com/manashmandal/gale/internal/docker"
)

type DockerAdapter struct {
	client docker.DockerClient
}

func NewDockerAdapter(client docker.DockerClient) *DockerAdapter {
	return &DockerAdapter{client: client}
}

func (a *DockerAdapter) Close() error {
	return a.client.Close()
}

func (a *DockerAdapter) CreateRunner(ctx context.Context, cfg Config) (*Runner, error) {
	dockerCfg := docker.RunnerConfig{
		Image:       cfg.Image,
		Token:       cfg.Token,
		RepoURL:     cfg.RepoURL,
		OrgName:     cfg.OrgName,
		Scope:       cfg.Scope,
		Labels:      cfg.Labels,
		Env:         cfg.Env,
		NetworkMode: cfg.NetworkMode,
	}

	r, err := a.client.CreateRunner(ctx, dockerCfg)
	if err != nil {
		return nil, err
	}

	return &Runner{
		ID:          r.ID,
		ContainerID: r.ContainerID,
		Status:      r.Status,
		Repo:        r.Repo,
		StartedAt:   r.StartedAt,
	}, nil
}

func (a *DockerAdapter) ListRunners(ctx context.Context) ([]Runner, error) {
	dockerRunners, err := a.client.ListRunners(ctx)
	if err != nil {
		return nil, err
	}

	runners := make([]Runner, len(dockerRunners))
	for i, r := range dockerRunners {
		runners[i] = Runner{
			ID:          r.ID,
			ContainerID: r.ContainerID,
			Status:      r.Status,
			Repo:        r.Repo,
			StartedAt:   r.StartedAt,
		}
	}
	return runners, nil
}

func (a *DockerAdapter) GetActiveRunnerCount(ctx context.Context) (int, error) {
	return a.client.GetActiveRunnerCount(ctx)
}

func (a *DockerAdapter) RemoveRunner(ctx context.Context, id string) error {
	return a.client.RemoveRunner(ctx, id)
}

func (a *DockerAdapter) CleanupExitedRunners(ctx context.Context) (int, error) {
	return a.client.CleanupExitedRunners(ctx)
}

func (a *DockerAdapter) StopRunner(ctx context.Context, id string, timeout int) error {
	return a.client.StopRunner(ctx, id, timeout)
}

func (a *DockerAdapter) IsRunnerExited(ctx context.Context, id string) (bool, error) {
	return a.client.IsContainerExited(ctx, id)
}

var _ Client = (*DockerAdapter)(nil)
