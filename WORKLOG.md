# Work Log

## 2025-12-29 - Native Runner Isolation and Timeout Coordination

### Issues Fixed Today

#### Issue 1: Symlink Race Condition During Cache Extraction

**Symptom:**
```
level=ERROR msg="failed to create runner" error="ensuring runner binary: extracting runner: symlink ../lib/node_modules/corepack/dist/corepack.js /Users/manash/.gale/native-runners/cache/2.330.0/externals/node24/bin/corepack: file exists"
```

**Root Cause:**
Multiple runners starting simultaneously were all trying to extract to the same shared cache directory (`~/.gale/native-runners/cache/2.330.0/`). When creating symlinks, one runner would succeed while others failed with "file exists" error.

**Fix (commit c30339c):**
Removed shared cache entirely. Each runner now downloads and extracts its own copy of the GitHub Actions runner binary directly to its isolated work directory.

```
Before:
~/.gale/native-runners/
├── cache/2.330.0/     # Shared - race condition here
└── work/
    ├── runner1/       # Copied from cache
    └── runner2/       # Symlink conflict!

After:
~/.gale/native-runners/
└── work/
    ├── runner1/       # Downloads & extracts its own binary
    ├── runner2/       # Downloads & extracts its own binary
    └── runner3/       # Completely independent
```

**Files changed:**
- `internal/native/client.go`: Replaced `EnsureRunnerBinary()` with `downloadAndExtractRunner()`
- `internal/runner/factory.go`: Removed pre-download of runner binary at startup

---

#### Issue 2: Competing Timeouts Killing Post Steps

**Symptom:**
- lint job: ✅ All Post steps completed
- build job: ❌ Post steps stuck at "in_progress"
- test job: ❌ Post steps skipped/stuck

**Root Cause:**
Race condition between two timeout mechanisms:
1. Native client's `waitForAllProcesses`: Waits up to **5 minutes** for process group to exit
2. Webhook handler's `gracefulShutdown`: Called `StopRunner` after only **70 seconds**

After 70 seconds, `gracefulShutdown` would forcibly kill the process group via SIGTERM while Post steps were still running. The lint job completed faster (under 70s), but build took longer and got killed.

**Fix (commit 1605ab5):**
Increased `gracefulShutdown` timeout from 70 seconds to **6 minutes**, allowing the native client's process group tracking time to work correctly.

**Files changed:**
- `internal/webhook/handler.go`: Changed polling from 30×2s (70s) to 72×5s (6 minutes)

---

#### Issue 3: macOS Ephemeral Mode (User's Fix)

**Root Cause:**
On macOS, the upstream GitHub Actions runner's `--ephemeral` mode exits before Post steps complete.

**Fix:**
On macOS, register runner as non-ephemeral but use `run.sh --once` to emulate ephemeral semantics, then de-register via GitHub API after exit.

```go
useGitHubEphemeral := runtime.GOOS != "darwin"

if useGitHubEphemeral {
    configArgs = append(configArgs, "--ephemeral")
}

// For macOS, use --once flag instead
if !useGitHubEphemeral {
    runArgs = append(runArgs, "--once")
}
```

**Files changed:**
- `internal/native/client.go`: Added PGID tracking, `--once` flag, GitHub API de-registration

---

### Debug Infrastructure Added

**Makefile targets (commits 95356e0, dd674c4, 9c9b3f4):**
```makefile
make debug-logs          # Show gale webhook server logs
make debug-runner-logs   # Show native runner logs
make debug-runner-status # Show runner exit status and signals
make debug-processes     # Show gale-related processes
make debug-cleanup       # List exited runners pending cleanup
make debug-all           # Export all debug info to .debug/gale-debug-*.txt
```

---

### Commits Today

| Commit | Description |
|--------|-------------|
| 95356e0 | Add debug commands to Makefile |
| dd674c4 | Export debug-all output to timestamped file |
| 9c9b3f4 | Use local build for debug commands |
| c30339c | Remove shared cache, fully isolate runners |
| 1605ab5 | Increase graceful shutdown timeout to 6 minutes |

---

### Key Learnings

1. **Process isolation is critical**: Shared state (cache directories) between parallel processes causes race conditions
2. **Competing timeouts cause subtle bugs**: When multiple components have timeouts, ensure outer > inner
3. **macOS runner behavior differs**: GitHub Actions runner behaves differently on macOS with ephemeral mode
4. **Debug infrastructure is essential**: `make debug-all` made remote troubleshooting possible

---

## 2025-12-29 - Native Runner Post Steps Fix

### Issue
Native runners' Post steps (like "Post Setup Go") were failing because Gale was deleting the runner directory too early. The runner process exits after main steps complete, but child processes (including the shell script running the step) continue running. Gale detected the exit and immediately removed the directory, causing subsequent commands in the same step to fail.

Example: `go test` passes, but `go tool cover` fails immediately after with "No such file or directory" because the Go binary was removed mid-step.

### Root Cause
Two issues:
1. `gracefulShutdown` called `RemoveRunner` immediately when it detected the runner process had exited
2. Even after deferring to periodic cleanup, `CleanupExitedRunners` would remove directories as soon as `Status == "exited"` was set

The GitHub Actions runner process (`Runner.Listener`) exits when it considers the job "done", but the actual step shell scripts may still be executing as child processes.

### Fix
1. Don't remove runner directory immediately in `gracefulShutdown` - defer to periodic cleanup
2. Add 2-minute grace period before cleaning up exited runners:

```go
const cleanupGracePeriod = 2 * time.Minute

type Runner struct {
    // ... existing fields
    ExitedAt time.Time  // Track when runner exited
}

func (c *Client) CleanupExitedRunners(ctx context.Context) (int, error) {
    // Only cleanup if exited more than 2 minutes ago
    if r.Status == "exited" && !r.ExitedAt.IsZero() {
        if now.Sub(r.ExitedAt) >= cleanupGracePeriod {
            // Safe to remove
        }
    }
}
```

### Additional Fixes
- Fixed symlink/file extraction errors when runner cache has partial extraction
- Added runner stdout/stderr logging to `runner.log` for debugging

### Files Modified
- `internal/webhook/handler.go` - Defer cleanup to periodic task
- `internal/native/client.go` - Add grace period, handle existing files in extraction, add logging
- `internal/native/client_test.go` - Add tests for grace period behavior

---

## 2025-12-28 - Native Runner Process Group Isolation

### Issue
Native runners were crashing during CI step transitions, specifically during "Post Setup Go" cleanup steps. The runner would die before completing post-job steps.

### Root Cause
Native runners were started as direct child processes of the Gale daemon without their own process group. Signals sent to the Gale process group could inadvertently terminate the runner during job execution.

### Fix
Added process group isolation by setting `Setpgid: true` when starting the runner:
```go
runCmd.SysProcAttr = &syscall.SysProcAttr{
    Setpgid: true,
}
```

This makes the runner start in its own process group, isolated from the Gale daemon. Signals to Gale no longer propagate to the runner.

### Files Modified
- `internal/native/client.go` - Added `SysProcAttr` with `Setpgid: true`

---

## 2025-12-28 10:45 UTC - Config Caching Fix (Auto-Restart Daemon)

### Issue
When webhook daemon was running and user registered a new repo, the running handler still had old config in memory. This caused "repo not registered" errors until manual restart.

### Root Cause
The webhook handler holds a reference to the config object loaded at startup. Config changes saved to disk by other commands (register, repo add, etc.) weren't visible to the running handler.

### Fix
Auto-restart the webhook daemon when config changes are made via:
- `gale webhook register`
- `gale repo add`
- `gale repo remove`
- `gale repo set`
- `gale repo clear`

### Files Modified
- `cmd/gale/webhook_register.go` - Restart daemon after registration
- `cmd/gale/repo.go` - Added `restartWebhookIfRunning()` helper, call it after repo changes

---

## 2025-12-28 10:26 UTC - Duplicate Webhook Race Condition Fix

### Issue
Duplicate "queued" webhooks from GitHub (retries or race conditions) could spawn multiple runners for the same job, leaving orphaned/dangling containers.

### Root Cause
`handleQueued` reserved a slot with a placeholder but didn't check if a runner was already spawned for that job ID before proceeding.

### Fix
Added check at the start of slot reservation:
```go
if _, exists := h.activeRunners[event.WorkflowJob.ID]; exists {
    h.mu.Unlock()
    h.logger.Debug("runner already spawned for job", "job_id", event.WorkflowJob.ID)
    return
}
```

### Files Modified
- `internal/webhook/handler.go` - Added duplicate check in `handleQueued`
- `internal/webhook/handler_test.go` - Added `TestServeHTTP_DuplicateQueuedWebhook`

---

## 2025-12-28 04:30 UTC - Native Runner Mode for macOS

### Feature
Added native runner mode that spawns GitHub Actions runners as native processes instead of Docker containers. This enables true macOS runners for Xcode, iOS builds, etc.

### Architecture
- Created `internal/runner/` package with unified `Client` interface
- Both Docker and Native backends implement the same interface
- `internal/native/client.go` - Native runner implementation
  - Downloads and caches official GitHub Actions runner binary
  - Configures runner with `--ephemeral` mode
  - Manages runner process lifecycle

### Configuration
```yaml
runner:
  mode: native  # "docker" or "native" (default: docker)
```

### Files Created
- `internal/runner/interface.go` - Unified runner interface
- `internal/runner/factory.go` - Client factory
- `internal/runner/docker_adapter.go` - Docker backend
- `internal/runner/native_adapter.go` - Native backend
- `internal/native/client.go` - Native runner implementation

### Files Modified
- `internal/config/config.go` - Added `runner.mode` field
- `internal/webhook/handler.go` - Use unified runner interface
- `internal/webhook/handler_test.go` - Updated tests
- `docs/configuration.md` - Added Runner Mode section
- `README.md` - Updated macOS support info

---

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
