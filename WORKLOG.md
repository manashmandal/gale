# Work Log

## 2025-12-28 03:45 UTC - macOS Runner Clarification

### Issue
User asked about running macOS containers when the host is macOS.

### Explanation
Docker on macOS runs containers inside a Linux VM. You cannot run macOS-native code in a Docker container - this is a fundamental Docker limitation. The `myoung34/github-runner` image only supports Linux (x86_64 and arm64).

### Alternatives for macOS Runners
1. **Native self-hosted runners** - Run the GitHub Actions runner binary directly on macOS
2. **macOS VM solutions** - Use Tart (Apple Virtualization Framework), Orchard, etc.
3. **Commercial solutions** - MacStadium, Cirrus CI macOS runners

### Changes
- Updated README to clarify this is a Docker limitation, not a Gale limitation
- Added links to native runner docs and Tart for macOS

---

## 2025-12-28 03:15 UTC - Daemon Mode Reliability Fixes

### Issue
Webhooks were sometimes received but no runner/container was started when running in daemon/background mode. Stopped containers were also not being cleaned up, requiring restart in foreground mode.

### Root Causes
1. **HTTP Request Context Cancellation**: `handleQueued` used `r.Context()` (HTTP request context) for Docker operations. With `WriteTimeout: 10s`, Docker container creation could be cancelled if it took too long.

2. **Race Condition in Runner Limit**: The max runner check released the mutex before container creation, allowing multiple concurrent requests to exceed the limit.

3. **No Periodic Cleanup**: Exited containers were only cleaned up when a "completed" webhook was received. If daemon restarted, orphaned containers remained.

### Fixes
1. **Use Background Context**: Changed Docker and GitHub API calls to use `context.Background()` instead of HTTP request context.

2. **Atomic Slot Reservation**: Reserve slot in `activeRunners` map before releasing mutex, preventing race conditions.

3. **Periodic Cleanup Goroutine**: Added `StartCleanup()` method that:
   - Cleans up orphaned containers on startup
   - Runs cleanup every 30 seconds in background

### Files Modified
- `internal/webhook/handler.go` - All three fixes
- `cmd/gale/webhook.go` - Call `StartCleanup()` on handler init

---

## 2025-12-28 02:30 UTC - Major UX Improvements and Config Overhaul

### Features Added

1. **Runner Label Matching from Config**
   - `requiresGaleRunner` now checks job labels against configured `runner.labels`
   - Jobs show "other runner" in status if they don't match gale labels
   - Added `Labels` and `ForGale` fields to `QueuedJob` struct

2. **Ergonomic Labels CLI**
   - `gale labels` - list current runner labels
   - `gale labels add <label>` - add a new label
   - `gale labels remove <label>` - remove a label

3. **Config Path and Versioning**
   - Default config moved to `~/.gale/config.yaml`
   - Added config `version` field for future schema migrations
   - Migration framework for upgrading older configs

4. **Improved Webhook Registration**
   - Interactive prompt when no URL specified
   - Consistent funnel hostname between register and server
   - Shows next steps after registration

5. **Robust Funnel Validation**
   - 10 retries with visible progress: `Attempt 3/10: connecting...`
   - Webhook URL only shown after validation passes
   - `--skip-validation` flag for edge cases
   - Helpful troubleshooting tips on failure

6. **Status Command Improvements**
   - Shows jobs by runner type (gale vs other)
   - Displays queued and in_progress separately
   - Consistent with docker logs

7. **AI Disclaimer**
   - Added disclaimer to README about AI-generated code

### Files Modified

- `cmd/gale/root.go` - Default config path to ~/.gale/config.yaml
- `cmd/gale/webhook.go` - Robust validation with progress
- `cmd/gale/webhook_register.go` - Interactive funnel prompt, consistent hostname
- `cmd/gale/status.go` - Show jobs by runner type
- `cmd/gale/labels.go` - New file for labels management
- `internal/config/config.go` - Version field and migration framework
- `internal/github/client.go` - Labels and ForGale fields on QueuedJob
- `internal/scaler/scaler.go` - Filter jobs by ForGale
- `internal/webhook/handler.go` - Check labels against config
- `docs/cli-reference.md` - Updated with new commands
- `docs/configuration.md` - New config path, labels section
- `README.md` - AI disclaimer

### Commits

1. `fix(webhook): match job labels against configured runner labels`
2. `feat(status): show jobs targeting other runners separately`
3. `docs: add AI code generation disclaimer`
4. `fix(webhook): use consistent funnel hostname for register and server`
5. `feat(webhook): prompt for funnel mode when registering without URL`
6. `feat(config): use ~/.gale/config.yaml as default path with versioning`
7. `chore: remove config.yaml from repo`
8. `feat(cli): add ergonomic labels management commands`
9. `docs: update docs and add tests for labels and config`
10. `feat(webhook): robust funnel validation with visible retry progress`

---

## 2025-12-27 15:45 UTC - CI/CD Fixes and Graceful Shutdown

### Issues Fixed

1. **CI Jobs Getting Stuck**
   - Root cause: Go version mismatch - `go.mod` requires Go 1.25.5, CI was using Go 1.23
   - When CI ran, Go tried to auto-download Go 1.25.5 toolchain which caused hangs
   - Fix: Updated CI workflows to use Go 1.25

2. **Jobs Stuck in "in_progress" State**
   - Root cause: `handleCompleted` in webhook handler immediately killed runner containers
   - Containers were killed before they could report completion to GitHub Actions
   - Fix: Implemented graceful shutdown with polling and staged termination

3. **Homebrew Installation Failing**
   - Root cause: GoReleaser was updating Cask (doesn't work for private repos)
   - The Formula with `GitHubPrivateRepositoryDownloadStrategy` wasn't being updated
   - Fix:
     - Removed `homebrew_casks` from `.goreleaser.yaml`
     - Added release workflow step to auto-update Formula
     - Deleted the non-functional Cask from homebrew-tap

### Changes Made

**Files Modified:**
- `.github/workflows/ci.yml` - Go 1.23 → 1.25
- `.github/workflows/release.yml` - Go 1.23 → 1.25, added Formula update step
- `.goreleaser.yaml` - Removed homebrew_casks section
- `internal/webhook/handler.go` - Graceful shutdown implementation
- `internal/docker/interfaces.go` - Added `IsContainerExited`, `StopRunner` methods
- `internal/docker/client.go` - Implemented new methods
- `internal/docker/mock.go` - Added mock implementations
- `internal/webhook/handler_test.go` - Updated tests for new behavior
- `README.md` - Go 1.21+ → 1.25+, added Homebrew install section
- `.gitignore` - Added `gale` binary

**Files Created:**
- `docs/homebrew.md` - Documentation for private repo Homebrew distribution

### Graceful Shutdown Flow

```
Job Completed Webhook
        ↓
Remove from activeRunners map
        ↓
Start background goroutine
        ↓
Poll every 2s for 30s (check if container exited)
        ↓
If still running → SIGTERM with 10s timeout
        ↓
Wait 10s
        ↓
Force remove container
```

### Commits

1. `fix(ci): upgrade to Go 1.25 to match project requirements`
2. `fix(release): update homebrew Formula instead of Cask`
3. `docs: add Homebrew installation guide for private repos`
4. `fix(webhook): graceful container shutdown on job completion`

### Pending

- Coverage check failing in CI (new code paths need tests)
- Need to verify graceful shutdown works in production

### To Test

```bash
# Build new binary
go build -o bin/gale ./cmd/gale

# Restart webhook server
gale webhook --funnel

# Trigger a workflow and verify:
# 1. Jobs complete successfully (not stuck)
# 2. Containers exit gracefully
# 3. No orphaned containers
```
