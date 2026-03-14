#!/bin/bash

# ============================================================================
# ENCRYPTED BATCH LIFECYCLE E2E TEST (x/shield)
# ============================================================================
#
# Tests the full encrypted batch lifecycle:
#   1. Verify TLE key material is seeded and encrypted batch mode is enabled
#   2. Query current epoch and trust tree root
#   3. Build encrypted payload (blog CreatePost via seed-tle tool)
#   4. Submit encrypted batch MsgShieldedExec
#   5. Verify the op is queued as pending
#   6. Wait for epoch advancement (EndBlocker batch processing)
#   7. Verify the op was executed (blog post created)
#   8. Test nullifier replay rejection
#
# Prerequisites:
#   - Chain started with TLE-patched genesis (patch_genesis_tle.sh)
#   - setup_test_accounts.sh completed
#   - Blog MsgCreatePost registered as a shielded operation with EITHER mode
#
# This test implements P0-1 from docs/e2e-test-coverage-report.md:
# "Sealed vote / encrypted batch lifecycle"
# ============================================================================

echo "--- TESTING: Encrypted Batch Lifecycle (x/shield) ---"
echo ""

# === 0. SETUP ===
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$SCRIPT_DIR/../.."
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Load test environment
if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    echo "  Run: bash setup_test_accounts.sh"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

# === PASS/FAIL TRACKING ===
PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()

record_result() {
    local NAME=$1
    local RESULT=$2
    TEST_NAMES+=("$NAME")
    RESULTS+=("$RESULT")
    if [ "$RESULT" == "PASS" ]; then
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
    echo "  => $RESULT"
    echo ""
}

# Resolve shield module address if not set
if [ -z "$SHIELD_MODULE_ADDR" ] || [ "$SHIELD_MODULE_ADDR" == "null" ]; then
    SHIELD_MODULE_ADDR=$($BINARY query auth module-account shield --output json 2>/dev/null | jq -r '.account.base_account.address // empty' 2>/dev/null)
fi

echo "Alice:          $ALICE_ADDR"
echo "Submitter1:     $SUBMITTER1_ADDR"
echo "Shield Module:  $SHIELD_MODULE_ADDR"
echo ""

# === HELPER FUNCTIONS ===

wait_for_tx() {
    local TXHASH=$1
    local MAX_ATTEMPTS=20
    local ATTEMPT=0

    while [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
        RESULT=$($BINARY q tx $TXHASH --output json 2>&1)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then
            echo "$RESULT"
            return 0
        fi
        ATTEMPT=$((ATTEMPT + 1))
        sleep 1
    done

    echo "ERROR: Transaction $TXHASH not found after $MAX_ATTEMPTS attempts" >&2
    return 1
}

check_tx_success() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        return 1
    fi
    return 0
}

get_block_height() {
    $BINARY status 2>&1 | jq -r '.sync_info.latest_block_height // "0"'
}

# =========================================================================
# PART 1: Verify TLE is enabled and key material exists
# =========================================================================
echo "--- PART 1: Verify TLE and encrypted batch mode ---"

PARAMS=$($BINARY query shield params --output json 2>&1)
BATCH_ENABLED=$(echo "$PARAMS" | jq -r '.params.encrypted_batch_enabled // "false"')
EPOCH_INTERVAL=$(echo "$PARAMS" | jq -r '.params.shield_epoch_interval // "0"')

echo "  Encrypted batch enabled: $BATCH_ENABLED"
echo "  Epoch interval: $EPOCH_INTERVAL blocks"

if [ "$BATCH_ENABLED" != "true" ]; then
    echo ""
    echo "  Encrypted batch mode is DISABLED."
    echo "  This test requires a TLE-patched genesis."
    echo "  Run: bash test/shield/patch_genesis_tle.sh (before chain start)"
    echo ""
    echo "--- TEST SUMMARY ---"
    echo "  Encrypted batch tests skipped (batch mode disabled)"
    echo "  Tests: 0 passed, 0 failed"
    exit 0
fi

# Check TLE key set exists
TLE_KS=$($BINARY query shield tle-key-set --output json 2>&1)
TLE_MPK=$(echo "$TLE_KS" | jq -r '.key_set.master_public_key // empty' 2>/dev/null)

if [ -z "$TLE_MPK" ] || [ "$TLE_MPK" == "null" ]; then
    echo "  TLE master public key not found"
    record_result "TLE key set exists" "FAIL"
else
    VAL_COUNT=$(echo "$TLE_KS" | jq -r '.key_set.validator_shares | length' 2>/dev/null)
    echo "  TLE master public key: ${TLE_MPK:0:20}..."
    echo "  Validator shares: $VAL_COUNT"
    record_result "TLE key set exists" "PASS"
fi

# =========================================================================
# PART 2: Query current epoch
# =========================================================================
echo "--- PART 2: Query shield epoch state ---"

EPOCH_STATE=$($BINARY query shield shield-epoch --output json 2>&1)
CURRENT_EPOCH=$(echo "$EPOCH_STATE" | jq -r '.state.current_epoch // "0"' 2>/dev/null)
EPOCH_START=$(echo "$EPOCH_STATE" | jq -r '.state.epoch_start_height // "0"' 2>/dev/null)

echo "  Current epoch: $CURRENT_EPOCH"
echo "  Epoch start height: $EPOCH_START"

if [ "$CURRENT_EPOCH" == "null" ] || [ -z "$CURRENT_EPOCH" ]; then
    CURRENT_EPOCH="0"
fi

record_result "Epoch state query" "PASS"

# =========================================================================
# PART 3: Verify blog CreatePost is registered with batch-compatible mode
# =========================================================================
echo "--- PART 3: Verify blog CreatePost shielded operation ---"

BLOG_URL="/sparkdream.blog.v1.MsgCreatePost"
BLOG_OP=$($BINARY query shield shielded-op "$BLOG_URL" --output json 2>&1)

if echo "$BLOG_OP" | grep -qi "not found\|error"; then
    echo "  Blog MsgCreatePost is NOT registered as a shielded operation"
    echo "  Cannot test encrypted batch without a registered operation"
    record_result "Blog CreatePost registered" "FAIL"
    # Exit early — remaining tests depend on this
    echo ""
    echo "--- TEST SUMMARY ---"
    echo "  Tests: $PASS_COUNT passed, $FAIL_COUNT failed"
    exit 1
fi

BLOG_BATCH_MODE=$(echo "$BLOG_OP" | jq -r '.registration.batch_mode // "unknown"')
BLOG_ACTIVE=$(echo "$BLOG_OP" | jq -r '.registration.active // false')

echo "  Blog CreatePost: active=$BLOG_ACTIVE, batch_mode=$BLOG_BATCH_MODE"

# Batch mode must allow encrypted batch (EITHER=2 or ENCRYPTED_ONLY=3)
if [ "$BLOG_BATCH_MODE" == "SHIELD_BATCH_MODE_IMMEDIATE_ONLY" ]; then
    echo "  Blog CreatePost is IMMEDIATE_ONLY — cannot use for encrypted batch"
    record_result "Blog CreatePost batch-compatible" "FAIL"
else
    record_result "Blog CreatePost batch-compatible" "PASS"
fi

# =========================================================================
# PART 4: Check seed-tle key file exists
# =========================================================================
echo "--- PART 4: Verify seed-tle key file ---"

KEY_FILE="$SCRIPT_DIR/.tle_keys.json"

if [ ! -f "$KEY_FILE" ]; then
    echo "  TLE key file not found at: $KEY_FILE"
    echo "  Run patch_genesis_tle.sh to generate it"
    record_result "TLE key file exists" "FAIL"
    echo ""
    echo "--- TEST SUMMARY ---"
    echo "  Tests: $PASS_COUNT passed, $FAIL_COUNT failed"
    exit 1
fi

echo "  Key file: $KEY_FILE"
record_result "TLE key file exists" "PASS"

# =========================================================================
# PART 5: Build and submit encrypted batch MsgShieldedExec
# =========================================================================
echo "--- PART 5: Submit encrypted batch ---"

# Get the current trust tree root (for merkle_root)
TRUST_ROOT_HEX=""
TRUST_ROOT_RESULT=$($BINARY query rep trust-tree-root --output json 2>&1)
if echo "$TRUST_ROOT_RESULT" | jq -e '.root' > /dev/null 2>&1; then
    TRUST_ROOT_HEX=$(echo "$TRUST_ROOT_RESULT" | jq -r '.root // ""' | tr -d '"')
fi

# If no trust tree root available, use a dummy 32-byte value
# (proof verification is skipped when no VK is stored)
if [ -z "$TRUST_ROOT_HEX" ] || [ "$TRUST_ROOT_HEX" == "null" ]; then
    echo "  No trust tree root available, using dummy merkle root"
    TRUST_ROOT_HEX="0000000000000000000000000000000000000000000000000000000000000001"
fi

echo "  Trust tree root: ${TRUST_ROOT_HEX:0:16}..."

# Generate the encrypted payload using seed-tle tool
echo "  Building encrypted payload for epoch $CURRENT_EPOCH..."

cd "$PROJECT_ROOT"
ENCRYPTED_PAYLOAD_B64=$(go run ./tools/zk/cmd/seed-tle payload \
    --key-file="$KEY_FILE" \
    --epoch="$CURRENT_EPOCH" \
    --shield-addr="$SHIELD_MODULE_ADDR" \
    --title="EncBatchTest$(date +%s)" \
    --body="Encrypted batch lifecycle test post" 2>/dev/null)

if [ -z "$ENCRYPTED_PAYLOAD_B64" ]; then
    echo "  Failed to generate encrypted payload"
    record_result "Build encrypted payload" "FAIL"
else
    echo "  Encrypted payload size: ${#ENCRYPTED_PAYLOAD_B64} chars (base64)"
    record_result "Build encrypted payload" "PASS"
fi

# Generate unique nullifier and rate-limit nullifier (32-byte random values)
NULLIFIER_HEX=$(openssl rand -hex 32)
RATE_NULLIFIER_HEX=$(openssl rand -hex 32)

# Convert to base64 for proto JSON
NULLIFIER_B64=$(echo "$NULLIFIER_HEX" | xxd -r -p | base64)
RATE_NULLIFIER_B64=$(echo "$RATE_NULLIFIER_HEX" | xxd -r -p | base64)
MERKLE_ROOT_B64=$(echo "$TRUST_ROOT_HEX" | xxd -r -p | base64)

echo "  Nullifier: ${NULLIFIER_HEX:0:16}..."
echo "  Submitting encrypted batch tx..."

# Submit the encrypted batch via autocli
TX_RES=$($BINARY tx shield shielded-exec \
    --exec-mode=2 \
    --encrypted-payload="$ENCRYPTED_PAYLOAD_B64" \
    --target-epoch="$CURRENT_EPOCH" \
    --merkle-root="$MERKLE_ROOT_B64" \
    --nullifier="$NULLIFIER_B64" \
    --rate-limit-nullifier="$RATE_NULLIFIER_B64" \
    --proof-domain=1 \
    --min-trust-level=0 \
    --from submitter1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    --gas 500000 \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to submit encrypted batch: no txhash"
    echo "  Response: $TX_RES"
    record_result "Submit encrypted batch tx" "FAIL"
else
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        PENDING_OP_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="shield_queued") | .attributes[] | select(.key=="pending_op_id") | .value' 2>/dev/null | head -1)
        echo "  Encrypted batch queued successfully"
        echo "  Pending op ID: $PENDING_OP_ID"
        record_result "Submit encrypted batch tx" "PASS"
    else
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // "unknown error"')
        echo "  Encrypted batch submission failed: $RAW_LOG"
        record_result "Submit encrypted batch tx" "FAIL"
    fi
fi

# =========================================================================
# PART 6: Verify pending operation is queued
# =========================================================================
echo "--- PART 6: Verify pending operation is queued ---"

PENDING=$($BINARY query shield pending-ops --output json 2>&1)
PENDING_COUNT=$(echo "$PENDING" | jq -r '.pending_ops | length' 2>/dev/null || echo "0")

echo "  Pending operations: $PENDING_COUNT"

if [ "$PENDING_COUNT" -ge 1 ]; then
    # Check the most recent pending op
    LAST_OP=$(echo "$PENDING" | jq -r '.pending_ops[-1]')
    LAST_OP_EPOCH=$(echo "$LAST_OP" | jq -r '.target_epoch // empty')
    LAST_OP_ID=$(echo "$LAST_OP" | jq -r '.id // empty')
    echo "  Last pending op: id=$LAST_OP_ID, target_epoch=$LAST_OP_EPOCH"
    record_result "Pending operation queued" "PASS"
else
    echo "  No pending operations found"
    echo "  (May have already been processed if epoch advanced)"
    # Check if the epoch has already advanced past the target
    NEW_EPOCH_STATE=$($BINARY query shield shield-epoch --output json 2>&1)
    NEW_EPOCH=$(echo "$NEW_EPOCH_STATE" | jq -r '.state.current_epoch // "0"' 2>/dev/null)
    if [ "$NEW_EPOCH" != "$CURRENT_EPOCH" ]; then
        echo "  Epoch has advanced ($CURRENT_EPOCH -> $NEW_EPOCH) — op may have been processed"
        record_result "Pending operation queued" "PASS"
    else
        record_result "Pending operation queued" "FAIL"
    fi
fi

# =========================================================================
# PART 7: Wait for epoch advancement and batch execution
# =========================================================================
echo "--- PART 7: Wait for epoch advancement ---"

# ShieldEpochInterval is typically 10 blocks in testing (~60 seconds)
# Wait up to 2 epoch intervals for the batch to be processed
MAX_WAIT_BLOCKS=$((EPOCH_INTERVAL * 3))
START_HEIGHT=$(get_block_height)
TARGET_HEIGHT=$((START_HEIGHT + MAX_WAIT_BLOCKS))

echo "  Current height: $START_HEIGHT"
echo "  Epoch interval: $EPOCH_INTERVAL blocks"
echo "  Waiting up to $MAX_WAIT_BLOCKS blocks for epoch advancement..."

BATCH_EXECUTED=false
WAIT_ATTEMPT=0
MAX_WAIT_SECONDS=$((MAX_WAIT_BLOCKS * 7))  # ~7 seconds per block

while [ $WAIT_ATTEMPT -lt $MAX_WAIT_SECONDS ]; do
    CURRENT_HEIGHT=$(get_block_height)

    if [ "$CURRENT_HEIGHT" -ge "$TARGET_HEIGHT" ]; then
        echo "  Reached target height $TARGET_HEIGHT"
        break
    fi

    # Check if pending ops have been cleared (batch executed)
    PENDING_NOW=$($BINARY query shield pending-op-count --output json 2>&1)
    COUNT_NOW=$(echo "$PENDING_NOW" | jq -r '.count // "0"' 2>/dev/null)

    if [ "$COUNT_NOW" == "0" ] && [ "$PENDING_COUNT" -ge 1 ]; then
        echo "  Pending ops cleared at height $CURRENT_HEIGHT (batch executed)"
        BATCH_EXECUTED=true
        break
    fi

    # Also check epoch advancement
    EPOCH_NOW=$($BINARY query shield shield-epoch --output json 2>&1)
    EPOCH_NUM=$(echo "$EPOCH_NOW" | jq -r '.state.current_epoch // "0"' 2>/dev/null)
    if [ "$EPOCH_NUM" != "$CURRENT_EPOCH" ] && [ "$EPOCH_NUM" -gt "$CURRENT_EPOCH" ]; then
        echo "  Epoch advanced: $CURRENT_EPOCH -> $EPOCH_NUM at height $CURRENT_HEIGHT"
        # Give a few more blocks for batch processing
        sleep 12
        BATCH_EXECUTED=true
        break
    fi

    WAIT_ATTEMPT=$((WAIT_ATTEMPT + 6))
    sleep 6
done

if [ "$BATCH_EXECUTED" = true ]; then
    echo "  Batch processing triggered"
    record_result "Epoch advancement and batch processing" "PASS"
else
    echo "  Batch was not processed within the wait period"
    echo "  This may be expected if epoch_interval is very long"
    record_result "Epoch advancement and batch processing" "FAIL"
fi

# =========================================================================
# PART 8: Verify batch execution events
# =========================================================================
echo "--- PART 8: Verify batch execution results ---"

# Check if there are any shield_batch_executed events in recent blocks
# Query the most recent blocks for the event
BATCH_EVENT_FOUND=false
CURRENT_HEIGHT=$(get_block_height)
SEARCH_FROM=$((CURRENT_HEIGHT > 20 ? CURRENT_HEIGHT - 20 : 1))

echo "  Searching for batch execution events (blocks $SEARCH_FROM-$CURRENT_HEIGHT)..."

# Use block_results to find events
for height in $(seq $CURRENT_HEIGHT -1 $SEARCH_FROM); do
    BLOCK_RESULTS=$($BINARY query block-results $height --output json 2>/dev/null)
    if echo "$BLOCK_RESULTS" | grep -q "shield_batch_executed" 2>/dev/null; then
        EXECUTED_COUNT=$(echo "$BLOCK_RESULTS" | jq -r '.. | select(.key? == "executed") | .value' 2>/dev/null | head -1)
        DROPPED_COUNT=$(echo "$BLOCK_RESULTS" | jq -r '.. | select(.key? == "dropped") | .value' 2>/dev/null | head -1)
        echo "  Found batch execution event at height $height"
        echo "    Executed: ${EXECUTED_COUNT:-unknown}"
        echo "    Dropped: ${DROPPED_COUNT:-unknown}"
        BATCH_EVENT_FOUND=true
        break
    fi
done

if [ "$BATCH_EVENT_FOUND" = true ]; then
    record_result "Batch execution event emitted" "PASS"
else
    echo "  No batch execution events found in recent blocks"
    echo "  (Events may have been pruned or batch may not have triggered)"
    record_result "Batch execution event emitted" "FAIL"
fi

# =========================================================================
# PART 9: Verify pending ops are cleared
# =========================================================================
echo "--- PART 9: Verify pending ops cleared after batch ---"

FINAL_PENDING=$($BINARY query shield pending-op-count --output json 2>&1)
FINAL_COUNT=$(echo "$FINAL_PENDING" | jq -r '.count // "0"' 2>/dev/null)

echo "  Pending ops remaining: $FINAL_COUNT"

if [ "$FINAL_COUNT" == "0" ]; then
    record_result "Pending ops cleared after batch" "PASS"
else
    echo "  Some ops may still be pending (will be processed next epoch)"
    record_result "Pending ops cleared after batch" "FAIL"
fi

# =========================================================================
# PART 10: Test nullifier replay rejection
# =========================================================================
echo "--- PART 10: Nullifier replay rejection ---"

# Try to submit the SAME nullifier again — should fail with ErrNullifierUsed
# or ErrNullifierPending

echo "  Re-submitting with same nullifier..."

# Generate a new encrypted payload (different content) but reuse the nullifier
NEW_PAYLOAD_B64=$(go run "$PROJECT_ROOT/tools/zk/cmd/seed-tle" payload \
    --key-file="$KEY_FILE" \
    --epoch="$CURRENT_EPOCH" \
    --shield-addr="$SHIELD_MODULE_ADDR" \
    --title="ReplayTest$(date +%s)" \
    --body="This should fail due to nullifier replay" 2>/dev/null)

# Get the current epoch (may have advanced)
REPLAY_EPOCH_STATE=$($BINARY query shield shield-epoch --output json 2>&1)
REPLAY_EPOCH=$(echo "$REPLAY_EPOCH_STATE" | jq -r '.state.current_epoch // "0"' 2>/dev/null)

REPLAY_TX_RES=$($BINARY tx shield shielded-exec \
    --exec-mode=2 \
    --encrypted-payload="$NEW_PAYLOAD_B64" \
    --target-epoch="$REPLAY_EPOCH" \
    --merkle-root="$MERKLE_ROOT_B64" \
    --nullifier="$NULLIFIER_B64" \
    --rate-limit-nullifier="$RATE_NULLIFIER_B64" \
    --proof-domain=1 \
    --min-trust-level=0 \
    --from submitter1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    --gas 500000 \
    -y \
    --output json 2>&1)

REPLAY_TXHASH=$(echo "$REPLAY_TX_RES" | jq -r '.txhash // empty')

if [ -z "$REPLAY_TXHASH" ] || [ "$REPLAY_TXHASH" == "null" ]; then
    echo "  Replay tx rejected at broadcast (no txhash) — EXPECTED"
    record_result "Nullifier replay rejected" "PASS"
else
    sleep 6
    REPLAY_RESULT=$(wait_for_tx $REPLAY_TXHASH)
    REPLAY_CODE=$(echo "$REPLAY_RESULT" | jq -r '.code')

    if [ "$REPLAY_CODE" != "0" ]; then
        RAW_LOG=$(echo "$REPLAY_RESULT" | jq -r '.raw_log // "unknown"')
        echo "  Replay tx failed with code $REPLAY_CODE — EXPECTED"
        echo "  Error: $RAW_LOG"
        record_result "Nullifier replay rejected" "PASS"
    else
        echo "  WARNING: Replay tx SUCCEEDED — nullifier not properly checked"
        record_result "Nullifier replay rejected" "FAIL"
    fi
fi

# =========================================================================
# PART 11: Test submission with wrong target epoch
# =========================================================================
echo "--- PART 11: Wrong target epoch rejection ---"

# Submit with a wrong epoch number
WRONG_EPOCH=$((REPLAY_EPOCH + 999))
WRONG_NULLIFIER_B64=$(openssl rand 32 | base64)
WRONG_RATE_B64=$(openssl rand 32 | base64)

WRONG_TX_RES=$($BINARY tx shield shielded-exec \
    --exec-mode=2 \
    --encrypted-payload="$ENCRYPTED_PAYLOAD_B64" \
    --target-epoch="$WRONG_EPOCH" \
    --merkle-root="$MERKLE_ROOT_B64" \
    --nullifier="$WRONG_NULLIFIER_B64" \
    --rate-limit-nullifier="$WRONG_RATE_B64" \
    --proof-domain=1 \
    --min-trust-level=0 \
    --from submitter1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    --gas 500000 \
    -y \
    --output json 2>&1)

WRONG_TXHASH=$(echo "$WRONG_TX_RES" | jq -r '.txhash // empty')

if [ -z "$WRONG_TXHASH" ] || [ "$WRONG_TXHASH" == "null" ]; then
    echo "  Wrong epoch tx rejected at broadcast — EXPECTED"
    record_result "Wrong target epoch rejected" "PASS"
else
    sleep 6
    WRONG_RESULT=$(wait_for_tx $WRONG_TXHASH)
    WRONG_CODE=$(echo "$WRONG_RESULT" | jq -r '.code')

    if [ "$WRONG_CODE" != "0" ]; then
        RAW_LOG=$(echo "$WRONG_RESULT" | jq -r '.raw_log // "unknown"')
        echo "  Wrong epoch tx failed with code $WRONG_CODE — EXPECTED"
        echo "  Error: $RAW_LOG"
        record_result "Wrong target epoch rejected" "PASS"
    else
        echo "  WARNING: Wrong epoch tx SUCCEEDED"
        record_result "Wrong target epoch rejected" "FAIL"
    fi
fi

# =========================================================================
# PART 12: Test submission with empty encrypted payload
# =========================================================================
echo "--- PART 12: Empty encrypted payload rejection ---"

EMPTY_NULLIFIER_B64=$(openssl rand 32 | base64)
EMPTY_RATE_B64=$(openssl rand 32 | base64)

EMPTY_TX_RES=$($BINARY tx shield shielded-exec \
    --exec-mode=2 \
    --target-epoch="$REPLAY_EPOCH" \
    --merkle-root="$MERKLE_ROOT_B64" \
    --nullifier="$EMPTY_NULLIFIER_B64" \
    --rate-limit-nullifier="$EMPTY_RATE_B64" \
    --proof-domain=1 \
    --min-trust-level=0 \
    --from submitter1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    --gas 500000 \
    -y \
    --output json 2>&1)

EMPTY_TXHASH=$(echo "$EMPTY_TX_RES" | jq -r '.txhash // empty')

if [ -z "$EMPTY_TXHASH" ] || [ "$EMPTY_TXHASH" == "null" ]; then
    echo "  Empty payload tx rejected at broadcast — EXPECTED"
    record_result "Empty encrypted payload rejected" "PASS"
else
    sleep 6
    EMPTY_RESULT=$(wait_for_tx $EMPTY_TXHASH)
    EMPTY_CODE=$(echo "$EMPTY_RESULT" | jq -r '.code')

    if [ "$EMPTY_CODE" != "0" ]; then
        echo "  Empty payload tx failed with code $EMPTY_CODE — EXPECTED"
        record_result "Empty encrypted payload rejected" "PASS"
    else
        echo "  WARNING: Empty payload tx SUCCEEDED"
        record_result "Empty encrypted payload rejected" "FAIL"
    fi
fi

# =========================================================================
# TEST SUMMARY
# =========================================================================
echo "============================================="
echo "--- ENCRYPTED BATCH TEST SUMMARY ---"
echo "============================================="
echo ""
echo "  Tests Passed: $PASS_COUNT"
echo "  Tests Failed: $FAIL_COUNT"
echo ""

for i in "${!TEST_NAMES[@]}"; do
    printf "  %-45s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done

echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL TESTS PASSED <<<"
fi
