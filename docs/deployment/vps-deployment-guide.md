# VPS Deployment Guide

**Date:** 2026-04-23
**Author:** Claude

---

## Overview

This guide covers deploying the Anthropic Transparent Proxy on a public cloud VPS with SSH tunnel access control.

## Prerequisites

- A VPS with SSH access (Ubuntu/Debian assumed)
- SSH key authentication configured
- Domain or static IP for the VPS

---

## VPS Setup

### 1. Install Proxy Binary

```bash
# SSH to VPS
ssh user@vps-host

# Create proxy user (for security isolation)
sudo useradd -r -s /bin/false proxy

# Create config directory
sudo mkdir -p /etc/proxy
sudo chown proxy:proxy /etc/proxy

# Copy binary to VPS (from your local machine)
scp bin/proxy user@vps-host:/tmp/proxy
ssh user@vps-host "sudo mv /tmp/proxy /usr/local/bin/proxy && sudo chmod +x /usr/local/bin/proxy && sudo chown proxy:proxy /usr/local/bin/proxy"
```

### 2. Configure Firewall

```bash
# SSH to VPS
ssh user@vps-host

# Configure UFW (Ubuntu/Debian)
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow 22/tcp    # SSH - needed for tunnel creation
sudo ufw enable

# Verify rules
sudo ufw status verbose
```

**Result:**
- Port 22 (SSH): Open to internet
- Port 8080 (proxy): Blocked from internet

### 3. Systemd Service

The service file is included in the repo at `deploy/proxy-anthropic.service`. Copy it to systemd:

```bash
# On VPS - copy service file from repo or create manually
sudo vim /etc/systemd/system/proxy-anthropic.service
```

```ini
[Unit]
Description=Anthropic Transparent Proxy
After=network.target

[Service]
Type=simple
User=proxy
Group=proxy
ExecStart=/usr/local/bin/proxy --config /etc/proxy/proxy.yaml
Restart=on-failure
RestartSec=5s
StandardOutput=journal
StandardError=journal
SyslogIdentifier=proxy-anthropic

[Install]
WantedBy=multi-user.target
```

```bash
# On VPS
sudo systemctl daemon-reload
sudo systemctl enable proxy-anthropic
sudo systemctl start proxy-anthropic
sudo systemctl status proxy-anthropic
```

### 4. Configuration File

Create `/etc/proxy/proxy.yaml`:

```yaml
server:
  listen: "127.0.0.1:8080"  # localhost only - critical for security
  read_timeout: 30s
  write_timeout: 120s
  idle_timeout: 90s

logging:
  level: info
  format: json

metrics:
  enabled: true
  path: /metrics

health:
  path: /health

routing:
  default_strategy: least-connections

models:
  # Add your model mappings
  claude-sonnet-4-20250514:
    backends:
      - endpoint: endpoint-a
        model: "claude-sonnet-4-20250514"
        weight: 10

endpoints:
  endpoint-a:
    url: "https://api.anthropic.com"
    api_key: "${ANTHROPIC_API_KEY}"
    timeout: 90s

endpoint_health:
  failures_to_disable: 5
  recovery_probe_interval: 30s
  successes_to_enable: 2
```

```bash
# On VPS
sudo chown proxy:proxy /etc/proxy/proxy.yaml
sudo systemctl restart proxy
```

---

## Client Setup

### 1. SSH Key Generation

```bash
# On client machine (one time)
ssh-keygen -t ed25519 -C "client@hostname"

# Copy public key to VPS
ssh-copy-id user@vps-host
```

### 2. SSH Config

Add to `~/.ssh/config`:

```
Host proxy-tunnel
    HostName your-vps-ip-or-domain
    User user
    LocalForward 8080 localhost:8080
    IdentityFile ~/.ssh/id_ed25519
    ServerAliveInterval 60
    ServerAliveCountMax 3
```

### 3. Create Tunnel

```bash
# Start tunnel (runs in foreground)
ssh proxy-tunnel -N

# Or run in background with ControlMaster
ssh -f -N -M -S /tmp/proxy-socket proxy-tunnel
```

### 4. Verify Connection

```bash
# Check tunnel is active
ssh -O check proxy-tunnel

# Test proxy is reachable
curl -s http://localhost:8080/health
```

---

## Usage

### Complete Workflow

```bash
# 1. Terminal 1: Start tunnel
ssh proxy-tunnel -N
# Keep this terminal open

# 2. Terminal 2: Use Claude Code
export ANTHROPIC_BASE_URL="http://localhost:8080"
export ANTHROPIC_API_KEY="any-value"  # Proxy ignores this
claude

# 3. Done: Ctrl+C the tunnel when finished
```

### SSH Tunnel Options

| Option | Command | Use case |
|--------|---------|----------|
| Foreground | `ssh proxy-tunnel -N` | Quick test, see output |
| Background | `ssh -f -N proxy-tunnel` | Persistent tunnel |
| ControlMaster | `ssh -f -N -M -S /tmp/socket proxy-tunnel` | Reuse connection |

### Auto-Reconnect

Add to `~/.ssh/config`:

```
Host proxy-tunnel
    # ... existing settings ...
    ServerAliveInterval 60
    ServerAliveCountMax 3
    TCPKeepAlive yes
```

Or use ` autossh` for automatic reconnection:

```bash
# Install autossh
brew install autossh  # macOS
# or: sudo apt install autossh  # Ubuntu

# Run with auto-reconnect
autossh -M 0 -o "ServerAliveInterval 60" -o "ServerAliveCountMax 3" proxy-tunnel -N
```

---

## Troubleshooting

### Tunnel Won't Start

```bash
# Check SSH key is added
ssh-add -l

# Add key if needed
ssh-add ~/.ssh/id_ed25519

# Test SSH directly
ssh user@vps-host
```

### Proxy Not Reachable

```bash
# On VPS, check proxy is running
sudo systemctl status proxy-anthropic

# Check it's listening on localhost
sudo ss -tlnp | grep 8080

# Check firewall
sudo ufw status
```

### Permission Denied

```bash
# On VPS, check proxy user has correct permissions
ls -la /usr/local/bin/proxy
ls -la /etc/proxy/proxy.yaml
sudo journalctl -u proxy-anthropic -n 50
```

---

## Security Checklist

- [ ] Proxy binds to `127.0.0.1:8080` (not `0.0.0.0:8080`)
- [ ] Firewall blocks port 8080 from internet
- [ ] SSH key authentication enabled (not password)
- [ ] Proxy runs as dedicated user (not root)
- [ ] API keys use environment variables (not hardcoded)
- [ ] Regular security updates enabled on VPS

---

## Maintenance

### Update Proxy

```bash
# Build new binary locally
make build

# Copy to VPS
scp bin/proxy user@vps-host:/tmp/proxy
ssh user@vps-host "sudo systemctl stop proxy-anthropic && sudo mv /tmp/proxy /usr/local/bin/proxy && sudo chmod +x /usr/local/bin/proxy && sudo chown proxy:proxy /usr/local/bin/proxy && sudo systemctl start proxy-anthropic"
```

### View Logs

```bash
# On VPS
sudo journalctl -u proxy-anthropic -f
```

### Rotate API Keys

Edit `/etc/proxy/proxy.yaml` with new keys, then:

```bash
sudo systemctl restart proxy-anthropic
```
