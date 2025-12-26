#!/bin/bash
# Discover Tailscale hosts for Kamal deployment
# Usage: ./scripts/tailscale-hosts.sh [tag]
#
# Requires:
#   - TAILSCALE_API_KEY environment variable
#   - TAILSCALE_TAILNET environment variable (e.g., "example.ts.net")
#
# Example:
#   export TAILSCALE_API_KEY="tskey-api-xxxx"
#   export TAILSCALE_TAILNET="mynet.ts.net"
#   ./scripts/tailscale-hosts.sh gale-runner

set -euo pipefail

TAG="${1:-}"
API_KEY="${TAILSCALE_API_KEY:-}"
TAILNET="${TAILSCALE_TAILNET:-}"

if [[ -z "$API_KEY" ]]; then
    echo "Error: TAILSCALE_API_KEY not set" >&2
    exit 1
fi

if [[ -z "$TAILNET" ]]; then
    echo "Error: TAILSCALE_TAILNET not set" >&2
    exit 1
fi

# Fetch devices from Tailscale API
response=$(curl -s -H "Authorization: Bearer ${API_KEY}" \
    "https://api.tailscale.com/api/v2/tailnet/${TAILNET}/devices")

# Filter by tag if provided
if [[ -n "$TAG" ]]; then
    echo "$response" | jq -r ".devices[] | select(.tags[]? | contains(\"tag:${TAG}\")) | .addresses[0]"
else
    echo "$response" | jq -r '.devices[] | .addresses[0]'
fi
