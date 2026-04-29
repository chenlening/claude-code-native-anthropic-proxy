# Security Hardening for Public Network Deployment

**Date:** 2026-04-23
**Status:** Proposed
**Author:** Claude

---

## Background

When deploying the transparent proxy on a public cloud VPS, the proxy faces network exposure to the open internet. Without protection, anyone can send requests to the proxy, potentially:

- Consuming API quota at the owner's expense
- Accessing configured backend endpoints
- Causing unexpected costs

This document outlines the security approach to protect the proxy while maintaining transparency and simplicity.

---

## Problem Statement

**Original design assumed trusted network:**
- Proxy listens on all interfaces (`:8080`)
- No authentication required
- Relies on network-level isolation

**Public deployment requirements:**
- Only authorized clients can access the proxy
- Backend API credentials must remain protected
- Zero performance impact on SSE streaming

---

## Design Decisions

### Decision 1: SSH Tunnel for Access Control

**Problem:** How to restrict access to the proxy?

| Option | Pros | Cons |
|--------|------|------|
| HTTPS + API tokens | Industry standard | Proxy-side TLS overhead |
| SSH tunnel | Kernel crypto, zero proxy overhead | Point-to-point only |
| VPN (WireGuard) | Full mesh, multi-user | Extra daemon to manage |
| Cloud private networking | Zero overhead | Only works within same cloud region |

**Decision:** SSH tunnel

**Rationale:**
- Kernel-level encryption (ChaCha20/AES-256-NI, hardware accelerated)
- No proxy-side TLS termination needed
- Aligns with single-binary deployment principle
- Negligible performance overhead (<0.5% CPU)
- Forward secrecy (compromised key can't decrypt past sessions)
- Well-understood, battle-tested (25+ years)

---

### Decision 2: Localhost-Only Binding

**Problem:** Where should the proxy listen?

| Option | Pros | Cons |
|--------|------|------|
| All interfaces (`:8080`) | Any client can reach it | Exposed to internet |
| localhost only (`127.0.0.1:8080`) | Only local/SSh-tunneled clients | No direct external access |

**Decision:** localhost only

**Rationale:**
- Proxy is never directly accessible from internet
- Only clients with SSH access can create tunnel
- Defense in depth: even if SSH key is compromised, proxy remains protected
- No firewall holes needed for proxy port

---

### Decision 3: No Token Middleware

**Problem:** Do we need API-level authentication?

| Option | Pros | Cons |
|--------|------|------|
| Token middleware | Per-user tracking, defense in depth | Adds complexity |
| SSH-only | Simpler, no overhead | No per-user granularity |

**Decision:** SSH-only (no token middleware)

**Rationale:**
- SSH key authentication is sufficient for small team
- Only SSH-key authorized users can reach VPS
- Transparent proxy principles: minimal complexity
- Per-user tracking not required for current use case

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        Client Machine                         │
│                                                              │
│  Terminal 1:                                                │
│  $ ssh -L 8080:localhost:8080 user@vps-host -N              │
│         │                                                    │
│         │ SSH tunnel (encrypted, AES-256)                   │
│         ▼                                                    │
│  ┌──────────────────┐                                        │
│  │ Claude Code App  │ ──HTTP──► localhost:8080              │
│  └──────────────────┘                                        │
└────────────────────────────┬────────────────────────────────┘
                             │
                             │ SSH encrypted tunnel
                             ▼
┌─────────────────────────────────────────────────────────────┐
│                         VPS (Cloud)                           │
│                                                              │
│  Firewall:                                                  │
│  - Port 22 (SSH): Open to internet (for admin access)       │
│  - Port 8080 (proxy): DENY from internet                    │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  SSH Server (sshd)                                     │  │
│  │  - Authenticates users with SSH keys                   │  │
│  │  - Creates local tunnel to localhost:8080             │  │
│  └────────────────────────────────────────────────────────┘  │
│                           │                                   │
│                           │ Plaintext (local)                │
│                           ▼                                   │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  Proxy (127.0.0.1:8080)                               │  │
│  │  - Receives plaintext from localhost                  │  │
│  │  - No TLS (SSH handles encryption)                   │  │
│  │  - Transparent proxy (model name only)                │  │
│  └────────────────────────────────────────────────────────┘  │
│                           │                                   │
│                           │ HTTPS                            │
│                           ▼                                   │
│                    ┌─────────────────┐                        │
│                    │  Backend API    │                        │
│                    │ (Anthropic/etc) │                        │
│                    └─────────────────┘                        │
└─────────────────────────────────────────────────────────────┘
```

---

## Configuration Changes

### 1. Server Listen Address

**File:** `cmd/proxy/main.go`

```go
// Before:
Addr: cfg.Server.Listen,

// After:
// Always bind to localhost for security
// (SSH tunnel handles external access)
server := &http.Server{
    Addr:         "127.0.0.1:8080",  // localhost only
    Handler:      mux,
    ReadTimeout:  cfg.Server.ReadTimeout,
    WriteTimeout: cfg.Server.WriteTimeout,
    IdleTimeout:  cfg.Server.IdleTimeout,
}
```

**Note:** The `server.listen` config field is removed or hardcoded to localhost since public access is not supported.

### 2. Configuration Schema

**File:** `internal/config/config.go`

```go
type Config struct {
    Server struct {
        // Listen address - always localhost in security mode
        Listen string `yaml:"listen"`
        // ...
    } `yaml:"server"`
    // auth section removed - no token middleware
    // ...
}
```

### 3. Firewall Configuration (VPS)

```bash
# UFW (Uncomplicated Firewall) on VPS
ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp    # SSH - needed for tunnel creation
ufw enable
```

**Result:**
- Port 22 open (SSH)
- Port 8080 blocked from internet (only accessible via SSH tunnel)
- Proxy never directly reachable from internet

---

## Deployment Steps

### 1. VPS Setup

```bash
# SSH to VPS as admin
ssh admin@vps-host

# Install proxy binary
scp anthropic-transparent-proxy admin@vps-host:/usr/local/bin/
ssh admin@vps-host "chmod +x /usr/local/bin/anthropic-transparent-proxy"

# Create config directory
ssh admin@vps-host "mkdir -p /etc/proxy"

# Configure firewall
ssh admin@vps-host "ufw default deny incoming && ufw default allow outgoing && ufw allow 22/tcp && ufw enable"
```

### 2. Proxy Configuration

**File:** `/etc/proxy/proxy.yaml`

```yaml
server:
  listen: "127.0.0.1:8080"
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

# ... existing routing, endpoints, models config
```

### 3. Systemd Service

**File:** `/etc/systemd/system/proxy.service`

```ini
[Unit]
Description=Anthropic Transparent Proxy
After=network.target

[Service]
Type=simple
User=proxy
Group=proxy
ExecStart=/usr/local/bin/proxy -config /etc/proxy/proxy.yaml
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
# On VPS
ssh admin@vps-host << 'EOF'
sudo useradd -r -s /bin/false proxy
sudo systemctl enable proxy
sudo systemctl start proxy
EOF
```

### 4. SSH Key Distribution

```bash
# On client machine - generate key (one time)
ssh-keygen -t ed25519 -C "client@hostname"

# Copy public key to VPS
ssh-copy-id user@vps-host
```

---

## Client Usage Guide

### Starting the Tunnel

```bash
# Terminal 1: Create SSH tunnel
ssh -L 8080:localhost:8080 user@vps-host -N

# The tunnel stays open until Ctrl+C
```

### Using the Proxy

```bash
# Terminal 2: Configure app to use proxy
export ANTHROPIC_API_BASE="http://localhost:8080/v1"

# Or for Claude Code:
export ANTHROPIC_API_KEY="sk-ant-..."  # your key
claude
```

### SSH Config for Convenience

**File:** `~/.ssh/config`

```
Host proxy-tunnel
    HostName vps-host
    User user
    LocalForward 8080 localhost:8080
    IdentityFile ~/.ssh/id_ed25519
    ServerAliveInterval 60
```

```bash
# Then simply:
ssh proxy-tunnel -N
```

### Complete Workflow

```bash
# 1. Start tunnel (Terminal 1)
ssh proxy-tunnel -N

# 2. Use app (Terminal 2)
export ANTHROPIC_API_BASE="http://localhost:8080/v1"
claude

# 3. Done - Ctrl+C tunnel when finished
```

---

## Security Properties

| Property | Protection |
|----------|------------|
| Network access | Only SSH port exposed; proxy port blocked by firewall |
| Authentication | SSH key auth (Ed25519/RSA, forward secrecy) |
| Encryption | ChaCha20/AES-256 (SSH tunnel), HTTPS (upstream) |
| Transparency | No token middleware; proxy unchanged |
| Single binary | No new dependencies; SSH is standard on Unix |

---

## Threat Analysis

| Threat | Mitigated by |
|--------|--------------|
| Random internet access to proxy | Firewall blocks port 8080 |
| Credential brute force | SSH handles auth; no proxy-level tokens |
| Man-in-the-middle | SSH tunnel encrypts all traffic |
| Token theft | No tokens to steal |
| Unauthorized VPS access | SSH key auth + password (admin responsibility) |

---

## Performance Impact

| Component | Overhead |
|-----------|----------|
| SSH encryption | <0.5% CPU (kernel-level, hardware accelerated) |
| Per-packet crypto | Microseconds (negligible for SSE) |
| Tunnel setup | ~1-2ms once at connection start |

**Conclusion:** SSH tunnel adds negligible overhead. The proxy's SSE streaming (32KB buffer) is unaffected.

---

## Alternatives Considered

| Alternative | Why not chosen |
|-------------|----------------|
| HTTPS + token middleware | Proxy-side TLS termination adds complexity |
| WireGuard VPN | Extra daemon to manage, not needed for small team |
| Cloud private networking | Only works for same-cloud clients |
| Token-only auth | Adds complexity; SSH-only is sufficient |

---

## Implementation Checklist

- [ ] Change proxy listen address to `127.0.0.1:8080`
- [ ] Remove auth middleware (SSH-only)
- [ ] Update config schema (remove auth section)
- [ ] Add firewall setup instructions to docs
- [ ] Add SSH tunnel usage guide to docs
- [ ] Update system-architecture.md with security notes

---

## References

- [OpenSSH Documentation](https://www.openssh.com/security.html)
- [SSH Tunnel Guide](https://www.ssh.com/academy/ssh/tunneling)
