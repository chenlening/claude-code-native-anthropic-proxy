# Install: anthropic-transparent-proxy

Install the anthropic-transparent-proxy on this machine. Follow each phase in order.

---

## Phase 1: OS Detection

**Step 1.1: Check OS**

Run: `uname -s`

- If output is `Linux`, continue to Step 1.2.
- If output is `Darwin`, continue to Step 1.3.
- If anything else, report error: "Unsupported OS. This install supports Linux (Ubuntu/Debian) and macOS." and stop.

**Step 1.2: Linux — verify distribution**

Run: `grep -qi 'ubuntu\|debian' /etc/os-release && echo "OK" || echo "FAIL"`

- If "OK", continue to Step 1.4.
- If "FAIL", report error: "This install requires Ubuntu or Debian Linux." and stop.

**Step 1.3: macOS — skip distro check**

macOS is supported as-is. Continue to Step 1.5.

**Step 1.4: Linux — check sudo**

Run: `command -v sudo && sudo -n true 2>/dev/null && echo "OK" || echo "WARN"`

- If "OK", continue.
- If "WARN", inform: "Password may be required for sudo operations." and continue.

**Step 1.5: Check existing service**

On Linux, run: `systemctl is-active proxy-anthropic 2>/dev/null || echo "inactive"`
On macOS, run: `launchctl list | grep com.anthropic.proxy || echo "not found"`

- If output indicates service is running/installed, inform: "Proxy service is already running. This install will rebuild and restart with the latest code. Config at configs/proxy.yaml will NOT be modified."
- If inactive/not found, continue.

Set a flag: if service was already active, `REINSTALL=true`, otherwise `REINSTALL=false`.

**Step 1.6: Ask about memory setup**

Ask: "Set up memory service? This uses mem0-aio (Docker all-in-one) + Ollama for local embeddings (~274MB download). For memory extraction, it can use the proxy's existing DeepSeek API key (cloud, near-zero cost) or a local model (~5GB download, slow in some regions)."

- If yes, set `MEMORY=true`. Then ask: "Use cloud API for memory extraction? (recommended: uses proxy's DeepSeek V4 Flash key, near-zero cost, no extra download)"
  - If yes, set `MEMORY_LLM=cloud`.
  - If no, set `MEMORY_LLM=local`.
- If no, set `MEMORY=false`.

---

## Phase 2: Go

**Step 2.1: Check existing Go version**

Run: `go version 2>/dev/null || echo "not found"`

- If "not found", proceed to Step 2.2.
- If Go version is 1.23 or higher, continue to Phase 3.
- If Go version is below 1.23, proceed to Step 2.2.

**Step 2.2: Install Go**

Determine the OS and architecture:

On Linux:
- Run: `GO_OS=linux && GO_ARCH=amd64`

On macOS:
- Run: `GO_OS=darwin && uname -m`
  - If `arm64`, set: `GO_ARCH=arm64`
  - If `x86_64`, set: `GO_ARCH=amd64`

Run the following commands in sequence:

```bash
cd /tmp
curl -LO "https://go.dev/dl/go1.23.7.${GO_OS}-${GO_ARCH}.tar.gz"
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf "go1.23.7.${GO_OS}-${GO_ARCH}.tar.gz"
rm "go1.23.7.${GO_OS}-${GO_ARCH}.tar.gz"
```

Add to PATH in shell profile:

On Linux, run: `grep -q 'export PATH=/usr/local/go/bin' ~/.bashrc || echo 'export PATH=/usr/local/go/bin:$PATH' >> ~/.bashrc`

On macOS, detect the shell profile file first:
Run: `test -f ~/.zshrc && echo "zshrc" || (test -f ~/.bash_profile && echo "bash_profile" || echo "unknown")`
- If "zshrc", run: `grep -q 'export PATH=/usr/local/go/bin' ~/.zshrc || echo 'export PATH=/usr/local/go/bin:$PATH' >> ~/.zshrc`
- If "bash_profile", run: `grep -q 'export PATH=/usr/local/go/bin' ~/.bash_profile || echo 'export PATH=/usr/local/go/bin:$PATH' >> ~/.bash_profile`
- If "unknown", run: `echo 'export PATH=/usr/local/go/bin:$PATH' >> ~/.zshrc`

Verify: `go version`
Expected: contains "go1.23.7". Continue to Phase 3.

---

## Phase 3: Build

**Step 3.1: Build the proxy binary**

Run from the project root directory:

```bash
go build -o bin/proxy ./cmd/proxy
```

Verify: `ls -lh bin/proxy`
Expected: binary file exists. Report its size.

**Step 3.2: Build the proxy-memory binary**

Run from the project root directory:

```bash
go build -o bin/proxy-memory ./cmd/proxy-memory
```

Verify: `ls -lh bin/proxy-memory`
Expected: binary file exists. Report its size.

---

## Phase 4: Service + Health Check

This phase has two branches: Linux (systemd) and macOS (launchd). Follow the correct branch below.

### Linux Branch: systemd

**Step 4.L1: Write the systemd service file**

```bash
PROJECT_DIR=$(git rev-parse --show-toplevel)
cat <<EOF | sudo tee /etc/systemd/system/proxy-anthropic.service
[Unit]
Description=Anthropic Transparent Proxy
After=network.target

[Service]
Type=simple
User=$(whoami)
Group=$(id -gn)
WorkingDirectory=${PROJECT_DIR}
Environment="PATH=/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
ExecStart=${PROJECT_DIR}/bin/proxy --config ${PROJECT_DIR}/configs/proxy.yaml
Restart=on-failure
RestartSec=5s
StandardOutput=journal
StandardError=journal
SyslogIdentifier=proxy-anthropic

[Install]
WantedBy=multi-user.target
EOF
```

**Step 4.L2: Reload systemd and enable**

```bash
sudo systemctl daemon-reload
sudo systemctl enable proxy-anthropic
```

**Step 4.L3: Start or restart the service**

Run: `systemctl is-active proxy-anthropic 2>/dev/null || echo "inactive"`
- If active: `sudo systemctl restart proxy-anthropic`
- If inactive: `sudo systemctl start proxy-anthropic`

**Step 4.L4: Health check**

```bash
for i in $(seq 1 15); do
  curl -sf http://localhost:8080/health > /dev/null 2>&1 && echo "READY" && break
  if [ $i -eq 15 ]; then
    echo "FAILED"
    break
  fi
  sleep 1
done
```

If "READY", continue to Phase 5.
If "FAILED", report error: "Service failed to start within 15 seconds. Check logs: journalctl -u proxy-anthropic -n 20" and stop.

Now skip ahead to Phase 5 (the macOS branch below is complete).

### macOS Branch: launchd

**Step 4.M1: Write the launchd plist file**

```bash
PROJECT_DIR=$(git rev-parse --show-toplevel)
cat <<EOF > ~/Library/LaunchAgents/com.anthropic.proxy.plist
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.anthropic.proxy</string>
    <key>ProgramArguments</key>
    <array>
        <string>${PROJECT_DIR}/bin/proxy</string>
        <string>--config</string>
        <string>${PROJECT_DIR}/configs/proxy.yaml</string>
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
    <string>${PROJECT_DIR}</string>
</dict>
</plist>
EOF
```

**Step 4.M2: Stop existing and load the service**

```bash
pkill -f "./bin/proxy" 2>/dev/null || true
launchctl unload ~/Library/LaunchAgents/com.anthropic.proxy.plist 2>/dev/null || true
launchctl load ~/Library/LaunchAgents/com.anthropic.proxy.plist
```

**Step 4.M3: Health check**

```bash
for i in $(seq 1 15); do
  curl -sf --noproxy '*' http://localhost:8080/health > /dev/null 2>&1 && echo "READY" && break
  if [ $i -eq 15 ]; then
    echo "FAILED"
    break
  fi
  sleep 1
done
```

If "READY", continue to Phase 5.
If "FAILED", report error: "Service failed to start within 15 seconds. Check logs: tail -50 /tmp/proxy-anthropic.log" and stop.

---

## Phase 4.5: Memory Setup (OPTIONAL)

Skip to Phase 5 if MEMORY=false.

This phase sets up the Mem0 memory backend for conversation context persistence.

**Architecture:**
- `jsonbored/mem0-aio:latest` — all-in-one Docker image (API + Qdrant vector DB + Next.js UI)
- Ollama — local embeddings only (`nomic-embed-text`, ~274MB)
- LLM for memory extraction — either cloud API (DeepSeek V4 Flash, recommended) or local model

**Key gotchas from prior installs:**
- `mem0-aio` **LLM config is persisted in SQLite** — env vars only seed the initial config. After changing env vars, call `POST /api/v1/config/reset`.
- For non-ollama LLM providers, the API key env var is `LLM_API_KEY` (not `DEEPSEEK_API_KEY`).
- Ollama pulls from `ollama.com` directly — Docker daemon proxy (port 7897) only works for Docker Hub, not Ollama registry traffic.
- Local LLM models (llama3.1 ~5GB) often stall on poor connections. Cloud API is recommended.

**Step 4.5.1: Check Docker**

Run: `command -v docker && echo "OK" || echo "NOT_FOUND"`

- If "NOT_FOUND", report: "Docker not found. Skipping memory setup." and skip to Phase 5.
- If "OK", continue.

**Step 4.5.2: Start Ollama container**

Create a Docker network if not exists:

```bash
docker network create deploy_default 2>/dev/null || true
```

Start Ollama (local embeddings only):

```bash
PROJECT_DIR=$(git rev-parse --show-toplevel)
mkdir -p "${PROJECT_DIR}/deploy/ollama_data"

docker rm -f mem0-ollama 2>/dev/null || true
docker run -d \
  --name mem0-ollama \
  --network deploy_default \
  -v "${PROJECT_DIR}/deploy/ollama_data:/root/.ollama" \
  --restart unless-stopped \
  ollama/ollama:latest
```

Wait for Ollama to be ready:

```bash
for i in $(seq 1 30); do
  docker exec mem0-ollama ollama list >/dev/null 2>&1 && echo "READY" && break
  if [ $i -eq 30 ]; then echo "FAILED" && exit 1; fi
  sleep 2
done
```

**Step 4.5.3: Pull nomic-embed-text embedding model**

```bash
docker exec mem0-ollama ollama pull nomic-embed-text
```

This is ~274MB and typically downloads without issues. If it stalls, re-run the command.

**Step 4.5.4: Start mem0-aio container**

For cloud LLM (MEMORY_LLM=cloud) — uses proxy's existing DeepSeek API key:

```bash
PROJECT_DIR=$(git rev-parse --show-toplevel)
DEEPSEEK_KEY=$(grep -A 5 'deepseek:' "${PROJECT_DIR}/configs/proxy.yaml" | grep 'api_key:' | head -1 | sed 's/.*api_key: *"\(.*\)"/\1/')
mkdir -p "${PROJECT_DIR}/deploy/mem0_data"

docker rm -f mem0 2>/dev/null || true
docker run -d \
  --name mem0 \
  --network deploy_default \
  -p 127.0.0.1:9091:8765 \
  -p 127.0.0.1:3000:3000 \
  -v "${PROJECT_DIR}/deploy/mem0_data:/appdata" \
  -e MEM0_API_HOST=0.0.0.0 \
  -e AUTH_DISABLED=true \
  -e LLM_PROVIDER=deepseek \
  -e LLM_API_KEY="${DEEPSEEK_KEY}" \
  -e LLM_MODEL=deepseek-v4-flash \
  -e EMBEDDER_PROVIDER=ollama \
  -e EMBEDDER_MODEL=nomic-embed-text \
  -e EMBEDDER_DIMENSIONS=768 \
  -e OLLAMA_BASE_URL=http://mem0-ollama:11434 \
  --restart unless-stopped \
  jsonbored/mem0-aio:latest
```

For local LLM (MEMORY_LLM=local) — uses Ollama for both embedding and extraction:

```bash
PROJECT_DIR=$(git rev-parse --show-toplevel)
mkdir -p "${PROJECT_DIR}/deploy/mem0_data"

docker rm -f mem0 2>/dev/null || true
docker run -d \
  --name mem0 \
  --network deploy_default \
  -p 127.0.0.1:9091:8765 \
  -p 127.0.0.1:3000:3000 \
  -v "${PROJECT_DIR}/deploy/mem0_data:/appdata" \
  -e MEM0_API_HOST=0.0.0.0 \
  -e AUTH_DISABLED=true \
  -e LLM_PROVIDER=ollama \
  -e LLM_MODEL=llama3.1:latest \
  -e EMBEDDER_PROVIDER=ollama \
  -e EMBEDDER_MODEL=nomic-embed-text \
  -e EMBEDDER_DIMENSIONS=768 \
  -e OLLAMA_BASE_URL=http://mem0-ollama:11434 \
  --restart unless-stopped \
  jsonbored/mem0-aio:latest
```

Wait for mem0-aio to be ready:

```bash
for i in $(seq 1 30); do
  curl -sf http://localhost:9091/api/v1/config/ >/dev/null 2>&1 && echo "READY" && break
  if [ $i -eq 30 ]; then echo "FAILED" && exit 1; fi
  sleep 2
done
```

**Step 4.5.5: Reset mem0 config to pick up env vars**

The mem0-aio config is stored in SQLite. Reset it so env vars take effect:

```bash
curl -sf -X POST http://localhost:9091/api/v1/config/reset
```

Verify the LLM provider is correct:

```bash
curl -sf http://localhost:9091/api/v1/config/ | python3 -m json.tool | grep -A 3 '"llm"'
```

Expected: shows the correct provider (deepseek or ollama) and model.

**Step 4.5.6: Verify memory service end-to-end**

Test creating and searching a memory:

```bash
# Create a test memory
curl -sf -X POST http://localhost:9091/api/v1/memories/ \
  -H "Content-Type: application/json" \
  -d '{
    "text": "user: I am testing the memory system.\nassistant: Memory test acknowledged.",
    "user_id": "default_user",
    "infer": true,
    "app": "claude-proxy"
  }' > /dev/null && echo "CREATE OK" || echo "CREATE FAILED"

# Verify it appears
sleep 2
TEST_COUNT=$(curl -sf http://localhost:8080/health | python3 -c "import sys,json; print(json.load(sys.stdin).get('memory',{}).get('total',0))")
echo "Memory count: ${TEST_COUNT}"
```

Report the memory count. If > 0, memory is working end-to-end.

**Step 4.5.7: Enable memory in proxy config**

If MEMORY_LLM=local (Ollama-only LLM), also pull the LLM model (~5GB, may take time on slow connections):

```bash
docker exec mem0-ollama ollama pull llama3.1:latest
```

Note: if this pull stalls repeatedly, fall back to cloud LLM by re-running the cloud mem0 container command above and resetting config.

Enable memory in the proxy configuration:

Set `memory.enabled: true` and `memory.context_injection.enabled: true` in `configs/proxy.yaml`.

Then restart the proxy:

On Linux: `sudo systemctl restart proxy-anthropic`
On macOS: `launchctl unload ~/Library/LaunchAgents/com.anthropic.proxy.plist && launchctl load ~/Library/LaunchAgents/com.anthropic.proxy.plist`

Report: "Memory enabled in proxy config and service restarted."

**Step 4.5.8: Configure R2 backup (optional)**

Ask: "Set up Cloudflare R2 backup? (Litestream continuously syncs the memory database to R2. Requires a Cloudflare API token.)"

- If user declines, skip to Phase 5.
- If user accepts, provide guidance on obtaining credentials:

**Getting R2 credentials from a Cloudflare API token:**

The S3-compatible R2 credentials are derived from a Cloudflare API token (starts with `cfut_`):
- **Access Key ID** = the 32-char UUID after `cfut_` in the token
- **Secret Access Key** = SHA-256 of the full token value:
  `printf 'cfut_...' | sha256sum | cut -d' ' -f1`
- Verify the token first:
  `curl -sf https://api.cloudflare.com/client/v4/user/tokens/verify -H 'Authorization: Bearer cfut_...' | python3 -m json.tool`

**R2 endpoint and bucket:**
- **R2 Endpoint**: `https://<account-id>.r2.cloudflarestorage.com`
  (account ID is a 32-char hex string, found in Cloudflare Dashboard → R2 → Overview)
- **Bucket name**: create one in R2 if needed

Ask for these four values, showing current defaults:
- R2 Access Key ID [default: `d46d9c25f9bc1545d7e1edc1d2881d45`]
- R2 Secret Access Key [default: `b167b8fc8268740756d956d825de2200405bf8dd6e19759998076d0f42ae5405`]
- R2 Bucket name [default: `claude-proxy`]
- R2 Endpoint URL [default: `https://4083de5f3464e55e39cd3b34a005430b.r2.cloudflarestorage.com`]

If the user presses Enter (accepting defaults), use these values directly.
If they provide new values, substitute them below.

Then generate the litestream config and start the sidecar:

```bash
PROJECT_DIR=$(git rev-parse --show-toplevel)

# Use defaults or user-provided values
R2_ACCESS_KEY="${R2_ACCESS_KEY:-d46d9c25f9bc1545d7e1edc1d2881d45}"
R2_SECRET_KEY="${R2_SECRET_KEY:-b167b8fc8268740756d956d825de2200405bf8dd6e19759998076d0f42ae5405}"
R2_BUCKET="${R2_BUCKET:-claude-proxy}"
R2_ENDPOINT="${R2_ENDPOINT:-https://4083de5f3464e55e39cd3b34a005430b.r2.cloudflarestorage.com}"

# Generate litestream config from template
sed \
  -e "s|\${R2_BUCKET}|${R2_BUCKET}|g" \
  -e "s|\${R2_ENDPOINT}|${R2_ENDPOINT}|g" \
  -e "s|\${R2_ACCESS_KEY}|${R2_ACCESS_KEY}|g" \
  -e "s|\${R2_SECRET_KEY}|${R2_SECRET_KEY}|g" \
  "${PROJECT_DIR}/deploy/litestream/litestream.yml.template" \
  > "${PROJECT_DIR}/deploy/litestream/litestream.yml"

# Start Litestream sidecar
docker rm -f mem0-litestream 2>/dev/null || true
docker run -d \
  --name mem0-litestream \
  --user 0:0 \
  --network deploy_default \
  -v mem0_storage:/data \
  -v "${PROJECT_DIR}/deploy/litestream/litestream.yml:/etc/litestream.yml:ro" \
  --restart unless-stopped \
  litestream/litestream:latest replicate
```

Wait a few seconds and verify:

```bash
docker logs mem0-litestream --tail 5
```

Should show Litestream initializing the replica. If there are authentication errors (401/403), check the R2 credentials.

Report: "R2 backup active — memory database syncing to ${R2_BUCKET}."

---

## Phase 5: Cleanup old settings.json

Only proceed if Phase 4 health check passed.

**Step 5.1: Check for existing settings.json**

Run: `ls -la ~/.claude/settings.json 2>/dev/null || echo "not found"`

- If "not found", skip to Phase 6.
- If file exists, continue to Step 5.2.

**Step 5.2: Backup settings.json**

Run: `cp ~/.claude/settings.json ~/.claude/settings.json.backup.$(date +%Y%m%d%H%M%S)`

Report: "Backed up settings.json to ~/.claude/settings.json.backup.[timestamp]"

**Step 5.3: Read and strip old proxy env keys**

Read `~/.claude/settings.json`.

Use the Write tool to rewrite `~/.claude/settings.json`, removing these keys from the `env` object if present:
- `ANTHROPIC_AUTH_TOKEN`
- `ANTHROPIC_BASE_URL`
- `ANTHROPIC_DEFAULT_HAIKU_MODEL`
- `ANTHROPIC_DEFAULT_SONNET_MODEL`
- `ANTHROPIC_DEFAULT_OPUS_MODEL`
- `ANTHROPIC_MODEL`
- `ANTHROPIC_SMALL_FAST_MODEL`
- `CLAUDE_CODE_SUBAGENT_MODEL`

Preserve all other keys unchanged (`permissions`, `enabledPlugins`, other env vars, etc.).

Verify: `cat ~/.claude/settings.json`
Report: "Removed old proxy env vars from ~/.claude/settings.json. You now control models through claude-proxy."

---

## Phase 6: Install claude-proxy wrapper

**Step 6.1: Copy wrapper to /usr/local/bin and pin config path**

```bash
PROJECT_DIR=$(git rev-parse --show-toplevel)
sudo cp "${PROJECT_DIR}/claude-proxy" /usr/local/bin/claude-proxy
sudo chmod +x /usr/local/bin/claude-proxy
# Pin CONFIG_FILE to the absolute project path so the wrapper works from any CWD
sudo sed -i '' "s|^CONFIG_FILE=\"configs/proxy.yaml\"|CONFIG_FILE=\"${PROJECT_DIR}/configs/proxy.yaml\"|" /usr/local/bin/claude-proxy 2>/dev/null || \
sudo sed -i "s|^CONFIG_FILE=\"configs/proxy.yaml\"|CONFIG_FILE=\"${PROJECT_DIR}/configs/proxy.yaml\"|" /usr/local/bin/claude-proxy
```

On macOS, if `/usr/local/bin` does not exist: `sudo mkdir -p /usr/local/bin`

**Step 6.2: Verify wrapper is on PATH**

Run: `which claude-proxy && echo "OK" || echo "FAIL"`
Expected: "OK" (shows `/usr/local/bin/claude-proxy`)

---

## Phase 7: Final Verify

**Step 7.1: Quick health check**

On Linux, run: `curl -sf http://localhost:8080/health`
On macOS, run: `curl -sf --noproxy '*' http://localhost:8080/health`

Report the JSON response briefly (endpoint count, overall status).

**Step 7.2: Print completion summary**

Report:

```
==========================================
 Installation complete!
==========================================
 IMPORTANT: Use 'claude-proxy' instead of 'claude' from now on.

 Proxy:    active (listening on localhost:8080)
 Runner:   claude-proxy (installed to /usr/local/bin)
 Health:   curl http://localhost:8080/health
 [Linux:   Logs: journalctl -u proxy-anthropic -f]
 [macOS:   Logs: tail -f /tmp/proxy-anthropic.log]
 Config:   [PROJECT_DIR]/configs/proxy.yaml
 Memory:   [active / not configured]
           proxy-memory CLI at bin/proxy-memory
           [if memory enabled: Mem0 API at localhost:9091, UI at localhost:3000]
==========================================
 Settings cleaned: removed old proxy env vars from ~/.claude/settings.json
 Restart Claude Code for changes to take effect.
==========================================
```

Show the correct log command based on OS.
