#!/usr/bin/env bash
# uninstall.sh — Remove anthropic-transparent-proxy installation
# Usage: ./scripts/uninstall.sh
set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}[uninstall]${NC} $*"; }
warn()  { echo -e "${YELLOW}[uninstall]${NC} $*"; }
error() { echo -e "${RED}[uninstall]${NC} $*"; }

INSTALL_DIR="${HOME}/anthropic-transparent-proxy"
SERVICE_FILE="/etc/systemd/system/proxy-anthropic.service"

info "Uninstalling proxy..."

# Step 1: Stop and disable service
if systemctl is-active proxy-anthropic &>/dev/null; then
    info "Stopping proxy service..."
    sudo systemctl stop proxy-anthropic
fi
sudo systemctl disable proxy-anthropic 2>/dev/null || true

# Step 2: Remove service file
if [[ -f "${SERVICE_FILE}" ]]; then
    info "Removing service file..."
    sudo rm -f "${SERVICE_FILE}"
    sudo systemctl daemon-reload
fi

# Step 3: Restore Claude settings from most recent backup
CLAUDE_DIR="${HOME}/.claude"
if [[ -d "${CLAUDE_DIR}" ]]; then
    latest_backup=$(ls -t "${CLAUDE_DIR}"/settings.json.backup.* 2>/dev/null | head -1)
    if [[ -n "${latest_backup}" ]]; then
        info "Restoring Claude settings from ${latest_backup}"
        cp "${latest_backup}" "${CLAUDE_DIR}/settings.json"
    else
        warn "No backup of Claude settings found. You may need to reconfigure manually."
    fi
fi

# Step 4: Optionally remove the installation directory
if [[ -d "${INSTALL_DIR}" ]]; then
    read -rp "Remove installation directory (${INSTALL_DIR})? [y/N] " confirm
    if [[ "${confirm}" =~ ^[yY] ]]; then
        info "Removing ${INSTALL_DIR}..."
        rm -rf "${INSTALL_DIR}"
    else
        info "Keeping ${INSTALL_DIR}. You can remove it manually."
    fi
fi

info "=========================================="
info " Uninstall complete!"
info "=========================================="
echo ""
warn "Note: API keys in proxy.yaml were committed to the repo."
warn "If you shared this repo, consider revoking them."