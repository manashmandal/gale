# Examples

Common usage patterns and example workflows.

## Workflow Examples

### Basic Workflow

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

### Using `self-hosted` Label

Gale also responds to the `self-hosted` label:

```yaml
jobs:
  build:
    runs-on: [self-hosted, linux]
    steps:
      - uses: actions/checkout@v4
      - run: make build
```

### Matrix Build

```yaml
jobs:
  test:
    runs-on: gale
    strategy:
      matrix:
        node: [16, 18, 20]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: ${{ matrix.node }}
      - run: npm test
```

### Docker-in-Docker

Runners have access to Docker:

```yaml
jobs:
  build:
    runs-on: gale
    steps:
      - uses: actions/checkout@v4
      - run: docker build -t myapp .
      - run: docker run myapp test
```

## Gale Configuration Examples

### Single Repository

```bash
export GITHUB_TOKEN="ghp_xxxx"
export GITHUB_OWNER="manashmandal"
export GITHUB_REPO="my-project"

gale start
```

### All Repositories (Org Mode)

```bash
export GITHUB_TOKEN="ghp_xxxx"
export GITHUB_OWNER="manashmandal"

gale start
```

### Multiple Specific Repos

```yaml
# config.yaml
github:
  token: ${GITHUB_TOKEN}
  owner: manashmandal
  repos:
    - project-a
    - project-b
    - project-c
```

### High-Capacity Setup

```yaml
# config.yaml
scaler:
  min_runners: 2      # Keep 2 warm
  max_runners: 20     # Scale up to 20
  poll_interval: 5s   # Faster polling
  scale_up_delay: 2s  # Faster scaling

runner:
  image: myoung34/github-runner:latest
  labels:
    - self-hosted
    - linux
    - x64
    - high-capacity
  network_mode: host  # Better network performance
```

### Custom Runner Image

```yaml
runner:
  image: my-registry/custom-runner:latest
  labels:
    - self-hosted
    - custom
  env:
    CUSTOM_VAR: "value"
    ANOTHER_VAR: "another"
```

## CLI Examples

### Debug Mode

```bash
# Start with debug logging
gale start -l debug

# Webhook with debug
gale webhook --funnel -l debug
```

### Managing Repos

```bash
# Add repos to monitor
gale repo add frontend-app backend-api

# List monitored repos
gale repo list

# Remove a repo
gale repo remove old-project

# Monitor all repos
gale repo clear
```

### Runner Management

```bash
# View all runners
gale runners list --all

# Clean up exited runners
gale runners clean

# Emergency stop all runners
gale runners stop
```

### Warm Pool

```bash
# Pre-start 3 runners for faster job pickup
gale pool 3

# Check current pool
gale pool

# Disable warm pool
gale pool 0
```

## Parallel Jobs Test

Test that Gale can handle multiple concurrent jobs:

```yaml
# .github/workflows/parallel-test.yml
name: Parallel Test

on: workflow_dispatch

jobs:
  job-1:
    runs-on: gale
    steps:
      - run: |
          echo "Job 1 on $RUNNER_NAME"
          sleep 30

  job-2:
    runs-on: gale
    steps:
      - run: |
          echo "Job 2 on $RUNNER_NAME"
          sleep 30

  job-3:
    runs-on: gale
    steps:
      - run: |
          echo "Job 3 on $RUNNER_NAME"
          sleep 30
```

Each job should get its own runner container.
