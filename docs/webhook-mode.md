# Webhook Mode

Instead of polling the GitHub API, Gale can receive webhook events directly from GitHub when jobs are queued. **This is the recommended mode for production use.**

## Why Webhook > Polling?

| Aspect | Polling Mode | Webhook Mode |
|--------|--------------|--------------|
| API Rate Limit | Consumes 5,000 req/hr limit | Zero API calls |
| Response Time | Up to `poll_interval` delay | Instant (~100ms) |
| Resource Usage | Constant CPU/network | Near-zero when idle |
| Scalability | Limited by rate limits | Unlimited repos |

**The Math**: Polling every 10s = 360 API calls/hour per repo. With 14 repos, you'd hit the 5,000/hr limit in under an hour. Webhook mode uses **zero** API calls for job detection.

## Starting Webhook Server

### Local Server

If you have a public IP or reverse proxy:

```bash
gale webhook --port 8080
```

### With Tailscale Funnel (Recommended)

Zero-config public HTTPS endpoint:

```bash
gale webhook --funnel
```

See [Tailscale Funnel](tailscale-funnel.md) for detailed setup.

## GitHub Webhook Configuration

1. Go to your repo/org → **Settings** → **Webhooks** → **Add webhook**

2. Configure:
   - **Payload URL**: Your server URL + `/webhook`
     - Funnel: `https://gale-hostname.tailnet.ts.net/webhook`
     - Local: `https://your-server:8080/webhook`
   - **Content type**: `application/json`
   - **Secret**: Optional but recommended for security
   - **Events**: Select **"Workflow jobs"** only

3. Save and verify the ping succeeds (green checkmark)

## Webhook Secret

For security, configure a webhook secret:

1. Generate a secret:
   ```bash
   openssl rand -hex 32
   ```

2. Add to GitHub webhook settings

3. Configure in Gale:
   ```yaml
   webhook:
     secret: ${WEBHOOK_SECRET}
   ```

   Or with GitHub App:
   ```yaml
   github:
     app:
       webhook_secret: ${GALE_WEBHOOK_SECRET}
   ```

## Running as Daemon

Run webhook server in the background:

```bash
# Start daemon
gale webhook --daemon

# With Funnel
gale webhook --funnel --daemon

# Stop daemon
gale stop
```

## Verifying Setup

1. Check webhook server is running:
   ```bash
   gale status
   ```

2. Trigger a test workflow with `runs-on: gale`

3. Watch logs:
   ```bash
   gale webhook -l debug
   ```

4. Check GitHub webhook delivery history for any errors
