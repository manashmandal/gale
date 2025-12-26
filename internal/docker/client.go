package docker

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/google/uuid"
)

const (
	LabelManagedBy = "gale.managed-by"
	LabelRunnerID  = "gale.runner-id"
	LabelRepo      = "gale.repo"
	ManagedByValue = "gale"
)

type Client struct {
	docker *client.Client
}

type RunnerConfig struct {
	Image       string
	Token       string
	RepoURL     string // For repo-level: https://github.com/owner/repo
	OrgName     string // For org-level: just the org name
	Scope       string // "org" or "repo"
	Labels      []string
	Env         map[string]string
	NetworkMode string
}

type Runner struct {
	ID          string
	ContainerID string
	Status      string
	Repo        string
}

func NewClient(host string) (*Client, error) {
	opts := []client.Opt{
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	}
	if host != "" {
		opts = append(opts, client.WithHost(host))
	}

	docker, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}

	return &Client{docker: docker}, nil
}

func (c *Client) Close() error {
	return c.docker.Close()
}

func (c *Client) EnsureImage(ctx context.Context, imageName string) error {
	// Check if image exists locally
	_, _, err := c.docker.ImageInspectWithRaw(ctx, imageName)
	if err == nil {
		return nil // Image exists
	}

	// Pull the image
	reader, err := c.docker.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pulling image %s: %w", imageName, err)
	}
	defer reader.Close()

	// Wait for pull to complete
	_, err = io.Copy(io.Discard, reader)
	return err
}

func (c *Client) CreateRunner(ctx context.Context, cfg RunnerConfig) (*Runner, error) {
	runnerID := uuid.New().String()[:8]
	containerName := fmt.Sprintf("gale-runner-%s", runnerID)

	env := []string{
		fmt.Sprintf("ACCESS_TOKEN=%s", cfg.Token),
		fmt.Sprintf("RUNNER_NAME=%s", containerName),
		fmt.Sprintf("LABELS=%s", strings.Join(cfg.Labels, ",")),
		"EPHEMERAL=true",
		"DISABLE_AUTO_UPDATE=true",
		"RUNNER_WORKDIR=/tmp/runner/work",
	}

	// Set scope-specific environment variables
	if cfg.Scope == "org" && cfg.OrgName != "" {
		env = append(env, fmt.Sprintf("ORG_NAME=%s", cfg.OrgName))
		env = append(env, "RUNNER_SCOPE=org")
	} else {
		env = append(env, fmt.Sprintf("REPO_URL=%s", cfg.RepoURL))
		env = append(env, "RUNNER_SCOPE=repo")
	}

	// Add custom env vars
	for k, v := range cfg.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Determine repo label
	repoLabel := cfg.RepoURL
	if cfg.Scope == "org" {
		repoLabel = cfg.OrgName // For org scope, store org name
	}

	containerConfig := &container.Config{
		Image:  cfg.Image,
		Env:    env,
		Labels: map[string]string{
			LabelManagedBy: ManagedByValue,
			LabelRunnerID:  runnerID,
			LabelRepo:      repoLabel,
		},
	}

	hostConfig := &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: "/var/run/docker.sock",
				Target: "/var/run/docker.sock",
			},
		},
		RestartPolicy: container.RestartPolicy{
			Name: container.RestartPolicyDisabled,
		},
	}

	if cfg.NetworkMode != "" {
		hostConfig.NetworkMode = container.NetworkMode(cfg.NetworkMode)
	}

	resp, err := c.docker.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, containerName)
	if err != nil {
		return nil, fmt.Errorf("creating container: %w", err)
	}

	if err := c.docker.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		// Cleanup on failure
		_ = c.docker.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		return nil, fmt.Errorf("starting container: %w", err)
	}

	return &Runner{
		ID:          runnerID,
		ContainerID: resp.ID,
		Status:      "running",
	}, nil
}

func (c *Client) ListRunners(ctx context.Context) ([]Runner, error) {
	f := filters.NewArgs()
	f.Add("label", fmt.Sprintf("%s=%s", LabelManagedBy, ManagedByValue))

	containers, err := c.docker.ContainerList(ctx, container.ListOptions{
		Filters: f,
		All:     true,
	})
	if err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}

	runners := make([]Runner, 0, len(containers))
	for _, c := range containers {
		runners = append(runners, Runner{
			ID:          c.Labels[LabelRunnerID],
			ContainerID: c.ID,
			Status:      c.State,
			Repo:        c.Labels[LabelRepo],
		})
	}

	return runners, nil
}

func (c *Client) GetActiveRunnerCount(ctx context.Context) (int, error) {
	runners, err := c.ListRunners(ctx)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, r := range runners {
		if r.Status == "running" {
			count++
		}
	}
	return count, nil
}

func (c *Client) RemoveRunner(ctx context.Context, containerID string) error {
	return c.docker.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force: true,
	})
}

func (c *Client) CleanupExitedRunners(ctx context.Context) (int, error) {
	runners, err := c.ListRunners(ctx)
	if err != nil {
		return 0, err
	}

	cleaned := 0
	for _, r := range runners {
		if r.Status == "exited" {
			if err := c.RemoveRunner(ctx, r.ContainerID); err == nil {
				cleaned++
			}
		}
	}
	return cleaned, nil
}
