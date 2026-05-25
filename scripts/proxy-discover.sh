#!/bin/bash
# proxy-discover.sh — Interactive model selection for the transparent proxy.
#
# Queries the proxy /health endpoint, lets the user pick a large model
# and a small model from numbered menus, and outputs export statements.
#
# Usage:
#   eval "$(proxy-discover.sh)"

set -uo pipefail

HEALTH_URL="${PROXY_HEALTH_URL:-http://localhost:8080/health}"
TTY="${PROXY_TTY:-/dev/tty}"

_prompt()  { echo "$@" >"$TTY"; }
_choose()  { local v; read -r v <"$TTY"; echo "$v"; }

# ── fetch models from proxy ───────────────────────────────────────────────────

_health=$(curl -s --max-time 3 "$HEALTH_URL" 2>/dev/null || true)

if [[ -z "$_health" ]]; then
    _prompt "[proxy-discover] proxy unreachable at $HEALTH_URL"
    exit 0
fi

_all_models=$(echo "$_health" | jq -r '
  [.endpoints | to_entries[]? | select(.value.status == "enabled") | .value.supported_models[]?]
  | unique | sort | .[]' 2>/dev/null)

if [[ -z "$_all_models" ]]; then
    _prompt "[proxy-discover] no enabled models found on proxy"
    exit 0
fi

readarray -t MODELS <<< "$_all_models"

# ── classify & tag ────────────────────────────────────────────────────────────

_is_small() { [[ "$1" =~ flash|turbo|highspeed|air$|air-|-air|lite$|mini$ ]]; }

declare -A TAG
for m in "${MODELS[@]}"; do
    if _is_small "$m"; then
        TAG["$m"]="small"
    else
        TAG["$m"]="large"
    fi
done

# ── helpers ───────────────────────────────────────────────────────────────────

_first_of() {
    local cat="$1"
    for m in "${MODELS[@]}"; do
        [[ "${TAG[$m]}" == "$cat" ]] && { echo "$m"; return; }
    done
    echo "${MODELS[0]}"
}

_pick_best_large() {
    local best="${MODELS[0]}"
    for m in "${MODELS[@]}"; do
        [[ "${TAG[$m]}" == "large" && ${#m} -gt ${#best} ]] && best="$m"
    done
    echo "$best"
}

_pick_best_small() {
    for m in "${MODELS[@]}"; do
        [[ "$m" =~ flash ]] && { echo "$m"; return; }
    done
    _first_of small
}

# ── no TTY → auto-pick best and exit ─────────────────────────────────────────

if [[ ! -w "$TTY" ]]; then
    _h="$(_pick_best_large)"
    _l="$(_pick_best_small)"
    cat <<EOF
export ANTHROPIC_MODEL="${_h}"
export ANTHROPIC_DEFAULT_OPUS_MODEL="${_h}"
export ANTHROPIC_DEFAULT_SONNET_MODEL="${_h}"
export ANTHROPIC_DEFAULT_HAIKU_MODEL="${_l}"
EOF
    exit 0
fi

# ── display helper ────────────────────────────────────────────────────────────

_show_menu() {
    local cat="$1" label="$2"
    local idx=1 recommended=1

    _prompt ""
    _prompt "Select ${label}:"
    _prompt ""

    for m in "${MODELS[@]}"; do
        local tag="${TAG[$m]}"
        local hint=" [recommended]" hint2=""
        if [[ "$tag" == "$cat" ]]; then
            hint2="$hint"
            [[ $recommended -eq 1 ]] && recommended="$idx"
        fi
        printf "  %2d) %s (%s)%s\n" "$idx" "$m" "$tag" "$hint2" >"$TTY"
        ((idx++))
    done

    echo "$recommended"
}

# ── step 1: pick large model ─────────────────────────────────────────────────

_default_large=$(_show_menu large "large model (Opus / Sonnet)")

_prompt ""
printf "  Enter number [${_default_large}]: " >"$TTY"
_choice=$(_choose)
_choice="${_choice:-$_default_large}"

if [[ "$_choice" =~ ^[0-9]+$ ]] && (( _choice >= 1 && _choice <= ${#MODELS[@]} )); then
    _selected_large="${MODELS[$((_choice-1))]}"
else
    _selected_large="${MODELS[$((_default_large-1))]}"
fi

# ── step 2: pick small model ─────────────────────────────────────────────────

_default_small=$(_show_menu small "small model (Haiku)")

_prompt ""
printf "  Enter number [${_default_small}]: " >"$TTY"
_choice=$(_choose)
_choice="${_choice:-$_default_small}"

if [[ "$_choice" =~ ^[0-9]+$ ]] && (( _choice >= 1 && _choice <= ${#MODELS[@]} )); then
    _selected_small="${MODELS[$((_choice-1))]}"
else
    _selected_small="${MODELS[$((_default_small-1))]}"
fi

_prompt ""
_prompt "  large: $_selected_large  |  small: $_selected_small"
_prompt ""

cat <<EOF
export ANTHROPIC_MODEL="${_selected_large}"
export ANTHROPIC_DEFAULT_OPUS_MODEL="${_selected_large}"
export ANTHROPIC_DEFAULT_SONNET_MODEL="${_selected_large}"
export ANTHROPIC_DEFAULT_HAIKU_MODEL="${_selected_small}"
EOF
