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
	gh := github.NewClient(cfg.GitHub.Token, cfg.GitHub.Owner, cfg.GitHub.Repo, cfg.GitHub.Scope)

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
	scope := "repo"
	target := fmt.Sprintf("%s/%s", s.cfg.GitHub.Owner, s.cfg.GitHub.Repo)
	if s.cfg.IsOrgScope() {
		scope = "org"
		target = s.cfg.GitHub.Owner
	}

	s.logger.Info("starting autoscaler",
		"scope", scope,
		"target", target,
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
		// Still maintain minimum runners even if we can't fetch jobs
		s.ensureMinRunners(ctx)
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
		"min_runners", s.cfg.Scaler.MinRunners,
	)

	if demand > 0 {
		// Log which repos have queued jobs
		repoJobs := make(map[string]int)
		for _, job := range queuedJobs {
			repoJobs[job.Repo]++
		}
		for repo, count := range repoJobs {
			s.logger.Debug("queued jobs by repo", "repo", repo, "count", count)
		}
	}

	// Calculate desired runner count
	// At minimum, maintain min_runners (warm pool)
	desired := demand
	if desired < s.cfg.Scaler.MinRunners {
		desired = s.cfg.Scaler.MinRunners
	}
	if desired > s.cfg.Scaler.MaxRunners {
		desired = s.cfg.Scaler.MaxRunners
	}

	// Scale up if needed (either for jobs or to maintain minimum)
	if desired > activeRunners {
		toCreate := desired - activeRunners
		if demand > 0 {
			s.scaleUp(ctx, toCreate, queuedJobs)
		} else {
			// Creating warm pool runners (no specific jobs)
			s.logger.Info("maintaining warm pool", "current", activeRunners, "target", desired)
			s.scaleUp(ctx, toCreate, nil)
		}
	}
}

// ensureMinRunners maintains the minimum runner count even when job fetching fails
func (s *Scaler) ensureMinRunners(ctx context.Context) {
	if s.cfg.Scaler.MinRunners == 0 {
		return
	}

	activeRunners, err := s.docker.GetActiveRunnerCount(ctx)
	if err != nil {
		s.logger.Error("failed to get active runners", "error", err)
		return
	}

	if activeRunners < s.cfg.Scaler.MinRunners {
		toCreate := s.cfg.Scaler.MinRunners - activeRunners
		s.logger.Info("maintaining minimum runners", "current", activeRunners, "min", s.cfg.Scaler.MinRunners)
		s.scaleUp(ctx, toCreate, nil)
	}
}

func (s *Scaler) scaleUp(ctx context.Context, count int, jobs []github.QueuedJob) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Throttle scale-up operations
	if s.cfg.Scaler.ScaleUpDelay > 0 && time.Since(s.lastScaleUp) < s.cfg.Scaler.ScaleUpDelay {
		s.logger.Debug("throttling scale-up", "since_last", time.Since(s.lastScaleUp))
		return
	}

	s.logger.Info("scaling up runners", "count", count)

	var wg sync.WaitGroup

	// Spawn runners - for repo scope, spawn for specific repos
	// For org scope, just spawn generic org runners
	for i := 0; i < count; i++ {
		wg.Add(1)

		// Determine which repo this runner is for
		var targetRepo string
		if jobs != nil && i < len(jobs) {
			targetRepo = jobs[i].Repo
		} else if jobs != nil && len(jobs) > 0 {
			targetRepo = jobs[0].Repo
		} else if s.cfg.GitHub.Repo != "" {
			// Warm pool runner for specific repo
			targetRepo = fmt.Sprintf("%s/%s", s.cfg.GitHub.Owner, s.cfg.GitHub.Repo)
		} else {
			// Warm pool runner for org (will use org-level registration)
			targetRepo = s.cfg.GitHub.Owner
		}

		go func(repo string) {
			defer wg.Done()

			cfg := docker.RunnerConfig{
				Image:       s.cfg.Runner.Image,
				Token:       s.cfg.GitHub.Token,
				Labels:      s.cfg.Runner.Labels,
				Env:         s.cfg.Runner.Env,
				NetworkMode: s.cfg.Runner.NetworkMode,
				Scope:       s.cfg.GitHub.Scope,
			}

			if s.cfg.IsOrgScope() {
				cfg.OrgName = s.cfg.GitHub.Owner
			} else {
				cfg.RepoURL = fmt.Sprintf("https://github.com/%s", repo)
			}

			runner, err := s.docker.CreateRunner(ctx, cfg)
			if err != nil {
				s.logger.Error("failed to create runner", "error", err, "target", repo)
				return
			}

			logMsg := "created runner"
			if jobs == nil {
				logMsg = "created warm pool runner"
			}
			s.logger.Info(logMsg,
				"runner_id", runner.ID,
				"container_id", runner.ContainerID[:12],
				"target", repo,
			)
		}(targetRepo)
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
