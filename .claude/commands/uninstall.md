# Uninstall: anthropic-transparent-proxy

Remove the anthropic-transparent-proxy from this machine. Follow each phase in order.

---

## Phase 1: OS Detection

**Step 1.1: Check OS**

Run: `uname -s`

- If output is `Linux`, continue to Step 1.2.
- If output is `Darwin`, continue to Step 1.3.
- If anything else, report error: "Unsupported OS. This uninstall supports Linux and macOS." and stop.

**Step 1.2: Linux — verify systemd**

Run: `command -v systemctl && echo "OK" || echo "FAIL"`

- If "OK", continue to Phase 2 (Linux Branch).
- If "FAIL", report error: "systemctl not found. This uninstall requires systemd on Linux." and stop.

**Step 1.3: macOS — continue**

Continue to Phase 2 (macOS Branch).

---

## Phase 2: Stop and Disable Service

### Linux Branch: systemd

**Step 2.L1: Check service status**

Run: `systemctl is-active proxy-anthropic 2>/dev/null || echo "inactive"`

Report the current status.

**Step 2.L2: Stop the service**

Run: `sudo systemctl stop proxy-anthropic 2>/dev/null || echo "already stopped"`

**Step 2.L3: Disable the service**

Run: `sudo systemctl disable proxy-anthropic 2>/dev/null || echo "already disabled"`

Now continue to Phase 3 (Linux Branch).

### macOS Branch: launchd

**Step 2.M1: Check service status**

Run: `launchctl list | grep com.anthropic.proxy || echo "not found"`

Report the current status.

**Step 2.M2: Unload the service**

Run: `launchctl unload ~/Library/LaunchAgents/com.anthropic.proxy.plist 2>/dev/null || echo "already unloaded"`

**Step 2.M3: Kill any running proxy processes**

Run: `pkill -f "./bin/proxy" 2>/dev/null || echo "no running proxy"`

Now continue to Phase 3 (macOS Branch).

---

## Phase 3: Remove Service Files

### Linux Branch: systemd

**Step 3.L1: Remove service file**

Run: `sudo rm -f /etc/systemd/system/proxy-anthropic.service && echo "removed" || echo "not found"`

**Step 3.L2: Reload systemd**

Run: `sudo systemctl daemon-reload`

Now continue to Phase 4.

### macOS Branch: launchd

**Step 3.M1: Remove plist file**

Run: `rm -f ~/Library/LaunchAgents/com.anthropic.proxy.plist && echo "removed" || echo "not found"`

Now continue to Phase 4.

---

## Phase 4: Restore settings.json

**Step 4.1: Check for backup files**

Run: `ls -t ~/.claude/settings.json.backup.* 2>/dev/null | head -1 || echo "no backups"`

- If "no backups", skip to Phase 5.
- If a backup file is listed, continue to Step 4.2.

**Step 4.2: Restore most recent backup**

Find the latest backup from Step 4.1 and run:

```bash
cp <latest_backup_path> ~/.claude/settings.json && echo "restored" || echo "failed"
```

Report: "Restored ~/.claude/settings.json from <backup_filename>."

**Step 4.3: Verify restore**

Run: `cat ~/.claude/settings.json`

Quickly verify the file is valid JSON (non-empty, has expected keys). Report any issues.

---

## Phase 5: Remove claude-proxy wrapper

**Step 5.1: Check if wrapper exists**

Run: `ls -la /usr/local/bin/claude-proxy 2>/dev/null || echo "not found"`

- If "not found", skip to Phase 6.
- If exists, continue to Step 5.2.

**Step 5.2: Remove wrapper**

Run: `sudo rm -f /usr/local/bin/claude-proxy && echo "removed" || echo "failed"`

If "removed", report: "Removed /usr/local/bin/claude-proxy."

---

## Phase 6: Remove Project Directory (Optional)

**Step 6.1: Locate project directory**

Run: `git rev-parse --show-toplevel 2>/dev/null || echo "not a git repo"`

- If "not a git repo", skip to Phase 7.
- If a path is returned, that is `PROJECT_DIR`. Continue to Step 6.2.

**Step 6.2: Ask user**

Ask the user: "Remove the project directory (PROJECT_DIR) and all its contents? This will delete the binary and config."

- If user says yes, run: `rm -rf PROJECT_DIR && echo "removed" || echo "failed"`
- If user says no, report: "Keeping PROJECT_DIR. You can remove it manually later."

---

## Phase 7: Summary

Report:

```
==========================================
 Uninstall complete!
==========================================
 Service:   stopped and removed
 Settings:  [restored from backup / unchanged]
 Wrapper:   [removed / not installed]
 Project:   [removed / kept at PROJECT_DIR]
==========================================
 Restart Claude Code for changes to take effect.
==========================================
```

Also report this warning:

```
Note: API keys in proxy.yaml were committed to the repo.
If you shared this repo, consider revoking them.
```
