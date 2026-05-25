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

- If output indicates service is running/installed, inform: "Proxy service is already running. This install will rebuild and restart with the latest code."
- If inactive/not found, continue.

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

**Step 6.1: Copy wrapper to /usr/local/bin**

```bash
PROJECT_DIR=$(git rev-parse --show-toplevel)
sudo cp "${PROJECT_DIR}/claude-proxy" /usr/local/bin/claude-proxy
sudo chmod +x /usr/local/bin/claude-proxy
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
==========================================
 Settings cleaned: removed old proxy env vars from ~/.claude/settings.json
 Restart Claude Code for changes to take effect.
==========================================
```

Show the correct log command based on OS.
