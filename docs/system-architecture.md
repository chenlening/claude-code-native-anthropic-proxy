# Anthropic Transparent Proxy - System Architecture

**Date:** 2026-04-22
**Status:** Draft
**Version:** 1.0

---

## Executive Summary

### Problem Statement

Claude Code only supports a single Anthropic API endpoint, creating several operational challenges:

1. **Multi-endpoint limitation**: Organizations need to route requests across multiple Anthropic API providers (official API, custom providers, different regions) but Claude Code cannot natively support this
2. **Model name mismatch**: Backend providers may use different model names than Claude Code expects
3. **No automatic failover**: If one endpoint fails, Claude Code cannot automatically retry on another endpoint
4. **Existing solutions have critical limitations**: Current open source proxy solutions (one-api, LiteLLM) introduce format conversion issues, database requirements, and unnecessary complexity

### Solution Overview

A **transparent Anthropic API proxy** designed specifically for Claude Code compatibility:

- Presents a single Anthropic-compatible endpoint to Claude Code
- Load balances across multiple upstream Anthropic endpoints
- Maps frontend model names to backend model pools
- Fails over automatically on errors
- **Stateless deployment** - no database required
- **Transparent streaming** - native Anthropic SSE format preserved
- **Minimal modification** - only replaces model name in request body

### Key Differentiators

| Feature | This Solution | one-api | LiteLLM |
|---------|---------------|---------|---------|
| Database Required | No | Yes (SQLite/MySQL) | Optional |
| Format Conversion | Minimal (model name only) | OpenAI→Anthropic | Multi-format |
| Anthropic Native | Full | Partial | Partial |
| Stateless | Yes | No | Partial |
| Claude Code Tool Use | Fully compatible | May have issues | May have issues |
| Deployment Complexity | Single binary | Database + migrations | Python runtime |
| Extended Thinking | Supported | Unknown | Unknown |

---

## Existing Solutions Deep Dive

### 1. one-api (GitHub: songquanpeng/one-api)

**Overview:**
one-api is a popular OpenAI API management and distribution system that supports multiple LLM providers including Anthropic.

**Architecture:**

```
┌─────────────────────────────────────────────────────────────────┐
│                         one-api                                  │
│  ┌─────────────┐  ┌──────────────┐  ┌──────────────────────┐   │
│  │ Web Admin   │  │ User/Auth    │  │ Channel Manager      │   │
│  │ Dashboard   │  │ Management   │  │ (load balancing)     │   │
│  └─────────────┘  └──────────────┘  └──────────────────────┘   │
│         │                │                     │                 │
│         └────────────────┼─────────────────────┘                 │
│                          │                                       │
│                          ▼                                       │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │                    Database (SQLite/MySQL)                   ││
│  │  - Users & Tokens                                            ││
│  │  - Channel Configurations                                    ││
│  │  - Usage Logs & Quotas                                       ││
│  │  - Billing Data                                              ││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
```

**Key Features:**
- Token-based authentication with multi-user support
- Channel management for multiple API providers
- Load balancing across channels
- Usage tracking and quota management
- Web-based admin dashboard
- Billing and cost tracking

**Database Requirements:**
- SQLite (default), MySQL, or PostgreSQL required
- Database stores: users, tokens, channel configs, usage logs
- Migration scripts needed for schema updates
- Backup and recovery procedures required

**Format Conversion:**
- Accepts OpenAI-compatible format from clients
- Converts to native format for each provider
- For Anthropic: OpenAI format → Anthropic Messages API format

**Limitations for Claude Code:**

| Limitation | Impact |
|------------|--------|
| Format conversion | Tool use may have compatibility issues due to format translation |
| Database requirement | Deployment complexity, stateful design |
| Authentication layer | Extra complexity for internal/trusted network use |
| Web dashboard | Overhead for simple proxy use case |
| Usage tracking | Adds latency and storage requirements |

**Critical Issue - Tool Use Incompatibility:**

Claude Code uses Anthropic's native tool use format which differs from OpenAI's function calling:
- Anthropic: `tools` array with `input_schema`
- OpenAI: `functions` or `tools` with different schema structure

Format conversion between these formats can cause:
- Missing tool definitions
- Incorrect parameter schemas
- Tool result format mismatches
- Streaming tool use events lost

---

### 2. LiteLLM Proxy (GitHub: BerkeleySkycastGroup/litellm)

**Overview:**
LiteLLM is a Python-based LLM gateway that provides unified API access across multiple providers.

**Architecture:**

```
┌─────────────────────────────────────────────────────────────────┐
│                       LiteLLM Proxy                              │
│  ┌─────────────┐  ┌──────────────┐  ┌──────────────────────┐   │
│  │ Python      │  │ Format       │  │ Provider Router      │   │
│  │ Runtime     │  │ Translator   │  │                      │   │
│  └─────────────┘  └──────────────┘  └──────────────────────┘   │
│                          │                                       │
│                          ▼                                       │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │                 Optional: Database/Cache                     ││
│  │  - Logging (optional)                                        ││
│  │  - Caching (optional)                                        ││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
```

**Key Features:**
- Unified API interface for multiple LLM providers
- Function calling/tool use translation
- Load balancing and fallback support
- Logging and monitoring (optional)
- Caching support (optional)
- OpenAI-compatible endpoint

**Anthropic Support:**
- Supports Anthropic Messages API
- Converts between OpenAI and Anthropic formats
- Tool/function calling translation layer

**Limitations for Claude Code:**

| Limitation | Impact |
|------------|--------|
| Python runtime | Higher overhead than native Go/Rust binary |
| Format conversion | Same tool use incompatibility risks as one-api |
| Partial Anthropic features | May not support extended thinking, prompt caching |
| Deployment complexity | Python dependencies, virtual environment setup |

**Streaming Considerations:**

Anthropic uses Server-Sent Events (SSE) for streaming:
```
event: message_start
data: {"type": "message_start", "message": {...}}

event: content_block_start
data: {"type": "content_block_start", "index": 0, ...}

event: content_block_delta
data: {"type": "content_block_delta", "index": 0, "delta": {...}}
```

Format conversion during streaming can:
- Break event type matching
- Lose content block indices
- Cause tool use streaming events to be malformed

---

### 3. Comparison Summary

| Aspect | one-api | LiteLLM | This Solution |
|--------|---------|---------|---------------|
| **Language** | Go | Python | Go |
| **Database** | Required | Optional | Not required |
| **Format** | OpenAI→Anthropic | Multi-format | Anthropic native |
| **Auth** | Required | Optional | None (trust boundary at network) |
| **Tool Use** | May have issues | May have issues | Fully compatible |
| **Streaming** | SSE conversion | SSE conversion | Transparent SSE |
| **Deployment** | Binary + DB | Python + deps | Single binary |
| **Extended Thinking** | Unknown | Unknown | Supported |
| **Prompt Caching** | Unknown | Unknown | Supported |
| **Stateless** | No | Partial | Yes |

---

## Gap Analysis & Why Not Existing Solutions

### Why Existing Solutions Don't Fit Claude Code

### 1. **Format Conversion Risk**

The fundamental issue: existing solutions convert between API formats.

**Anthropic Tool Use Format:**
```json
{
  "tools": [
    {
      "name": "get_weather",
      "description": "Get weather info",
      "input_schema": {
        "type": "object",
        "properties": {
          "location": {"type": "string"}
        },
        "required": ["location"]
      }
    }
  ]
}
```

**OpenAI Function Calling Format:**
```json
{
  "functions": [
    {
      "name": "get_weather",
      "description": "Get weather info",
      "parameters": {
        "type": "object",
        "properties": {
          "location": {"type": "string"}
        },
        "required": ["location"]
      }
    }
  ]
}
```

While similar, edge cases exist:
- Nested object schemas
- Optional parameters
- Default values
- Tool result format in response

**Claude Code's tool use is tightly coupled to Anthropic's native format.** Any conversion introduces risk of subtle bugs that are hard to detect until they fail in production.

---

### 2. **Database Requirement Complexity**

one-api requires a database for:
- User accounts and authentication tokens
- Channel (endpoint) configurations
- Usage logs for billing/quota tracking
- Access control rules

**Deployment implications:**
- Database migrations for version upgrades
- Backup and disaster recovery procedures
- Database maintenance (SQLite file growth, MySQL tuning)
- Cannot deploy as truly stateless container (e.g., Kubernetes pod with no persistent storage)

**For Claude Code proxy use case:**
- Authentication is unnecessary (trust boundary at network level)
- Usage tracking can be done externally via logs/metrics
- Configuration can be file-based (YAML)
- A database adds unnecessary complexity

---

### 3. **Missing Anthropic-Specific Features**

Anthropic has unique features not present in OpenAI:

| Feature | Description | Format Conversion Risk |
|---------|-------------|------------------------|
| **Extended Thinking** | Claude's internal reasoning process | Streaming format is unique |
| **Prompt Caching** | Cache system prompts for efficiency | Requires specific header handling |
| **Token-Efficient Tools** | Compact tool definitions | Schema format differs |
| **Computer Use** | Screen interaction tools | Complex nested schemas |

Existing solutions may not support these because:
- They focus on OpenAI-compatible format
- These features don't have OpenAI equivalents
- Conversion logic doesn't account for Anthropic-specific extensions

---

### 4. **Stateless Deployment Gap**

Modern deployment practices favor stateless services:
- Kubernetes pods can be ephemeral
- Horizontal scaling requires no shared state
- Blue-green deployments need no data migration
- Disaster recovery is simpler

Existing solutions with database requirements cannot be truly stateless. This proxy design enables:
- Single binary, no external dependencies
- Configuration via YAML file (mounted as configmap)
- Health checks without database queries
- Zero downtime deployments

---

### 5. **Claude Code Compatibility Matrix**

| Claude Code Feature | one-api | LiteLLM | This Solution |
|---------------------|---------|---------|---------------|
| Basic messages API | Supported | Supported | Supported |
| Tool use (function calling) | Partial | Partial | Full |
| Streaming SSE | Converted | Converted | Transparent |
| Extended thinking | Unknown | Unknown | Full |
| Prompt caching | Unknown | Unknown | Full |
| Multi-turn conversations | Supported | Supported | Supported |
| Image inputs | Supported | Supported | Supported |
| Token-efficient tools | Unknown | Unknown | Full |

---

## Architecture Overview

### High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      Claude Code Client                          │
└────────────────────────────┬────────────────────────────────────┘
                             │ Anthropic API requests (native format)
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Transparent Proxy                             │
│                                                                  │
│  ┌─────────────┐  ┌──────────────┐  ┌──────────────────────┐   │
│  │ HTTP Server │──│ Model Router │──│ Load Balancer        │   │
│  │ (net/http)  │  │              │  │ (least-connections)  │   │
│  └─────────────┘  └──────────────┘  └──────────────────────┘   │
│         │                │                     │                 │
│         │                │                     │                 │
│         ▼                ▼                     ▼                 │
│  ┌─────────────┐  ┌──────────────┐  ┌──────────────────────┐   │
│  │ Endpoint    │  │ Endpoint Pool│  │ Connection Tracker   │   │
│  │ Health      │  │ Manager      │  │ (per-model)          │   │
│  └─────────────┘  └──────────────┘  └──────────────────────┘   │
│         │                                                       │
│         ▼                                                       │
│  ┌─────────────┐  ┌──────────────┐  ┌──────────────────────┐   │
│  │ Metrics     │  │ Access Logger│  │ Health Checker       │   │
│  │ (Prometheus)│  │              │  │                      │   │
│  └─────────────┘  └──────────────┘  └──────────────────────┘   │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │                    YAML Configuration File                   ││
│  │  - Endpoints & API Keys                                      ││
│  │  - Model Mappings                                            ││
│  │  - Load Balancing Strategy                                   ││
│  │  - Health Check Parameters                                   ││
│  └─────────────────────────────────────────────────────────────┘│
└────────────────────────────┬────────────────────────────────────┘
                             │ Proxied requests (native Anthropic format)
                             │ Only modification: model name in request body
                             ▼
        ┌────────────────────┬────────────────────┬────────────────────┐
        │                    │                    │                    │
        ▼                    ▼                    ▼                    ▼
┌───────────────┐  ┌───────────────┐  ┌───────────────┐  ┌───────────────┐
│  Endpoint A   │  │  Endpoint B   │  │  Endpoint C   │  │  Endpoint D   │
│ (Anthropic)   │  │ (Custom       │  │ (Another      │  │ (Regional     │
│  Official API │  │  Provider)    │  │  Provider)    │  │  Endpoint)    │
└───────────────┘  └───────────────┘  └───────────────┘  └───────────────┘
```

### Key Design Principle: Transparency

**What passes through unchanged:**
- Request headers (except x-api-key per endpoint)
- Request body (except `model` field)
- Response headers
- Response body (SSE streaming events)
- All Anthropic-specific features (tool use, extended thinking, prompt caching)

**What is modified:**
- `model` field in request body (frontend model → backend model name)
- `x-api-key` header (proxy endpoint key → backend endpoint key)
- Target URL (proxy endpoint → selected backend endpoint)

---

### Core Components

| Component | Responsibility | Why Needed |
|-----------|---------------|------------|
| **HTTP Server** | Accept Anthropic API requests, handle streaming responses | Entry point for Claude Code |
| **Model Router** | Parse request model, resolve to backend model pool | Enable model name mapping |
| **Load Balancer** | Select endpoint from pool using least-connections | Distribute load, avoid hot endpoints |
| **Connection Tracker** | Track active connections per model per endpoint | Enable intelligent load balancing |
| **Endpoint Health** | Monitor endpoint health, disable/re-enable endpoints | Automatic failover capability |
| **Endpoint Pool Manager** | Manage endpoint configurations and weights | Support weighted distribution |
| **Metrics Collector** | Expose Prometheus metrics | Observability without database |
| **Access Logger** | Structured access logging | Usage tracking via logs |
| **Health Checker** | `/health` endpoint and periodic endpoint health checks | Kubernetes readiness probes |

---

### Why Model-Level Connection Tracking

Traditional load balancers track connections per endpoint. This proxy tracks per **model per endpoint**:

```
endpoint-a connections:
  ├── claude-sonnet-4-20250514: 5  (endpoint supports this model)
  ├── claude-opus-4-20250514: 2    (endpoint supports this model)
  └── total: 7

endpoint-b connections:
  ├── claude-sonnet-4-20250514: 3  (endpoint supports this model)
  └── total: 3                     (endpoint doesn't have opus)
```

**Why this matters:**
- Different models have different token limits and rate limits
- Opus requests may hit different bottlenecks than Sonnet requests
- Prevents routing opus requests to endpoints that only support sonnet
- Enables accurate load distribution per model

---

### Why Least-Connections Strategy

**Least-connections** selects the endpoint with the fewest active connections for the requested model:

```
Request for claude-sonnet-4-20250514 arrives:
  endpoint-a: 5 connections for this model
  endpoint-b: 3 connections for this model ← SELECTED
  endpoint-c: 4 connections for this model
```

**Advantages over round-robin:**
- Accounts for request duration variance (some requests take longer)
- Naturally distributes to less busy endpoints
- No need for external traffic analysis
- Self-adjusting without configuration changes

---

### Why No Database

**Configuration via YAML file:**
```yaml
# proxy.yaml - single file contains all config
endpoints:
  endpoint-a:
    url: "https://api.anthropic.com"
    api_key: "${ANTHROPIC_API_KEY_A}"
```

**Advantages:**
- Zero external dependencies
- Configuration as code (version controlled)
- Kubernetes ConfigMap/Secret mounting
- Instant startup (no database connection)
- No migrations, no schema changes
- Truly stateless pods

**What would require a database in other solutions:**
- User authentication → Trust boundary at network level (VPN, firewall)
- Usage tracking → Prometheus metrics + access logs
- Dynamic config → File-based config, mounted from K8s ConfigMap

---

## Request Flow & Data Flow

### Request Processing Flow

```
Client Request (Anthropic native format)
     │
     ▼
┌─────────────────┐
│ 1. Parse Model  │  Extract "model" field from request body
│                 │  Request format preserved (no conversion)
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ 2. Resolve Pool │  Lookup model in config → get backend pool
│                 │  Pool contains: endpoints, weights, backend model names
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ 3. Select       │  Apply least-connections:
│    Backend      │  - Get connection counts for this model per endpoint
│                 │  - Filter out disabled/unhealthy endpoints
│                 │  - Select endpoint with lowest count
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ 4. Check Health │  Is endpoint healthy?
│                 │  - Not disabled by failure threshold?
│                 │  - Recent successful requests?
└────────┬────────┘
         │
    ┌────┴────┐
    │         │
   YES       NO
    │         │
    ▼         ▼
    │    ┌─────────────────┐
    │    │ Try Next Backend │  Fallback to next healthy endpoint
    │    │ (by connection   │
    │    │  count)          │
    │    └────────┬────────┘
    │             │
    └──────┬──────┘
           │
           ▼
┌─────────────────────┐
│ 5. Increment Count  │  Track active connection for model-endpoint pair
│                     │  Connection tracker: atomic increment
└────────┬────────────┘
         │
         ▼
┌─────────────────────┐
│ 6. Forward Request  │  Modify only: model name, API key, target URL
│                     │  Body: {"model": "claude-sonnet-4"} → {"model": "custom-sonnet"}
│                     │  Header: x-api-key → endpoint's key
│                     │  URL: proxy → selected endpoint URL
└────────┬────────────┘
         │
         ▼
┌─────────────────────┐
│ 7. Stream Response  │  Transparent SSE streaming:
│                     │  - Receive event from backend
│                     │  - Forward to client immediately
│                     │  - No buffering, no conversion
│                     │  - http.Flusher for real-time delivery
└────────┬────────────┘
         │
    ┌────┴────┐
    │         │
 SUCCESS   FAILURE
    │         │
    ▼         ▼
┌─────────┐  ┌──────────────────┐
│ Record  │  │ Record Failure    │
│ Success │  │ - Increment failure count
│ Decrement│  │ - Decrement connection
│ Count   │  │ - May disable endpoint
│ Done    │  │ - Retry next endpoint
└─────────┘  └──────────────────┘
```

### Failover Behavior

```
Request arrives for claude-sonnet-4-20250514
         │
         ▼
┌─────────────────────────────────────────────────────┐
│ Attempt 1: endpoint-b (least connections: 3)       │
│ Connection count: 3 → 4                            │
└─────────────────────────────────────────────────────┘
         │
    ┌────┴────┐
    │         │
 SUCCESS   FAILURE (timeout/5xx/connection error)
    │         │
    ▼         ▼
   DONE    ┌─────────────────────────────────────────┐
           │ Record failure for endpoint-b           │
           │ Failure count: 0 → 1                    │
           │ If failures >= 5: disable endpoint-b    │
           │ Connection count: 4 → 3                 │
           └─────────────────────────────────────────┘
                    │
                    ▼
           ┌─────────────────────────────────────────┐
           │ Attempt 2: endpoint-c (next least: 4)   │
           │ Connection count: 4 → 5                 │
           └─────────────────────────────────────────┘
                    │
               ┌────┴────┐
               │         │
            SUCCESS   FAILURE
               │         │
               ▼         ▼
              DONE    Try endpoint-a (count: 5)
                       │
                       ▼
                    (all endpoints failed or disabled)
                       │
                       ▼
              Return HTTP 503 to client
              {
                "type": "error",
                "error": {
                  "type": "api_error",
                  "message": "All endpoints unavailable"
                }
              }
```

### Recovery Probe Flow

```
Background goroutine (every 30 seconds):
         │
         ▼
┌─────────────────────────────────────────────────────┐
│ For each disabled endpoint:                         │
│                                                     │
│ 1. Send lightweight health check                    │
│    - HEAD request to base URL (no API cost)         │
│    - Or minimal messages request (verify API)       │
│                                                     │
│ 2. If success:                                      │
│    - Increment success count                        │
│    - If successes >= 2: re-enable endpoint          │
│                                                     │
│ 3. If failure:                                      │
│    - Reset success count to 0                       │
│    - Keep endpoint disabled                         │
└─────────────────────────────────────────────────────┘
```

---

## Key Design Decisions

### Decision 1: Model-Level Least-Connections

**Problem:** How to load balance when different endpoints support different models?

**Options Considered:**

| Strategy | Pros | Cons |
|----------|------|------|
| Round-robin | Simple, no state | Doesn't account for request duration |
| Weighted round-robin | Can favor better endpoints | Still doesn't account for load |
| Least-connections (endpoint-level) | Accounts for load | Ignores model differences |
| Least-connections (model-level) | Accounts for load AND model | Slightly more complex state |

**Decision:** Model-level least-connections

**Rationale:**
- Different models have different rate limits and performance characteristics
- Some endpoints may only support certain models
- Duration varies significantly (opus requests take longer than haiku)
- Self-adjusting without manual configuration

---

### Decision 2: No Database

**Problem:** Should we store state in a database?

**Options Considered:**

| Approach | Pros | Cons |
|----------|------|------|
| SQLite database | Queryable state, history | Deployment complexity, migrations |
| In-memory only | Fast, no external deps | Lost on restart (minor issue) |
| YAML config + in-memory state | Simple, version-controlled | No queryable history |

**Decision:** YAML config + in-memory state

**Rationale:**
- Runtime state (connection counts) is ephemeral - lost on restart is acceptable
- Configuration (endpoints, models) is static - YAML is sufficient
- Usage tracking via logs/metrics (Prometheus) - no database needed
- Deployment simplicity is paramount for Claude Code proxy use case
- Kubernetes ConfigMap enables dynamic config updates without restart

---

### Decision 3: Minimal Format Modification

**Problem:** Should we convert between API formats for flexibility?

**Options Considered:**

| Approach | Pros | Cons |
|----------|------|------|
| Full OpenAI compatibility | More clients supported | Tool use incompatibility, complexity |
| Anthropic native only | Claude Code compatible | Limited to Anthropic clients |
| Minimal modification (model name only) | Claude Code compatible, simple | No format conversion benefits |

**Decision:** Minimal modification (model name only)

**Rationale:**
- Claude Code uses Anthropic native format - conversion introduces risk
- Tool use is tightly coupled to Anthropic format
- Extended thinking, prompt caching have no OpenAI equivalents
- Simplicity reduces bugs and maintenance
- Target use case is Claude Code specifically, not general LLM proxy

---

### Decision 4: Go as Implementation Language

**Problem:** What language to implement in?

**Options Considered:**

| Language | Pros | Cons |
|----------|------|------|
| Python | Rich ecosystem, LiteLLM patterns | Runtime overhead, dependencies |
| Go | Single binary, fast, concurrent | Less LLM ecosystem |
| Rust | Single binary, fastest | Steeper learning curve |

**Decision:** Go

**Rationale:**
- Single binary deployment (no runtime dependencies)
- Excellent concurrency support (goroutines for streaming)
- Standard library sufficient (net/http, no heavy frameworks)
- Cross-compilation for multiple platforms
- Fast startup (critical for Kubernetes pods)
- Proven in similar projects (one-api, Traefik, Caddy)

---

## Configuration

### YAML Configuration Format

```yaml
# proxy.yaml
server:
  listen: ":8080"
  read_timeout: 30s
  write_timeout: 120s    # Long timeout for streaming
  idle_timeout: 90s

logging:
  level: info
  format: json           # Structured for log aggregation

metrics:
  enabled: true
  path: /metrics         # Prometheus endpoint

health:
  path: /health          # Kubernetes readiness probe

routing:
  default_strategy: least-connections

# Model mappings: frontend model → backend model pool
models:
  claude-sonnet-4-20250514:
    backends:
      - endpoint: endpoint-a
        model: "claude-sonnet-4-20250514"
        weight: 10
      - endpoint: endpoint-b
        model: "custom-sonnet-model"    # Provider uses different name
        weight: 5
      - endpoint: endpoint-c
        model: "sonnet-v4"
        weight: 5

  claude-opus-4-20250514:
    backends:
      - endpoint: endpoint-a
        model: "claude-opus-4-20250514"
        weight: 10
      - endpoint: endpoint-c
        model: "opus-premium"
        weight: 8

# Endpoint definitions
endpoints:
  endpoint-a:
    url: "https://api.anthropic.com"
    api_key: "${ANTHROPIC_API_KEY_A}"    # Environment variable
    timeout: 90s

  endpoint-b:
    url: "https://custom-provider.example.com/v1"
    api_key: "${CUSTOM_PROVIDER_KEY}"
    timeout: 90s

  endpoint-c:
    url: "https://another-provider.io/anthropic"
    api_key: "${ANOTHER_PROVIDER_KEY}"
    timeout: 90s

# Automatic endpoint health management
endpoint_health:
  failures_to_disable: 5
  recovery_probe_interval: 30s
  successes_to_enable: 2
```

---

## Implementation Technology

| Component | Technology | Rationale |
|-----------|------------|-----------|
| HTTP Server | `net/http` (Go standard library) | No external dependencies, proven |
| Streaming | SSE with `http.Flusher` | Real-time streaming without buffering |
| Configuration | YAML with `gopkg.in/yaml.v3` | Human-readable, widely supported |
| Metrics | Prometheus `client_golang` | Standard observability |
| Testing | Go `testing` + `httptest` | Built-in, no external framework |
| Logging | Structured JSON logs | Compatible with log aggregators |

---

## Observability

### Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `proxy_requests_total` | Counter | Total requests processed |
| `proxy_requests_by_model` | Counter | Requests per model |
| `proxy_requests_by_endpoint` | Counter | Requests per endpoint |
| `proxy_request_duration_seconds` | Histogram | Request latency distribution |
| `proxy_endpoint_connections` | Gauge | Active connections per endpoint |
| `proxy_endpoint_failures_total` | Counter | Failures per endpoint |
| `proxy_endpoint_enabled` | Gauge | Endpoint enabled status (1/0) |

### Health Endpoint

```bash
# Kubernetes readiness probe
GET /health
Response: {"status": "healthy", "endpoints": {"endpoint-a": "enabled", "endpoint-b": "disabled"}}
```

### Access Logs

```json
{
  "timestamp": "2026-04-22T10:30:00Z",
  "method": "POST",
  "path": "/v1/messages",
  "model": "claude-sonnet-4-20250514",
  "endpoint": "endpoint-a",
  "backend_model": "claude-sonnet-4-20250514",
  "status": 200,
  "duration_ms": 2340,
  "streaming": true,
  "tokens_input": 150,
  "tokens_output": 500
}
```

---

## Security for Public Network Deployment

When deploying the proxy on a public cloud VPS, network access must be restricted to prevent unauthorized usage.

### Access Control Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        Client Machine                         │
│  Terminal: ssh -L 8080:localhost:8080 user@vps-host -N      │
│                                                              │
│  ┌──────────────────┐                                        │
│  │ Claude Code App  │ ──HTTP──► localhost:8080                │
│  └──────────────────┘                                        │
└────────────────────────────┬────────────────────────────────┘
                             │ SSH encrypted tunnel
                             ▼
┌─────────────────────────────────────────────────────────────┐
│                         VPS (Cloud)                           │
│                                                              │
│  Firewall:                                                  │
│  - Port 22 (SSH): Open to internet                          │
│  - Port 8080 (proxy): BLOCKED from internet                 │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  Proxy (127.0.0.1:8080)                               │  │
│  │  - Binds to localhost only                            │  │
│  │  - No TLS (SSH handles encryption)                   │  │
│  └────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

### Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Access control | SSH tunnel | Kernel-level crypto, no proxy-side TLS overhead |
| Proxy binding | localhost only (`127.0.0.1:8080`) | Never directly accessible from internet |
| Authentication | SSH key auth | Sufficient for team access |
| Token middleware | None | Not needed when SSH restricts access |

### Security Properties

| Property | Protection |
|----------|------------|
| Network access | Only SSH port exposed; proxy port blocked by firewall |
| Authentication | SSH key auth (Ed25519/RSA, forward secrecy) |
| Encryption | ChaCha20/AES-256 (SSH tunnel), HTTPS (upstream) |
| Cost protection | Only SSH-key users can reach proxy |

### Deployment Checklist

1. **Proxy binds to localhost only:**
   ```yaml
   server:
     listen: "127.0.0.1:8080"
   ```

2. **VPS firewall (UFW example):**
   ```bash
   ufw default deny incoming
   ufw default allow outgoing
   ufw allow 22/tcp    # SSH
   ufw enable
   ```

3. **SSH key distribution:**
   ```bash
   # On client
   ssh-keygen -t ed25519 -C "client@hostname"
   ssh-copy-id user@vps-host
   ```

4. **Create SSH tunnel:**
   ```bash
   ssh -L 8080:localhost:8080 user@vps-host -N
   ```

### Threat Analysis

| Threat | Mitigated by |
|--------|--------------|
| Random internet access to proxy | Firewall blocks port 8080 |
| Credential brute force | SSH handles auth |
| Man-in-the-middle | SSH tunnel encrypts all traffic |
| Unauthorized VPS access | SSH key auth (admin responsibility) |

---

## Sources

- [one-api GitHub Repository](https://github.com/songquanpeng/one-api)
- [LiteLLM GitHub Repository](https://github.com/BerkeleySkycastGroup/litellm)
- [Anthropic Developer Guide for Tool Use](https://docs.anthropic.com/en/docs/build-with-claude/tool-use)
- [LiteLLM Function Calling Documentation](https://docs.litellm.ai/docs/completion/function_call)
- [OpenSSH Documentation](https://www.openssh.com/security.html)