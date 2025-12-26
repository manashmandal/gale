<p align="center">
  <img src="assets/logo.png" alt="Gale Logo" width="120" height="120">
</p>

<h1 align="center">Gale</h1>

<p align="center">
  <strong>Just-In-Time Autoscaler for GitHub Actions Self-Hosted Runners</strong>
</p>

<p align="center">
  <a href="#tldr">TL;DR</a> •
  <a href="#features">Features</a> •
  <a href="#quick-start">Quick Start</a> •
  <a href="#webhook-mode-recommended">Webhook Mode</a> •
  <a href="#tailscale-funnel">Tailscale Funnel</a>
</p>

---

## TL;DR

```bash
# Install
git clone https://github.com/manashmandal/gale.git && cd gale
go build -o bin/gale ./cmd/gale

# Setup (interactive)
./bin/gale init

# Run (pick one)
./bin/gale start                    # Polling mode
./bin/gale webhook                  # Webhook mode (local)
./bin/gale webhook --funnel         # Webhook + Tailscale Funnel (zero-config public HTTPS)
```

Then use `runs-on: [self-hosted, gale]` in your workflows. Done.

---

## Why Gale?

> *"I have a perfectly good machine sitting idle. Why am I waiting 15 minutes for a GitHub-hosted runner to npm install?"*

Sound familiar? You want self-hosted runners, but:

- **Kubernetes?** You just want to run some tests, not become a CNCF certified architect
- **Argo Workflows?** Cool, but you'll spend more time on YAML than actual code
- **Actions Runner Controller?** Hope you enjoy debugging Helm charts at 2 AM
- **Always-on runners?** Your electricity bill called, it's concerned

**Gale is for the rest of us.** One binary. Docker. That mass of compute power under your desk finally doing something useful.

Your machine will:
- Pick up jobs instantly (webhook mode = no polling delays)
- Spawn runners on-demand (no idle containers eating RAM)
- Clean up after itself (ephemeral = fire-and-forget)
- Scale from zero to hero (and back to zero when you're done)

*Results may vary based on how beefy your machine is. A potato will still run like a potato.* 🥔

---

## Overview

Gale monitors your GitHub repositories for queued jobs and dynamically spawns Docker-based runners on demand. When a job completes, the runner exits and is cleaned up automatically.

## Features

- **JIT Scaling**: Runners spawn only when jobs are queued, saving resources
- **Webhook Mode**: Event-driven scaling via GitHub webhooks (recommended)
- **Tailscale Funnel**: Zero-config public HTTPS endpoint for webhooks
- **Multi-Repo Support**: Monitor all repos in an org or select specific ones
- **Ephemeral Runners**: Runners automatically exit after completing a job
- **Auto Cleanup**: Exited containers are automatically removed
- **Docker-based**: Uses the popular `myoung34/github-runner` image
- **GitHub App Support**: Better security with auto-rotating tokens and higher rate limits
- **Daemon Mode**: Run in background with `gale start --daemon`

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

```mermaid
flowchart TB
    subgraph Gale
        GH[GitHub Client] --> SC[Scaler Logic]
        SC --> DC[Docker Client]
    end

    subgraph Sources
        API[GitHub API<br/>Polling Mode]
        WH[GitHub Webhook<br/>Webhook Mode]
    end

    API --> GH
    WH --> GH

    subgraph Docker Host
        DC --> R1[Runner 1<br/>ephemeral]
        DC --> R2[Runner 2<br/>ephemeral]
        DC --> RN[Runner N<br/>ephemeral]
    end

    R1 --> GHA[GitHub Actions]
    R2 --> GHA
    RN --> GHA
```

1. **Detect**: Gale receives job events (webhook) or polls GitHub API
2. **Filter**: Only jobs matching configured labels are processed
3. **Scale**: Spawns new runners for queued jobs (up to max_runners)
4. **Execute**: Runners pick up jobs and execute them
5. **Cleanup**: Ephemeral runners exit after one job, containers are removed

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

Instead of polling the GitHub API, Gale can receive webhook events directly from GitHub when jobs are queued. **This is the recommended mode for production use.**

### Why Webhook > Polling?

| Aspect | Polling Mode | Webhook Mode |
|--------|--------------|--------------|
| API Rate Limit | Consumes 5,000 req/hr limit | Zero API calls |
| Response Time | Up to `poll_interval` delay | Instant (~100ms) |
| Resource Usage | Constant CPU/network | Near-zero when idle |
| Scalability | Limited by rate limits | Unlimited repos |

**The Math**: Polling every 10s = 360 API calls/hour per repo. With 14 repos, you'd hit the 5,000/hr limit in under an hour. Webhook mode uses **zero** API calls for job detection.

### Webhook Setup Options

#### Option 1: Tailscale Funnel (Recommended)

Zero-config public HTTPS endpoint. No port forwarding, no DNS, no TLS certificates to manage.

```bash
gale webhook --funnel
```

On first run, authenticate with Tailscale when prompted. Gale will display your public webhook URL:

```
┌─────────────────────────────────────────────────────────────┐
│  Gale Webhook Server (Tailscale Funnel)                     │
├─────────────────────────────────────────────────────────────┤
│  Webhook URL: https://gale.your-tailnet.ts.net/webhook      │
└─────────────────────────────────────────────────────────────┘
```

Use this URL in your GitHub webhook settings.

#### Option 2: Local Server

If you have a public IP or reverse proxy:

```bash
gale webhook --port 8080
```

### GitHub Webhook Configuration

1. Go to your repo/org → Settings → Webhooks → Add webhook
2. Configure:
   - **Payload URL**: Your Funnel URL or `https://your-server:8080/webhook`
   - **Content type**: `application/json`
   - **Secret**: (optional but recommended)
   - **Events**: Select "Workflow jobs"
3. Save and verify the ping succeeds

## Tailscale Funnel

[Tailscale Funnel](https://tailscale.com/kb/1223/funnel/) allows you to expose your local Gale webhook server to the public internet without:

- Port forwarding or firewall configuration
- Static IP addresses
- DNS setup
- TLS certificate management

### How It Works

```mermaid
flowchart LR
    GH[GitHub Webhook] -->|HTTPS| TS[Tailscale Edge<br/>TLS termination]
    TS -->|WireGuard| G[Gale<br/>local machine]
    G -->|Docker API| R[Runner Container]
```

1. GitHub sends webhook to your public Funnel URL
2. Tailscale receives the request at their edge servers
3. Request is encrypted and forwarded to your machine via WireGuard
4. Gale receives the webhook and spawns a runner

### Setup

```bash
# First run - will prompt for Tailscale authentication
gale webhook --funnel

# Run in background (daemon mode)
gale webhook --funnel --daemon
```

The hostname defaults to `gale-<machine-hostname>` (e.g., `gale-xps`, `gale-macbook`) to differentiate between machines. Override with `--hostname`:

```bash
gale webhook --funnel --hostname my-custom-name
```

### Prerequisites

1. [Tailscale](https://tailscale.com/download) installed (not required to be running - gale uses embedded tsnet)
2. Funnel enabled in your Tailscale ACL policy:
   ```json
   "nodeAttrs": [
     {
       "target": ["*"],
       "attr": ["funnel"]
     }
   ]
   ```

### Benefits

| Feature | Traditional Setup | Tailscale Funnel |
|---------|-------------------|------------------|
| Public IP | Required | Not needed |
| Port forwarding | Manual config | Automatic |
| TLS certificates | Let's Encrypt/manual | Automatic |
| DNS | Required | Automatic (`*.ts.net`) |
| Firewall | Open ports | No changes |
| Setup time | Hours | Minutes |

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
