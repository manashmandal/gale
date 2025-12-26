# GitHub App Authentication

GitHub Apps provide significant advantages over Personal Access Tokens (PATs) for production use.

## Why GitHub App?

| Feature | PAT | GitHub App |
|---------|-----|------------|
| Rate Limit | 5,000/hr shared | 5,000/hr per installation |
| Token Rotation | Manual | Automatic (1hr tokens) |
| Permissions | User-wide | Granular per-app |
| Multi-org | Separate PATs | Single app, multiple installs |
| Security | Long-lived | Short-lived tokens |
| Webhook | Manual per-repo | Configured in app |

## Quick Start

### Option 1: Interactive Setup

```bash
gale app create
```

This wizard will:
1. Guide you through creating a GitHub App
2. Download the private key
3. Configure Gale automatically

### Option 2: Manual Setup

View setup instructions:

```bash
gale app setup
```

## Manual GitHub App Creation

1. Go to **GitHub Settings** → **Developer Settings** → **GitHub Apps** → **New GitHub App**

2. Configure the app:
   - **Name**: `gale-runner` (or your choice)
   - **Homepage URL**: Your server URL or `https://github.com/manashmandal/gale`
   - **Webhook URL**: Your Gale webhook URL (if using webhook mode)
   - **Webhook Secret**: Generate and save securely

3. Set permissions:
   - **Repository permissions:**
     - Actions: Read-only
     - Metadata: Read-only
   - **Subscribe to events:**
     - Workflow jobs

4. Set visibility:
   - **Where can this app be installed?**: Only on this account (private)

5. Create the app and note the **App ID**

6. Generate a **private key** and download the `.pem` file

7. Install the app on desired repos/org

## Configuration

### Using Private Key File (Recommended)

```yaml
github:
  app:
    app_id: 123456
    private_key_path: /etc/gale/private-key.pem
    webhook_secret: ${GALE_WEBHOOK_SECRET}
```

### Using Environment Variable

```bash
export GALE_PRIVATE_KEY="$(cat /path/to/private-key.pem)"
```

```yaml
github:
  app:
    app_id: 123456
    # private_key loaded from GALE_PRIVATE_KEY env var
    webhook_secret: ${GALE_WEBHOOK_SECRET}
```

### Inline Key (Not Recommended)

```yaml
github:
  app:
    app_id: 123456
    private_key: |
      -----BEGIN RSA PRIVATE KEY-----
      MIIEpAIBAAKCAQEA...
      -----END RSA PRIVATE KEY-----
    webhook_secret: ${GALE_WEBHOOK_SECRET}
```

## Validation

Verify your configuration:

```bash
gale app validate
```

This checks:
- App ID is configured
- Private key is valid and accessible
- Can authenticate with GitHub
- App has correct permissions

## How It Works

```mermaid
sequenceDiagram
    participant GH as GitHub
    participant Gale
    participant Runner

    GH->>Gale: Webhook: job queued
    Note over Gale: Extract installation_id
    Gale->>GH: Generate JWT
    Gale->>GH: Request installation token
    GH-->>Gale: Installation token (1hr)
    Gale->>Runner: Create with token
    Runner->>GH: Register & run job
```

1. GitHub sends webhook with `installation_id`
2. Gale generates a JWT using the private key
3. Gale exchanges JWT for an installation access token
4. Token is used to create the runner (valid for 1 hour)
5. Tokens are cached and automatically refreshed

## Switching from PAT to App

1. Create and configure the GitHub App (see above)

2. Update your config:
   ```yaml
   github:
     # token: ${GITHUB_TOKEN}  # Remove or comment out
     app:
       app_id: 123456
       private_key_path: /etc/gale/private-key.pem
   ```

3. Restart Gale:
   ```bash
   gale stop
   gale webhook --funnel
   ```

4. Verify:
   ```bash
   gale app validate
   ```
