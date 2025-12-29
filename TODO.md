# Security Audit TODO

Security audit completed: 2025-12-29

## Summary

| Severity | Count |
|----------|-------|
| Critical | 2 |
| High | 5 |
| Medium | 10 |
| Low | 6 |

---

## Critical

### CRIT-01: Docker Socket Mount Provides Container Escape Vector
- **Location:** `internal/docker/client.go:131-138`
- **Description:** Mounting `/var/run/docker.sock` into runner containers grants containers effective root access to the host system, allowing container escape, access to host filesystem, and ability to spawn privileged containers.
- **Remediation:**
  - [ ] Use a Docker socket proxy (e.g., Tecnativa/docker-socket-proxy) to restrict API access
  - [ ] Implement Docker-in-Docker with proper isolation
  - [ ] Consider using rootless Docker or Podman
  - [ ] Document this risk prominently for users

### CRIT-02: Webhook Signature Verification Optional
- **Location:** `internal/webhook/handler.go:281-289`
- **Description:** Webhook signature verification is optional. Without it, any attacker who knows the webhook endpoint can forge webhook events to spawn arbitrary runners.
- **Remediation:**
  - [ ] Make webhook signature verification mandatory by default
  - [ ] Require `--insecure` flag to disable verification
  - [ ] Auto-generate secrets during `gale init`

---

## High

### HIGH-01: GitHub Token Stored in Plain Text in Config
- **Location:** `internal/config/config.go:37`, `cmd/gale/init.go:106-108`
- **Description:** The configuration supports storing GitHub tokens directly in the YAML config file. While environment variable expansion is supported, the interactive setup offers direct storage as an option.
- **Remediation:**
  - [ ] Remove the option to store tokens directly in config files
  - [ ] Always use environment variable references
  - [ ] Add pre-commit hooks to detect secrets

### HIGH-02: Token Passed as Environment Variable to Containers
- **Location:** `internal/docker/client.go:92-94`
- **Description:** The GitHub token is passed as `ACCESS_TOKEN` environment variable to runner containers. This token could be leaked through process listings, container inspection, or logging.
- **Remediation:**
  - [ ] Use Docker secrets or mounted files with restricted permissions
  - [ ] Implement short-lived registration tokens exclusively
  - [ ] Clear tokens from environment after registration

### HIGH-03: Command Injection Risk in Native Runner
- **Location:** `internal/native/client.go:461-463`, `508-509`
- **Description:** The native client executes shell commands using `sh -c` with directory paths that could potentially be manipulated.
- **Remediation:**
  - [ ] Use direct command execution instead of shell interpretation
  - [ ] Properly escape or validate directory paths
  - [ ] Use Go's native file system operations where possible

### HIGH-04: Private Key Can Be Stored in Config
- **Location:** `internal/config/config.go:50`
- **Description:** GitHub App private keys can be stored inline in the YAML configuration file.
- **Remediation:**
  - [ ] Remove inline private key storage option
  - [ ] Only support file paths and environment variables
  - [ ] Validate file permissions on key files (should be 0600)

### HIGH-05: PID File Written with World-Readable Permissions
- **Location:** `cmd/gale/start.go:167`
- **Description:** The PID file is created with 0644 permissions.
- **Remediation:**
  - [ ] Use 0600 permissions for PID files
  - [ ] Create PID files in user-owned directories only

---

## Medium

### MED-01: Tar Extraction Path Traversal Risk
- **Location:** `internal/native/client.go:763`
- **Description:** The tar extraction function does not validate that extracted files remain within the target directory. Malicious tarballs with `../` entries could write files outside the intended directory.
- **Remediation:**
  - [ ] Validate that resolved paths are within the destination directory
  - [ ] Use `filepath.Clean` and check for path traversal attempts

### MED-02: Missing Request Size Limit in Some Handlers
- **Location:** `internal/native/client.go:877`, `955`, `1001`
- **Description:** While the webhook handler limits body size to 1MB, API responses from GitHub are read without size limits.
- **Remediation:**
  - [ ] Use `io.LimitReader` for all `io.ReadAll` calls
  - [ ] Set reasonable limits based on expected response sizes

### MED-03: Insecure HTTP Timeout Configuration
- **Location:** `cmd/gale/webhook.go:404-410`
- **Description:** The local HTTP server has no explicit `MaxHeaderBytes` limit.
- **Remediation:**
  - [ ] Add `MaxHeaderBytes` configuration
  - [ ] Consider implementing rate limiting

### MED-04: Sensitive Data in Debug Logs
- **Location:** `internal/native/client.go:322-323`
- **Description:** Debug logging writes to stderr and may contain sensitive information about runners.
- **Remediation:**
  - [ ] Use structured logging with log levels
  - [ ] Ensure debug logging is disabled by default in production
  - [ ] Redact sensitive information from logs

### MED-05: Webhook Handler Uses Background Context for Long Operations
- **Location:** `internal/webhook/handler.go:377`, `400`
- **Description:** The webhook handler uses `context.Background()` for GitHub API calls and runner creation, ignoring the HTTP request context.
- **Remediation:**
  - [ ] Use request context with extended timeout
  - [ ] Implement proper cancellation handling

### MED-06: HTTP Client Without TLS Verification Control
- **Location:** `internal/native/client.go:700-718`
- **Description:** HTTP requests to download runners use `http.DefaultClient` without explicit TLS configuration.
- **Remediation:**
  - [ ] Use explicit TLS configuration
  - [ ] Implement certificate pinning for GitHub downloads
  - [ ] Verify checksums (already partially implemented)

### MED-07: Missing Input Validation on Repository Names
- **Location:** `cmd/gale/webhook_register.go:160-166`
- **Description:** Repository names from command line arguments are parsed without validation.
- **Remediation:**
  - [ ] Validate repository names against GitHub naming rules
  - [ ] Implement proper input sanitization

### MED-08: Process Cleanup Uses SIGKILL Without Warning
- **Location:** `internal/native/client.go:577`
- **Description:** Process cleanup uses SIGKILL directly without giving processes time to terminate gracefully.
- **Remediation:**
  - [ ] Send SIGTERM first with timeout
  - [ ] Only use SIGKILL as fallback

### MED-09: Docker Network Mode Not Restricted
- **Location:** `internal/docker/client.go:144-146`
- **Description:** The network mode is configurable without restrictions. Users could configure `host` network mode, bypassing container network isolation.
- **Remediation:**
  - [ ] Validate allowed network modes
  - [ ] Warn users about security implications of `host` mode

### MED-10: Config File Created with Default Permissions
- **Location:** `cmd/gale/init.go:205`
- **Description:** Config directory is created with 0755 permissions (config file itself is 0600).
- **Remediation:**
  - [ ] Use 0700 for configuration directories containing sensitive files

---

## Low

### LOW-01: Error Responses May Leak Information
- **Location:** `internal/webhook/handler.go:276-278`
- **Description:** Error messages are returned directly to clients.
- **Remediation:**
  - [ ] Return generic error messages to clients
  - [ ] Log detailed errors server-side only

### LOW-02: No Rate Limiting on Webhook Endpoint
- **Location:** `internal/webhook/handler.go:266`
- **Description:** The webhook endpoint has no rate limiting, relying solely on signature verification.
- **Remediation:**
  - [ ] Implement rate limiting per source IP
  - [ ] Add exponential backoff for failed verification attempts

### LOW-03: Health Endpoint Leaks Runner Count
- **Location:** `cmd/gale/webhook.go:386-389`
- **Description:** The health endpoint returns the active runner count.
- **Remediation:**
  - [ ] Only return "OK" for public health checks
  - [ ] Create separate authenticated status endpoint

### LOW-04: Temporary State Directory in /tmp
- **Location:** `cmd/gale/webhook.go:450`
- **Description:** Tailscale state is stored in `/tmp/gale-tsnet`.
- **Remediation:**
  - [ ] Store state in user-specific directory
  - [ ] Use XDG-compliant paths

### LOW-05: Missing Symlink Validation in Tar Extraction
- **Location:** `internal/native/client.go:786-790`
- **Description:** Symlink extraction does not validate the target.
- **Remediation:**
  - [ ] Validate symlink targets are within the destination
  - [ ] Reject absolute symlink targets

### LOW-06: Go 1.25.5 Requirement (Unstable Version)
- **Location:** `go.mod:3`
- **Description:** The module requires Go 1.25.5, which appears to be a future/unstable version.
- **Remediation:**
  - [ ] Use stable Go version (e.g., 1.23.x)
  - [ ] Update dependencies accordingly

---

## Positive Security Observations

- [x] Proper HMAC signature verification with constant-time comparison (`internal/webhook/handler.go:585-596`)
- [x] Cryptographically secure secret generation using `crypto/rand` (`internal/secret/generate.go`)
- [x] Config files saved with 0600 permissions (`internal/config/config.go:110`)
- [x] Environment variable expansion for secure credential management (`internal/config/config.go:83`)
- [x] Download checksum verification for runner binaries (`internal/native/client.go:127-131`)
- [x] Non-root container user in Dockerfile (`Dockerfile:31-34`)
- [x] Request body size limiting to 1MB (`internal/webhook/handler.go:273`)

---

## Compliance Notes

| Framework | Status | Notes |
|-----------|--------|-------|
| OWASP Top 10 | Partial | A01 (Broken Access Control), A03 (Injection) concerns |
| CIS Docker Benchmark | Partial | Docker socket mounting violates several controls |
| GitHub Security Guidelines | Good | Proper token handling patterns available |
