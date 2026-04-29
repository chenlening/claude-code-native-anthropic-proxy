# anthropic-transparent-proxy

A transparent proxy for Claude Code that routes Anthropic API requests across multiple backends with load balancing, health checks, and SSE streaming.

## How it works

Claude Code → localhost:8080 → selects backend (least-connections) → upstream API → streams SSE back

Only modifies:
- `model` field in request body (frontend name → backend name)
- `Authorization` header (proxy key → backend API key)
- Target URL (proxy endpoint → selected backend)

Everything else passes through transparently, including prompt caching, tool use, and extended thinking.

## Quick install

Prerequisites: Ubuntu/Debian, sudo, internet access.

When a user asks to install on Linux, read `.claude/commands/install.md` and follow those steps.
When a user asks to install on macOS, point them to `docs/macos-install.md` for the manual installation guide.

What the installer does:
- Installs Go 1.23+ if missing or too old (from go.dev)
- Builds the binary (`go build -o bin/proxy ./cmd/proxy`)
- Installs and starts a systemd service (`proxy-anthropic`)
- Configures `~/.claude/settings.json` with `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL`, and model settings

## Uninstall

Manually:
```bash
sudo systemctl stop proxy-anthropic
sudo systemctl disable proxy-anthropic
sudo rm /etc/systemd/system/proxy-anthropic.service
sudo systemctl daemon-reload
```

## Common operations

**Always use systemd to manage the proxy** — do not run `./bin/proxy` directly. The service ensures proper logging, restart on failure, and shutdown handling.

### Service management

```bash
# Check status
systemctl status proxy-anthropic

# Restart
sudo systemctl restart proxy-anthropic

# View logs
journalctl -u proxy-anthropic -f

# View recent errors
journalctl -u proxy-anthropic -n 50 --priority err
```

### Health and monitoring

```bash
# Health dashboard
curl http://localhost:8080/health

# Prometheus metrics (if enabled in config)
curl http://localhost:8080/metrics
```

### Configuration

Config is at `configs/proxy.yaml`. After editing, restart the service:

```bash
sudo systemctl restart proxy-anthropic
```

Key config sections:
- `server` — listen address, timeouts
- `models` — frontend model name → backend pool mappings
- `endpoints` — backend URLs and API keys
- `endpoint_health` — failure thresholds and recovery intervals

## Multi-environment notes

Each machine has a different environment. Here's what may need adaptation:

- **Go version**: The install command downloads Go 1.23+ from go.dev automatically. No manual step needed.
- **Port conflicts**: The default listen port is `:8080` (configured in `configs/proxy.yaml`). Change if another service uses this port.
- **API keys**: Hardcoded in `configs/proxy.yaml`. For production, replace with your own keys and consider using environment variables (`configs/.env.proxy.example` pattern).
- **Claude Code settings**: The install command configures `~/.claude/settings.json` automatically with model settings from proxy.yaml.
- **systemd**: Requires systemd (standard on Ubuntu/Debian). Will not work on non-systemd init systems.
- **macOS**: See `docs/macos-install.md` for launchd-based installation.
- **Firewall**: The proxy binds to `0.0.0.0:8080` by default. For local-only access, change `server.listen` to `127.0.0.1:8080` in config.
- **Firewall**: The proxy binds to `0.0.0.0:8080` by default. For local-only access, change `server.listen` to `127.0.0.1:8080` in config.

## Testing on a fresh machine

After installing (read `.claude/commands/install.md` and follow the steps), verify:

```bash
curl -sf http://localhost:8080/health
```

Should return JSON with endpoint status and latency stats.

## Project structure

```
cmd/proxy/main.go          # Entry point
internal/
  config/                  # YAML config loading with env var support
  endpoint/                # Endpoint state and health management
  routing/                 # Load balancer and model router
  proxy/                   # HTTP proxy handler
  metrics/                 # Prometheus metrics
  healthcheck/             # Health endpoint with HTML dashboard
configs/proxy.yaml         # Main configuration
deploy/proxy-anthropic.service  # systemd service template
scripts/
  uninstall.sh             # One-command uninstaller
.claude/
  commands/
    install.md             # Installation steps (read and follow when user asks to install)
docs/                      # Architecture and optimization docs
```
