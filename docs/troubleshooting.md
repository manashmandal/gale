# Troubleshooting

## Tailscale Funnel Issues

### Connection Timeout / SSL Handshake Errors

**Symptoms:**

- `curl: (35) OpenSSL SSL_connect: SSL_ERROR_SYSCALL`
- Connection hangs then times out
- First request fails but subsequent requests work

**Causes:**

1. **First-time certificate request**: When using Tailscale Funnel for the first time (or after certificate expiry), tsnet requests a TLS certificate on-demand. This initial request can take 10-20 seconds and may cause the first connection to timeout.

   **Solution**: Wait for the certificate to be obtained (check logs for `cert("hostname"): got cert`), then retry.

2. **HTTP server WriteTimeout too short**: If `WriteTimeout` is set on the HTTP server, long-running webhook responses or slow certificate negotiation can cause connection drops.

   **Solution**: The funnel server should not have a `WriteTimeout` set, or it should be set to a high value (e.g., 60+ seconds).

### Funnel Not Accessible from Public Internet

**Symptoms:**

- Local curl via Tailscale internal IP works
- Public curl via funnel ingress IP fails

**Possible causes:**

1. **Funnel not enabled**: Ensure Funnel is enabled for your tailnet at https://login.tailscale.com/admin/dns

2. **Node not authorized for Funnel**: The specific node may need to be authorized in the Tailscale admin console.

3. **DNS propagation**: After enabling funnel, DNS may take a few minutes to propagate.

### Testing Funnel Connectivity

To test the funnel from the same machine (bypassing local Tailscale DNS resolution):

```bash
# Get public funnel IP
PUBLIC_IP=$(dig +short your-hostname.tail*.ts.net @8.8.8.8)

# Test with explicit IP resolution
curl --resolve "your-hostname.tail*.ts.net:443:$PUBLIC_IP" https://your-hostname.tail*.ts.net/health
```

## Runner Container Issues

### Container Stuck / Not Terminating

**Symptoms:**

- Runner container keeps running after job completion
- Multiple stale containers accumulate

**Solution**: Gale has a built-in runner timeout (default: 1 hour). Containers that exceed this timeout are automatically killed. Check your `runner.timeout` configuration if needed.

### Container Fails to Start

**Symptoms:**

- Job queued but never starts
- Errors about Docker socket

**Possible causes:**

1. **Docker socket not accessible**: Ensure Docker is running and the socket is accessible.

2. **Image pull issues**: Check network connectivity and Docker Hub rate limits.

3. **Resource constraints**: Ensure sufficient disk space and memory.

## Native Runner Issues

### Runner Not Picking Up Jobs

**Symptoms:**

- Jobs stay queued
- No runners appear in GitHub

**Debug steps:**

1. Check logs:

   ```bash
   gale logs -f
   ```

2. Dump debug info:

   ```bash
   gale debug
   ```

3. Verify runner directories exist:
   ```bash
   ls -la ~/.gale/native-runners/work/
   ```

### Native Runner on macOS

Native mode on macOS works but has some platform-specific behavior:

- Uses `--once` flag instead of `--ephemeral` to ensure Post steps complete
- Runner exits after completing one job (including all Post steps)
- If issues persist, consider using Docker mode which is more reliable

**Recommendation:** Docker mode is the most reliable option. Only use native mode if you specifically need macOS features (Xcode, iOS builds, etc.).

## Debugging Commands

### View Logs

```bash
gale logs              # Recent logs
gale logs -f           # Follow in real-time
gale logs -n 100       # Last 100 lines
gale logs --clear      # Clear log file
```

### Debug Information

```bash
gale debug             # Outputs JSON with:
                       # - System info (OS, arch, hostname)
                       # - Config (mode, labels, max runners)
                       # - Runner directories and logs
                       # - Disk usage
```
