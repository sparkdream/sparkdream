#!/bin/bash
# Start both chains in the background and wait for readiness.
set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

BINARY="${BINARY:-sparkdreamd}"
CHAIN_A_HOME="$SCRIPT_DIR/data/chain-a"
CHAIN_B_HOME="$SCRIPT_DIR/data/chain-b"

# Stop any previous instances (chain-a/chain-b plus any ~/.sparkdream chain
# from auto-bootstrap or ignite chain init).
bash "$SCRIPT_DIR/stop_chains.sh" 2>/dev/null || true
pkill -f "sparkdreamd.*\.sparkdream" 2>/dev/null || true
sleep 2

echo "=== Starting chain-a (fedtest-a) ==="
$BINARY start --home "$CHAIN_A_HOME" > "$CHAIN_A_HOME/chain.log" 2>&1 &
PID_A=$!
echo "  PID: $PID_A"

echo "=== Starting chain-b (fedtest-b) ==="
$BINARY start --home "$CHAIN_B_HOME" > "$CHAIN_B_HOME/chain.log" 2>&1 &
PID_B=$!
echo "  PID: $PID_B"

# Wait for chain-a
# Wait helper. Uses the `if jq -e ... &>/dev/null` pattern so set -e
# does not abort us on the first iteration when RPC isn't bound yet
# (status returns "connection refused" → not valid JSON → jq exits non-zero).
wait_for_rpc() {
    local NAME=$1; local NODE=$2; local CHAIN_LOG=$3
    echo "Waiting for $NAME RPC at $NODE..."
    local i
    for i in $(seq 1 60); do
        if $BINARY status --node "$NODE" 2>&1 | jq -e '.sync_info.latest_block_height | tonumber > 1' &>/dev/null; then
            local H
            H=$($BINARY status --node "$NODE" 2>&1 | jq -r '.sync_info.latest_block_height // "0"')
            echo "  $NAME ready at height $H"
            return 0
        fi
        sleep 1
    done
    echo "  FAILED: $NAME did not produce blocks within 60s"
    echo "  Tail of $CHAIN_LOG:"
    tail -20 "$CHAIN_LOG" 2>/dev/null || true
    return 1
}

echo ""
wait_for_rpc "chain-a" "tcp://localhost:26657" "$CHAIN_A_HOME/chain.log" || exit 1
wait_for_rpc "chain-b" "tcp://localhost:36657" "$CHAIN_B_HOME/chain.log" || exit 1

echo ""
echo "Both chains are running:"
echo "  chain-a: PID=$PID_A, RPC=localhost:26657"
echo "  chain-b: PID=$PID_B, RPC=localhost:36657"

echo "PID_A=$PID_A" > "$SCRIPT_DIR/.chain_pids"
echo "PID_B=$PID_B" >> "$SCRIPT_DIR/.chain_pids"
