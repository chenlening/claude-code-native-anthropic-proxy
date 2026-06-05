# Install Remote Client: anthropic-transparent-proxy

Set up this machine as a client that connects to a remote proxy via direct HTTP. No Go, no build, no local service, no SSH tunnel — just the wrapper.

---

## Phase 1: Gather Connection Details

**Step 1.1: Auto-detect and ask for proxy URL**

Auto-detect local IPs:

```bash
if command -v ip &>/dev/null; then
    ip -4 addr show 2>/dev/null | grep -oP 'inet \K[\d.]+' | grep -v '127.0.0.1' | head -5
elif command -v ifconfig &>/dev/null; then
    ifconfig 2>/dev/null | grep 'inet ' | grep -v '127.0.0.1' | awk '{print $2}' | head -5
fi
```

Also check if a proxy is already running locally:

```bash
curl -sf --max-time 2 http://localhost:8080/health && echo "FOUND" || echo "NOT_FOUND"
```

Ask the user:

1. Show detected IPs if any were found.
2. "Proxy URL?" (default: http://localhost:8080 if a local proxy was detected, otherwise http://localhost:8080)

The URL should include protocol and port, e.g. `http://192.168.1.100:8080`.

Store as `PROXY_URL`. Set `API_KEY="proxy"`.

---

## Phase 2: Install Dependencies

**Step 2.1: Check for curl and jq**

Run: `uname -s`

- If "Linux", install with apt: `sudo apt-get update -qq && sudo apt-get install -y -qq curl jq`
- If "Darwin", curl should be present; install jq with brew if missing: `brew install jq`
- Otherwise, report error: "Unsupported OS." and stop.

Verify: `curl --version && jq --version`

---

## Phase 3: Verify Proxy Connectivity

**Step 3.1: Health check**

```bash
curl -sf --max-time 10 ${PROXY_URL}/health || echo FAIL
```

- If "FAIL", report: "Proxy not responding at ${PROXY_URL}. Check that the proxy service is running and the URL is correct." Stop.
- If JSON response, continue.

**Step 3.2: Verify models endpoint**

```bash
curl -sf ${PROXY_URL}/v1/models | jq -r '.data[].id' | head -5
```

Expected: list of available model IDs.

---

## Phase 4: Install Wrapper

**Step 4.1: Write the wrapper with embedded config**

Read the template file `claude-proxy-remote` from the project repo. Then create `/usr/local/bin/claude-proxy-remote` with the placeholders replaced by the actual values:

Replace in the template:
- `PLACEHOLDER_PROXY_URL` → `${PROXY_URL}`
- `PLACEHOLDER_API_KEY` → `${API_KEY}`

```bash
PROJECT_DIR=$(git rev-parse --show-toplevel)
cp "${PROJECT_DIR}/claude-proxy-remote" /tmp/claude-proxy-remote.tmp

sed -i "s|PLACEHOLDER_PROXY_URL|${PROXY_URL}|g" /tmp/claude-proxy-remote.tmp
sed -i "s|PLACEHOLDER_API_KEY|${API_KEY}|g" /tmp/claude-proxy-remote.tmp

sudo cp /tmp/claude-proxy-remote.tmp /usr/local/bin/claude-proxy-remote
sudo chmod +x /usr/local/bin/claude-proxy-remote
rm /tmp/claude-proxy-remote.tmp
```

**Step 4.2: Verify wrapper is on PATH**

Run: `which claude-proxy-remote`

Expected: `/usr/local/bin/claude-proxy-remote`

---

## Phase 5: Clean Up Settings

**Step 5.1: Check for existing settings.json**

Run: `ls -la ~/.claude/settings.json 2>/dev/null || echo "not found"`

- If "not found", skip to Phase 6.
- If exists, continue.

**Step 5.2: Backup settings.json**

```bash
cp ~/.claude/settings.json ~/.claude/settings.json.backup.$(date +%Y%m%d%H%M%S)
```

**Step 5.3: Strip old proxy env vars**

Read `~/.claude/settings.json`. Rewrite it removing these keys from the `env` object if present:
- `ANTHROPIC_AUTH_TOKEN`
- `ANTHROPIC_BASE_URL`
- `ANTHROPIC_DEFAULT_HAIKU_MODEL`
- `ANTHROPIC_DEFAULT_SONNET_MODEL`
- `ANTHROPIC_DEFAULT_OPUS_MODEL`
- `ANTHROPIC_MODEL`
- `ANTHROPIC_SMALL_FAST_MODEL`
- `CLAUDE_CODE_SUBAGENT_MODEL`

Preserve all other keys unchanged.

Report: "Removed old proxy env vars from ~/.claude/settings.json."

---

## Phase 6: Verify and Summarize

**Step 6.1: Run health check through wrapper**

```bash
claude-proxy-remote --health-check
```

Expected: JSON health response from remote proxy.

**Step 6.2: Print completion summary**

Report:

```
==========================================
 Remote client install complete!
==========================================
 IMPORTANT: Use 'claude-proxy-remote' instead of 'claude' from now on.

 Proxy:     ${PROXY_URL}
 Runner:    claude-proxy-remote (installed to /usr/local/bin)
 Health:    claude-proxy-remote --health-check
 Settings:  cleaned (old proxy env vars removed)
==========================================
 Restart Claude Code for changes to take effect.
==========================================
```
