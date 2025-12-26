# GitHub App Support Design

## Overview

Replace PAT-based authentication with a private GitHub App for better security, higher rate limits, and easier multi-repo management.

## Why GitHub App?

| Feature | PAT | GitHub App |
|---------|-----|------------|
| Rate Limit | 5,000/hr shared | 5,000/hr per installation |
| Token Rotation | Manual | Automatic (1hr tokens) |
| Permissions | User-wide | Granular per-app |
| Multi-org | Separate PATs | Single app, multiple installs |
| Security | Long-lived token | Short-lived installation tokens |
| Webhook | Manual per-repo | Automatic via app config |

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    GitHub App (Private)                      │
│                                                             │
│  App ID: 123456                                             │
│  Private Key: gale-app.pem                                  │
│  Webhook URL: https://your-server:8080/webhook              │
│  Permissions: actions:read, metadata:read                   │
│  Events: workflow_job                                       │
└─────────────────────────────────────────────────────────────┘
                              │
                    Install on repos/org
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                   Gale Webhook Server                        │
│                                                             │
│  1. Receive workflow_job event from GitHub                  │
│  2. Verify webhook signature (HMAC-SHA256)                  │
│  3. Extract installation ID from payload                    │
│  4. Generate JWT using App ID + Private Key                 │
│  5. Exchange JWT for Installation Access Token              │
│  6. Create runner container with installation token         │
└─────────────────────────────────────────────────────────────┘
```

## Authentication Flow

```
┌──────────┐      ┌──────────┐      ┌──────────┐
│  Gale    │      │  GitHub  │      │  Runner  │
│  Server  │      │   API    │      │Container │
└────┬─────┘      └────┬─────┘      └────┬─────┘
     │                 │                 │
     │  1. Generate JWT (App ID + Key)   │
     │────────────────>│                 │
     │                 │                 │
     │  2. POST /installations/{id}/access_tokens
     │────────────────>│                 │
     │                 │                 │
     │  3. Installation Token (1hr TTL)  │
     │<────────────────│                 │
     │                 │                 │
     │  4. Create container with token   │
     │────────────────────────────────────>
     │                 │                 │
     │                 │  5. Register runner
     │                 │<────────────────│
     │                 │                 │
```

## Files to Create/Modify

### New Files

**`internal/github/app.go`**
```go
package github

type AppClient struct {
    appID      int64
    privateKey *rsa.PrivateKey

    // Token cache (installation_id -> token)
    tokenCache map[int64]*InstallationToken
    cacheMu    sync.RWMutex
}

// NewAppClient creates client from app credentials
func NewAppClient(appID int64, privateKey []byte) (*AppClient, error)

// GenerateJWT creates JWT for GitHub App authentication
func (a *AppClient) GenerateJWT() (string, error)

// GetInstallationToken returns cached or new installation token
func (a *AppClient) GetInstallationToken(ctx context.Context, installationID int64) (string, error)
```

**`cmd/gale/app.go`**
```go
// gale app create  - Create GitHub App via API (requires PAT)
// gale app setup   - Show manual setup instructions
// gale app validate - Test app configuration
```

### Modified Files

**`internal/config/config.go`**
```go
type GitHubConfig struct {
    // Existing PAT mode
    Token string `yaml:"token"`

    // New App mode
    App GitHubAppConfig `yaml:"app"`
}

type GitHubAppConfig struct {
    AppID          int64  `yaml:"app_id"`
    PrivateKeyPath string `yaml:"private_key_path"`
    WebhookSecret  string `yaml:"webhook_secret"`
}

// IsAppMode returns true if using GitHub App authentication
func (c *Config) IsAppMode() bool
```

**`internal/webhook/handler.go`**
```go
type WorkflowJobEvent struct {
    // Add installation field
    Installation struct {
        ID int64 `json:"id"`
    } `json:"installation"`
}

type Handler struct {
    // Add app client
    appClient *github.AppClient
}
```

## Configuration

### PAT Mode (existing)
```yaml
github:
  token: ${GITHUB_TOKEN}
  owner: manashmandal
```

### App Mode (new)
```yaml
github:
  app:
    app_id: 123456
    private_key_path: /etc/gale/private-key.pem
    # Or use env var: GALE_PRIVATE_KEY
    webhook_secret: ${GALE_WEBHOOK_SECRET}
```

## CLI Commands

```bash
# Auto-create GitHub App (requires initial PAT with admin:org scope)
gale app create --name "my-gale-runner"

# Show manual setup instructions
gale app setup

# Validate app configuration
gale app validate

# Test authentication
gale app test
```

## GitHub App Permissions

Required permissions:
- **Repository permissions:**
  - Actions: Read-only (to see workflow jobs)
  - Metadata: Read-only (required for all apps)

- **Subscribe to events:**
  - Workflow jobs

## Setup Steps (Manual)

1. Go to GitHub → Settings → Developer Settings → GitHub Apps
2. Click "New GitHub App"
3. Configure:
   - **Name:** `gale-runner` (must be unique)
   - **Homepage URL:** `https://github.com/manashmandal/gale`
   - **Webhook URL:** `https://your-server:8080/webhook`
   - **Webhook secret:** Generate random string
   - **Permissions:**
     - Repository → Actions: Read-only
     - Repository → Metadata: Read-only
   - **Subscribe to events:** Workflow jobs
   - **Where can this app be installed:** Only on this account
4. Click "Create GitHub App"
5. Note the **App ID**
6. Click "Generate a private key" → downloads `.pem` file
7. Click "Install App" → Select repositories
8. Configure gale:
   ```yaml
   github:
     app:
       app_id: <your-app-id>
       private_key_path: /path/to/downloaded-key.pem
       webhook_secret: <your-webhook-secret>
   ```
9. Start gale: `gale webhook`

## Token Caching

Installation tokens are valid for 1 hour. The `AppClient` caches tokens:

```go
type InstallationToken struct {
    Token     string
    ExpiresAt time.Time
}

func (a *AppClient) GetInstallationToken(ctx context.Context, installID int64) (string, error) {
    a.cacheMu.RLock()
    if token, ok := a.tokenCache[installID]; ok {
        if time.Now().Add(5 * time.Minute).Before(token.ExpiresAt) {
            a.cacheMu.RUnlock()
            return token.Token, nil
        }
    }
    a.cacheMu.RUnlock()

    // Fetch new token
    return a.refreshToken(ctx, installID)
}
```

## Error Handling

- **Invalid private key:** Clear error message with format expected
- **App not installed:** Guide user to install on repo
- **Token expired:** Auto-refresh from cache
- **Webhook signature mismatch:** Log and reject request

## Migration Path

1. Users can continue using PAT mode (no changes required)
2. To migrate: create app, update config, restart gale
3. Both modes can coexist (auto-detected from config)
