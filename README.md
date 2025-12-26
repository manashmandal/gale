# Gale

A JIT (Just-In-Time) autoscaler for GitHub Actions self-hosted runners. Gale monitors your GitHub repositories for queued jobs and dynamically spawns Docker-based runners on demand.

## Features

- **JIT Scaling**: Runners are created only when jobs are queued, saving resources
- **Multi-Repo Support**: Monitor all repositories under a user/organization or a single repo
- **Ephemeral Runners**: Runners automatically exit after completing a job
- **Auto Cleanup**: Exited containers are automatically removed
- **Docker-based**: Uses the popular `myoung34/github-runner` image
- **Configurable**: Set max runners, poll intervals, labels, and more

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

1. Create a GitHub Personal Access Token at https://github.com/settings/tokens with `repo` scope

2. Run gale:

```bash
export GITHUB_TOKEN="ghp_xxxxxxxxxxxx"
export GITHUB_OWNER="your-username"

# Monitor all repos (org mode)
./bin/gale

# Or monitor a single repo
export GITHUB_REPO="your-repo"
./bin/gale
```

3. Trigger a workflow with `runs-on: self-hosted` - gale will automatically spawn a runner!

## Configuration

Gale uses a YAML configuration file with environment variable expansion:

```yaml
# config.yaml
github:
  token: ${GITHUB_TOKEN}
  owner: ${GITHUB_OWNER}
  repo: ${GITHUB_REPO:-}      # Leave empty for org-wide monitoring
  scope: ${GITHUB_SCOPE:-}    # "org" or "repo" (auto-detected if empty)

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

## Usage

### Command Line Options

```bash
./bin/gale [options]

Options:
  -config string
        Path to config file (default "config.yaml")
  -log-level string
        Log level: debug, info, warn, error
  -version
        Show version
```

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

Create a workflow that uses self-hosted runners:

```yaml
# .github/workflows/build.yml
name: Build

on: [push]

jobs:
  build:
    runs-on: self-hosted
    steps:
      - uses: actions/checkout@v4
      - run: echo "Hello from self-hosted runner!"
```

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
- Increase `poll_interval` (e.g., `30s` or `60s`)
- Use single-repo mode for high-frequency polling
- Consider caching (future feature)

## License

MIT License

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.
