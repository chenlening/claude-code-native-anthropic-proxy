# Install: anthropic-transparent-proxy

Install the anthropic-transparent-proxy on this machine. Follow each phase in order.

---

## Phase 1: Preflight

**Step 1.1: Check OS**

Run: `grep -qi 'ubuntu\|debian' /etc/os-release && echo "OK" || echo "FAIL"`

- If output is "OK", continue to Step 1.2.
- If output is "FAIL", report error: "This install command requires Ubuntu or Debian." and stop.

**Step 1.2: Check sudo availability**

Run: `command -v sudo && sudo -n true 2>/dev/null && echo "OK" || echo "WARN"`

- If output is "OK", continue.
- If output is "WARN", inform the user: "Password may be required for sudo operations." and continue.

**Step 1.3: Check existing service status**

Run: `systemctl is-active proxy-anthropic 2>/dev/null || echo "inactive"`

- If output is "active", inform the user: "Proxy service is already running. This install will restart with the latest build."
- If output is "inactive", continue.

---

## Phase 2: Go

**Step 2.1: Check existing Go version**

Run: `go version 2>/dev/null || echo "not found"`

- If output is "not found", proceed to Step 2.2.
- If Go version is 1.23 or higher, continue to Phase 3.
- If Go version is below 1.23, proceed to Step 2.2.

**Step 2.2: Install Go 1.23.7**

Run each command in sequence:

```bash
cd /tmp
curl -LO https://go.dev/dl/go1.23.7.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.23.7.linux-amd64.tar.gz
rm go1.23.7.linux-amd64.tar.gz
```

Add to PATH in shell profile:
Run: `grep -q 'export PATH=/usr/local/go/bin' ~/.bashrc || echo 'export PATH=/usr/local/go/bin:$PATH' >> ~/.bashrc`

Verify: `go version`

Expected output should contain "go1.23.7". Continue to Phase 3.

---

## Phase 3: Build

**Step 3.1: Build the proxy binary**

Run from the project root directory:

```bash
go build -o bin/proxy ./cmd/proxy
```

Verify: `ls -lh bin/proxy`

Expected: a binary file exists. Report its size (e.g., "Build complete: 12M").

---

## Phase 4: Systemd

**Step 4.1: Write the systemd service file**

Determine the current working directory of this project. It is the directory containing `.claude/commands/install.md`.

Run the following commands to write the service file (the unquoted EOF allows shell variable expansion):

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

**Step 4.2: Reload systemd and enable the service**

Run each command in sequence:

```bash
sudo systemctl daemon-reload
sudo systemctl enable proxy-anthropic
```

**Step 4.3: Start (or restart) the service**

Run: `systemctl is-active proxy-anthropic 2>/dev/null || echo "inactive"`

- If active, run: `sudo systemctl restart proxy-anthropic`
- If inactive, run: `sudo systemctl start proxy-anthropic`

**Step 4.4: Wait for service health**

Poll the health endpoint up to 15 times with 1-second intervals:

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

- If output is "READY", inform: "Service is ready."
- If output is "FAILED", report error: "Service failed to start within 15 seconds. Check logs: journalctl -u proxy-anthropic -n 20" and stop.

---

## Phase 5: Settings.json

**Step 5.1: Read proxy.yaml to find frontend model names**

Read the project's `configs/proxy.yaml` file.

From the `models:` section, extract:
- The key containing "haiku" (frontend model name)
- The key containing "sonnet" (frontend model name)
- The key containing "opus" (frontend model name)

Expected values:
- haiku: `claude-haiku-3-5-20241022`
- sonnet: `claude-sonnet-4-20250514`
- opus: `claude-opus-4-20250514`

**Step 5.2: Check for existing settings.json**

Check if `~/.claude/settings.json` exists.

Run: `ls -la ~/.claude/settings.json 2>/dev/null || echo "not found"`

- If "not found", proceed to Step 5.3 (create new file).
- If file exists, proceed to Step 5.4 (backup and merge).

**Step 5.3: Create new settings.json**

If `~/.claude/settings.json` does not exist, first ensure the directory exists:

Run: `mkdir -p ~/.claude`

Then use the Write tool to create `~/.claude/settings.json`. Use the model names you extracted from proxy.yaml in Step 5.1. The content should follow this pattern (substitute the actual model names):

```json
{
  "env": {
    "ANTHROPIC_AUTH_TOKEN": "anykey",
    "ANTHROPIC_BASE_URL": "http://localhost:8080",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "[haiku model name from Step 5.1]",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "[sonnet model name from Step 5.1]",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "[opus model name from Step 5.1]"
  }
}
```

**Important**: `ANTHROPIC_AUTH_TOKEN` must be set to `"anykey"` (or any non-empty value) for Claude Code to start. The proxy handles the actual auth, so this is just a placeholder.

Continue to Phase 6.

**Step 5.4: Backup existing settings.json**

Run: `cp ~/.claude/settings.json ~/.claude/settings.json.backup.$(date +%Y%m%d%H%M%S)`

Report: "Backed up settings.json to ~/.claude/settings.json.backup.[timestamp]"

**Step 5.5: Read and merge settings.json**

Read `~/.claude/settings.json`.

**Step 5.6: Update env vars in settings.json**

Use the Write tool to rewrite `~/.claude/settings.json` with a clean, merged `env` object.

- The new `env` object must contain:
  - `ANTHROPIC_AUTH_TOKEN` set to `"anykey"` (required for Claude Code to start)
  - `ANTHROPIC_BASE_URL` set to `"http://localhost:8080"`
  - `ANTHROPIC_DEFAULT_HAIKU_MODEL` set to the haiku model name from Step 5.1
  - `ANTHROPIC_DEFAULT_SONNET_MODEL` set to the sonnet model name from Step 5.1
  - `ANTHROPIC_DEFAULT_OPUS_MODEL` set to the opus model name from Step 5.1
- All **other existing keys** outside of `env` must be preserved unchanged (`permissions`, `enabledPlugins`, etc.).
- Any keys **inside `env`** that conflict with the new proxy settings (e.g., old `ANTHROPIC_BASE_URL` pointing to a different URL, or old `ANTHROPIC_DEFAULT_*` model values) must be **removed** — do not leave duplicate keys in the file.

Run: `cat ~/.claude/settings.json` to verify the file has exactly one occurrence of each key and no duplicate `env` keys.

Report: "Claude Code settings updated with ANTHROPIC_BASE_URL, ANTHROPIC_DEFAULT_HAIKU_MODEL, ANTHROPIC_DEFAULT_SONNET_MODEL, and ANTHROPIC_DEFAULT_OPUS_MODEL."

---

## Phase 6: Verify

**Step 6.1: Check health endpoint**

Run: `curl -sf http://localhost:8080/health`

- If successful, report the JSON response briefly (e.g., endpoint count, overall status).
- If failed, report error: "Health check failed. Check logs: journalctl -u proxy-anthropic -n 50"

---

## Completion

Report a summary:

```
==========================================
 Installation complete!
==========================================
 Proxy service: active ([N] endpoints)
 Listening on:  localhost:8080
 Health check:  curl http://localhost:8080/health
 Logs:          journalctl -u proxy-anthropic -f
 Config file:   [PROJECT_DIR]/configs/proxy.yaml
 Claude Code:   configured to use proxy
==========================================
 Restart Claude Code for changes to take effect.
==========================================
```
