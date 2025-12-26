package github

import (
	"context"
	"sync"
)

type MockGitHubClient struct {
	mu sync.Mutex

	IsOrgScopeFunc           func() bool
	SetForceModeFunc         func(force bool)
	GetRateLimitInfoFunc     func() (used, limit, threshold int, forceMode bool)
	GetQueuedJobsFunc        func(ctx context.Context) ([]QueuedJob, error)
	GetRegistrationTokenFunc func(ctx context.Context, repoFullName string) (string, error)

	IsOrgScopeCalls           int
	SetForceModeCalls         []bool
	GetRateLimitInfoCalls     int
	GetQueuedJobsCalls        int
	GetRegistrationTokenCalls []string

	orgScope           bool
	forceMode          bool
	queuedJobs         []QueuedJob
	rateLimitUsed      int
	rateLimitLimit     int
	rateLimitThreshold int
}

func NewMockGitHubClient() *MockGitHubClient {
	m := &MockGitHubClient{
		queuedJobs:         []QueuedJob{},
		rateLimitLimit:     5000,
		rateLimitThreshold: 2500,
	}

	// Set up default implementations
	m.IsOrgScopeFunc = func() bool {
		return m.orgScope
	}

	m.SetForceModeFunc = func(force bool) {
		m.forceMode = force
	}

	m.GetRateLimitInfoFunc = func() (used, limit, threshold int, forceMode bool) {
		return m.rateLimitUsed, m.rateLimitLimit, m.rateLimitThreshold, m.forceMode
	}

	m.GetQueuedJobsFunc = func(ctx context.Context) ([]QueuedJob, error) {
		return m.queuedJobs, nil
	}

	m.GetRegistrationTokenFunc = func(ctx context.Context, repoFullName string) (string, error) {
		return "mock-registration-token", nil
	}

	return m
}

func (m *MockGitHubClient) IsOrgScope() bool {
	m.mu.Lock()
	m.IsOrgScopeCalls++
	m.mu.Unlock()
	return m.IsOrgScopeFunc()
}

func (m *MockGitHubClient) SetForceMode(force bool) {
	m.mu.Lock()
	m.SetForceModeCalls = append(m.SetForceModeCalls, force)
	m.mu.Unlock()
	m.SetForceModeFunc(force)
}

func (m *MockGitHubClient) GetRateLimitInfo() (used, limit, threshold int, forceMode bool) {
	m.mu.Lock()
	m.GetRateLimitInfoCalls++
	m.mu.Unlock()
	return m.GetRateLimitInfoFunc()
}

func (m *MockGitHubClient) GetQueuedJobs(ctx context.Context) ([]QueuedJob, error) {
	m.mu.Lock()
	m.GetQueuedJobsCalls++
	m.mu.Unlock()
	return m.GetQueuedJobsFunc(ctx)
}

func (m *MockGitHubClient) GetRegistrationToken(ctx context.Context, repoFullName string) (string, error) {
	m.mu.Lock()
	m.GetRegistrationTokenCalls = append(m.GetRegistrationTokenCalls, repoFullName)
	m.mu.Unlock()
	return m.GetRegistrationTokenFunc(ctx, repoFullName)
}

func (m *MockGitHubClient) SetOrgScope(orgScope bool) {
	m.mu.Lock()
	m.orgScope = orgScope
	m.mu.Unlock()
}

func (m *MockGitHubClient) SetQueuedJobs(jobs []QueuedJob) {
	m.mu.Lock()
	m.queuedJobs = jobs
	m.mu.Unlock()
}

func (m *MockGitHubClient) SetRateLimitInfo(used, limit, threshold int) {
	m.mu.Lock()
	m.rateLimitUsed = used
	m.rateLimitLimit = limit
	m.rateLimitThreshold = threshold
	m.mu.Unlock()
}

func (m *MockGitHubClient) Reset() {
	m.mu.Lock()
	m.IsOrgScopeCalls = 0
	m.SetForceModeCalls = nil
	m.GetRateLimitInfoCalls = 0
	m.GetQueuedJobsCalls = 0
	m.GetRegistrationTokenCalls = nil
	m.queuedJobs = []QueuedJob{}
	m.orgScope = false
	m.forceMode = false
	m.rateLimitUsed = 0
	m.mu.Unlock()
}

var _ GitHubClient = (*MockGitHubClient)(nil)
