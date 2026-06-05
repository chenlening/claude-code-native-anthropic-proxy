#!/usr/bin/env bash
set -euo pipefail

echo "=== Mem0 Memory Service Setup ==="
echo ""

# ── Prerequisites ────────────────────────────────────────────────────────
command -v docker >/dev/null 2>&1 || { echo "ERROR: docker is required"; exit 1; }

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# ── Configuration ────────────────────────────────────────────────────────
MEM0_IMAGE="jsonbored/mem0-aio:latest"
OLLAMA_IMAGE="ollama/ollama:latest"
LITESTREAM_IMAGE="litestream/litestream:latest"
NETWORK="deploy_default"
STO RAGE_VOLUME="mem0_storage"
EMBEDDING_MODEL="nomic-embed-text"

# ── Detect LLM mode ──────────────────────────────────────────────────────
LLM_MODE="${1:-cloud}"
if [[ "$LLM_MODE" != "cloud" && "$LLM_MODE" != "local" ]]; then
    echo "Usage: $0 [cloud|local]"
    echo "  cloud - Use DeepSeek V4 Flash API for memory extraction (recommended)"
    echo "  local - Use local Ollama model for everything (needs ~5GB download)"
    exit 1
fi

# ── Prompt for R2 backup credentials ─────────────────────────────────────
# Source saved defaults if available
if [[ -f "$PROJECT_DIR/deploy/.env" ]]; then
    source "$PROJECT_DIR/deploy/.env"
fi

echo ""
echo "Cloudflare R2 backup (optional — press Enter to skip):"
echo "  Litestream will continuously replicate the memory database to R2."
echo "  This protects against data loss if the local machine fails."
echo ""
echo "Credentials are derived from a Cloudflare API token (Method B):"
echo "  - Access Key ID = the 32-char UUID portion after 'cfut_' in the token"
echo "  - Secret Access Key = SHA-256 of the full token value:"
echo "    printf 'cfut_...' | sha256sum | cut -d' ' -f1"
echo "  - Verify the token first with:"
echo "    curl -s https://api.cloudflare.com/client/v4/user/tokens/verify \\"
echo "      -H 'Authorization: Bearer cfut_...'"
echo "  - Endpoint URL: https://<account-id>.r2.cloudflarestorage.com"
echo "    (account ID is in Cloudflare Dashboard → R2 → Overview)"
echo ""

# Prompt with saved defaults
read -rp "  R2 Access Key ID [${R2_ACCESS_KEY:-}]: " INPUT
R2_ACCESS_KEY="${INPUT:-$R2_ACCESS_KEY}"

if [[ -n "$R2_ACCESS_KEY" ]]; then
    read -rp "  R2 Secret Access Key [${R2_SECRET_KEY:+****}]: " INPUT
    R2_SECRET_KEY="${INPUT:-$R2_SECRET_KEY}"
    read -rp "  R2 Bucket name [${R2_BUCKET:-claude-proxy}]: " INPUT
    R2_BUCKET="${INPUT:-${R2_BUCKET:-claude-proxy}}"
    read -rp "  R2 Endpoint URL [${R2_ENDPOINT:-}]: " INPUT
    R2_ENDPOINT="${INPUT:-$R2_ENDPOINT}"
    BACKUP_ENABLED=true

    # Save to .env for next time
    cat > "$PROJECT_DIR/deploy/.env" <<DOTENV
# R2 backup credentials — sourced by setup-mem0.sh as defaults
R2_ACCESS_KEY=${R2_ACCESS_KEY}
R2_SECRET_KEY=${R2_SECRET_KEY}
R2_BUCKET=${R2_BUCKET}
R2_ENDPOINT=${R2_ENDPOINT}
DOTENV
else
    BACKUP_ENABLED=false
    echo "Skipping backup setup."
fi

# ── Create directories and network ───────────────────────────────────────
mkdir -p "$PROJECT_DIR/deploy/ollama_data"
mkdir -p "$PROJECT_DIR/deploy/mem0_data"
mkdir -p "$PROJECT_DIR/deploy/litestream"
docker network create "$NETWORK" 2>/dev/null || true
docker volume create "$STORAGE_VOLUME" 2>/dev/null || true

# ── Start Ollama ─────────────────────────────────────────────────────────
echo ""
echo "Starting Ollama container..."
docker rm -f mem0-ollama 2>/dev/null || true
docker run -d \
  --name mem0-ollama \
  --network "$NETWORK" \
  -v "$PROJECT_DIR/deploy/ollama_data:/root/.ollama" \
  --restart unless-stopped \
  "$OLLAMA_IMAGE"

echo "Waiting for Ollama to be ready..."
for i in $(seq 1 30); do
    if docker exec mem0-ollama ollama list >/dev/null 2>&1; then
        echo "Ollama ready."
        break
    fi
    if [ $i -eq 30 ]; then
        echo "ERROR: Ollama failed to start."
        exit 1
    fi
    sleep 2
done

# ── Pull embedding model ─────────────────────────────────────────────────
echo "Pulling embedding model ($EMBEDDING_MODEL, ~274MB)..."
docker exec mem0-ollama ollama pull "$EMBEDDING_MODEL"

# ── Extract DeepSeek API key from proxy config ───────────────────────────
if [[ "$LLM_MODE" == "cloud" ]]; then
    DEEPSEEK_KEY=$(grep -A 5 'deepseek:' "$PROJECT_DIR/configs/proxy.yaml" | \
        grep 'api_key:' | head -1 | \
        sed 's/.*api_key: *"\(.*\)"/\1/')
    if [[ -z "$DEEPSEEK_KEY" ]]; then
        echo "WARNING: Could not extract DeepSeek API key from configs/proxy.yaml"
        echo "Falling back to local LLM mode."
        LLM_MODE="local"
    fi
fi

# ── Start mem0-aio ───────────────────────────────────────────────────────
echo ""
echo "Starting mem0-aio container..."
docker rm -f mem0 2>/dev/null || true

# Common mem0 run args
MEM0_ARGS=(
  -d
  --name mem0
  --network "$NETWORK"
  -p 127.0.0.1:9091:8765
  -p 127.0.0.1:3000:3000
  -v "$PROJECT_DIR/deploy/mem0_data:/appdata"
  -v "${STORAGE_VOLUME}:/mem0/storage"
  -e MEM0_API_HOST=0.0.0.0
  -e AUTH_DISABLED=true
  -e EMBEDDER_PROVIDER=ollama
  -e EMBEDDER_MODEL="$EMBEDDING_MODEL"
  -e EMBEDDER_DIMENSIONS=768
  -e OLLAMA_BASE_URL="http://mem0-ollama:11434"
  --restart unless-stopped
)

if [[ "$LLM_MODE" == "cloud" ]]; then
    echo "  LLM: DeepSeek V4 Flash (cloud API)"
    docker run \
      "${MEM0_ARGS[@]}" \
      -e LLM_PROVIDER=deepseek \
      -e LLM_API_KEY="$DEEPSEEK_KEY" \
      -e LLM_MODEL=deepseek-v4-flash \
      "$MEM0_IMAGE"
else
    echo "  LLM: local Ollama (llama3.1, ~5GB download needed)"
    docker run \
      "${MEM0_ARGS[@]}" \
      -e LLM_PROVIDER=ollama \
      -e LLM_MODEL=llama3.1:latest \
      "$MEM0_IMAGE"

    echo ""
    echo "Pulling LLM model (llama3.1, ~5GB, may take a while)..."
    docker exec mem0-ollama ollama pull llama3.1:latest || {
        echo "WARNING: llama3.1 pull failed (network issues?)."
        echo "Memory extraction will not work until this model is pulled."
        echo "Consider re-running with: $0 cloud"
    }
fi

# ── Generate Litestream config ───────────────────────────────────────────
if [[ "$BACKUP_ENABLED" == "true" ]]; then
    echo ""
    echo "Generating Litestream config for R2 backup..."
    sed \
      -e "s|\${R2_BUCKET}|${R2_BUCKET}|g" \
      -e "s|\${R2_ENDPOINT}|${R2_ENDPOINT}|g" \
      -e "s|\${R2_ACCESS_KEY}|${R2_ACCESS_KEY}|g" \
      -e "s|\${R2_SECRET_KEY}|${R2_SECRET_KEY}|g" \
      "$PROJECT_DIR/deploy/litestream/litestream.yml.template" \
      > "$PROJECT_DIR/deploy/litestream/litestream.yml"

    echo "Starting Litestream backup sidecar..."
    docker rm -f mem0-litestream 2>/dev/null || true
    docker run -d \
      --name mem0-litestream \
      --user 0:0 \
      --network "$NETWORK" \
      -v "${STORAGE_VOLUME}:/data" \
      -v "$PROJECT_DIR/deploy/litestream/litestream.yml:/etc/litestream.yml:ro" \
      --restart unless-stopped \
      "$LITESTREAM_IMAGE" replicate
fi

# ── Wait for mem0-aio to be ready ────────────────────────────────────────
echo ""
echo "Waiting for mem0-aio to start..."
for i in $(seq 1 30); do
    if curl -sf http://localhost:9091/api/v1/config/ >/dev/null 2>&1; then
        echo "mem0-aio ready."
        break
    fi
    if [ $i -eq 30 ]; then
        echo "ERROR: mem0-aio failed to start."
        exit 1
    fi
    sleep 2
done

# ── Reset config to pick up env vars ─────────────────────────────────────
echo "Resetting mem0 config from env vars..."
curl -sf -X POST http://localhost:9091/api/v1/config/reset >/dev/null

# ── Verify ───────────────────────────────────────────────────────────────
echo ""
echo "=== Verification ==="
echo "LLM config:"
curl -sf http://localhost:9091/api/v1/config/ | python3 -m json.tool 2>/dev/null | grep -A 5 '"llm"'
echo ""

# End-to-end test
echo "Testing memory creation..."
curl -sf -X POST http://localhost:9091/api/v1/memories/ \
  -H "Content-Type: application/json" \
  -d '{"text": "user: Memory system test.\nassistant: Test acknowledged.", "user_id": "default_user", "infer": true, "app": "claude-proxy"}' \
  >/dev/null && echo "  Create: OK" || echo "  Create: FAILED"

# ── Litestream verification ──────────────────────────────────────────────
if [[ "$BACKUP_ENABLED" == "true" ]]; then
    sleep 3
    echo ""
    echo "Litestream status:"
    docker logs mem0-litestream --tail 5 2>&1 || echo "  (checking...)"
fi

# ── Done ─────────────────────────────────────────────────────────────────
echo ""
echo "=== Setup complete ==="
echo "  Mem0 API:      http://localhost:9091"
echo "  Mem0 UI:       http://localhost:3000"
if [[ "$BACKUP_ENABLED" == "true" ]]; then
    echo "  R2 Backup:     active (litestream → ${R2_BUCKET})"
fi
echo "  View logs:     docker logs mem0 -f"
echo "  Ollama logs:   docker logs mem0-ollama -f"
echo ""
echo "To enable memory in the proxy, set in configs/proxy.yaml:"
echo "  memory.enabled: true"
echo "  memory.context_injection.enabled: true"
echo "Then restart: sudo systemctl restart proxy-anthropic"
