# Deployment

Options for running Gale in production.

## Systemd Service

Create a systemd service for automatic startup and restart:

```ini
# /etc/systemd/system/gale.service
[Unit]
Description=Gale GitHub Actions Runner Autoscaler
After=docker.service
Requires=docker.service

[Service]
Type=simple
User=gale
Environment="GITHUB_TOKEN=ghp_xxxx"
Environment="GITHUB_OWNER=your-username"
ExecStart=/usr/local/bin/gale webhook --funnel
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl daemon-reload
sudo systemctl enable gale
sudo systemctl start gale

# View logs
journalctl -u gale -f
```

## Docker

### Basic Docker Run

```bash
docker run -d \
  --name gale \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e GITHUB_TOKEN=ghp_xxxx \
  -e GITHUB_OWNER=your-username \
  ghcr.io/manashmandal/gale:latest
```

### Docker Compose

```yaml
# docker-compose.yml
version: '3.8'

services:
  gale:
    image: ghcr.io/manashmandal/gale:latest
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./config.yaml:/etc/gale/config.yaml:ro
    environment:
      - GITHUB_TOKEN=${GITHUB_TOKEN}
      - GITHUB_OWNER=${GITHUB_OWNER}
    command: webhook --config /etc/gale/config.yaml
```

### With Tailscale Funnel

```yaml
# docker-compose.yml
version: '3.8'

services:
  gale:
    image: ghcr.io/manashmandal/gale:latest
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./config.yaml:/etc/gale/config.yaml:ro
      - gale-tsnet:/root/.config/gale  # Persist Tailscale state
    environment:
      - GITHUB_TOKEN=${GITHUB_TOKEN}
      - GITHUB_OWNER=${GITHUB_OWNER}
    command: webhook --funnel --config /etc/gale/config.yaml

volumes:
  gale-tsnet:
```

## Kamal Deployment

Gale includes [Kamal](https://kamal-deploy.org/) configuration for easy deployment to remote servers.

### Setup

1. Install Kamal:
   ```bash
   gem install kamal
   ```

2. Configure secrets:
   ```bash
   cp .kamal/secrets .kamal/secrets.local
   # Edit .kamal/secrets.local with your values
   ```

3. Deploy:
   ```bash
   # Deploy to configured host
   ./scripts/deploy.sh

   # Deploy to a Tailscale host
   ./scripts/deploy.sh --tailscale my-server

   # Auto-discover hosts with tag:gale on Tailscale
   ./scripts/deploy.sh --tailscale
   ```

### Tailscale Integration

To use Tailscale for deployment:

1. Get a Tailscale API key from https://login.tailscale.com/admin/settings/keys

2. Tag your target servers with `tag:gale` in Tailscale ACLs

3. Configure environment:
   ```bash
   export TAILSCALE_API_KEY="tskey-api-xxxx"
   export TAILSCALE_TAILNET="your-tailnet.ts.net"
   ```

4. Deploy:
   ```bash
   ./scripts/deploy.sh --tailscale
   ```

### Manual Kamal Commands

```bash
# Deploy
kamal deploy -c config/deploy.yml

# View logs
kamal logs -c config/deploy.yml

# Open shell
kamal shell -c config/deploy.yml

# Rollback
kamal rollback -c config/deploy.yml
```

## Rate Limiting Considerations

When monitoring many repositories (org mode) with polling, Gale makes multiple API calls per cycle. GitHub's rate limit is 5,000 requests/hour.

**Recommendations:**
- Use **webhook mode** (eliminates polling)
- Use **GitHub App** (5,000 requests/hour per installation)
- Increase `poll_interval` if using polling mode
- Use single-repo mode for high-frequency scenarios

## Health Checks

Check Gale status:

```bash
gale status
```

For Docker deployments, add a health check:

```yaml
healthcheck:
  test: ["CMD", "gale", "status"]
  interval: 30s
  timeout: 10s
  retries: 3
```
