# Anthropic Transparent Proxy

A transparent Anthropic API proxy for Claude Code with multi-endpoint load balancing and automatic failover.

## Features

- **Transparent streaming** - Native Anthropic SSE format preserved, no format conversion
- **Model-level least-connections load balancing** - Tracks connections per model per endpoint
- **Automatic failover** - Retries on next endpoint when one fails
- **Model name mapping** - Frontend model names mapped to backend model names
- **Stateless deployment** - No database required, YAML configuration
- **Prometheus metrics** - Request counts, duration, endpoint status
- **Health endpoint** - `/health` for Kubernetes readiness probes
- **Single binary** - Easy deployment, no external dependencies

## Architecture

```
Claude Code → Proxy → Multiple Anthropic Endpoints
                    ↓
              Load Balancer (least-connections)
                    ↓
              Model Router (frontend → backend)
                    ↓
              Health Manager (auto-disable/enable)
```

## Quick Start

**Linux:**

Clone this repo, open Claude Code in the project directory, and say "install this project". I will read `.claude/commands/install.md` and guide you through the installation steps.

```bash
make build
./bin/proxy --config configs/proxy.yaml
```

**macOS:**

See [docs/macos-install.md](docs/macos-install.md) for step-by-step installation instructions using launchd.

## Claude Code Configuration

When you ask me to install, I read `.claude/commands/install.md` and configure this automatically. For manual setup, add to `~/.claude/settings.json`:

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

The model env vars must match the frontend model names in `configs/proxy.yaml`. Claude Code uses these to route requests through the proxy.

## Configuration

Configuration is via YAML file. See `configs/proxy.yaml` for the default config.

### Example Configuration

```yaml
server:
  listen: ":8080"
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

# Model mappings: frontend model → backend pool
models:
  claude-sonnet-4-20250514:
    backends:
      - endpoint: endpoint-a
        model: "claude-sonnet-4-20250514"
        weight: 10
      - endpoint: endpoint-b
        model: "custom-sonnet-model"
        weight: 5

endpoints:
  endpoint-a:
    url: "https://api.anthropic.com"
    api_key: "${ANTHROPIC_API_KEY}"
    timeout: 90s

  endpoint-b:
    url: "https://custom-provider.example.com/v1"
    api_key: "${CUSTOM_PROVIDER_KEY}"
    timeout: 90s

endpoint_health:
  failures_to_disable: 5
  recovery_probe_interval: 30s
  successes_to_enable: 2
```

### Environment Variables

API keys support environment variable expansion:
- `${VAR_NAME}` → value of environment variable
- `${VAR_NAME:-default}` → value or default if unset

### Model Mapping

The proxy maps frontend model names (what Claude Code requests) to backend model names (what providers use):

```yaml
models:
  claude-sonnet-4-20250514:     # Frontend model (Claude Code requests this)
    backends:
      - endpoint: endpoint-b
        model: "custom-sonnet"  # Backend model (provider uses this name)
```

## API Endpoints

| Endpoint | Description |
|----------|-------------|
| `POST /v1/messages` | Anthropic Messages API (proxied) |
| `GET /health` | Health check for Kubernetes probes |
| `GET /metrics` | Prometheus metrics (if enabled) |

### Health Check Response

```json
{
  "status": "healthy",
  "endpoints": {
    "endpoint-a": "enabled",
    "endpoint-b": "enabled"
  }
}
```

Status values:
- `healthy` - All endpoints enabled
- `degraded` - Some endpoints disabled
- `unhealthy` - All endpoints disabled (returns HTTP 503)

## Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `anthropic_proxy_requests_total` | Counter | Total requests processed |
| `anthropic_proxy_requests_by_model` | Counter | Requests per model |
| `anthropic_proxy_requests_by_endpoint` | Counter | Requests per endpoint |
| `anthropic_proxy_request_duration_seconds` | Histogram | Request latency |
| `anthropic_proxy_endpoint_connections` | Gauge | Active connections per endpoint/model |
| `anthropic_proxy_endpoint_failures_total` | Counter | Failures per endpoint |
| `anthropic_proxy_endpoint_enabled` | Gauge | Endpoint enabled status (1/0) |

## Load Balancing

The proxy uses **least-connections** strategy at the **model level**:

- Tracks active connections per model per endpoint
- Selects endpoint with lowest connection count for the requested model
- Excludes disabled endpoints from selection
- Automatically adjusts to request duration variance

## Failover Behavior

When an endpoint fails:
1. Connection count decremented
2. Failure recorded
3. After 5 failures (configurable), endpoint is disabled
4. Request retried on next healthy endpoint
5. Disabled endpoints are periodically probed for recovery
6. After 2 successful probes, endpoint is re-enabled

## Deployment on Public VPS

When deploying on a public cloud VPS, use SSH tunneling for secure access:

### Why SSH Tunnel?

| Approach | Pros | Cons |
|----------|------|------|
| SSH tunnel | Kernel crypto, no proxy TLS overhead | Point-to-point only |
| HTTPS + tokens | Industry standard | Proxy-side TLS complexity |
| VPN (WireGuard) | Full mesh | Extra daemon to manage |

### Setup

**1. Proxy Configuration**

Ensure proxy binds to localhost only:

```yaml
server:
  listen: "127.0.0.1:8080"  # localhost only, not ":8080"
```

**2. VPS Firewall**

```bash
# UFW example
ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp    # SSH
ufw enable
```

**3. SSH Key Setup (client)**

```bash
# Generate key (one time)
ssh-keygen -t ed25519 -C "client@hostname"

# Copy to VPS
ssh-copy-id user@vps-host
```

**4. Create SSH Tunnel**

```bash
# Terminal 1: Start tunnel
ssh -L 8080:localhost:8080 user@vps-host -N

# The tunnel stays open until Ctrl+C
```

**5. Use the Proxy**

```bash
# Terminal 2: Configure app
export ANTHROPIC_BASE_URL="http://localhost:8080"
export ANTHROPIC_API_KEY="any-key"  # Proxy handles actual keys

# Run Claude Code
claude
```

### SSH Config for Convenience

Add to `~/.ssh/config`:

```
Host proxy-tunnel
    HostName vps-host
    User user
    LocalForward 8080 localhost:8080
    IdentityFile ~/.ssh/id_ed25519
    ServerAliveInterval 60
```

Then simply:

```bash
ssh proxy-tunnel -N
```

### Security Notes

| Property | Protection |
|----------|------------|
| Network access | Only SSH port (22) exposed; proxy port blocked |
| Authentication | SSH key auth (Ed25519, forward secrecy) |
| Encryption | ChaCha20/AES-256 (SSH tunnel) |

The proxy never directly faces the internet. All traffic goes through the encrypted SSH tunnel.

## Testing

```bash
# Run all tests
make test

# Or directly
go test -v ./...
```

## Development

### Project Structure

```
anthropic-transparent-proxy/
├── cmd/proxy/main.go          # Entry point
├── internal/
│   ├── config/                # YAML config loading
│   ├── endpoint/              # Endpoint state, health manager
│   ├── routing/               # Load balancer, model router
│   ├── proxy/                 # HTTP handler, request parsing
│   ├── metrics/               # Prometheus metrics
│   └── healthcheck/           # Health endpoint
├── configs/
│   ├── proxy.yaml             # Default config
│   └── proxy.yaml.example     # Example config
├── Makefile
├── go.mod
└── README.md
```

### Building

```bash
make build
```

Binary output: `bin/proxy`

### Running

The proxy can run directly or via systemd service:

**Direct run:**
```bash
./bin/proxy --config configs/proxy.yaml
```

**Systemd service (Linux, recommended for persistent operation):**
```bash
# Link the service file
sudo ln -sf /home/nice/chenlening/workspace/anthropic-transparent-proxy/deploy/proxy-anthropic.service /etc/systemd/system/

# Reload systemd, enable and start
sudo systemctl daemon-reload
sudo systemctl enable proxy-anthropic
sudo systemctl start proxy-anthropic

# Check status
sudo systemctl status proxy-anthropic

# View logs
sudo journalctl -u proxy-anthropic -f
```

Service file location: `deploy/proxy-anthropic.service`

**macOS (launchd):**

See [docs/macos-install.md](docs/macos-install.md) for installation instructions using launchd instead of systemd.

## Why This Proxy?

Existing solutions like one-api and LiteLLM have limitations for Claude Code:

| Issue | Existing Solutions | This Proxy |
|-------|-------------------|------------|
| Format conversion | OpenAI→Anthropic (tool use bugs) | Anthropic native only |
| Database required | Yes (deployment complexity) | No (stateless) |
| Claude Code tool use | May have issues | Fully compatible |
| Extended thinking | Unknown support | Fully supported |

This proxy is specifically designed for Claude Code, preserving Anthropic's native format for perfect compatibility with tool use, extended thinking, and prompt caching.

## License

MIT