package scaler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/manashmandal/gale/internal/config"
	"github.com/manashmandal/gale/internal/docker"
	"github.com/manashmandal/gale/internal/github"
)

type Scaler struct {
	cfg    *config.Config
	gh     *github.Client
	docker *docker.Client
	logger *slog.Logger

	mu            sync.Mutex
	lastScaleUp   time.Time
	pendingScales int
}

type Stats struct {
	QueuedJobs    int
	ActiveRunners int
	MaxRunners    int
	MinRunners    int
}

func New(cfg *config.Config, logger *slog.Logger) (*Scaler, error) {
	gh := github.NewClient(cfg.GitHub.Token, cfg.GitHub.Owner, cfg.GitHub.Repo)

	dockerClient, err := docker.NewClient(cfg.Docker.Host)
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}

	return &Scaler{
		cfg:    cfg,
		gh:     gh,
		docker: dockerClient,
		logger: logger,
	}, nil
}

func (s *Scaler) Close() error {
	return s.docker.Close()
}

func (s *Scaler) Run(ctx context.Context) error {
	s.logger.Info("starting autoscaler",
		"repo", fmt.Sprintf("%s/%s", s.cfg.GitHub.Owner, s.cfg.GitHub.Repo),
		"max_runners", s.cfg.Scaler.MaxRunners,
		"poll_interval", s.cfg.Scaler.PollInterval,
	)

	// Ensure runner image is available
	s.logger.Info("ensuring runner image is available", "image", s.cfg.Runner.Image)
	if err := s.docker.EnsureImage(ctx, s.cfg.Runner.Image); err != nil {
		return fmt.Errorf("ensuring runner image: %w", err)
	}

	ticker := time.NewTicker(s.cfg.Scaler.PollInterval)
	defer ticker.Stop()

	// Initial reconcile
	s.reconcile(ctx)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("shutting down autoscaler")
			return ctx.Err()
		case <-ticker.C:
			s.reconcile(ctx)
		}
	}
}

func (s *Scaler) reconcile(ctx context.Context) {
	// Cleanup exited runners first
	cleaned, err := s.docker.CleanupExitedRunners(ctx)
	if err != nil {
		s.logger.Warn("failed to cleanup exited runners", "error", err)
	} else if cleaned > 0 {
		s.logger.Info("cleaned up exited runners", "count", cleaned)
	}

	// Get current state
	queuedJobs, err := s.gh.GetQueuedJobs(ctx)
	if err != nil {
		s.logger.Error("failed to get queued jobs", "error", err)
		return
	}

	activeRunners, err := s.docker.GetActiveRunnerCount(ctx)
	if err != nil {
		s.logger.Error("failed to get active runners", "error", err)
		return
	}

	demand := len(queuedJobs)

	s.logger.Debug("reconcile state",
		"queued_jobs", demand,
		"active_runners", activeRunners,
	)

	// Calculate desired runner count
	desired := demand
	if desired < s.cfg.Scaler.MinRunners {
		desired = s.cfg.Scaler.MinRunners
	}
	if desired > s.cfg.Scaler.MaxRunners {
		desired = s.cfg.Scaler.MaxRunners
	}

	// Scale up if needed
	if desired > activeRunners {
		s.scaleUp(ctx, desired-activeRunners)
	}
}

func (s *Scaler) scaleUp(ctx context.Context, count int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Throttle scale-up operations
	if s.cfg.Scaler.ScaleUpDelay > 0 && time.Since(s.lastScaleUp) < s.cfg.Scaler.ScaleUpDelay {
		s.logger.Debug("throttling scale-up", "since_last", time.Since(s.lastScaleUp))
		return
	}

	s.logger.Info("scaling up runners", "count", count)

	repoURL := fmt.Sprintf("https://github.com/%s/%s", s.cfg.GitHub.Owner, s.cfg.GitHub.Repo)

	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			runner, err := s.docker.CreateRunner(ctx, docker.RunnerConfig{
				Image:       s.cfg.Runner.Image,
				Token:       s.cfg.GitHub.Token,
				RepoURL:     repoURL,
				Labels:      s.cfg.Runner.Labels,
				Env:         s.cfg.Runner.Env,
				NetworkMode: s.cfg.Runner.NetworkMode,
			})
			if err != nil {
				s.logger.Error("failed to create runner", "error", err)
				return
			}
			s.logger.Info("created runner", "runner_id", runner.ID, "container_id", runner.ContainerID[:12])
		}()
	}
	wg.Wait()

	s.lastScaleUp = time.Now()
}

func (s *Scaler) GetStats(ctx context.Context) (*Stats, error) {
	queuedJobs, err := s.gh.GetQueuedJobs(ctx)
	if err != nil {
		return nil, err
	}

	activeRunners, err := s.docker.GetActiveRunnerCount(ctx)
	if err != nil {
		return nil, err
	}

	return &Stats{
		QueuedJobs:    len(queuedJobs),
		ActiveRunners: activeRunners,
		MaxRunners:    s.cfg.Scaler.MaxRunners,
		MinRunners:    s.cfg.Scaler.MinRunners,
	}, nil
}
