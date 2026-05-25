# Usage: source scripts/backend.sh <name>
# Sources the corresponding configs/backends/<name>.env into the current shell.

_backend_name="${1:?Usage: source scripts/backend.sh <backend>}"
_backend_file="$(dirname "${BASH_SOURCE[0]}")/../configs/backends/${_backend_name}.env"

if [[ ! -f "$_backend_file" ]]; then
    echo "Error: backend '${_backend_name}' not found" >&2
    echo "Available:" >&2
    ls "$(dirname "${BASH_SOURCE[0]}")/../configs/backends/"*.env | sed 's|.*/||;s|\.env$||' | sed 's/^/  /' >&2
    return 1
fi

source "$_backend_file"

echo "Switched to ${_backend_name}: ANTHROPIC_BASE_URL=${ANTHROPIC_BASE_URL}"
echo "  large -> ${ANTHROPIC_DEFAULT_SONNET_MODEL}  |  small -> ${ANTHROPIC_DEFAULT_HAIKU_MODEL}"

unset _backend_name _backend_file
