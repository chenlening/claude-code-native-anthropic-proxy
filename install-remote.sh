#!/usr/bin/env bash
# install-remote.sh — one-command remote client installer for anthropic-transparent-proxy
#
# On any client machine (one-command install):
#   git clone --depth 1 git@github.com:2012geek/anthropic-transparent-proxy.git /tmp/proxy-install && bash /tmp/proxy-install/install-remote.sh && rm -rf /tmp/proxy-install
#
# Or if you already have the repo cloned:
#   bash install-remote.sh
#
# Non-interactive (explicit URL override):
#   PROXY_URL=http://custom:8080 bash install-remote.sh
set -euo pipefail

# ── Colors ──────────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m'

log_info()  { echo -e "${GREEN}==>${NC} ${BOLD}$1${NC}"; }
log_step()  { echo -e "${GREEN}  ✓${NC} $1"; }
log_warn()  { echo -e "${YELLOW}  ⚠${NC} $1"; }
log_error() { echo -e "${RED}  ✗${NC} $1" >&2; }
log_fatal() { echo -e "${RED}ERROR:${NC} $1" >&2; exit 1; }

echo ""
echo -e "${BOLD}=== Anthropic Transparent Proxy — Remote Client Installer ===${NC}"
echo ""

# ── Hardcoded proxy IPs ───────────────────────────────────────────────────
# The script auto-probes each URL below and uses the first reachable one.
# PROXY_URL env var overrides all of these. Update IPs to match your proxy.
PROXY_CANDIDATES=(
    "http://192.168.136.124:8080"
    "http://100.125.153.20:8080"
    "http://100.114.30.115:8080"
)
API_KEY="${API_KEY:-proxy}"

# ── Phase 0: Ensure curl is available (needed for probing) ───────────────
OS="$(uname -s)"
if ! command -v curl &>/dev/null; then
    log_info "Installing curl..."
    case "$OS" in
        Linux)
            sudo apt-get update -qq
            sudo apt-get install -y -qq curl
            ;;
        Darwin)
            log_fatal "curl not found. macOS should have curl pre-installed."
            ;;
        *)
            log_fatal "Unsupported OS: ${OS}"
            ;;
    esac
fi

# ── Phase 1: Connection details ────────────────────────────────────────────
log_info "Phase 1: Connection details"

INTERACTIVE=false
[[ -t 0 ]] && INTERACTIVE=true

PROXY_URL="${PROXY_URL:-}"

probe_proxy() {
    echo "  Probing proxy candidates..." >&2
    for url in "${PROXY_CANDIDATES[@]}"; do
        printf "    %-35s" "${url} ... " >&2
        if curl -sf --noproxy '*' --max-time 3 "${url}/health" >/dev/null 2>&1; then
            echo -e "${GREEN}OK${NC}" >&2
            echo "$url"
            return 0
        else
            echo -e "${RED}no response${NC}" >&2
        fi
    done
    return 1
}

if [[ -n "$PROXY_URL" ]]; then
    # Explicit override via env var — use directly
    PROXY_URL="${PROXY_URL%/}"
    log_step "Using PROXY_URL from environment: ${PROXY_URL}"
else
    # Auto-probe hardcoded candidates
    FOUND=$(probe_proxy) && true
    if [[ -n "$FOUND" ]]; then
        PROXY_URL="$FOUND"
        log_step "Auto-detected proxy: ${PROXY_URL}"
    elif $INTERACTIVE; then
        log_warn "No proxy found at hardcoded IPs. Enter the proxy URL manually."
        echo ""
        read -rp "  Proxy URL: " INPUT
        PROXY_URL="${INPUT%/}"
    fi
fi

if [[ -z "$PROXY_URL" ]]; then
    log_fatal "No proxy reachable. Check that the proxy is running and network is accessible."
fi

log_step "Proxy: ${PROXY_URL}"
echo ""

# ── Phase 2: Install dependencies (jq) ────────────────────────────────────
log_info "Phase 2: Install dependencies (jq)"

# Install jq if missing
if ! command -v jq &>/dev/null; then
    log_step "Installing jq..."
    case "$OS" in
        Linux)
            sudo apt-get update -qq
            sudo apt-get install -y -qq jq
            ;;
        Darwin)
            if ! command -v brew &>/dev/null; then
                log_fatal "Homebrew not found. Install it from https://brew.sh"
            fi
            brew install jq
            ;;
        *)
            log_fatal "Unsupported OS: ${OS}"
            ;;
    esac
fi
log_step "curl: $(curl --version | head -1)"
log_step "jq: $(jq --version)"
echo ""

# ── Phase 3: Verify proxy connectivity ─────────────────────────────────────
log_info "Phase 3: Verify proxy connectivity"

# Health check
printf "  Health check %s ... " "${PROXY_URL}/health"
HEALTH_OK=false
for i in $(seq 1 10); do
    if curl -sf --noproxy '*' --max-time 5 "${PROXY_URL}/health" >/dev/null 2>&1; then
        echo -e "${GREEN}OK${NC}"
        HEALTH_OK=true
        break
    fi
    if [[ $i -eq 10 ]]; then
        echo -e "${RED}FAILED${NC}"
        log_fatal "Proxy not responding at ${PROXY_URL}. Check that the proxy service is running and the URL is correct."
    fi
    sleep 1
done

# Verify models
MODEL_JSON=$(curl -sf --noproxy '*' "${PROXY_URL}/v1/models" 2>/dev/null || echo '{"data":[]}')
MODEL_COUNT=$(echo "$MODEL_JSON" | jq -r '.data | length' 2>/dev/null || echo "0")
if [[ "$MODEL_COUNT" -gt 0 ]]; then
    log_step "Available models: ${MODEL_COUNT}"
    echo "$MODEL_JSON" | jq -r '.data[].id' 2>/dev/null | sed 's/^/    - /'
else
    log_warn "No models found — proxy may be misconfigured"
fi
echo ""

# ── Phase 4: Install wrapper ───────────────────────────────────────────────
log_info "Phase 4: Install claude-proxy-remote wrapper"

sudo mkdir -p /usr/local/bin

sudo tee /usr/local/bin/claude-proxy-remote > /dev/null <<'WRAPPER_EOF'
#!/bin/bash
# Claude Code remote wrapper — connects to proxy via direct HTTP
# Installed by install-remote.sh — regenerate to update connection config.

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

PROXY_URL="PLACEHOLDER_PROXY_URL"
API_KEY="PLACEHOLDER_API_KEY"

TEMP_SETTINGS="/tmp/claude-proxy-remote-settings-$$.json"

cleanup() {
    if [[ -f "$TEMP_SETTINGS" ]]; then
        rm "$TEMP_SETTINGS"
    fi
}
trap cleanup EXIT

log_info()  { echo -e "${GREEN}✓${NC} $1"; }
log_error() { echo -e "${RED}✗${NC} $1" >&2; }
log_warn()  { echo -e "${YELLOW}⚠${NC} $1"; }

# ── Health check ────────────────────────────────────────────────────────

check_health() {
    if curl -sf --noproxy '*' --max-time 5 "${PROXY_URL}/health" &>/dev/null; then
        log_info "Proxy: responding (${PROXY_URL}/health)"
        return
    fi
    log_error "Proxy not responding at ${PROXY_URL}"
    echo ""
    echo "Troubleshooting:"
    echo "  1. Is the proxy running? Check with: curl ${PROXY_URL}/health"
    echo "  2. Can you reach the server? ping <proxy-ip>"
    echo "  3. Is the port open? nc -zv <proxy-ip> <port>"
    exit 1
}

# ── Model selection ─────────────────────────────────────────────────────

LAST_MODEL_FILE="$HOME/.claude-proxy-remote-last"

_is_small_model() {
    case "$1" in
        *flash*|*turbo*|*highspeed*|*air*|*lite*|*mini*|*haiku*) return 0 ;;
        *) return 1 ;;
    esac
}

select_models() {
    local models_json
    models_json=$(curl -s --noproxy '*' "${PROXY_URL}/v1/models" 2>/dev/null)

    if [[ -z "$models_json" ]] || ! echo "$models_json" | jq -e '.data' >/dev/null 2>&1; then
        log_error "Failed to fetch models from proxy"
        exit 1
    fi

    local model_ids
    model_ids=($(echo "$models_json" | jq -r '.data[].id'))

    if [[ ${#model_ids[@]} -eq 0 ]]; then
        log_error "No models available"
        exit 1
    fi

    local last_large=""
    local last_small=""
    if [[ -f "$LAST_MODEL_FILE" ]]; then
        last_large=$(sed -n '1p' "$LAST_MODEL_FILE")
        last_small=$(sed -n '2p' "$LAST_MODEL_FILE")
    fi

    _tag() {
        if _is_small_model "$1"; then echo "small"; else echo "large"; fi
    }

    _show_menu() {
        local cat="$1" label="$2" last_val="$3"
        local idx=1 recommended=1

        echo "" >/dev/tty
        echo "Select ${label}:" >/dev/tty
        echo "" >/dev/tty

        for m in "${model_ids[@]}"; do
            local tag="$(_tag "$m")"
            local mark=""
            if [[ "$m" == "$last_val" ]]; then
                mark=" (default)"
                recommended=$idx
            elif [[ "$tag" == "$cat" && $recommended -eq 1 ]]; then
                recommended=$idx
            fi
            printf "  %2d) %s (%s)%s\n" "$idx" "$m" "$tag" "$mark" >/dev/tty
            ((idx++))
        done

        echo "$recommended"
    }

    _choose() {
        local v
        read -r v </dev/tty
        echo "$v"
    }

    local default_large
    default_large=$(_show_menu large "large model (Opus / Sonnet)" "$last_large")

    echo ""
    printf "  Enter number [%s]: " "$default_large"
    local large_choice
    large_choice=$(_choose)
    large_choice="${large_choice:-$default_large}"

    if [[ "$large_choice" =~ ^[0-9]+$ ]] && (( large_choice >= 1 && large_choice <= ${#model_ids[@]} )); then
        SELECTED_LARGE="${model_ids[$((large_choice-1))]}"
    else
        SELECTED_LARGE="${model_ids[$((default_large-1))]}"
    fi

    local default_small
    default_small=$(_show_menu small "small model (Haiku)" "$last_small")

    echo ""
    printf "  Enter number [%s]: " "$default_small"
    local small_choice
    small_choice=$(_choose)
    small_choice="${small_choice:-$default_small}"

    if [[ "$small_choice" =~ ^[0-9]+$ ]] && (( small_choice >= 1 && small_choice <= ${#model_ids[@]} )); then
        SELECTED_SMALL="${model_ids[$((small_choice-1))]}"
    else
        SELECTED_SMALL="${model_ids[$((default_small-1))]}"
    fi

    echo ""
    log_info "Large: $SELECTED_LARGE  |  Small: $SELECTED_SMALL"
    echo ""

    printf '%s\n%s\n' "$SELECTED_LARGE" "$SELECTED_SMALL" > "$LAST_MODEL_FILE"
}

# ── Settings ────────────────────────────────────────────────────────────

create_temp_settings() {
    cat > "$TEMP_SETTINGS" <<EOF
{
  "env": {
    "ANTHROPIC_BASE_URL": "$PROXY_URL",
    "ANTHROPIC_AUTH_TOKEN": "$API_KEY",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "$SELECTED_LARGE",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "$SELECTED_LARGE",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "$SELECTED_SMALL",
    "NO_PROXY": "*",
    "no_proxy": "*",
    "HTTP_PROXY": "",
    "HTTPS_PROXY": "",
    "http_proxy": "",
    "https_proxy": ""
  },
  "model": "sonnet"
}
EOF
}

# ── CLI checks ──────────────────────────────────────────────────────────

check_claude_cli() {
    if ! command -v claude &>/dev/null; then
        log_error "Claude Code CLI not found"
        echo ""
        echo "Install from: https://claude.ai/code"
        exit 1
    fi
}

check_jq() {
    if ! command -v jq &>/dev/null; then
        log_error "jq not found (required for model selection)"
        echo ""
        echo "Install with:"
        if [[ "$(uname -s)" == "Darwin" ]]; then
            echo "  brew install jq"
        else
            echo "  sudo apt-get install -y jq"
        fi
        exit 1
    fi
}

# ── CLI ──────────────────────────────────────────────────────────────────

case "$1" in
    --health-check)
        check_health
        curl -sf --noproxy '*' "${PROXY_URL}/health" | jq . 2>/dev/null || \
            curl -sf --noproxy '*' "${PROXY_URL}/health"
        exit 0
        ;;
    --show-config)
        echo "Remote proxy client configuration:"
        echo "  Proxy URL: ${PROXY_URL}"
        echo "  API key:   ${API_KEY}"
        exit 0
        ;;
    --help|-h)
        echo "Usage: claude-proxy-remote [claude_options]"
        echo ""
        echo "Wrapper to launch Claude Code via remote proxy (direct HTTP)."
        echo ""
        echo "Options:"
        echo "  --health-check    Check proxy health"
        echo "  --show-config     Display connection configuration"
        echo "  --help, -h        Show this help"
        echo ""
        echo "Any other options are passed to claude."
        exit 0
        ;;
esac

# ── Main ─────────────────────────────────────────────────────────────────

main() {
    check_claude_cli
    check_jq
    check_health
    select_models
    create_temp_settings

    exec claude --settings "$TEMP_SETTINGS" --model sonnet "$@"
}

main "$@"
WRAPPER_EOF

# Replace placeholders with actual values
if [[ "$(uname -s)" == "Darwin" ]]; then
    sudo sed -i '' "s|PLACEHOLDER_PROXY_URL|${PROXY_URL}|g" /usr/local/bin/claude-proxy-remote
    sudo sed -i '' "s|PLACEHOLDER_API_KEY|${API_KEY}|g" /usr/local/bin/claude-proxy-remote
else
    sudo sed -i "s|PLACEHOLDER_PROXY_URL|${PROXY_URL}|g" /usr/local/bin/claude-proxy-remote
    sudo sed -i "s|PLACEHOLDER_API_KEY|${API_KEY}|g" /usr/local/bin/claude-proxy-remote
fi

sudo chmod +x /usr/local/bin/claude-proxy-remote

if command -v claude-proxy-remote &>/dev/null; then
    log_step "Wrapper installed: $(which claude-proxy-remote)"
else
    log_warn "Wrapper installed but not on current PATH. Location: /usr/local/bin/claude-proxy-remote"
fi
echo ""

# ── Phase 5: Cleanup settings.json ─────────────────────────────────────────
log_info "Phase 5: Clean up ~/.claude/settings.json"

SETTINGS_FILE="$HOME/.claude/settings.json"

if [[ -f "$SETTINGS_FILE" ]]; then
    BACKUP="${SETTINGS_FILE}.backup.$(date +%Y%m%d%H%M%S)"
    cp "$SETTINGS_FILE" "$BACKUP"
    log_step "Backed up: ${BACKUP}"

    TMP_SETTINGS="${SETTINGS_FILE}.tmp"
    jq 'del(.env.ANTHROPIC_AUTH_TOKEN,
            .env.ANTHROPIC_BASE_URL,
            .env.ANTHROPIC_DEFAULT_HAIKU_MODEL,
            .env.ANTHROPIC_DEFAULT_SONNET_MODEL,
            .env.ANTHROPIC_DEFAULT_OPUS_MODEL,
            .env.ANTHROPIC_MODEL,
            .env.ANTHROPIC_SMALL_FAST_MODEL,
            .env.CLAUDE_CODE_SUBAGENT_MODEL)' \
        "$SETTINGS_FILE" > "$TMP_SETTINGS" && mv "$TMP_SETTINGS" "$SETTINGS_FILE"
    log_step "Removed old proxy env vars from settings.json"
else
    log_step "No settings.json found — skipping cleanup"
fi
echo ""

# ── Phase 6: Verify ──────────────────────────────────────────────────────────
log_info "Phase 6: Verifying installation"

if claude-proxy-remote --health-check 2>/dev/null; then
    log_step "End-to-end health check: OK"
else
    log_warn "Health check through wrapper failed — check proxy URL and connectivity"
fi
echo ""

# ── Done ────────────────────────────────────────────────────────────────────
echo -e "${GREEN}==========================================${NC}"
echo -e "${GREEN}  Remote client install complete!${NC}"
echo -e "${GREEN}==========================================${NC}"
echo ""
echo -e "  ${BOLD}IMPORTANT:${NC} Use '${BOLD}claude-proxy-remote${NC}' instead of 'claude' from now on."
echo ""
echo "  Proxy:     ${PROXY_URL}"
echo "  Runner:    claude-proxy-remote (installed to /usr/local/bin)"
echo "  Health:    claude-proxy-remote --health-check"
echo "  Settings:  cleaned (old proxy env vars removed)"
echo ""
echo -e "  ${BOLD}Restart Claude Code for changes to take effect.${NC}"
echo ""
