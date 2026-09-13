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

# ------------------------------------------------------------------
# Hard port check. This runs AFTER the stop/pkill above, so anything
# still listening is a FOREIGN process that will break the bind. The
# check in check_prereqs.sh stays advisory on purpose: it runs minutes
# earlier, before stop_chains.sh, where this suite's own leftover
# chains can legitimately still hold these ports.
#
# Worth failing hard here, because a gRPC bind failure is close to
# invisible: it does not stop CometBFT. The node keeps producing
# blocks and RPC keeps answering, so an RPC-only readiness probe
# passes and the run dies ~20s later inside Hermes with an opaque
# "h2 protocol error". The bind error itself lands at the very END of
# chain.log, after ~70 lines of cobra usage output.
# ------------------------------------------------------------------
# NB: probe by LISTENER, not by connecting. Under WSL2 a bash /dev/tcp
# connect to a closed loopback port hangs instead of being refused, which
# turns any connect-based poll into a wedge. `ss` is a local lookup and
# answers instantly; the /dev/tcp fallback is bounded by `timeout` for the
# rare host without iproute2.
port_in_use() {
    local p=$1
    if command -v ss >/dev/null 2>&1; then
        ss -tln 2>/dev/null | awk '{print $4}' | grep -qE ":${p}\$"
    else
        timeout 1 bash -c "exec 3<>/dev/tcp/127.0.0.1/${p}" 2>/dev/null
    fi
}

BUSY=""
for p in 56657 56656 9390 1617 36657 36656 9190 1417; do
    if port_in_use "$p"; then BUSY="$BUSY $p"; fi
done
if [ -n "$BUSY" ]; then
    echo "ERROR: port(s) already in use by another process:$BUSY"
    echo "  chain-a needs 56657/56656 (RPC/P2P) 9390 (gRPC) 1617 (LCD)"
    echo "  chain-b needs 36657/36656 (RPC/P2P) 9190 (gRPC) 1417 (LCD)"
    echo "  Identify the holder with:"
    echo "    ss -ltnp | grep -E '$(echo "$BUSY" | tr -s " " "|" | sed "s/^|//")'"
    exit 1
fi

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

# Readiness on RPC alone is not enough. Hermes talks to the chain over
# gRPC, and gRPC can be dead while RPC is perfectly healthy (see the note
# on the port check above), so probe both before declaring the chain up.
wait_for_grpc() {
    local NAME=$1; local PORT=$2; local CHAIN_LOG=$3
    echo "Waiting for $NAME gRPC on port $PORT..."
    local i
    for i in $(seq 1 30); do
        if port_in_use "$PORT"; then
            echo "  $NAME gRPC is listening on $PORT"
            return 0
        fi
        sleep 1
    done
    echo "  FAILED: $NAME gRPC never came up on port $PORT"
    echo "  The node may still be producing blocks -- gRPC failing to bind"
    echo "  does not stop CometBFT. Bind error from $CHAIN_LOG:"
    grep -i "failed to listen\|address already in use" "$CHAIN_LOG" 2>/dev/null | tail -3
    return 1
}

echo ""
wait_for_rpc  "chain-a" "tcp://localhost:56657" "$CHAIN_A_HOME/chain.log" || exit 1
wait_for_grpc "chain-a" 9390 "$CHAIN_A_HOME/chain.log" || exit 1
wait_for_rpc  "chain-b" "tcp://localhost:36657" "$CHAIN_B_HOME/chain.log" || exit 1
wait_for_grpc "chain-b" 9190 "$CHAIN_B_HOME/chain.log" || exit 1

echo ""
echo "Both chains are running:"
echo "  chain-a: PID=$PID_A, RPC=localhost:56657"
echo "  chain-b: PID=$PID_B, RPC=localhost:36657"

echo "PID_A=$PID_A" > "$SCRIPT_DIR/.chain_pids"
echo "PID_B=$PID_B" >> "$SCRIPT_DIR/.chain_pids"
