#!/bin/sh
# Always restore mem0 DB from latest R2 snapshot, then continuously replicate.
# This ensures the DB matches R2 (the canonical source) on every restart.
set -e

LITESTREAM_VERSION="0.5.11"
LITESTREAM_BIN="/usr/local/bin/litestream"
DB_PATH="/data/openmemory.db"
CONFIG="/etc/litestream.yml"

# Healthcheck mode — just verify litestream is running
if [ "${1:-}" = "healthcheck" ]; then
    pgrep litestream >/dev/null && exit 0 || exit 1
fi

# Install litestream if not present
if [ ! -x "$LITESTREAM_BIN" ]; then
    echo "[litestream] downloading litestream v${LITESTREAM_VERSION}..."
    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64)  L_ARCH="amd64" ;;
        aarch64) L_ARCH="arm64" ;;
        *)       echo "unsupported arch: $ARCH"; exit 1 ;;
    esac
    wget -q "https://github.com/benbjohnson/litestream/releases/download/v${LITESTREAM_VERSION}/litestream-${LITESTREAM_VERSION}-linux-${L_ARCH}.tar.gz" -O /tmp/litestream.tar.gz
    tar -xzf /tmp/litestream.tar.gz -C /usr/local/bin litestream
    rm /tmp/litestream.tar.gz
    echo "[litestream] installed"
fi

# Ensure DB directory exists and restore from R2
mkdir -p "$(dirname "$DB_PATH")"
# Remove existing DB so restore always pulls the canonical R2 state
rm -f "$DB_PATH" "$DB_PATH"-wal "$DB_PATH"-shm
echo "[litestream] restoring latest snapshot from R2..."
if litestream restore -o "$DB_PATH" "$DB_PATH" 2>&1; then
    echo "[litestream] restored from R2 backup"
else
    echo "[litestream] no backup found, starting fresh"
    touch "$DB_PATH"
fi

echo "[litestream] database ready, starting replication..."
exec litestream replicate
