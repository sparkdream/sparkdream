#!/bin/bash
# ------------------------------------------------------------------
# Stop both chains and the Hermes IBC relayer.
# Safe to call repeatedly. Matches on home dir (sparkdreamd argv has
# --home, not the chain id) and on the hermes config path.
# ------------------------------------------------------------------

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

echo "Stopping multi-chain test environment..."

# Stop chain processes by home-dir match
pkill -f "sparkdreamd.*data/chain-a" 2>/dev/null && echo "  Sent TERM to chain-a" || true
pkill -f "sparkdreamd.*data/chain-b" 2>/dev/null && echo "  Sent TERM to chain-b" || true

# Stop Hermes by config path
pkill -f "hermes.*hermes_config.toml" 2>/dev/null && echo "  Sent TERM to hermes" || true

# Wait up to 10s for graceful shutdown
for i in $(seq 1 10); do
    REMAIN=0
    pgrep -f "sparkdreamd.*data/chain-[ab]" >/dev/null 2>&1 && REMAIN=1
    pgrep -f "hermes.*hermes_config.toml"   >/dev/null 2>&1 && REMAIN=1
    [ "$REMAIN" -eq 0 ] && break
    sleep 1
done

# Force-kill anything still running
pkill -9 -f "sparkdreamd.*data/chain-[ab]" 2>/dev/null || true
pkill -9 -f "hermes.*hermes_config.toml"   2>/dev/null || true

# Clean up pid + ipc files
rm -f "$SCRIPT_DIR/.chain_pids" \
      "$SCRIPT_DIR/.hermes_pid" \
      "$SCRIPT_DIR/.ibc_channels" 2>/dev/null || true

echo "  All processes stopped."
