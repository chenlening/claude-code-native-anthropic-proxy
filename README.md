# Anthropic Transparent Proxy

A transparent Anthropic API proxy for Claude Code with multi-endpoint load balancing, automatic failover, and dynamic model discovery.

## Features

- **Transparent streaming** — Native Anthropic SSE format preserved, no format conversion
- **Least-connections load balancing** — Tracks connections per model per endpoint
- **Automatic failover** — Retries on next endpoint when rate-limited or failing
- **Dynamic model discovery** — Auto-discovers supported models from each backend via `/v1/models`
- **Stateless deployment** — No database required, YAML configuration
- **Prometheus metrics** — Request counts, duration, endpoint status
- **Health dashboard** — HTML health endpoint with latency stats
- **Single binary** — Easy deployment, no external dependencies

## Quick Start

**Linux:**

```bash
make build
./bin/proxy --config configs/proxy.yaml
```

**macOS:**

See [docs/macos-install.md](docs/macos-install.md) for launchd-based installation.

## Configuration

Configuration is via YAML file. See `configs/proxy.yaml.example` for a template.

```yaml
server:
  listen: ":8080"
  read_timeout: 30s
  write_timeout: 0s
  idle_timeout: 90s

logging:
  level: info
  format: json

metrics:
  enabled: true
  path: /metrics

health:
  path: /health

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

### Environment Variables

API keys support environment variable expansion:
- `${VAR_NAME}` — value of environment variable
- `${VAR_NAME:-default}` — value or default if unset

### Endpoint Configuration

| Field | Description |
|-------|-------------|
| `url` | Base URL for the endpoint's Anthropic-compatible API |
| `models_endpoint` | Optional custom URL for model discovery (defaults to `url + /v1/models`) |
| `api_key` | API key for the endpoint (supports `${ENV_VAR}` expansion) |
| `timeout` | Response header timeout for streaming requests |
| `offline` | If `true`, permanently excludes endpoint from routing |

## API Endpoints

| Endpoint | Description |
|----------|-------------|
| `POST /v1/messages` | Anthropic Messages API (proxied) |
| `GET /v1/models` | Discover available models across all endpoints |
| `GET /health` | Health check with HTML dashboard |

## Load Balancing

The proxy uses **least-connections** strategy at the model level:

- Tracks active connections per model per endpoint
- Selects the endpoint with the lowest connection count for the requested model
- Excludes disabled endpoints from selection
- On 429 (rate limit), retries on the next available endpoint

## Failover Behavior

1. Connection count decremented on failure
2. Failure recorded against the endpoint
3. After 5 failures (configurable), endpoint is disabled
4. Request retried on next healthy endpoint
5. Disabled endpoints are periodically probed for recovery
6. After 2 successful probes, endpoint is re-enabled

## Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `anthropic_proxy_requests_total` | Counter | Total requests processed |
| `anthropic_proxy_requests_by_model` | Counter | Requests per model |
| `anthropic_proxy_requests_by_endpoint` | Counter | Requests per endpoint |
| `anthropic_proxy_request_duration_seconds` | Histogram | Request latency |
| `anthropic_proxy_endpoint_failures_total` | Counter | Failures per endpoint |
| `anthropic_proxy_endpoint_enabled` | Gauge | Endpoint enabled status (1/0) |

## Claude Code Configuration

Add to `~/.claude/settings.json`:

```json
{
  "env": {
    "ANTHROPIC_AUTH_TOKEN": "anykey",
    "ANTHROPIC_BASE_URL": "http://localhost:8080"
  }
}
```

`ANTHROPIC_AUTH_TOKEN` can be any non-empty value — the proxy handles actual authentication.

For direct backend access without the proxy, use the backend switcher:

```bash
source scripts/backend.sh <backend-name>
```

Available backends are configured in `configs/backends/*.env`.

## Development

### Project Structure

```
anthropic-transparent-proxy/
├── cmd/proxy/main.go          # Entry point
├── internal/
│   ├── config/                # YAML config loading with env var support
│   ├── endpoint/              # Endpoint state, health, model discovery, recovery
│   ├── proxy/                 # HTTP handler, request parsing, SSE streaming
│   ├── metrics/               # Prometheus metrics
│   ├── healthcheck/           # Health endpoint with HTML dashboard
│   └── models/                # /v1/models handler
├── configs/
│   ├── proxy.yaml             # Active config
│   ├── proxy.yaml.example     # Example config template
│   └── backends/              # Direct backend connection env files
├── tests/                     # Integration tests
├── scripts/                   # Utility scripts
├── deploy/                    # systemd service file
└── docs/                      # Documentation
```

### Building

```bash
make build        # Build binary to bin/proxy
make test         # Run unit tests
make run          # Run with configs/proxy.yaml
```

Binary output: `bin/proxy`

### systemd Service (Linux)

```bash
sudo ln -sf deploy/proxy-anthropic.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now proxy-anthropic

# Status and logs
systemctl status proxy-anthropic
journalctl -u proxy-anthropic -f
```

## Testing

```bash
# Unit tests
make test

# Integration tests (requires running proxy)
make test-integration
```

## License

MIT
