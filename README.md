# Gale

A JIT (Just-In-Time) autoscaler for GitHub Actions self-hosted runners. Gale monitors your GitHub repositories for queued jobs and dynamically spawns Docker-based runners on demand.

## Features

- **JIT Scaling**: Runners are created only when jobs are queued, saving resources
- **Multi-Repo Support**: Monitor all repositories under a user/organization or a single repo
- **Ephemeral Runners**: Runners automatically exit after completing a job
- **Auto Cleanup**: Exited containers are automatically removed
- **Docker-based**: Uses the popular `myoung34/github-runner` image
- **Configurable**: Set max runners, poll intervals, labels, and more
- **Webhook Mode**: Event-driven scaling via GitHub webhooks (recommended) *(untested)*
- **GitHub App Support**: Better security with auto-rotating tokens and higher rate limits *(untested)*
- **Multi-Repo Support**: Select specific repositories to monitor

## Installation

### From Source

```bash
git clone https://github.com/manashmandal/gale.git
cd gale
go build -o bin/gale ./cmd/gale
```

### Requirements

- Go 1.21+
- Docker
- GitHub Personal Access Token with `repo` scope

## Quick Start

1. Run the interactive setup:
```bash
./bin/gale init
```

2. Start the autoscaler:
```bash
./bin/gale start
```

3. Trigger a workflow with `runs-on: gale` - gale will automatically spawn a runner!

### Manual Setup

```bash
export GITHUB_TOKEN="ghp_xxxxxxxxxxxx"
export GITHUB_OWNER="your-username"

# Monitor all repos (org mode)
./bin/gale start

# Or monitor a single repo
export GITHUB_REPO="your-repo"
./bin/gale start
```

## Configuration

Gale uses a YAML configuration file with environment variable expansion:

```yaml
# config.yaml
github:
  token: ${GITHUB_TOKEN}
  owner: ${GITHUB_OWNER}
  repos: []                   # Empty = all repos, or list specific repos
  # repos:
  #   - my-project
  #   - another-repo
  scope: ${GITHUB_SCOPE:-}    # "org", "repo", or "repos" (auto-detected)

docker:
  host: ""                    # Default: unix:///var/run/docker.sock

scaler:
  min_runners: 0              # Minimum runners to keep alive
  max_runners: 10             # Maximum concurrent runners
  poll_interval: 10s          # How often to check for jobs
  scale_up_delay: 5s          # Throttle between scale-up operations

runner:
  image: myoung34/github-runner:latest
  labels:
    - self-hosted
    - linux
    - x64
    - docker
  env: {}                     # Additional environment variables
  network_mode: ""            # Docker network mode

log_level: info               # debug, info, warn, error
```

### Environment Variables

| Variable | Description | Required |
|----------|-------------|----------|
| `GITHUB_TOKEN` | Personal Access Token with `repo` scope | Yes |
| `GITHUB_OWNER` | Username or organization name | Yes |
| `GITHUB_REPO` | Repository name (empty = all repos) | No |
| `GITHUB_SCOPE` | `org` or `repo` (auto-detected) | No |

## CLI Commands

### `gale init` - Interactive Setup

```bash
gale init                    # Start setup wizard
gale init --force           # Overwrite existing config
```

### `gale start` - Start Autoscaler

```bash
gale start                   # Start with default config
gale start -c /etc/gale.yaml # Use custom config
gale start -l debug          # Enable debug logging
```

### `gale status` - Show Status

```bash
gale status                  # Show config, runners, and queued jobs
```

### `gale runners` - Manage Runners

```bash
gale runners list            # List running containers
gale runners list --all      # Include exited containers
gale runners clean           # Remove exited containers
gale runners stop            # Stop all runners
```

### `gale config` - Manage Configuration

```bash
gale config show             # Display current config
gale config validate         # Check config for errors
gale config set KEY VALUE    # Set a config value
gale config path             # Show config file path

# Examples:
gale config set scaler.max_runners 20
gale config set github.scope org
gale config set log_level debug
```

### `gale pool` - Warm Pool Management

```bash
gale pool                    # Show current warm pool size
gale pool 3                  # Keep 3 runners always running
gale pool 0                  # Scale to zero when idle (default)
```

A warm pool keeps runners pre-started and ready to pick up jobs immediately, eliminating cold start time. Set to 0 (default) to scale to zero when there are no queued jobs.

### `gale version` - Show Version

```bash
gale version
```

### `gale webhook` - Webhook Server (Recommended)

```bash
gale webhook                 # Start webhook server
gale webhook --port 9000     # Use custom port
gale webhook -l debug        # Enable debug logging
```

Webhook mode receives GitHub events directly instead of polling the API. Benefits:
- No API rate limiting issues
- Instant response to new jobs
- Lower resource usage

### `gale app` - GitHub App Management

```bash
gale app setup              # Show setup instructions
gale app validate           # Validate app configuration
gale app create             # Interactive app creation wizard
```

### `gale repo` - Repository Management

```bash
gale repo list              # List monitored repos
gale repo add <repo>        # Add a repo to monitor
gale repo add repo1 repo2   # Add multiple repos
gale repo remove <repo>     # Stop monitoring a repo
gale repo set repo1 repo2   # Set exact list of repos
gale repo clear             # Monitor all repos (default)
```

When specific repos are configured, Gale only responds to events from those repos.

## Usage Examples

### Example: Single Repository

```bash
export GITHUB_TOKEN="ghp_xxxx"
export GITHUB_OWNER="manashmandal"
export GITHUB_REPO="my-project"

./bin/gale --log-level debug
```

### Example: All Repositories (Org Mode)

```bash
export GITHUB_TOKEN="ghp_xxxx"
export GITHUB_OWNER="manashmandal"

./bin/gale --log-level debug
```

## How It Works

```
┌─────────────────────────────────────────────────────────────┐
│                         Gale                                │
│                                                             │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐     │
│  │   GitHub    │    │   Scaler    │    │   Docker    │     │
│  │   Client    │───▶│   Logic     │───▶│   Client    │     │
│  └─────────────┘    └─────────────┘    └─────────────┘     │
│        │                   │                  │             │
│        ▼                   ▼                  ▼             │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐     │
│  │ Poll for    │    │ Calculate   │    │ Create/     │     │
│  │ queued jobs │    │ desired     │    │ Remove      │     │
│  │ across repos│    │ runner count│    │ containers  │     │
│  └─────────────┘    └─────────────┘    └─────────────┘     │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                    Docker Host                              │
│                                                             │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐   │
│  │ Runner 1 │  │ Runner 2 │  │ Runner 3 │  │ Runner N │   │
│  │(ephemeral)│  │(ephemeral)│  │(ephemeral)│  │(ephemeral)│   │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘   │
└─────────────────────────────────────────────────────────────┘
```

1. **Poll**: Gale periodically checks GitHub API for queued workflow jobs
2. **Filter**: Only jobs with `runs-on: self-hosted` are considered
3. **Scale**: Compares queued jobs vs active runners, spawns new runners if needed
4. **Execute**: Runners pick up jobs and execute them
5. **Cleanup**: Ephemeral runners exit after one job, gale removes the containers

## Example Workflow

Create a workflow that uses Gale runners:

```yaml
# .github/workflows/build.yml
name: Build

on: [push]

jobs:
  build:
    runs-on: gale
    steps:
      - uses: actions/checkout@v4
      - run: echo "Hello from Gale runner!"
```

You can also use `runs-on: self-hosted` which is also detected by Gale.

When this workflow is triggered, gale will:
1. Detect the queued job
2. Spawn a runner container
3. The runner picks up and executes the job
4. Runner exits, container is cleaned up

## Running as a Service

### Systemd

```ini
# /etc/systemd/system/gale.service
[Unit]
Description=Gale GitHub Actions Runner Autoscaler
After=docker.service
Requires=docker.service

[Service]
Type=simple
Environment="GITHUB_TOKEN=ghp_xxxx"
Environment="GITHUB_OWNER=your-username"
ExecStart=/usr/local/bin/gale -config /etc/gale/config.yaml
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

### Docker

```bash
docker run -d \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e GITHUB_TOKEN=ghp_xxxx \
  -e GITHUB_OWNER=your-username \
  gale:latest
```

## Rate Limiting

When monitoring many repositories (org mode), gale makes multiple API calls per poll cycle. GitHub's API rate limit is 5000 requests/hour for authenticated requests.

To reduce API usage:
- **Use webhook mode** (recommended) - eliminates polling entirely
- **Use GitHub App** - gets 5000 requests/hour per installation
- Increase `poll_interval` (e.g., `30s` or `60s`)
- Use single-repo mode for high-frequency polling

## Webhook Mode (Recommended)

Instead of polling the GitHub API, Gale can receive webhook events directly from GitHub when jobs are queued. This is the recommended mode for production use.

### Webhook Setup

1. Configure your webhook in GitHub:
   - Go to your repo or org Settings > Webhooks > Add webhook
   - **Payload URL**: `https://your-server:8080/webhook`
   - **Content type**: `application/json`
   - **Secret**: (optional but recommended)
   - **Events**: Select "Workflow jobs"

2. Configure Gale:
```yaml
webhook:
  port: 8080
  secret: ${GALE_WEBHOOK_SECRET}  # Optional
```

3. Start Gale:
```bash
gale webhook
```

## GitHub App Authentication

GitHub Apps provide significant advantages over Personal Access Tokens:

| Feature | PAT | GitHub App |
|---------|-----|------------|
| Rate Limit | 5,000/hr shared | 5,000/hr per installation |
| Token Rotation | Manual | Automatic (1hr tokens) |
| Permissions | User-wide | Granular per-app |
| Multi-org | Separate PATs | Single app, multiple installs |
| Security | Long-lived | Short-lived tokens |
| Webhook | Manual per-repo | Configured in app |

### Quick Start with GitHub App

1. Create a GitHub App:
```bash
gale app create
```

2. Or view manual setup instructions:
```bash
gale app setup
```

3. Configure Gale:
```yaml
github:
  app:
    app_id: 123456
    private_key_path: /path/to/private-key.pem
    webhook_secret: ${GALE_WEBHOOK_SECRET}
```

4. Validate configuration:
```bash
gale app validate
```

5. Start webhook server:
```bash
gale webhook
```

### GitHub App Permissions

When creating your GitHub App, configure these permissions:

**Repository permissions:**
- Actions: Read-only
- Metadata: Read-only

**Subscribe to events:**
- Workflow jobs

### Private Key Options

You can provide the private key in three ways:

1. **File path** (recommended):
```yaml
github:
  app:
    private_key_path: /etc/gale/private-key.pem
```

2. **Environment variable**:
```bash
export GALE_PRIVATE_KEY="$(cat /path/to/key.pem)"
```

3. **Inline in config** (not recommended for production):
```yaml
github:
  app:
    private_key: |
      -----BEGIN RSA PRIVATE KEY-----
      ...
      -----END RSA PRIVATE KEY-----
```

## Deployment with Kamal

Gale includes [Kamal](https://kamal-deploy.org/) configuration for easy deployment to remote servers, with optional Tailscale integration.

### Setup

1. Install Kamal:
```bash
gem install kamal
```

2. Configure secrets:
```bash
cp .kamal/secrets .kamal/secrets.local
# Edit .kamal/secrets.local with your values
```

3. Deploy:
```bash
# Deploy to configured host
./scripts/deploy.sh

# Deploy to a Tailscale host
./scripts/deploy.sh --tailscale my-server

# Auto-discover hosts with tag:gale on Tailscale
./scripts/deploy.sh --tailscale
```

### Tailscale Integration

To use Tailscale for deployment:

1. Get a Tailscale API key from https://login.tailscale.com/admin/settings/keys

2. Tag your target servers with `tag:gale` in Tailscale ACLs

3. Configure environment:
```bash
export TAILSCALE_API_KEY="tskey-api-xxxx"
export TAILSCALE_TAILNET="your-tailnet.ts.net"
```

4. Deploy:
```bash
./scripts/deploy.sh --tailscale
```

### Manual Kamal Commands

```bash
# Deploy
kamal deploy -c config/deploy.yml

# View logs
kamal logs -c config/deploy.yml

# Open shell
kamal shell -c config/deploy.yml

# Rollback
kamal rollback -c config/deploy.yml
```

## License

MIT License

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.
