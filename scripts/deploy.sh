#!/bin/bash
# Deploy Gale using Kamal with optional Tailscale integration
#
# Usage:
#   ./scripts/deploy.sh                    # Deploy to GALE_HOST
#   ./scripts/deploy.sh --tailscale        # Deploy to Tailscale hosts with tag:gale
#   ./scripts/deploy.sh --tailscale myhost # Deploy to specific Tailscale host

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_DIR"

USE_TAILSCALE=false
TAILSCALE_HOST=""

while [[ $# -gt 0 ]]; do
    case $1 in
        --tailscale)
            USE_TAILSCALE=true
            if [[ -n "${2:-}" && ! "$2" =~ ^-- ]]; then
                TAILSCALE_HOST="$2"
                shift
            fi
            shift
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Load secrets
if [[ -f .kamal/secrets.local ]]; then
    export $(grep -v '^#' .kamal/secrets.local | xargs)
elif [[ -f .kamal/secrets ]]; then
    echo "Warning: Using .kamal/secrets template. Copy to .kamal/secrets.local and fill in values." >&2
fi

# Determine deployment host
if [[ "$USE_TAILSCALE" == "true" ]]; then
    if [[ -n "$TAILSCALE_HOST" ]]; then
        # Use specific Tailscale hostname
        export GALE_HOST="$TAILSCALE_HOST"
        export TAILSCALE_SSH=1
    else
        # Discover hosts with tag:gale
        if [[ -z "${TAILSCALE_API_KEY:-}" || -z "${TAILSCALE_TAILNET:-}" ]]; then
            echo "Error: TAILSCALE_API_KEY and TAILSCALE_TAILNET required for discovery" >&2
            exit 1
        fi

        hosts=$("$SCRIPT_DIR/tailscale-hosts.sh" gale)
        if [[ -z "$hosts" ]]; then
            echo "Error: No hosts found with tag:gale" >&2
            exit 1
        fi

        export GALE_HOST=$(echo "$hosts" | head -1)
        export TAILSCALE_SSH=1
        echo "Discovered host: $GALE_HOST"
    fi
fi

echo "Deploying Gale to ${GALE_HOST:-default host}..."

# Run Kamal deploy
kamal deploy -c config/deploy.yml
