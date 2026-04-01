#!/bin/bash
#
# E2E test for the full archive → replay pipeline.
#
# This test:
#   1. Initializes a fresh chain and produces blocks
#   2. Archives the blocks into multiple files using small batch size
#   3. Initializes a second (empty) node
#   4. Replays the archived blocks into the second node
#   5. Verifies the replayed node has the same state (app hash)
#
# Requires: sparkdreamd binary on PATH (built with testparams tag)
# No external credentials needed — runs entirely locally.
#
source "$(dirname "$0")/helpers.sh"

echo "=== Replay-from-Archive E2E Test ==="
echo ""

BINARY="sparkdreamd"

# Check binary
if ! command -v "$BINARY" >/dev/null 2>&1; then
    skip "$BINARY not found on PATH — build with: ignite chain build --build.tags testparams"
    exit 0
fi

# Use temp directories for both nodes and archives
SOURCE_HOME=$(mktemp -d)
REPLAY_HOME=$(mktemp -d)
ARCHIVE_DIR=$(mktemp -d)
RPC_PORT=26757
P2P_PORT=26756
CHAIN_ID="replay-test-1"
TARGET_HEIGHT=15
ARCHIVE_BATCH_SIZE=5

cleanup() {
    # Kill any leftover sparkdreamd processes from this test
    if [ -n "$NODE_PID" ] && kill -0 "$NODE_PID" 2>/dev/null; then
        kill "$NODE_PID" 2>/dev/null
        wait "$NODE_PID" 2>/dev/null || true
    fi
    rm -rf "$SOURCE_HOME" "$REPLAY_HOME" "$ARCHIVE_DIR"
}
trap cleanup EXIT

# -------------------------------------------------------------------------
# Step 1: Initialize and start the source chain
# -------------------------------------------------------------------------
echo "Step 1: Initialize source chain"

$BINARY init source-node --chain-id "$CHAIN_ID" --home "$SOURCE_HOME" > /dev/null 2>&1
if [ $? -ne 0 ]; then
    fail "Failed to init source node"
    finish
fi
pass "Source node initialized"

# Configure for fast blocks (1s) and single validator
# Use sed to patch config for quick block production
sed -i 's/timeout_commit = "5s"/timeout_commit = "1s"/' "$SOURCE_HOME/config/config.toml" 2>/dev/null || true
sed -i 's/timeout_propose = "3s"/timeout_propose = "1s"/' "$SOURCE_HOME/config/config.toml" 2>/dev/null || true

# Add a genesis account and validator
$BINARY keys add validator --keyring-backend test --home "$SOURCE_HOME" > /dev/null 2>&1
VALIDATOR_ADDR=$($BINARY keys show validator -a --keyring-backend test --home "$SOURCE_HOME" 2>/dev/null)

$BINARY genesis add-genesis-account "$VALIDATOR_ADDR" 100000000uspark --home "$SOURCE_HOME" --keyring-backend test > /dev/null 2>&1
$BINARY genesis gentx validator 50000000uspark --chain-id "$CHAIN_ID" --home "$SOURCE_HOME" --keyring-backend test > /dev/null 2>&1
$BINARY genesis collect-gentxs --home "$SOURCE_HOME" > /dev/null 2>&1

echo "  Starting source node..."
$BINARY start --home "$SOURCE_HOME" \
    --rpc.laddr "tcp://127.0.0.1:${RPC_PORT}" \
    --grpc.enable=false \
    --api.enable=false \
    --p2p.laddr "tcp://127.0.0.1:${P2P_PORT}" \
    --minimum-gas-prices "0uspark" \
    > "$SOURCE_HOME/node.log" 2>&1 &
NODE_PID=$!

# Wait for the node to start producing blocks
# (IAVL storage upgrade on first run can take a while)
echo "  Waiting for node to start..."
MAX_WAIT=90
WAITED=0
while [ $WAITED -lt $MAX_WAIT ]; do
    CURRENT=$(curl -sf "http://127.0.0.1:${RPC_PORT}/status" 2>/dev/null | jq -r '.result.sync_info.latest_block_height // "0"')
    if [ "$CURRENT" != "0" ] && [ "$CURRENT" != "null" ] && [ -n "$CURRENT" ]; then
        break
    fi
    sleep 1
    WAITED=$(( WAITED + 1 ))
done

if [ $WAITED -ge $MAX_WAIT ]; then
    fail "Node failed to start within ${MAX_WAIT}s"
    tail -20 "$SOURCE_HOME/node.log"
    finish
fi
pass "Source node started (height $CURRENT)"

# Wait for target height + 1 (we need the next block's header to get the
# app hash after executing the target height block)
WAIT_HEIGHT=$(( TARGET_HEIGHT + 1 ))
echo "  Waiting for height $WAIT_HEIGHT..."
MAX_WAIT=60
WAITED=0
while [ $WAITED -lt $MAX_WAIT ]; do
    CURRENT=$(curl -sf "http://127.0.0.1:${RPC_PORT}/status" 2>/dev/null | jq -r '.result.sync_info.latest_block_height // "0"')
    if [ "$CURRENT" -ge "$WAIT_HEIGHT" ] 2>/dev/null; then
        break
    fi
    sleep 1
    WAITED=$(( WAITED + 1 ))
done

if [ "$CURRENT" -lt "$WAIT_HEIGHT" ] 2>/dev/null; then
    fail "Node did not reach height $WAIT_HEIGHT (stuck at $CURRENT)"
    finish
fi
pass "Source chain reached height $CURRENT"

# Record the app hash for comparison. The app_hash in block N+1's header
# is the state root after executing block N.
VERIFY_HEIGHT=$TARGET_HEIGHT
SOURCE_APP_HASH=$(curl -sf "http://127.0.0.1:${RPC_PORT}/block?height=$(( VERIFY_HEIGHT + 1 ))" | \
    jq -r '.result.block.header.app_hash')
echo "  Source app hash after height $VERIFY_HEIGHT: $SOURCE_APP_HASH"

# -------------------------------------------------------------------------
# Step 2: Archive the blocks into multiple files
# -------------------------------------------------------------------------
echo ""
echo "Step 2: Archive blocks from source node (batch size: $ARCHIVE_BATCH_SIZE)"

RPC_URL="http://127.0.0.1:${RPC_PORT}" \
    OUTPUT_DIR="$ARCHIVE_DIR" \
    BATCH_SIZE="$ARCHIVE_BATCH_SIZE" \
    SLEEP_MS=0 \
    "$SCRIPTS_DIR/block-archiver.sh" > "$ARCHIVE_DIR/archiver.log" 2>&1

if [ $? -ne 0 ]; then
    fail "Block archiver failed"
    cat "$ARCHIVE_DIR/archiver.log"
    finish
fi

ARCHIVE_COUNT=$(ls "$ARCHIVE_DIR"/blocks_*.jsonl.gz 2>/dev/null | wc -l)
if [ "$ARCHIVE_COUNT" -eq 0 ]; then
    fail "No archive files produced"
    finish
fi
pass "Archived blocks ($ARCHIVE_COUNT file(s))"

if [ "$ARCHIVE_COUNT" -lt 2 ]; then
    fail "Expected multiple archive files but got $ARCHIVE_COUNT (batch size $ARCHIVE_BATCH_SIZE should split $TARGET_HEIGHT blocks)"
    finish
fi
pass "Multiple archive files created (verifies cross-file replay)"

# List all archive files
for AFILE in $(ls "$ARCHIVE_DIR"/blocks_*.jsonl.gz | sort -t_ -k2 -n); do
    BLOCK_COUNT=$(zcat "$AFILE" | wc -l)
    echo "  $(basename "$AFILE") ($BLOCK_COUNT blocks)"
done

# Stop the source node — we no longer need it
kill "$NODE_PID" 2>/dev/null
wait "$NODE_PID" 2>/dev/null || true
NODE_PID=""
pass "Source node stopped"

# -------------------------------------------------------------------------
# Step 3: Initialize a fresh replay node
# -------------------------------------------------------------------------
echo ""
echo "Step 3: Initialize replay node"

$BINARY init replay-node --chain-id "$CHAIN_ID" --home "$REPLAY_HOME" > /dev/null 2>&1
cp "$SOURCE_HOME/config/genesis.json" "$REPLAY_HOME/config/genesis.json"
pass "Replay node initialized with source genesis"

# -------------------------------------------------------------------------
# Step 4: Replay archived blocks
# -------------------------------------------------------------------------
echo ""
echo "Step 4: Replay blocks from archive ($ARCHIVE_COUNT files)"

if $BINARY replay-from-archive \
    --home "$REPLAY_HOME" \
    --archive-dir "$ARCHIVE_DIR" \
    --end-height "$VERIFY_HEIGHT" \
    --validate=false \
    > "$REPLAY_HOME/replay.log" 2>&1; then
    REPLAY_EXIT=0
else
    REPLAY_EXIT=$?
    fail "Replay command failed (exit code $REPLAY_EXIT)"
    tail -30 "$REPLAY_HOME/replay.log"
    finish
fi

# Extract final height from replay log
REPLAY_FINAL=$(grep -o 'end_height=[0-9]*' "$REPLAY_HOME/replay.log" | tail -1 | cut -d= -f2 || true)
if [ -z "$REPLAY_FINAL" ]; then
    REPLAY_FINAL=$(grep -o '"end_height":[0-9]*' "$REPLAY_HOME/replay.log" | tail -1 | grep -o '[0-9]*' || true)
fi
pass "Replay completed (final height: ${REPLAY_FINAL:-unknown})"

# Verify the expected archive files were processed (replay may skip files
# beyond --end-height, so count files that overlap with the replay range)
EXPECTED_PROCESSED=$(ls "$ARCHIVE_DIR"/blocks_*.jsonl.gz | while read -r f; do
    FROM=$(basename "$f" | sed 's/blocks_\([0-9]*\)_to_.*/\1/')
    [ "$FROM" -le "$VERIFY_HEIGHT" ] && echo "$f"
done | wc -l)
PROCESSED_COUNT=$(grep -c "Processing archive" "$REPLAY_HOME/replay.log" || true)
if [ "$PROCESSED_COUNT" -eq "$EXPECTED_PROCESSED" ]; then
    pass "All $PROCESSED_COUNT relevant archive files were processed"
else
    fail "Expected $EXPECTED_PROCESSED archive files processed, but log shows $PROCESSED_COUNT"
fi

# -------------------------------------------------------------------------
# Step 5: Verify state matches
# -------------------------------------------------------------------------
echo ""
echo "Step 5: Verify replayed state"

# Extract the app hash from the replay log
REPLAY_APP_HASH=$(grep -o 'final_app_hash=[A-F0-9]*' "$REPLAY_HOME/replay.log" | tail -1 | cut -d= -f2 || true)
if [ -z "$REPLAY_APP_HASH" ]; then
    REPLAY_APP_HASH=$(grep -o '"final_app_hash":"[A-F0-9]*"' "$REPLAY_HOME/replay.log" | tail -1 | grep -o '[A-F0-9]*' | tail -1 || true)
fi

echo "  Source app hash:  $SOURCE_APP_HASH"
echo "  Replay app hash:  $REPLAY_APP_HASH"

if [ -n "$SOURCE_APP_HASH" ] && [ -n "$REPLAY_APP_HASH" ] && [ "$SOURCE_APP_HASH" = "$REPLAY_APP_HASH" ]; then
    pass "App hashes match — replay produced identical state"
else
    fail "App hashes do not match: source=$SOURCE_APP_HASH replay=$REPLAY_APP_HASH"
fi

# -------------------------------------------------------------------------
# Step 6: Verify replayed node can start
# -------------------------------------------------------------------------
echo ""
echo "Step 6: Verify replayed node can start"

REPLAY_RPC_PORT=26857
REPLAY_P2P_PORT=26856
$BINARY start --home "$REPLAY_HOME" \
    --rpc.laddr "tcp://127.0.0.1:${REPLAY_RPC_PORT}" \
    --grpc.enable=false \
    --api.enable=false \
    --p2p.laddr "tcp://127.0.0.1:${REPLAY_P2P_PORT}" \
    --minimum-gas-prices "0uspark" \
    > "$REPLAY_HOME/start.log" 2>&1 &
NODE_PID=$!

# Give it a few seconds to start
sleep 5

MAX_WAIT=30
WAITED=0
STARTED=false
while [ $WAITED -lt $MAX_WAIT ]; do
    if ! kill -0 "$NODE_PID" 2>/dev/null; then
        break
    fi
    REPLAY_HEIGHT=$(curl -sf "http://127.0.0.1:${REPLAY_RPC_PORT}/status" 2>/dev/null | jq -r '.result.sync_info.latest_block_height // "0"')
    if [ "$REPLAY_HEIGHT" != "0" ] && [ -n "$REPLAY_HEIGHT" ]; then
        STARTED=true
        break
    fi
    sleep 1
    WAITED=$(( WAITED + 1 ))
done

if [ "$STARTED" = true ]; then
    pass "Replayed node started and is at height $REPLAY_HEIGHT"
    kill "$NODE_PID" 2>/dev/null
    wait "$NODE_PID" 2>/dev/null || true
    NODE_PID=""
else
    fail "Replayed node failed to start"
    tail -20 "$REPLAY_HOME/start.log"
fi

finish
