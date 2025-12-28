# CLI Reference

Complete reference for all Gale commands.

## Global Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--config` | `-c` | `~/.gale/config.yaml` | Path to config file |
| `--log-level` | `-l` | `info` | Log level (debug, info, warn, error) |

## Commands

### `gale init`

Interactive setup wizard.

```bash
gale init                    # Start setup wizard
gale init --force           # Overwrite existing config
```

### `gale start`

Start the autoscaler in polling mode.

```bash
gale start                   # Start with default config
gale start -c /etc/gale.yaml # Use custom config
gale start -l debug          # Enable debug logging
gale start --daemon          # Run in background
```

### `gale webhook`

Start webhook server for event-driven scaling.

```bash
gale webhook                 # Start webhook server
gale webhook --port 9000     # Use custom port
gale webhook --funnel        # Use Tailscale Funnel
gale webhook --hostname my-gale  # Custom Tailscale hostname
gale webhook --daemon        # Run in background
gale webhook -l debug        # Enable debug logging
```

**Subcommands:**

```bash
gale webhook register <repo>  # Register webhook on GitHub repo
gale webhook register --org   # Register org-level webhook
gale webhook list             # List registered webhooks
gale webhook unregister <repo> # Remove a webhook
gale webhook stop             # Stop webhook daemon
gale webhook restart          # Restart webhook daemon
```

### `gale status`

Show current status including config, runners, and queued jobs.

```bash
gale status
```

### `gale runners`

Manage runner containers.

```bash
gale runners list            # List running containers
gale runners list --all      # Include exited containers
gale runners clean           # Remove exited containers
gale runners stop            # Stop all runners
```

### `gale config`

Manage configuration.

```bash
gale config show             # Display current config
gale config validate         # Check config for errors
gale config set KEY VALUE    # Set a config value
gale config path             # Show config file path
```

**Examples:**
```bash
gale config set scaler.max_runners 20
gale config set github.scope org
gale config set log_level debug
```

### `gale pool`

Manage warm pool of pre-started runners.

```bash
gale pool                    # Show current warm pool size
gale pool 3                  # Keep 3 runners always running
gale pool 0                  # Scale to zero when idle
```

### `gale labels`

Manage runner labels (runs-on).

```bash
gale labels                  # List current labels
gale labels add <label>      # Add a new label
gale labels remove <label>   # Remove a label
```

**Examples:**
```bash
gale labels add docker       # Accept jobs with runs-on: docker
gale labels add gpu-runner   # Accept jobs with runs-on: gpu-runner
gale labels remove docker    # Stop accepting runs-on: docker
```

Jobs with any configured label in their `runs-on` field will be picked up by Gale runners.

### `gale repo`

Manage monitored repositories.

```bash
gale repo list              # List monitored repos
gale repo add <repo>        # Add a repo to monitor
gale repo add repo1 repo2   # Add multiple repos
gale repo remove <repo>     # Stop monitoring a repo
gale repo set repo1 repo2   # Set exact list of repos
gale repo clear             # Monitor all repos (default)
```

### `gale app`

Manage GitHub App configuration.

```bash
gale app setup              # Show setup instructions
gale app validate           # Validate app configuration
gale app create             # Interactive app creation wizard
```

### `gale stop`

Stop the running daemon.

```bash
gale stop
```

### `gale version`

Show version information.

```bash
gale version
```
