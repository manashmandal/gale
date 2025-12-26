package github

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/google/go-github/v68/github"
)

type Client struct {
	client *github.Client
	owner  string
	repo   string // empty for org-level
	scope  string // "org" or "repo"
}

type QueuedJob struct {
	RunID   int64
	JobID   int64
	JobName string
	Status  string
	Repo    string // owner/repo format
}

func NewClient(token, owner, repo, scope string) *Client {
	client := github.NewClient(nil).WithAuthToken(token)
	return &Client{
		client: client,
		owner:  owner,
		repo:   repo,
		scope:  scope,
	}
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
	var queuedJobs []QueuedJob
	repoFullName := fmt.Sprintf("%s/%s", owner, repo)

	// Get runs that are queued or in progress
	opts := &github.ListWorkflowRunsOptions{
		Status:      "queued",
		ListOptions: github.ListOptions{PerPage: 100},
	}

	queuedRuns, _, err := c.client.Actions.ListRepositoryWorkflowRuns(ctx, owner, repo, opts)
	if err != nil {
		return nil, fmt.Errorf("listing queued runs: %w", err)
	}

	opts.Status = "in_progress"
	inProgressRuns, _, err := c.client.Actions.ListRepositoryWorkflowRuns(ctx, owner, repo, opts)
	if err != nil {
		return nil, fmt.Errorf("listing in-progress runs: %w", err)
	}

	allRuns := append(queuedRuns.WorkflowRuns, inProgressRuns.WorkflowRuns...)

	for _, run := range allRuns {
		jobs, _, err := c.client.Actions.ListWorkflowJobs(ctx, owner, repo, *run.ID, &github.ListWorkflowJobsOptions{
			Filter:      "all",
			ListOptions: github.ListOptions{PerPage: 100},
		})
		if err != nil {
			continue
		}

		for _, job := range jobs.Jobs {
			if job.Status != nil && (*job.Status == "queued" || *job.Status == "waiting") {
				// Check if job requires self-hosted runner
				if !c.requiresSelfHosted(job) {
					continue
				}

				qj := QueuedJob{
					RunID:  *run.ID,
					JobID:  *job.ID,
					Status: *job.Status,
					Repo:   repoFullName,
				}
				if job.Name != nil {
					qj.JobName = *job.Name
				}
				queuedJobs = append(queuedJobs, qj)
			}
		}
	}

	return queuedJobs, nil
}

func (c *Client) requiresSelfHosted(job *github.WorkflowJob) bool {
	if job.Labels == nil {
		return false
	}
	for _, label := range job.Labels {
		if strings.EqualFold(label, "self-hosted") {
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
	var allRepos []Repo

	// Try to list organization repos first
	opts := &github.RepositoryListByOrgOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	for {
		repos, resp, err := c.client.Repositories.ListByOrg(ctx, c.owner, opts)
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
	var allRepos []Repo

	opts := &github.RepositoryListByUserOptions{
		Type:        "owner",
		ListOptions: github.ListOptions{PerPage: 100},
	}

	for {
		repos, resp, err := c.client.Repositories.ListByUser(ctx, c.owner, opts)
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
