# Custom Installation Script Guide

## Overview

`install-custom.sh` is a modified version of the NextDNS installer that points to YOUR repository instead of the official NextDNS repository.

## Quick Start

### Option 1: Use Your GitHub Releases

If you have forked NextDNS on GitHub and want to install from your releases:

```bash
export CUSTOM_GITHUB_REPO="yourusername/nextdns"
sh install-custom.sh
```

### Option 2: Install Local Build

For testing the local build without GitHub:

```bash
# Just copy the binary to the standard location
sudo cp nextdns /usr/local/bin/nextdns
sudo chmod +x /usr/local/bin/nextdns

# Then configure
sudo nextdns install -config-id=YOUR_CONFIG_ID
```

### Option 3: Use Custom Package Repository

If you have your own package repository:

```bash
export CUSTOM_GITHUB_REPO="yourusername/nextdns"
export CUSTOM_REPO_URL="https://your-repo.example.com"
sh install-custom.sh
```

## Configuration Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `CUSTOM_GITHUB_REPO` | Your GitHub repository (username/repo) | nextdns/nextdns |
| `CUSTOM_REPO_URL` | Your package repository URL | https://repo.nextdns.io |
| `NEXTDNS_VERSION` | Specific version to install | latest |
| `DEBUG` | Enable debug output | 0 |

## Examples

### Install from Your GitHub Fork

```bash
export CUSTOM_GITHUB_REPO="mojo_333/nextdns"
sh install-custom.sh
```

The installer will download binaries from:
`https://github.com/mojo_333/nextdns/releases/download/v{VERSION}/nextdns_{VERSION}_{OS}_{ARCH}.tar.gz`

### Install Specific Version

```bash
export CUSTOM_GITHUB_REPO="mojo_333/nextdns"
export NEXTDNS_VERSION="1.44.0"
sh install-custom.sh
```

### Debug Mode

```bash
export DEBUG=1
export CUSTOM_GITHUB_REPO="mojo_333/nextdns"
sh install-custom.sh
```

## Preparing GitHub Releases

To use the custom installer with GitHub releases, you need to:

### 1. Create GitHub Release

```bash
# Tag your version
git tag -a v1.44.0 -m "Release v1.44.0 with bug fixes and optimizations"
git push origin v1.44.0
```

### 2. Build Binaries for All Platforms

```bash
# Build for multiple platforms
GOOS=linux GOARCH=amd64 go build -o nextdns -ldflags="-s -w" .
tar czf nextdns_1.44.0_linux_amd64.tar.gz nextdns

GOOS=linux GOARCH=arm64 go build -o nextdns -ldflags="-s -w" .
tar czf nextdns_1.44.0_linux_arm64.tar.gz nextdns

GOOS=darwin GOARCH=amd64 go build -o nextdns -ldflags="-s -w" .
tar czf nextdns_1.44.0_darwin_amd64.tar.gz nextdns

# ... etc for other platforms
```

### 3. Upload to GitHub Release

1. Go to https://github.com/yourusername/nextdns/releases
2. Click "Draft a new release"
3. Choose the tag (v1.44.0)
4. Upload all the tar.gz files
5. Publish release

### 4. Users Can Install

```bash
export CUSTOM_GITHUB_REPO="yourusername/nextdns"
sh install-custom.sh
```

## Quick Test Installation (No GitHub Required)

For immediate testing without GitHub setup:

```bash
# Navigate to build directory
cd /home/mojo_333/nextdns

# Install binary directly
sudo mkdir -p /usr/local/bin
sudo cp nextdns /usr/local/bin/nextdns
sudo chmod +x /usr/local/bin/nextdns

# Configure and start
sudo /usr/local/bin/nextdns install -config-id=YOUR_CONFIG_ID
sudo /usr/local/bin/nextdns start
```

## Platform Support

The custom installer supports:

**Binary Install (works with any GitHub repo):**
- ✅ Linux (all distributions)
- ✅ macOS
- ✅ FreeBSD
- ✅ OpenBSD
- ✅ NetBSD

**Package Install (requires custom package repo):**
- RPM (CentOS, Fedora, RHEL) - Needs custom RPM repo
- DEB (Debian, Ubuntu) - Needs custom DEB repo
- APK (Alpine) - Needs custom APK repo
- OpenWrt packages - Needs custom opkg repo

**Note:** If you set `CUSTOM_GITHUB_REPO`, the installer will automatically use binary install for package managers (since you likely don't have a custom package repository).

## Differences from Original install.sh

| Feature | Original | Custom |
|---------|----------|--------|
| GitHub repo | nextdns/nextdns | Configurable via `CUSTOM_GITHUB_REPO` |
| Package repo | repo.nextdns.io | Configurable via `CUSTOM_REPO_URL` |
| Binary install | From official releases | From your GitHub releases |
| Package install | From official repos | Falls back to binary install |
| Homebrew | Uses official tap | Falls back to binary install |

## Troubleshooting

### "Cannot get latest version"

Your GitHub repository needs a release with proper tags:
```bash
git tag -a v1.44.0 -m "Release v1.44.0"
git push origin v1.44.0
```

Then create a GitHub release for that tag with binaries.

### "404 Not Found" when downloading

Check that your GitHub release has the correct filename format:
```
nextdns_{VERSION}_{OS}_{ARCH}.tar.gz
```

Example: `nextdns_1.44.0_linux_amd64.tar.gz`

### Permission denied

Run with sudo:
```bash
sudo sh install-custom.sh
```

### Want to use local build instead

Just copy directly:
```bash
sudo cp nextdns /usr/local/bin/nextdns
sudo nextdns install -config-id=YOUR_CONFIG_ID
```

## Production Deployment

For production use, it's recommended to:

1. **Fork the repository** on GitHub
2. **Create releases** with pre-built binaries
3. **Use the custom installer** pointing to your fork
4. **Test thoroughly** before deploying

Or simply:

1. **Copy the binary** to production servers
2. **Run install command** directly
3. **Configure** via CLI

## Support

The custom installer is based on the official NextDNS installer with modified repository URLs. All functionality remains the same, just pointing to your repository instead.
