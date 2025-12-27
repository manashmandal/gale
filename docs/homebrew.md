# Homebrew Distribution (Private Repos)

This document explains how Gale is distributed via Homebrew for private repositories.

## Overview

GitHub release assets from private repositories require authentication to download. Standard Homebrew Casks cannot access private repos, so Gale uses a custom Formula with `GitHubPrivateRepositoryDownloadStrategy`.

## Installation

### Prerequisites

Set your GitHub token with `repo` scope:

```bash
export HOMEBREW_GITHUB_API_TOKEN="ghp_your_token_here"
```

Add this to your shell profile (`~/.zshrc`, `~/.bashrc`, etc.) for persistence.

### Install

```bash
brew tap manashmandal/tap
brew install manashmandal/tap/gale
```

### Update

```bash
brew update && brew upgrade gale
```

### Troubleshooting

If you encounter issues, try a clean reinstall:

```bash
brew uninstall gale
brew untap manashmandal/tap
brew tap manashmandal/tap
brew install manashmandal/tap/gale
```

## How It Works

### The Problem with Casks

Standard Homebrew Casks use direct URLs to download assets:

```ruby
url "https://github.com/owner/repo/releases/download/v1.0.0/file.tar.gz"
```

For private repos, this returns 404 because authentication is required.

### The Solution: Custom Download Strategy

The [homebrew-tap](https://github.com/manashmandal/homebrew-tap) repository contains:

```
homebrew-tap/
├── Formula/
│   └── gale.rb              # Formula using private strategy
└── lib/
    └── private_strategy.rb  # Custom download strategy
```

The `GitHubPrivateRepositoryDownloadStrategy` in `lib/private_strategy.rb`:

1. Parses the release URL to extract owner, repo, tag, and filename
2. Uses `HOMEBREW_GITHUB_API_TOKEN` for authentication
3. Fetches release metadata via GitHub API
4. Downloads the asset using the authenticated API endpoint

```ruby
# Formula/gale.rb
require_relative "../lib/private_strategy"

class Gale < Formula
  on_macos do
    on_arm do
      url "https://github.com/manashmandal/gale/releases/download/v2025.1227.0/gale_2025.1227.0_darwin_arm64.tar.gz",
          using: GitHubPrivateRepositoryDownloadStrategy
      sha256 "..."
    end
  end
  # ...
end
```

## Release Automation

The Formula is automatically updated when a new release is created. The [release workflow](../.github/workflows/release.yml):

1. GoReleaser builds binaries and creates the GitHub release
2. A post-release step fetches checksums from the release
3. Generates an updated Formula with new version and SHA256 values
4. Commits the Formula to the homebrew-tap repository

### Manual Formula Update

If you need to manually update the Formula:

```bash
# Get checksums from release
curl -sL "https://api.github.com/repos/manashmandal/gale/releases/latest" \
  -H "Authorization: token $GITHUB_TOKEN" | \
  jq -r '.assets[] | "\(.name): \(.digest)"'

# Update Formula/gale.rb with new version and checksums
```

## Going Public

If the repository becomes public in the future:

1. Standard Casks will work without the custom download strategy
2. Users won't need `HOMEBREW_GITHUB_API_TOKEN`
3. The Formula can be simplified to use standard URLs

To switch to a public Cask, re-enable `homebrew_casks` in `.goreleaser.yaml` and update the Formula to remove the `using:` parameter.
