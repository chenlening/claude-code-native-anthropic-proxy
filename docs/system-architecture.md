# Anthropic Transparent Proxy - System Architecture

**Date:** 2026-05-25
**Status:** Current
**Version:** 2.0

---

## Executive Summary

### Problem Statement

Claude Code only supports a single Anthropic API endpoint, creating several operational challenges:

1. **Multi-endpoint limitation**: Organizations need to route requests across multiple Anthropic API providers (official API, custom providers, different regions) but Claude Code cannot natively support this
2. **No automatic failover**: If one endpoint fails or is rate-limited, Claude Code cannot automatically retry on another endpoint
3. **No endpoint health awareness**: Claude Code has no visibility into endpoint health, latency, or capacity
4. **Existing solutions have critical limitations**: Current open source proxy solutions (one-api, LiteLLM) introduce format conversion issues, database requirements, and unnecessary complexity

### Solution Overview

A **transparent Anthropic API proxy** designed specifically for Claude Code compatibility:

- Presents a single Anthropic-compatible endpoint to Claude Code
- Discovers supported models from each backend via `/v1/models` at startup and periodically
- Load balances across multiple upstream Anthropic endpoints using least-connections
- Retries on HTTP 429 (rate limit) across available endpoints
- Automatically disables failing endpoints and probes for recovery
- **Stateless deployment** - no database required
- **Zero modification** - request body forwarded completely unchanged, preserving all Anthropic features
- **Transparent streaming** - native Anthropic SSE format preserved

### Key Differentiators

| Feature | This Solution | one-api | LiteLLM |
|---------|---------------|---------|---------|
| Database Required | No | Yes (SQLite/MySQL) | Optional |
| Format Conversion | None | OpenAI→Anthropic | Multi-format |
| Anthropic Native | Full | Partial | Partial |
| Stateless | Yes | No | Partial |
| Claude Code Tool Use | Fully compatible | May have issues | May have issues |
| Deployment Complexity | Single binary | Database + migrations | Python runtime |
| Extended Thinking | Supported | Unknown | Unknown |
| Model Discovery | Dynamic (/v1/models) | Static config | Static config |

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
| **Format** | OpenAI→Anthropic | Multi-format | Anthropic native (unchanged) |
| **Auth** | Required | Optional | None (trust boundary at network) |
| **Tool Use** | May have issues | May have issues | Fully compatible |
| **Streaming** | SSE conversion | SSE conversion | Transparent SSE |
| **Deployment** | Binary + DB | Python + deps | Single binary |
| **Extended Thinking** | Unknown | Unknown | Supported |
| **Prompt Caching** | Unknown | Unknown | Supported |
| **Stateless** | No | Partial | Yes |
| **Model Discovery** | Static config | Static config | Dynamic /v1/models |

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
│  │ HTTP Server │──│ Model        │──│ Load Balancer        │   │
│  │ (net/http)  │  │ Discovery    │  │ (least-connections)  │   │
│  └─────────────┘  └──────────────┘  └──────────────────────┘   │
│         │                │                     │                 │
│         │                │                     │                 │
│         ▼                ▼                     ▼                 │
│  ┌─────────────┐  ┌──────────────┐  ┌──────────────────────┐   │
│  │ Endpoint    │  │ Health       │  │ Connection Tracker   │   │
│  │ Health      │  │ Manager      │  │ (per-model)          │   │
│  └─────────────┘  └──────────────┘  └──────────────────────┘   │
│         │                                                       │
│         ▼                                                       │
│  ┌─────────────┐  ┌──────────────┐  ┌──────────────────────┐   │
│  │ Metrics     │  │ Health       │  │ Models Handler       │   │
│  │ (Prometheus)│  │ Checker+HTML │  │ (/v1/models)         │   │
│  └─────────────┘  └──────────────┘  └──────────────────────┘   │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │                    YAML Configuration File                   ││
│  │  - Endpoints & API Keys                                      ││
│  │  - Health Check Parameters                                   ││
│  │  - Server & Logging Settings                                 ││
│  └─────────────────────────────────────────────────────────────┘│
└────────────────────────────┬────────────────────────────────────┘
                             │ Proxied requests (unchanged body)
                             │ Only modification: Authorization header + URL
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

### Key Design Principle: Zero Modification Transparency

**What passes through unchanged:**
- Request body (including `model` field)
- All request headers (except Authorization)
- Response headers
- Response body (SSE streaming events)
- All Anthropic-specific features (tool use, extended thinking, prompt caching)

**What is modified:**
- `Authorization` header (proxy endpoint key → backend endpoint key)
- Target URL (proxy endpoint → selected backend endpoint)

This is the core differentiator: **the request body is never modified.** Claude Code sends whatever model name it knows, and the proxy routes to backends that have discovered support for that model name. If a backend uses a different model name internally, it must be exposed as an alias via its `/v1/models` endpoint.

---

### Core Components

| Component | Responsibility | Why Needed |
|-----------|---------------|------------|
| **HTTP Server** | Accept Anthropic API requests, handle streaming responses | Entry point for Claude Code |
| **Model Discovery** | Probe each endpoint's `/v1/models`, build model→endpoints map | Know which endpoints serve which models |
| **Load Balancer** | Select endpoint from candidates using least-connections | Distribute load, avoid hot endpoints |
| **Connection Tracker** | Track active connections per model per endpoint | Enable intelligent load balancing |
| **Health Manager** | Monitor failures, disable/re-enable endpoints | Automatic failover |
| **Recovery Probe** | Periodically test disabled endpoints with real requests | Auto-recovery when backends heal |
| **Metrics Collector** | Expose Prometheus metrics + in-memory latency stats | Observability without database |
| **Health Checker** | `/health` endpoint with JSON and HTML dashboard | Monitoring and readiness probes |
| **Models Handler** | `/v1/models` endpoint returning union of healthy endpoint models | Model discovery for clients |

---

### Why Model Discovery Instead of Static Config

Traditional proxies statically configure which models route to which backends. This proxy uses **dynamic model discovery**:

1. At startup, each endpoint's `/v1/models` endpoint (or custom `models_endpoint`) is called
2. The returned model IDs are stored per endpoint: `{ "claude-sonnet-4-20250514": ["aliyun", "gzl"], ... }`
3. When a request arrives, the proxy looks up which endpoints support the requested model name
4. Discovery refreshes every 5 minutes to pick up new models

**Why this approach:**
- No need to manually sync model names across providers
- If a provider adds a new model, it's automatically available
- If a provider removes a model, it's automatically excluded
- Works even when providers use the same model names as Anthropic's official API
- Falls back gracefully: if discovery fails, a default probe model is used for health checks

---

### Why Model-Level Connection Tracking

Traditional load balancers track connections per endpoint. This proxy tracks per **model per endpoint**:

```
aliyun connections:
  ├── claude-sonnet-4-20250514: 5  (endpoint supports this model)
  ├── claude-opus-4-20250514: 2    (endpoint supports this model)
  └── total: 7

gzl connections:
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
  aliyun: 5 connections for this model
  gzl: 3 connections for this model ← SELECTED
  minimax: 4 connections for this model
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
endpoints:
  aliyun:
    url: "https://api.anthropic.com"
    api_key: "${ANTHROPIC_API_KEY}"
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
│                 │  Body preserved unchanged (no conversion)
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ 2. Lookup       │  Find endpoints that support this model
│    Endpoints    │  (from dynamic model discovery)
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ 3. Filter       │  Remove disabled endpoints
│    Healthy      │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ 4. Select       │  Least-connections among remaining candidates
│    Backend      │  - Get connection count for this model per endpoint
│                 │  - Select endpoint with lowest count
└────────┬────────┘
         │
    ┌────┴─────────────┐
    │                  │
 Found endpoint    None found
    │                  │
    ▼                  ▼
    │           Return 503:
    │           "model not supported
    │            by any endpoint"
    │
    ▼
┌─────────────────┐
│ 5. Forward      │  Modify only: Authorization header, target URL
│    Request      │  Body sent completely unchanged
│                 │  Header: Authorization → Bearer {ep.APIKey}
│                 │  URL: proxy → selected endpoint URL
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ 6. Stream       │  Transparent SSE streaming:
│    Response     │  - Receive SSE events from backend
│                 │  - Forward to client immediately
│                 │  - No buffering, no conversion
│                 │  - http.Flusher for real-time delivery
└────────┬────────┘
         │
    ┌────┴────────────┐
    │                 │
  2xx              429 (rate limited)
    │                 │
    ▼                 ▼
┌─────────┐  ┌──────────────────┐
│ Success │  │ Record Failure    │
│ Record  │  │ - Decrement conn  │
│ Success │  │ - Retry next      │
│ Done    │  │   endpoint (step 4)│
└─────────┘  └──────────────────┘
                  │
             ┌────┴────┐
             │         │
         Other 4xx/5xx
             │
             ▼
      ┌──────────────────┐
      │ Return error      │
      │ response to client│
      │ (no retry)        │
      └──────────────────┘
```

### Retry Behavior (429 Only)

The proxy **only retries on HTTP 429 (rate limiting)**. This is an intentional design choice:
- 5xx errors, timeouts, and connection failures are returned to the client immediately
- The client (Claude Code) can then decide to retry, which will hit the proxy's load balancer again — potentially selecting a different endpoint
- 429 is the one case where transparent retry adds clear value: rate limits are transient and endpoint-specific
- Retrying on 5xx would mask upstream bugs and add latency for errors the client should see

```
Request arrives for claude-sonnet-4-20250514
         │
         ▼
┌─────────────────────────────────────────────────────┐
│ Attempt 1: aliyun (least connections: 3)            │
│ Connection count: 3 → 4                            │
└─────────────────────────────────────────────────────┘
         │
    ┌────┴────┐
    │         │
 SUCCESS   429 (rate limited)
    │         │
    ▼         ▼
   DONE   ┌─────────────────────────────────────────┐
          │ Decrement connection: 4 → 3             │
          │ Record failure on aliyun                │
          │ (may disable if failures >= threshold)  │
          └─────────────────────────────────────────┘
                   │
                   ▼
          ┌─────────────────────────────────────────┐
          │ Attempt 2: minimax (next least: 4)      │
          │ Connection count: 4 → 5                 │
          └─────────────────────────────────────────┘
                   │
              ┌────┴────┐
              │         │
           SUCCESS   429 / non-2xx
              │         │
              ▼         ▼
             DONE   (all endpoints exhausted)
                      │
                      ▼
             Return HTTP 503 to client
             "no backend available"
```

### Endpoint Health & Recovery

When an endpoint accumulates `failures_to_disable` consecutive failures (default: 5), it is automatically disabled. A background goroutine probes disabled endpoints every `recovery_probe_interval` (default: 30s) by sending a real POST `/v1/messages` request. If `successes_to_enable` consecutive probes succeed (default: 2), the endpoint is re-enabled.

```
Background goroutine (every 30 seconds):
         │
         ▼
┌─────────────────────────────────────────────────────┐
│ For each disabled endpoint:                         │
│                                                     │
│ 1. Send real POST /v1/messages verification         │
│    with endpoint's probe model                      │
│                                                     │
│ 2. If success (HTTP 200):                           │
│    - Increment success counter                      │
│    - If successes >= 2: re-enable endpoint           │
│                                                     │
│ 3. If failure (non-200 / network error):            │
│    - Reset success counter to 0                     │
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
- Configuration (endpoints, keys) is static - YAML is sufficient
- Usage tracking via logs/metrics (Prometheus) - no database needed
- Deployment simplicity is paramount for Claude Code proxy use case

---

### Decision 3: Zero Modification (Not Minimal)

**Problem:** Should we convert between API formats for flexibility?

**Options Considered:**

| Approach | Pros | Cons |
|----------|------|------|
| Full OpenAI compatibility | More clients supported | Tool use incompatibility, complexity |
| Model name rewriting | Flexible naming | Body modification, streaming issues |
| Zero modification | All features preserved, simple | Backends must support the model names clients use |

**Decision:** Zero modification — request body forwarded unchanged

**Rationale:**
- Claude Code uses Anthropic native format — any modification introduces risk
- Model name rewriting requires parsing and modifying streaming bodies, which is fragile
- Dynamic model discovery means backends declare what model names they support
- Tool use is tightly coupled to Anthropic format
- Extended thinking, prompt caching have no OpenAI equivalents
- Anthropic's API protocol evolves over time (new event types, new fields, thinking blocks, etc.) — any body modification logic would need ongoing maintenance to stay compatible with protocol changes. Zero modification means the proxy automatically supports new protocol features without code changes.
- Simplicity reduces bugs and maintenance

---

### Decision 4: Retry Only on 429

**Problem:** When should the proxy transparently retry on another endpoint?

**Options Considered:**

| Approach | Pros | Cons |
|----------|------|------|
| Retry on all errors | Max availability | Masks upstream bugs, adds latency |
| Retry on 5xx only | Handles transient failures | Client may retry anyway (double retry) |
| Retry on 429 only | Handles rate limits, no masking | Client sees 5xx immediately |

**Decision:** Retry only on HTTP 429

**Rationale:**
- Rate limits are endpoint-specific and transient — retrying another endpoint always makes sense
- 5xx errors should be visible to the client so it can decide whether to retry
- If the client retries a 5xx, the proxy's load balancer will naturally try a different endpoint
- Avoids double-retry problems and latency amplification

---

### Decision 5: Go as Implementation Language

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
server:
  listen: ":8080"
  read_timeout: 30s
  write_timeout: 0s          # 0 = no timeout, important for SSE streaming
  idle_timeout: 90s

logging:
  level: debug               # debug, info, warn, error
  format: json               # Structured for log aggregation

metrics:
  enabled: true
  path: /metrics             # Prometheus endpoint

health:
  path: /health              # Health check + HTML dashboard

routing:
  default_strategy: least-connections

# Endpoint definitions
# Each endpoint represents an upstream Anthropic-compatible API
endpoints:
  aliyun:
    url: "https://api.example.com/anthropic"
    api_key: "${ALIYUN_API_KEY}"            # ${ENV_VAR} or plain text
    timeout: 300s                            # Response header timeout
    models_endpoint: ""                      # Optional: custom URL for model discovery
    offline: false                           # true = permanently excluded from routing

  gzl:
    url: "https://open.example.cn/api/anthropic"
    api_key: "${GZL_API_KEY}"
    timeout: 300s
    offline: false

# Automatic endpoint health management
endpoint_health:
  failures_to_disable: 5     # Consecutive failures before disabling
  recovery_probe_interval: 30s
  successes_to_enable: 2     # Consecutive successful probes to re-enable
```

### Endpoint Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `url` | string | Yes | Base URL of the Anthropic-compatible API |
| `api_key` | string | Yes | API key. Supports `${ENV_VAR}` and `${ENV_VAR:-default}` |
| `timeout` | duration | No | Response header timeout. Default: 90s |
| `models_endpoint` | string | No | Custom URL for model discovery. Default: `{url}/v1/models` |
| `offline` | bool | No | If true, endpoint is permanently excluded from routing |

### Environment Variable Expansion

The config supports `${VAR}` and `${VAR:-default}` syntax in `api_key` values. This lets you keep secrets out of the config file:

```yaml
endpoints:
  production:
    url: "https://api.anthropic.com"
    api_key: "${ANTHROPIC_PROD_KEY}"
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
| Logging | `log/slog` (Go standard library) | Structured JSON logging |
| Build | `go build` | Single binary |

---

## Observability

### Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `anthropic_proxy_requests_total` | Counter | Total requests processed |
| `anthropic_proxy_requests_by_model` | Counter | Requests per model (frontend model name) |
| `anthropic_proxy_requests_by_endpoint` | Counter | Requests per endpoint |
| `anthropic_proxy_request_duration_seconds` | Histogram | Request latency distribution (by model + endpoint) |
| `anthropic_proxy_endpoint_failures_total` | Counter | Failures per endpoint |
| `anthropic_proxy_endpoint_enabled` | Gauge | Endpoint enabled status (1=enabled, 0=disabled) |

### Health Endpoint

**`GET /health`** returns JSON (or HTML dashboard when requested by a browser):

```json
{
  "status": "healthy",
  "total_requests": 15420,
  "endpoints": {
    "aliyun": {
      "status": "enabled",
      "requests": 8230,
      "failures": 2,
      "active_connections": 3,
      "lastRequestTime": "2026-05-25T10:30:00Z",
      "lastFailureTime": "2026-05-25T09:15:00Z",
      "lastFailureReason": "status=429 body=...",
      "supported_models": ["claude-sonnet-4-20250514", "claude-opus-4-20250514"]
    },
    "gzl": {
      "status": "disabled",
      "requests": 4100,
      "failures": 8,
      "active_connections": 0,
      "lastProbeTime": "2026-05-25T10:29:30Z",
      "lastProbeSuccess": false,
      "supported_models": ["claude-sonnet-4-20250514"]
    }
  },
  "models": {
    "claude-sonnet-4-20250514": {
      "requests": 12000,
      "latency": {"count": 12000, "min_ms": 1200, "max_ms": 45000, "avg_ms": 3200}
    }
  },
  "by_backend": [
    {
      "frontend_model": "claude-sonnet-4-20250514",
      "backend_model": "claude-sonnet-4-20250514",
      "endpoint": "aliyun",
      "latency": {"count": 8000, "min_ms": 1200, "max_ms": 30000, "avg_ms": 2800}
    }
  ]
}
```

**Status values:**
- `healthy` — all endpoints enabled
- `degraded` — some endpoints disabled, but at least one enabled
- `unhealthy` — no enabled endpoints

### Structured Access Logs

```json
{
  "time": "2026-05-25T10:30:00Z",
  "level": "INFO",
  "msg": "routing request",
  "frontend_model": "claude-sonnet-4-20250514",
  "endpoint": "aliyun",
  "attempt": 1
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
