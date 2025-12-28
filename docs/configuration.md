# Configuration

Gale uses a YAML configuration file with environment variable expansion.

## Configuration File

The default config file is `~/.gale/config.yaml`. Use `-c` to specify a custom path:

```bash
gale start -c /etc/gale/config.yaml
```

## Full Configuration Reference

```yaml
# ~/.gale/config.yaml
version: "1"                  # Config version (for migrations)

github:
  token: ${GITHUB_TOKEN}
  owner: ${GITHUB_OWNER}
  repos: []                   # Empty = all repos, or list specific repos
  # repos:
  #   - my-project
  #   - another-repo
  scope: ${GITHUB_SCOPE:-}    # "org", "repo", or "repos" (auto-detected)

  # GitHub App configuration (alternative to token)
  app:
    app_id: 123456
    private_key_path: /path/to/private-key.pem
    # private_key: |          # Or inline (not recommended)
    #   -----BEGIN RSA PRIVATE KEY-----
    #   ...
    webhook_secret: ${GALE_WEBHOOK_SECRET}

docker:
  host: ""                    # Default: unix:///var/run/docker.sock

scaler:
  min_runners: 0              # Minimum runners to keep alive (warm pool)
  max_runners: 10             # Maximum concurrent runners
  poll_interval: 10s          # How often to check for jobs
  scale_up_delay: 5s          # Throttle between scale-up operations

runner:
  mode: docker                # "docker" or "native" (default: docker)
  image: myoung34/github-runner:latest  # Docker image (docker mode only)
  labels:
    - self-hosted
    - linux
    - x64
    - docker
  env: {}                     # Additional environment variables
  network_mode: ""            # Docker network mode (docker mode only)
  timeout: 30m                # Max runner lifetime (0 = no timeout)

webhook:
  port: 8080                  # Webhook server port
  secret: ${WEBHOOK_SECRET}   # GitHub webhook secret

log_level: info               # debug, info, warn, error
```

## Environment Variables

| Variable | Description | Required |
|----------|-------------|----------|
| `GITHUB_TOKEN` | Personal Access Token with `repo` scope | Yes (unless using App) |
| `GITHUB_OWNER` | Username or organization name | Yes |
| `GITHUB_REPO` | Repository name (empty = all repos) | No |
| `GITHUB_SCOPE` | `org` or `repo` (auto-detected) | No |
| `GALE_WEBHOOK_SECRET` | Secret for webhook signature verification | No |
| `GALE_PRIVATE_KEY` | GitHub App private key (alternative to file) | No |

## Scope Detection

Gale automatically detects the scope based on your configuration:

- **`org`**: When `repos` is empty - monitors all repos in the organization
- **`repo`**: When a single repo is specified
- **`repos`**: When multiple specific repos are listed

## Managing Configuration

Use the CLI to view and modify configuration:

```bash
# Show current config
gale config show

# Validate config
gale config validate

# Set a value
gale config set scaler.max_runners 20
gale config set github.scope org
gale config set log_level debug

# Show config file path
gale config path
```

## Runner Labels

The `runner.labels` setting determines which jobs Gale will pick up. Jobs with any matching label in their `runs-on` field will be handled by Gale runners.

**Manage labels via CLI:**
```bash
gale labels                  # List current labels
gale labels add docker       # Add a new label
gale labels remove docker    # Remove a label
```

**Example workflow:**
```yaml
# This job will be picked up if 'gale-linux' is in your labels
jobs:
  build:
    runs-on: gale-linux
    steps:
      - uses: actions/checkout@v4
```

**Default labels:**
- `gale`
- `self-hosted`
- `linux`
- `x64`

## Warm Pool

The `min_runners` setting maintains a warm pool of pre-started runners:

```bash
# Keep 3 runners always running
gale pool 3

# Scale to zero when idle (default)
gale pool 0
```

Benefits of warm pool:
- Eliminates cold start time for jobs
- Runners are immediately available
- Trade-off: consumes resources even when idle

## Runner Timeout

The `runner.timeout` setting automatically kills runners that have been running too long:

```yaml
runner:
  timeout: 30m    # Kill runners after 30 minutes
```

This prevents stuck or zombie runners from consuming resources indefinitely. When a runner exceeds the timeout:

1. Gale gracefully stops the container (10s grace period)
2. If stop fails, the container is force-removed
3. A warning is logged with the runner details

**Recommended values:**
- `30m` - Suitable for most CI/CD jobs
- `1h` - For longer-running builds or tests
- `0` - Disable timeout (not recommended)

**Note:** This is a safety mechanism. For workflow-level timeouts, use `timeout-minutes` in your GitHub Actions workflow:

```yaml
jobs:
  build:
    runs-on: gale
    timeout-minutes: 15    # GitHub cancels job after 15 minutes
```

## Runner Mode

The `runner.mode` setting determines how runners are spawned:

### Docker Mode (default)

```yaml
runner:
  mode: docker
  image: myoung34/github-runner:latest
```

- Runners are spawned as Docker containers
- Works on any system with Docker installed
- Containers run Linux regardless of host OS
- Best for CI/CD isolation and reproducibility

### Native Mode

```yaml
runner:
  mode: native
```

- Runners are spawned as native processes
- Downloads and runs the official GitHub Actions runner binary
- Supports macOS (arm64/x64) and Linux (x64/arm64)
- No Docker required
- Runners execute directly on the host OS

**Use native mode when:**
- You need true macOS runners (Xcode, iOS builds)
- Docker is not available or desired
- You want runners to use host resources directly

**Caveats:**
- Less isolation than Docker mode
- Runner binaries are cached in `~/.gale/native-runners/`
- Cleanup of work directories is automatic
