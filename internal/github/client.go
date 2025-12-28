package github

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v68/github"
)

const (
	DefaultRateLimitThreshold = 2500 // Stop at 2500 of 5000 calls
	DefaultCallsPerMinute     = 5    // Max 5 API calls per minute
)

// ErrRateLimitThreshold is returned when rate limit threshold is reached
var ErrRateLimitThreshold = fmt.Errorf("rate limit threshold reached")

type Client struct {
	client *github.Client
	owner  string
	repo   string // empty for org-level
	scope  string // "org" or "repo"

	mu             sync.RWMutex
	rateLimitUsed  int
	rateLimitLimit int
	threshold      int
	forceMode      bool

	// Local rate limiting (calls per minute)
	callsMu     sync.Mutex
	callTimes   []time.Time
	callsPerMin int
}

type QueuedJob struct {
	RunID   int64
	JobID   int64
	JobName string
	Status  string
	Repo    string // owner/repo format
}

type ClientOptions struct {
	Token       string
	Owner       string
	Repo        string
	Scope       string
	Threshold   int  // Rate limit threshold (default 2500)
	Force       bool // Ignore threshold
	CallsPerMin int  // Max API calls per minute (default 5)
}

func NewClient(token, owner, repo, scope string) *Client {
	return NewClientWithOptions(ClientOptions{
		Token:       token,
		Owner:       owner,
		Repo:        repo,
		Scope:       scope,
		Threshold:   DefaultRateLimitThreshold,
		Force:       false,
		CallsPerMin: DefaultCallsPerMinute,
	})
}

func NewClientWithOptions(opts ClientOptions) *Client {
	client := github.NewClient(nil).WithAuthToken(opts.Token)
	threshold := opts.Threshold
	if threshold <= 0 {
		threshold = DefaultRateLimitThreshold
	}
	callsPerMin := opts.CallsPerMin
	if callsPerMin <= 0 {
		callsPerMin = DefaultCallsPerMinute
	}
	return &Client{
		client:      client,
		owner:       opts.Owner,
		repo:        opts.Repo,
		scope:       opts.Scope,
		threshold:   threshold,
		forceMode:   opts.Force,
		callsPerMin: callsPerMin,
		callTimes:   make([]time.Time, 0, callsPerMin),
	}
}

// waitForRateLimit blocks until we can make another API call (max 5/min)
func (c *Client) waitForRateLimit() {
	c.callsMu.Lock()
	defer c.callsMu.Unlock()

	now := time.Now()
	oneMinuteAgo := now.Add(-time.Minute)

	// Remove calls older than 1 minute
	valid := c.callTimes[:0]
	for _, t := range c.callTimes {
		if t.After(oneMinuteAgo) {
			valid = append(valid, t)
		}
	}
	c.callTimes = valid

	// If at limit, wait until oldest call expires
	if len(c.callTimes) >= c.callsPerMin {
		waitUntil := c.callTimes[0].Add(time.Minute)
		sleepDuration := time.Until(waitUntil)
		if sleepDuration > 0 {
			c.callsMu.Unlock()
			time.Sleep(sleepDuration)
			c.callsMu.Lock()
			// Clean up again after sleeping
			now = time.Now()
			oneMinuteAgo = now.Add(-time.Minute)
			valid = c.callTimes[:0]
			for _, t := range c.callTimes {
				if t.After(oneMinuteAgo) {
					valid = append(valid, t)
				}
			}
			c.callTimes = valid
		}
	}

	// Record this call
	c.callTimes = append(c.callTimes, time.Now())
}

// SetForceMode enables/disables force mode (ignores rate limit threshold)
func (c *Client) SetForceMode(force bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.forceMode = force
}

// GetRateLimitInfo returns current rate limit usage
func (c *Client) GetRateLimitInfo() (used, limit, threshold int, force bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.rateLimitUsed, c.rateLimitLimit, c.threshold, c.forceMode
}

// updateRateLimit updates rate limit info from response
func (c *Client) updateRateLimit(resp *github.Response) {
	if resp == nil || resp.Rate.Limit == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rateLimitUsed = resp.Rate.Limit - resp.Rate.Remaining
	c.rateLimitLimit = resp.Rate.Limit
}

// checkRateLimit returns error if threshold exceeded (unless force mode)
func (c *Client) checkRateLimit() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.forceMode {
		return nil
	}
	if c.rateLimitUsed >= c.threshold {
		return fmt.Errorf("%w: used %d/%d (threshold: %d). Use --force to override",
			ErrRateLimitThreshold, c.rateLimitUsed, c.rateLimitLimit, c.threshold)
	}
	return nil
}

func (c *Client) IsOrgScope() bool {
	return c.scope == "org"
}

// GetQueuedJobs returns all queued jobs across repos (org scope) or single repo
func (c *Client) GetQueuedJobs(ctx context.Context) ([]QueuedJob, error) {
	if c.IsOrgScope() {
		return c.getOrgQueuedJobs(ctx)
	}
	return c.getRepoQueuedJobs(ctx, c.owner, c.repo)
}

func (c *Client) getOrgQueuedJobs(ctx context.Context) ([]QueuedJob, error) {
	repos, err := c.listRepos(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing repos: %w", err)
	}

	var (
		allJobs []QueuedJob
		mu      sync.Mutex
		wg      sync.WaitGroup
	)

	// Check repos in parallel (limit concurrency)
	sem := make(chan struct{}, 10)

	for _, repo := range repos {
		wg.Add(1)
		go func(owner, name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			jobs, err := c.getRepoQueuedJobs(ctx, owner, name)
			if err != nil {
				return // Skip repos with errors
			}

			if len(jobs) > 0 {
				mu.Lock()
				allJobs = append(allJobs, jobs...)
				mu.Unlock()
			}
		}(repo.Owner, repo.Name)
	}

	wg.Wait()
	return allJobs, nil
}

func (c *Client) getRepoQueuedJobs(ctx context.Context, owner, repo string) ([]QueuedJob, error) {
	// Check rate limit before making calls
	if err := c.checkRateLimit(); err != nil {
		return nil, err
	}

	var queuedJobs []QueuedJob
	repoFullName := fmt.Sprintf("%s/%s", owner, repo)

	// Get runs that are queued or in progress
	opts := &github.ListWorkflowRunsOptions{
		Status:      "queued",
		ListOptions: github.ListOptions{PerPage: 100},
	}

	c.waitForRateLimit() // Wait for local rate limit (5/min)
	queuedRuns, resp, err := c.client.Actions.ListRepositoryWorkflowRuns(ctx, owner, repo, opts)
	c.updateRateLimit(resp)
	if err != nil {
		return nil, fmt.Errorf("listing queued runs: %w", err)
	}

	if err := c.checkRateLimit(); err != nil {
		return nil, err
	}

	opts.Status = "in_progress"
	c.waitForRateLimit() // Wait for local rate limit (5/min)
	inProgressRuns, resp, err := c.client.Actions.ListRepositoryWorkflowRuns(ctx, owner, repo, opts)
	c.updateRateLimit(resp)
	if err != nil {
		return nil, fmt.Errorf("listing in-progress runs: %w", err)
	}

	allRuns := append(queuedRuns.WorkflowRuns, inProgressRuns.WorkflowRuns...)

	for _, run := range allRuns {
		if err := c.checkRateLimit(); err != nil {
			return queuedJobs, err // Return what we have so far
		}

		c.waitForRateLimit() // Wait for local rate limit (5/min)
		jobs, resp, err := c.client.Actions.ListWorkflowJobs(ctx, owner, repo, *run.ID, &github.ListWorkflowJobsOptions{
			Filter:      "all",
			ListOptions: github.ListOptions{PerPage: 100},
		})
		c.updateRateLimit(resp)
		if err != nil {
			continue
		}

		for _, job := range jobs.Jobs {
			if job.Status == nil {
				continue
			}
			status := *job.Status
			// Include queued, waiting, and in_progress jobs for visibility
			if status != "queued" && status != "waiting" && status != "in_progress" {
				continue
			}
			// Check if job requires self-hosted runner
			if !c.requiresSelfHosted(job) {
				continue
			}

			qj := QueuedJob{
				RunID:  *run.ID,
				JobID:  *job.ID,
				Status: status,
				Repo:   repoFullName,
			}
			if job.Name != nil {
				qj.JobName = *job.Name
			}
			queuedJobs = append(queuedJobs, qj)
		}
	}

	return queuedJobs, nil
}

func (c *Client) requiresSelfHosted(job *github.WorkflowJob) bool {
	if job.Labels == nil {
		return false
	}
	for _, label := range job.Labels {
		// Match "gale" or "self-hosted" labels
		if strings.EqualFold(label, "gale") || strings.EqualFold(label, "self-hosted") {
			return true
		}
	}
	return false
}

type Repo struct {
	Owner string
	Name  string
}

func (c *Client) listRepos(ctx context.Context) ([]Repo, error) {
	if err := c.checkRateLimit(); err != nil {
		return nil, err
	}

	var allRepos []Repo

	// Try to list organization repos first
	opts := &github.RepositoryListByOrgOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	for {
		if err := c.checkRateLimit(); err != nil {
			return allRepos, err // Return what we have
		}

		c.waitForRateLimit() // Wait for local rate limit (5/min)
		repos, resp, err := c.client.Repositories.ListByOrg(ctx, c.owner, opts)
		c.updateRateLimit(resp)
		if err != nil {
			// If org listing fails, try user repos
			return c.listUserRepos(ctx)
		}

		for _, repo := range repos {
			if !repo.GetArchived() && !repo.GetDisabled() {
				allRepos = append(allRepos, Repo{
					Owner: c.owner,
					Name:  repo.GetName(),
				})
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allRepos, nil
}

func (c *Client) listUserRepos(ctx context.Context) ([]Repo, error) {
	if err := c.checkRateLimit(); err != nil {
		return nil, err
	}

	var allRepos []Repo

	opts := &github.RepositoryListByUserOptions{
		Type:        "owner",
		ListOptions: github.ListOptions{PerPage: 100},
	}

	for {
		if err := c.checkRateLimit(); err != nil {
			return allRepos, err // Return what we have
		}

		c.waitForRateLimit() // Wait for local rate limit (5/min)
		repos, resp, err := c.client.Repositories.ListByUser(ctx, c.owner, opts)
		c.updateRateLimit(resp)
		if err != nil {
			return nil, fmt.Errorf("listing user repos: %w", err)
		}

		for _, repo := range repos {
			if !repo.GetArchived() && !repo.GetDisabled() {
				allRepos = append(allRepos, Repo{
					Owner: c.owner,
					Name:  repo.GetName(),
				})
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allRepos, nil
}

// GetRegistrationToken returns a registration token for org or repo level
func (c *Client) GetRegistrationToken(ctx context.Context, repo string) (string, error) {
	if c.IsOrgScope() {
		token, _, err := c.client.Actions.CreateOrganizationRegistrationToken(ctx, c.owner)
		if err != nil {
			// Fallback: for personal accounts, use repo-level token
			parts := strings.Split(repo, "/")
			if len(parts) == 2 {
				token, _, err = c.client.Actions.CreateRegistrationToken(ctx, parts[0], parts[1])
				if err != nil {
					return "", fmt.Errorf("creating repo registration token: %w", err)
				}
				return *token.Token, nil
			}
			return "", fmt.Errorf("creating org registration token: %w", err)
		}
		return *token.Token, nil
	}

	// Repo-level token
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid repo format: %s", repo)
	}
	token, _, err := c.client.Actions.CreateRegistrationToken(ctx, parts[0], parts[1])
	if err != nil {
		return "", fmt.Errorf("creating registration token: %w", err)
	}
	return *token.Token, nil
}

// WebhookRegistration represents a registered webhook
type WebhookRegistration struct {
	ID        int64
	URL       string
	CreatedAt time.Time
	Active    bool
}

// CreateRepoWebhook registers a webhook on a specific repository
func (c *Client) CreateRepoWebhook(ctx context.Context, owner, repo, webhookURL, secret string) (*WebhookRegistration, error) {
	hook := &github.Hook{
		Name:   github.Ptr("web"),
		Active: github.Ptr(true),
		Events: []string{"workflow_job"},
		Config: &github.HookConfig{
			URL:         github.Ptr(webhookURL),
			ContentType: github.Ptr("json"),
			Secret:      github.Ptr(secret),
			InsecureSSL: github.Ptr("0"),
		},
	}

	c.waitForRateLimit()
	created, resp, err := c.client.Repositories.CreateHook(ctx, owner, repo, hook)
	c.updateRateLimit(resp)
	if err != nil {
		return nil, fmt.Errorf("creating repo webhook: %w", err)
	}

	return &WebhookRegistration{
		ID:        created.GetID(),
		URL:       created.GetURL(),
		CreatedAt: created.GetCreatedAt().Time,
		Active:    created.GetActive(),
	}, nil
}

// CreateOrgWebhook registers a webhook on an organization
func (c *Client) CreateOrgWebhook(ctx context.Context, org, webhookURL, secret string) (*WebhookRegistration, error) {
	hook := &github.Hook{
		Name:   github.Ptr("web"),
		Active: github.Ptr(true),
		Events: []string{"workflow_job"},
		Config: &github.HookConfig{
			URL:         github.Ptr(webhookURL),
			ContentType: github.Ptr("json"),
			Secret:      github.Ptr(secret),
			InsecureSSL: github.Ptr("0"),
		},
	}

	c.waitForRateLimit()
	created, resp, err := c.client.Organizations.CreateHook(ctx, org, hook)
	c.updateRateLimit(resp)
	if err != nil {
		return nil, fmt.Errorf("creating org webhook: %w", err)
	}

	return &WebhookRegistration{
		ID:        created.GetID(),
		URL:       created.GetURL(),
		CreatedAt: created.GetCreatedAt().Time,
		Active:    created.GetActive(),
	}, nil
}

// ListRepoWebhooks lists all webhooks for a repository
func (c *Client) ListRepoWebhooks(ctx context.Context, owner, repo string) ([]*github.Hook, error) {
	c.waitForRateLimit()
	hooks, resp, err := c.client.Repositories.ListHooks(ctx, owner, repo, &github.ListOptions{PerPage: 100})
	c.updateRateLimit(resp)
	if err != nil {
		return nil, fmt.Errorf("listing repo webhooks: %w", err)
	}
	return hooks, nil
}

// ListOrgWebhooks lists all webhooks for an organization
func (c *Client) ListOrgWebhooks(ctx context.Context, org string) ([]*github.Hook, error) {
	c.waitForRateLimit()
	hooks, resp, err := c.client.Organizations.ListHooks(ctx, org, &github.ListOptions{PerPage: 100})
	c.updateRateLimit(resp)
	if err != nil {
		return nil, fmt.Errorf("listing org webhooks: %w", err)
	}
	return hooks, nil
}

// DeleteRepoWebhook removes a webhook from a repository
func (c *Client) DeleteRepoWebhook(ctx context.Context, owner, repo string, hookID int64) error {
	c.waitForRateLimit()
	resp, err := c.client.Repositories.DeleteHook(ctx, owner, repo, hookID)
	c.updateRateLimit(resp)
	if err != nil {
		return fmt.Errorf("deleting repo webhook: %w", err)
	}
	return nil
}

// DeleteOrgWebhook removes a webhook from an organization
func (c *Client) DeleteOrgWebhook(ctx context.Context, org string, hookID int64) error {
	c.waitForRateLimit()
	resp, err := c.client.Organizations.DeleteHook(ctx, org, hookID)
	c.updateRateLimit(resp)
	if err != nil {
		return fmt.Errorf("deleting org webhook: %w", err)
	}
	return nil
}

// GetOwner returns the owner/org configured for this client
func (c *Client) GetOwner() string {
	return c.owner
}
