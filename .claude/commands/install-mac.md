# Install: anthropic-transparent-proxy (macOS)

Install the anthropic-transparent-proxy on this machine using launchd. Follow each phase in order.

---

## Phase 1: Preflight

**Step 1.1: Check OS**

Run: `uname -s`

- If output is "Darwin", continue to Step 1.2.
- If output is anything else, report error: "This install command requires macOS. This is a macOS-only install command — for Linux, use `/install` instead." and stop.

**Step 1.2: Check existing service status**

Run: `launchctl list | grep com.anthropic.proxy || echo "not found"`

- If output contains "com.anthropic.proxy", inform the user: "Proxy service is already installed. This install will rebuild and restart with the latest code."
- If output is "not found", continue.

---

## Phase 2: Go

**Step 2.1: Check existing Go version**

Run: `go version 2>/dev/null || echo "not found"`

- If output is "not found", proceed to Step 2.2.
- If Go version is 1.23 or higher, continue to Phase 3.
- If Go version is below 1.23, proceed to Step 2.2.

**Step 2.2: Install Go 1.23.7 for macOS**

Detect the CPU architecture first:

Run: `uname -m`

- If output is "arm64", set `GO_ARCH=darwin-arm64`
- If output is "x86_64", set `GO_ARCH=darwin-amd64`

Run each command in sequence:

```bash
cd /tmp
curl -LO https://go.dev/dl/go1.23.7.${GO_ARCH}.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.23.7.${GO_ARCH}.tar.gz
rm go1.23.7.${GO_ARCH}.tar.gz
```

Add to PATH in shell profile. First check which shell config to use:

Run: `test -f ~/.zshrc && echo "zshrc" || (test -f ~/.bash_profile && echo "bash_profile" || echo "unknown")`

- If "zshrc", run: `grep -q 'export PATH=/usr/local/go/bin' ~/.zshrc || echo 'export PATH=/usr/local/go/bin:$PATH' >> ~/.zshrc`
- If "bash_profile", run: `grep -q 'export PATH=/usr/local/go/bin' ~/.bash_profile || echo 'export PATH=/usr/local/go/bin:$PATH' >> ~/.bash_profile`
- If "unknown", run: `echo 'export PATH=/usr/local/go/bin:$PATH' >> ~/.zshrc` (create it)

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

## Phase 4: launchd

**Step 4.1: Write the launchd plist file**

Determine the current working directory of this project. It is the directory containing `.claude/commands/install-mac.md`.

Run the following commands to write the plist file (the unquoted EOF allows shell variable expansion):

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

**Step 4.2: Stop existing process and load the service**

Run each command in sequence:

```bash
pkill -f "./bin/proxy" 2>/dev/null || true
launchctl unload ~/Library/LaunchAgents/com.anthropic.proxy.plist 2>/dev/null || true
launchctl load ~/Library/LaunchAgents/com.anthropic.proxy.plist
```

**Step 4.3: Wait for service health**

Poll the health endpoint up to 15 times with 1-second intervals. **Important**: use `--noproxy '*'` to bypass any system proxy settings (macOS users may have `ALL_PROXY` or `HTTP_PROXY` set):

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

- If output is "READY", inform: "Service is ready."
- If output is "FAILED", report error: "Service failed to start within 15 seconds. Check logs: tail -50 /tmp/proxy-anthropic.log" and stop.

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
- Any keys **inside `env`** that conflict with the new proxy settings (e.g., old `ANTHROPIC_BASE_URL` pointing to a different URL, old `ANTHROPIC_MODEL`, `ANTHROPIC_SMALL_FAST_MODEL`, `CLAUDE_CODE_SUBAGENT_MODEL`, or old `ANTHROPIC_DEFAULT_*` model values) must be **removed** — do not leave duplicate keys in the file.

Run: `cat ~/.claude/settings.json` to verify the file has exactly one occurrence of each key and no duplicate `env` keys.

Report: "Claude Code settings updated with ANTHROPIC_BASE_URL, ANTHROPIC_DEFAULT_HAIKU_MODEL, ANTHROPIC_DEFAULT_SONNET_MODEL, and ANTHROPIC_DEFAULT_OPUS_MODEL."

---

## Phase 6: Verify

**Step 6.1: Check health endpoint**

Run: `curl -sf --noproxy '*' http://localhost:8080/health`

- If successful, report the JSON response briefly (e.g., endpoint count, overall status).
- If failed, report error: "Health check failed. Check logs: tail -50 /tmp/proxy-anthropic.log"

---

## Completion

Report a summary:

```
==========================================
 Installation complete!
==========================================
 Proxy service: com.anthropic.proxy (loaded)
 Listening on:  localhost:8080
 Health check:  curl --noproxy '*' http://localhost:8080/health
 Logs:          /tmp/proxy-anthropic.log
 Config file:   [PROJECT_DIR]/configs/proxy.yaml
 Claude Code:   configured to use proxy
==========================================
 Restart Claude Code for changes to take effect.
==========================================
```
