package github

import "context"

// GitHubClient defines the interface for GitHub API operations
// This allows for mocking in tests
type GitHubClient interface {
	IsOrgScope() bool
	SetForceMode(force bool)
	GetRateLimitInfo() (used, limit, threshold int, forceMode bool)
	GetQueuedJobs(ctx context.Context) ([]QueuedJob, error)
	GetRegistrationToken(ctx context.Context, repoFullName string) (string, error)
}

// Ensure Client implements GitHubClient
var _ GitHubClient = (*Client)(nil)
