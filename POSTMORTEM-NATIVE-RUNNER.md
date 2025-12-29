# Postmortem: Native Runner Post Steps Failing on macOS

**Status:** FIXED (Attempt 6 - Use --once flag instead of non-ephemeral mode)
**Date:** 2025-12-29
**Platform:** macOS ARM64 (Apple Silicon)
**Gale Version:** Latest main branch
**Last Updated:** 2025-12-29 - Fixed by using --once flag on macOS

---

## Executive Summary

Native GitHub Actions runners on macOS exit before Post steps complete, causing jobs to hang indefinitely at "Post Setup Go" step. The main job steps pass successfully, but Post steps never finish.

---

## Problem Statement

When running GitHub Actions workflows using Gale's native runner mode on macOS:

1. All main steps complete successfully (Checkout, Setup Go, Download dependencies, Run tests) ✅
2. The "Run tests with coverage check" step passes with green checkmark ✅
3. "Post Setup Go" step shows orange circle (in progress) and hangs indefinitely ❌
4. "Post Checkout" never starts ❌
5. Job eventually times out or is marked as failed

### Error Evidence

```
ok      github.com/manashmandal/gale/internal/webhook    19.211s    coverage: 66.8% of statements
/Users/manash/.gale/native-runners/work/7634600a/_work/_tool/go/1.25.5/arm64/bin/go: No such file or directory
```

The Go binary installed by `actions/setup-go` disappears mid-execution.

---

## System Architecture

### Gale Native Runner Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                         GALE DAEMON                              │
│                                                                  │
│  1. Receives "queued" webhook from GitHub                        │
│  2. Creates runner directory: ~/.gale/native-runners/work/<id>/  │
│  3. Copies runner binary from cache                              │
│  4. Runs config.sh --ephemeral --unattended                      │
│  5. Starts run.sh (via exec.Command)                             │
│  6. Monitors process via waitForCompletion goroutine             │
│  7. Receives "completed" webhook, runs gracefulShutdown          │
│  8. Periodic cleanup removes old directories                     │
└─────────────────────────────────────────────────────────────────┘
```

### GitHub Actions Runner Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    run.sh (shell script)                         │
│                           │                                      │
│                           ▼                                      │
│                   Runner.Listener                                │
│                    (main process)                                │
│                           │                                      │
│              ┌────────────┴────────────┐                        │
│              ▼                         ▼                        │
│       Runner.Worker              Runner.Worker                  │
│      (job execution)            (another job)                   │
│              │                                                   │
│    ┌─────────┼─────────┐                                        │
│    ▼         ▼         ▼                                        │
│  Step 1   Step 2   Post Steps                                   │
│ (Checkout) (Build)  (Cleanup)                                   │
└─────────────────────────────────────────────────────────────────┘
```

**Critical Insight:** In ephemeral mode (`--ephemeral`), `Runner.Listener` may exit BEFORE `Runner.Worker` completes Post steps.

---

## Key Files

| File                                | Purpose                             |
| ----------------------------------- | ----------------------------------- |
| `internal/native/client.go`         | Native runner lifecycle management  |
| `internal/webhook/handler.go`       | Webhook handling, graceful shutdown |
| `internal/runner/native_adapter.go` | Adapter for runner interface        |
| `cmd/gale/webhook.go`               | Webhook server startup              |

---

## Investigation Timeline

### Attempt 1: Process Group Isolation

**Hypothesis:** Signals from Gale were killing the runner
**Fix:** Added `Setpgid: true` to isolate runner process group
**Result:** ❌ Did not fix the issue

### Attempt 2: Defer Directory Removal

**Hypothesis:** `gracefulShutdown` was removing directory too early
**Fix:** Return without removing when runner exits, let periodic cleanup handle it
**Result:** ❌ Did not fix the issue

### Attempt 3: Add Grace Period to Cleanup

**Hypothesis:** Periodic cleanup was removing directories immediately
**Fix:** Added 2-minute grace period before `CleanupExitedRunners` removes directory
**Result:** ⚠️ Partial fix - main steps now complete, but Post steps still hang

### Attempt 4: Wait for Child Processes

**Hypothesis:** `Runner.Listener` exits before `Runner.Worker` completes
**Fix:** Added `waitForAllProcesses()` using `pgrep` to wait for all processes in runner directory
**Result:** ❌ Did not fix the issue - `pgrep -f` doesn't detect child processes on macOS

### Attempt 5: Multi-Method Process Detection (2025-12-29)

**Hypothesis:** `pgrep -f` doesn't detect orphaned/reparented processes on macOS
**Fix:** Implemented three detection methods in `getProcessesInDir()`:

1. `lsof -t +D <dir>` - finds processes with open files in directory (catches orphaned processes)
2. `pgrep -f <dir>` - finds processes with directory in command line (original method)
3. `pgrep -f Runner.Worker` - specifically finds Runner.Worker processes

Also added:

- 5-second initial wait after Runner.Listener exits for Runner.Worker to initialize
- Double-check confirmation (wait 1s, check again) before declaring no processes
- Detailed logging of what each detection method finds
- Periodic detailed logging every 10 iterations

**Result:** ❌ Didn't fix the issue - the fundamental problem was using non-ephemeral mode

### Attempt 6: Use --once flag instead of non-ephemeral mode (2025-12-29)

**Hypothesis:** The problem is using non-ephemeral mode without --once flag
**Root Cause Analysis:**

- `--ephemeral` on macOS: Runner.Listener exits before Runner.Worker completes Post steps (upstream bug)
- Non-ephemeral without `--once`: Runner keeps waiting for more jobs, never exits
- This caused `cmd.Wait()` to block forever, requiring manual SIGTERM after timeout
- SIGTERM could kill Post steps if they hadn't completed

**Fix:** Use `--once` flag on macOS instead of non-ephemeral mode:

- `--once` makes the runner complete ONE job (including ALL Post steps), then exit cleanly
- Unlike `--ephemeral`, `--once` doesn't cause premature Listener exit
- Runner exits naturally after Post steps complete, triggering proper cleanup

**Changes:**

1. Added `--once` flag to run.sh args on macOS
2. Simplified `waitForAllProcesses()` - no longer needs minimum wait hacks
3. Reduced gracefulShutdown timeout from 4 minutes to 2 minutes (safety net only)

**Result:** ✅ Fixed - Runner now exits cleanly after completing all steps including Post steps

---

## Debug Logging Output

With current debug logging, we see:

```
[GALE DEBUG] Runner.Listener 8d753a64 exited at 2025-12-29 03:16:28 (code=0, signaled=false)
[GALE DEBUG] Waiting for child processes in /Users/manash/.gale/native-runners/work/8d753a64 to complete...
[GALE DEBUG] All processes completed (waited Xs)

time=2025-12-29T03:16:29 level=INFO msg="[DEBUG] handleCompleted called" job_id=59056570360 runner_id=8d753a64 exists=true status=completed
time=2025-12-29T03:16:31 level=INFO msg="[DEBUG] gracefulShutdown poll" iteration=0 exited=true
time=2025-12-29T03:16:31 level=INFO msg="runner exited gracefully - NOT removing"
```

**Key Observation:** The `pgrep` check returns NO processes, suggesting child processes are NOT being detected properly, OR they have already exited/been orphaned in a way that breaks the parent-child relationship.

---

## Hypotheses Not Yet Tested

### Hypothesis A: macOS Process Orphaning Behavior

On macOS, when a parent process exits, children may be reparented to `launchd` (PID 1) and lose their original working directory association. `pgrep -f <directory>` may not find them because their `/proc` entry or command line no longer references the directory.

**Test:** Use `lsof +D <directory>` instead of `pgrep -f` to find processes with open files in the directory.

### Hypothesis B: Runner.Worker Uses Different Working Directory

The `Runner.Worker` process might execute steps in a different working directory (e.g., `_work/`) that doesn't match our `pgrep` pattern.

**Test:** Log the full process tree during execution using `pstree` or `ps auxf`.

### Hypothesis C: run.sh Exits Immediately (Daemonizes)

The `run.sh` script might background `Runner.Listener` and exit immediately, causing our `cmd.Wait()` to return before any work is done.

**Test:** Read and log the contents of `run.sh` to understand its behavior.

### Hypothesis D: GitHub Actions Runner Bug on macOS ARM64

There may be a bug in the GitHub Actions runner v2.330.0 specifically on macOS ARM64 with ephemeral mode.

**Test:** Try runner version 2.319.1 or disable ephemeral mode.

### Hypothesis E: The "completed" Webhook Arrives Too Early

GitHub might send the "completed" webhook when the runner reports job completion, but BEFORE Post steps actually finish.

**Test:** Add timestamp logging to correlate webhook arrival with actual step completion.

### Hypothesis F: Post Steps Run as Detached Processes

Post steps might be launched as fully detached processes (double-fork daemon pattern) that have no traceable relationship to the runner.

**Test:** Monitor all system processes during job execution, not just those related to the runner directory.

---

## Code Locations for Key Logic

### Runner Creation

```go
// internal/native/client.go:199-225
runScript := filepath.Join(runnerDir, "run.sh")
runCmd := exec.Command(runScript)
runCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
runCmd.Start()
go c.waitForCompletion(runner)
```

### Process Monitoring

```go
// internal/native/client.go:247-300
func (c *Client) waitForCompletion(runner *Runner) {
    runner.cmd.Wait()  // Waits for run.sh to exit
    c.waitForAllProcesses(runner, 5*time.Minute)  // Waits for children
    runner.Status = "exited"
    runner.ExitedAt = time.Now()
}
```

### Child Process Detection (UPDATED - Attempt 5)

```go
// internal/native/client.go:364-426
func (c *Client) getProcessesInDir(dir string) []string {
    // Uses three detection methods:
    // 1. lsof -t +D <dir> - processes with open files
    // 2. pgrep -f <dir> - processes with dir in command line
    // 3. pgrep -f Runner.Worker - specific Runner.Worker processes
    pidSet := make(map[string]bool)
    // ... combines results from all methods
}

func (c *Client) getProcessesViaLsof(dir string) []string {
    cmd := exec.Command("lsof", "-t", "+D", dir)
    // ...
}

func (c *Client) getRunnerWorkerProcesses() []string {
    cmd := exec.Command("pgrep", "-f", "Runner.Worker")
    // ...
}
```

### Graceful Shutdown

```go
// internal/webhook/handler.go:382-449
func (h *Handler) gracefulShutdown(runnerID string, jobID int64) {
    // Polls IsRunnerExited every 2s for 30s
    // If exited, returns WITHOUT removing directory
    // If not exited after 30s, sends SIGTERM, waits, force removes
}
```

---

## Configuration

```yaml
# ~/.gale/config.yaml
runner:
  mode: native # "docker" or "native"
  labels:
    - gale
    - gale-macos
    - self-hosted
```

Runner started with:

```bash
./config.sh --unattended --ephemeral --name gale-native-<id> --token <token> --labels gale,gale-macos,self-hosted --work _work --replace --url https://github.com/owner/repo
./run.sh
```

---

## Environment Details

- **Host OS:** macOS (Darwin)
- **Architecture:** ARM64 (Apple Silicon, M-series)
- **Runner Version:** actions/runner v2.330.0
- **Go Version:** 1.25.5 (installed by actions/setup-go)
- **Gale Runner Mode:** native

---

## Files to Examine

1. **Runner's own logs:** `~/.gale/native-runners/work/<id>/runner.log`
2. **Runner's diag folder:** `~/.gale/native-runners/work/<id>/_diag/`
3. **run.sh script:** `~/.gale/native-runners/cache/2.330.0/run.sh`
4. **System process list during execution:** `ps aux | grep -E "(Runner|gale)"`

---

## Suggested Next Steps

1. **Examine run.sh:** Read the actual content of the runner's `run.sh` script to understand if it daemonizes or waits.

2. **Use lsof instead of pgrep:** Replace `pgrep -f <dir>` with `lsof +D <dir>` to find processes with open files.

3. **Add process tree logging:** During job execution, periodically log `pstree -p <runner_pid>` or `ps --forest`.

4. **Try non-ephemeral mode:** Remove `--ephemeral` flag to see if the runner stays alive longer.

5. **Check runner's \_diag logs:** The GitHub Actions runner writes detailed logs to `_diag/` folder.

6. **Test with Docker mode:** Verify Docker mode works correctly to isolate the issue to native mode.

7. **Monitor with dtrace/dtruss:** On macOS, use system tracing to see exactly when processes exit.

---

## Related Links

- GitHub Actions Runner: https://github.com/actions/runner
- Runner Ephemeral Mode: https://docs.github.com/en/actions/hosting-your-own-runners/managing-self-hosted-runners/autoscaling-with-self-hosted-runners#using-ephemeral-runners-for-autoscaling
- Gale Repository: https://github.com/manashmandal/gale

---

## Conclusion

**RESOLVED:** The root cause was using non-ephemeral mode without the `--once` flag on macOS.

The GitHub Actions runner has two relevant flags:

- `--ephemeral`: Unregisters from GitHub after one job. **Bug on macOS**: Runner.Listener may exit before Runner.Worker completes Post steps.
- `--once`: Makes runner exit after completing one job (including ALL Post steps), then exit cleanly.

The fix was to use `--once` instead of trying to detect when Post steps complete. This allows the runner to exit naturally after all steps are done, triggering proper cleanup flow.
