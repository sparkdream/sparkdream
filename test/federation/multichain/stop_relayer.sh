#!/bin/bash
# ------------------------------------------------------------------
# Stop only the Hermes IBC relayer (leave chains running).
# Used by Task 2.5 (silent SendPacket regression).
# ------------------------------------------------------------------

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

if [ -f "$SCRIPT_DIR/.hermes_pid" ]; then
    PID=$(cat "$SCRIPT_DIR/.hermes_pid")
    if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then
        kill "$PID" 2>/dev/null || true
        # Wait for graceful exit
        for i in $(seq 1 5); do
            kill -0 "$PID" 2>/dev/null || break
            sleep 1
        done
        kill -9 "$PID" 2>/dev/null || true
    fi
    rm -f "$SCRIPT_DIR/.hermes_pid"
fi

# Best-effort cleanup of any other hermes processes pointing at our config
pkill -f "hermes.*hermes_config.toml" 2>/dev/null || true
sleep 1
pkill -9 -f "hermes.*hermes_config.toml" 2>/dev/null || true

echo "  Hermes relayer stopped."
