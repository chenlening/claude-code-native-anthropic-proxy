# macOS Installation Guide

Install the Anthropic Transparent Proxy on macOS using launchd (macOS's native service manager, equivalent to systemd on Linux).

## Prerequisites

- macOS (any recent version)
- Go 1.23+ (installed automatically if missing)

## Installation

### Step 1: Install Go (if needed)

Check if Go is installed:

```bash
go version 2>/dev/null || echo "not found"
```

If not found or below 1.23, install it:

```bash
# Detect architecture
ARCH=$(uname -m)  # arm64 or x86_64

cd /tmp
curl -LO https://go.dev/dl/go1.23.7.darwin-${ARCH}.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.23.7.darwin-${ARCH}.tar.gz
rm go1.23.7.darwin-${ARCH}.tar.gz

# Add to PATH
echo 'export PATH=/usr/local/go/bin:$PATH' >> ~/.zshrc
export PATH=/usr/local/go/bin:$PATH

# Verify
go version
```

### Step 2: Build the Proxy

```bash
git clone https://github.com/2012geek/anthropic-transparent-proxy.git
cd anthropic-transparent-proxy
go build -o bin/proxy ./cmd/proxy
```

### Step 2: Create the launchd Service

Create the service plist file:

```bash
mkdir -p ~/Library/LaunchAgents
cat > ~/Library/LaunchAgents/com.anthropic.proxy.plist << 'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.anthropic.proxy</string>
    <key>ProgramArguments</key>
    <array>
        <string>/Users/YOUR_USERNAME/workspace/anthropic-transparent-proxy/bin/proxy</string>
        <string>--config</string>
        <string>/Users/YOUR_USERNAME/workspace/anthropic-transparent-proxy/configs/proxy.yaml</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
    </dict>
    <key>StandardOutPath</key>
    <string>/tmp/proxy-anthropic.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/proxy-anthropic.log</string>
    <key>WorkingDirectory</key>
    <string>/Users/YOUR_USERNAME/workspace/anthropic-transparent-proxy</string>
</dict>
</plist>
EOF
```

**Important:** Replace `YOUR_USERNAME` with your actual macOS username in the paths above.

### Step 3: Load the Service

```bash
# Stop any running proxy processes
pkill -f "./bin/proxy"

# Load the launchd service
launchctl load ~/Library/LaunchAgents/com.anthropic.proxy.plist
```

### Step 4: Verify

```bash
# Check health (bypass any proxy env vars)
curl --noproxy '*' http://localhost:8080/health
```

## Managing the Service

```bash
# View logs
tail -f /tmp/proxy-anthropic.log

# Stop the service
launchctl unload ~/Library/LaunchAgents/com.anthropic.proxy.plist

# Restart (after config changes)
launchctl unload ~/Library/LaunchAgents/com.anthropic.proxy.plist
launchctl load ~/Library/LaunchAgents/com.anthropic.proxy.plist
```

## Configuring Claude Code

Add to `~/.claude/settings.json`:

```json
{
  "env": {
    "ANTHROPIC_AUTH_TOKEN": "anykey",
    "ANTHROPIC_BASE_URL": "http://localhost:8080",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "claude-haiku-3-5-20241022",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "claude-sonnet-4-20250514",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "claude-opus-4-20250514"
  }
}
```

Then restart Claude Code for changes to take effect.

## Changing the Port

The default port is 8080 (to avoid conflicts with other services on 8080). To change:

1. Edit `configs/proxy.yaml` and change `listen: ":8080"` to your desired port
2. Restart the service:
   ```bash
   launchctl unload ~/Library/LaunchAgents/com.anthropic.proxy.plist
   launchctl load ~/Library/LaunchAgents/com.anthropic.proxy.plist
   ```
3. Update `ANTHROPIC_BASE_URL` in `~/.claude/settings.json` to match

## Uninstall

```bash
# Stop and remove service
launchctl unload ~/Library/LaunchAgents/com.anthropic.proxy.plist
rm ~/Library/LaunchAgents/com.anthropic.proxy.plist
```

## Troubleshooting

### Service won't start

Check if the binary path is correct and the file is executable:
```bash
ls -la ~/Library/LaunchAgents/com.anthropic.proxy.plist
ls -la ~/workspace/anthropic-transparent-proxy/bin/proxy
```

### Port already in use

Check what's using the port:
```bash
lsof -i :8080
```

### Health check fails with proxy error

If you have `ALL_PROXY` or `HTTP_PROXY` env vars set, use `--noproxy '*'`:
```bash
curl --noproxy '*' http://localhost:8080/health
```

Or unset the proxy temporarily:
```bash
unset ALL_PROXY
curl http://localhost:8080/health
```

## Comparison: launchd vs systemd

| systemd (Linux) | launchd (macOS) |
|----------------|-----------------|
| `systemctl start proxy-anthropic` | `launchctl start com.anthropic.proxy` |
| `systemctl stop proxy-anthropic` | `launchctl stop com.anthropic.proxy` |
| `systemctl status proxy-anthropic` | `launchctl list | grep com.anthropic` |
| `journalctl -u proxy-anthropic` | `tail -f /tmp/proxy-anthropic.log` |
| Service file: `/etc/systemd/system/` | Service file: `~/Library/LaunchAgents/` |