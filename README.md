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
  <a href="#installation">Installation</a> •
  <a href="#getting-started">Getting Started</a> •
  <a href="#how-it-works">How It Works</a> •
  <a href="#documentation">Docs</a>
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
./bin/gale webhook --funnel         # Webhook + Tailscale Funnel (zero-config HTTPS)
```

Then use `runs-on: gale` in your workflows. Done.

---

## Why Gale?

> *"I have a perfectly good machine sitting idle. Why am I waiting 15 minutes for a GitHub-hosted runner to npm install?"*

Sound familiar? You want self-hosted runners, but:

- **Kubernetes?** You just want to run some tests, not become a CNCF certified architect
- **Argo Workflows?** Cool, but you'll spend more time on YAML than actual code
- **Actions Runner Controller?** Hope you enjoy debugging Helm charts at 2 AM
- **Always-on runners?** Your electricity bill called, it's concerned

**Gale is for the rest of us.** One binary. Docker. That mass of compute power under your desk finally doing something useful.

---

## Features

- **JIT Scaling** — Runners spawn only when jobs are queued, saving resources
- **Webhook Mode** — Event-driven scaling via GitHub webhooks (recommended)
- **Tailscale Funnel** — Zero-config public HTTPS endpoint for webhooks
- **Multi-Repo Support** — Monitor all repos in an org or select specific ones
- **Ephemeral Runners** — Runners automatically exit after completing a job
- **Auto Cleanup** — Exited containers are automatically removed
- **GitHub App Support** — Better security with auto-rotating tokens
- **Daemon Mode** — Run in background with `gale start --daemon`

---

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
- GitHub Personal Access Token with `repo` scope (or GitHub App)

---

## Getting Started

### 1. Run Interactive Setup

```bash
./bin/gale init
```

### 2. Start Gale

**Option A: Polling Mode**
```bash
./bin/gale start
```

**Option B: Webhook Mode (Recommended)**
```bash
./bin/gale webhook --funnel
```

### 3. Use in Your Workflow

```yaml
# .github/workflows/build.yml
name: Build
on: [push]

jobs:
  build:
    runs-on: gale  # or [self-hosted, gale]
    steps:
      - uses: actions/checkout@v4
      - run: echo "Hello from Gale!"
```

That's it! Gale will automatically spawn a runner when the job is queued.

---

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

1. **Detect** — Gale receives job events (webhook) or polls GitHub API
2. **Filter** — Only jobs matching configured labels are processed
3. **Scale** — Spawns new runners for queued jobs (up to max_runners)
4. **Execute** — Runners pick up jobs and execute them
5. **Cleanup** — Ephemeral runners exit after one job, containers are removed

---

## Documentation

| Document | Description |
|----------|-------------|
| [Configuration](docs/configuration.md) | Config file reference and environment variables |
| [CLI Reference](docs/cli-reference.md) | Complete command reference |
| [Webhook Mode](docs/webhook-mode.md) | Setting up event-driven scaling |
| [Tailscale Funnel](docs/tailscale-funnel.md) | Zero-config public HTTPS endpoint |
| [GitHub App](docs/github-app.md) | Using GitHub App authentication |
| [Deployment](docs/deployment.md) | Systemd, Docker, and Kamal deployment |
| [Examples](docs/examples.md) | Usage patterns and workflow examples |

---

## Quick Reference

```bash
# Status and monitoring
gale status                  # Show status, runners, queued jobs
gale runners list            # List running containers

# Configuration
gale config show             # Display current config
gale config set KEY VALUE    # Set a config value

# Repository management
gale repo add <repo>         # Add a repo to monitor
gale repo list               # List monitored repos

# Warm pool
gale pool 3                  # Keep 3 runners pre-started
gale pool 0                  # Scale to zero when idle
```

See [CLI Reference](docs/cli-reference.md) for all commands.

---

## License

MIT License

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.
