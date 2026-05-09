#!/bin/bash
# ------------------------------------------------------------------
# Start the Hermes IBC relayer in the background.
#
# Required for cross-chain packet delivery — channels alone do nothing.
# Writes the PID to .hermes_pid and the log to data/hermes.log.
#
# Prerequisites: setup_ibc.sh has run (channel exists).
# ------------------------------------------------------------------
set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
HERMES="${HERMES:-hermes}"
HERMES_CONFIG="$SCRIPT_DIR/hermes_config.toml"
HERMES_LOG="$SCRIPT_DIR/data/hermes.log"

if ! command -v "$HERMES" >/dev/null 2>&1; then
    echo "ERROR: hermes not on PATH. Set HERMES=/path/to/hermes."
    exit 1
fi

mkdir -p "$(dirname "$HERMES_LOG")"

# Stop any previous relayer instance
if [ -f "$SCRIPT_DIR/.hermes_pid" ]; then
    OLD_PID=$(cat "$SCRIPT_DIR/.hermes_pid" 2>/dev/null || echo "")
    if [ -n "$OLD_PID" ] && kill -0 "$OLD_PID" 2>/dev/null; then
        echo "Stopping previous hermes (PID $OLD_PID)..."
        kill "$OLD_PID" 2>/dev/null || true
        sleep 1
        kill -9 "$OLD_PID" 2>/dev/null || true
    fi
fi
pkill -9 -f "hermes.*hermes_config.toml" 2>/dev/null || true

# Validate config first; bail early on bad TOML
echo "=== Validating hermes config ==="
if ! "$HERMES" --config "$HERMES_CONFIG" health-check 2>&1 | tail -5; then
    echo "ERROR: hermes health-check failed. Inspect $HERMES_CONFIG and chain RPC reachability."
    exit 1
fi

echo "=== Starting hermes ==="
"$HERMES" --config "$HERMES_CONFIG" start > "$HERMES_LOG" 2>&1 &
HERMES_PID=$!
echo "$HERMES_PID" > "$SCRIPT_DIR/.hermes_pid"

# Wait for the relayer to fully boot. Look for the "Hermes has started"
# marker, falling back to a fixed wait if the marker format changes.
SAW_READY=false
for i in $(seq 1 30); do
    if grep -q "Hermes has started" "$HERMES_LOG" 2>/dev/null; then
        SAW_READY=true
        break
    fi
    if ! kill -0 "$HERMES_PID" 2>/dev/null; then
        echo "ERROR: hermes exited during startup. Tail of log:"
        tail -40 "$HERMES_LOG"
        exit 1
    fi
    sleep 1
done

if [ "$SAW_READY" != "true" ]; then
    echo "WARN: 'Hermes has started' not seen within 30s; continuing (process is alive)."
fi

echo "  Hermes running: PID=$HERMES_PID, log=$HERMES_LOG"
