package github

import (
	"context"

	"github.com/google/go-github/v68/github"
)

// GitHubClient defines the interface for GitHub API operations
// This allows for mocking in tests
type GitHubClient interface {
	IsOrgScope() bool
	SetForceMode(force bool)
	GetRateLimitInfo() (used, limit, threshold int, forceMode bool)
	GetQueuedJobs(ctx context.Context) ([]QueuedJob, error)
	GetRegistrationToken(ctx context.Context, repoFullName string) (string, error)
	GetOwner() string

	// Webhook management
	CreateRepoWebhook(ctx context.Context, owner, repo, webhookURL, secret string) (*WebhookRegistration, error)
	CreateOrgWebhook(ctx context.Context, org, webhookURL, secret string) (*WebhookRegistration, error)
	ListRepoWebhooks(ctx context.Context, owner, repo string) ([]*github.Hook, error)
	ListOrgWebhooks(ctx context.Context, org string) ([]*github.Hook, error)
	DeleteRepoWebhook(ctx context.Context, owner, repo string, hookID int64) error
	DeleteOrgWebhook(ctx context.Context, org string, hookID int64) error
}

// Ensure Client implements GitHubClient
var _ GitHubClient = (*Client)(nil)
