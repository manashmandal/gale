package github

import (
	"context"
	"fmt"

	"github.com/google/go-github/v68/github"
)

type Client struct {
	client *github.Client
	owner  string
	repo   string
}

type QueuedJob struct {
	RunID   int64
	JobID   int64
	JobName string
	Status  string
}

func NewClient(token, owner, repo string) *Client {
	client := github.NewClient(nil).WithAuthToken(token)
	return &Client{
		client: client,
		owner:  owner,
		repo:   repo,
	}
}

func (c *Client) GetQueuedJobs(ctx context.Context) ([]QueuedJob, error) {
	var queuedJobs []QueuedJob

	// Get runs that are queued or in progress
	opts := &github.ListWorkflowRunsOptions{
		Status: "queued",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	queuedRuns, _, err := c.client.Actions.ListRepositoryWorkflowRuns(ctx, c.owner, c.repo, opts)
	if err != nil {
		return nil, fmt.Errorf("listing queued runs: %w", err)
	}

	opts.Status = "in_progress"
	inProgressRuns, _, err := c.client.Actions.ListRepositoryWorkflowRuns(ctx, c.owner, c.repo, opts)
	if err != nil {
		return nil, fmt.Errorf("listing in-progress runs: %w", err)
	}

	allRuns := append(queuedRuns.WorkflowRuns, inProgressRuns.WorkflowRuns...)

	// Get queued jobs from each run
	for _, run := range allRuns {
		jobs, _, err := c.client.Actions.ListWorkflowJobs(ctx, c.owner, c.repo, *run.ID, &github.ListWorkflowJobsOptions{
			Filter: "all",
			ListOptions: github.ListOptions{
				PerPage: 100,
			},
		})
		if err != nil {
			continue // Skip on error, don't fail entire operation
		}

		for _, job := range jobs.Jobs {
			if job.Status != nil && (*job.Status == "queued" || *job.Status == "waiting") {
				qj := QueuedJob{
					RunID:  *run.ID,
					JobID:  *job.ID,
					Status: *job.Status,
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

func (c *Client) GetRunnerRegistrationToken(ctx context.Context) (string, error) {
	token, _, err := c.client.Actions.CreateRegistrationToken(ctx, c.owner, c.repo)
	if err != nil {
		return "", fmt.Errorf("creating registration token: %w", err)
	}
	return *token.Token, nil
}
