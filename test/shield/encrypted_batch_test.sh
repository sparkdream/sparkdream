#!/bin/bash

# ============================================================================
# ENCRYPTED BATCH LIFECYCLE E2E TEST (x/shield)
# ============================================================================
#
# Tests the full encrypted batch lifecycle:
#   1. Verify TLE key material is seeded and encrypted batch mode is enabled
#   2. Query current epoch and trust tree root
#   3. Build encrypted payload (blog CreatePost via seed-tle, encrypted to the
#      chain's G2 master public key exactly as a browser client would)
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
# .test_env may predate the shield account (or the query raced the first
# block at setup); resolve the address live when it is missing.
if [ -z "${SHIELD_MODULE_ADDR:-}" ]; then
    SHIELD_MODULE_ADDR=$($BINARY query auth module-account shield --output json 2>/dev/null | jq -r '.account.base_account.address // .account.value.address // ""' 2>/dev/null)
fi

# === PASS/FAIL TRACKING ===
PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()

SKIP_COUNT=0
record_result() {
    local NAME=$1
    local RESULT=$2
    TEST_NAMES+=("$NAME")
    RESULTS+=("$RESULT")
    if [ "$RESULT" == "PASS" ]; then
        PASS_COUNT=$((PASS_COUNT + 1))
    elif [ "$RESULT" == "SKIP" ]; then
        SKIP_COUNT=$((SKIP_COUNT + 1))
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
    echo "  SKIPPED: encrypted batch tests need a TLE-patched genesis (batch mode disabled)"
    exit 3
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
CURRENT_EPOCH=$(echo "$EPOCH_STATE" | jq -r '.epoch_state.current_epoch // "0"' 2>/dev/null)
EPOCH_START=$(echo "$EPOCH_STATE" | jq -r '.epoch_state.epoch_start_height // "0"' 2>/dev/null)

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


# ensure_shield_reserve: the shield module pays the gas for every shielded
# exec, and a fresh test chain's community pool is empty, so the BeginBlocker
# has nothing to draw from. Top the pool up from alice and poll (bounded) until
# the module account holds SPARK.
ensure_shield_reserve() {
    local BAL
    BAL=$($BINARY query shield module-balance --output json 2>/dev/null | jq -r '.balance.amount // "0"')
    if [ "${BAL:-0}" -ge 10000000 ] 2>/dev/null; then
        return 0
    fi
    echo "  Shield reserve is ${BAL:-0}; funding the community pool from alice..."
    $BINARY tx distribution fund-community-pool 300000000${BOND_DENOM} --from alice -y \
        --chain-id "$CHAIN_ID" --keyring-backend test --fees 5000${BOND_DENOM} --output json >/dev/null 2>&1
    local i
    for i in $(seq 1 40); do
        sleep 2
        BAL=$($BINARY query shield module-balance --output json 2>/dev/null | jq -r '.balance.amount // "0"')
        if [ "${BAL:-0}" -ge 10000000 ] 2>/dev/null; then
            echo "  Shield reserve funded: $BAL"
            return 0
        fi
    done
    echo "  WARNING: shield reserve still ${BAL:-0} after funding the pool"
    return 1
}

# =========================================================================
# PART 5: Build and submit encrypted batch MsgShieldedExec
# =========================================================================
echo "--- PART 5: Submit encrypted batch ---"
ensure_shield_reserve
# Re-read the epoch after the funding wait: the ante handler refuses a
# target_epoch that is not the current one, and epochs are short here.
CURRENT_EPOCH=$($BINARY query shield shield-epoch --output json 2>/dev/null | jq -r '.epoch_state.current_epoch // "0"')
echo "  Current epoch (post-funding): $CURRENT_EPOCH"

# Get the current trust tree root (for merkle_root)
TRUST_ROOT_HEX=""
# The trust-tree query returns the root base64-encoded; the CLI wants hex.
TRUST_ROOT_RESULT=$($BINARY query rep trust-tree --output json 2>&1)
if echo "$TRUST_ROOT_RESULT" | jq -e '.root' > /dev/null 2>&1; then
    TRUST_ROOT_HEX=$(echo "$TRUST_ROOT_RESULT" | jq -r '.root // ""' | base64 -d 2>/dev/null | od -An -tx1 | tr -d ' \n')
fi

# If no trust tree root available, use a dummy 32-byte value
# (proof verification is skipped when no VK is stored)
if [ -z "$TRUST_ROOT_HEX" ] || [ "$TRUST_ROOT_HEX" == "null" ]; then
    echo "  No trust tree root available, using dummy merkle root"
    TRUST_ROOT_HEX="0000000000000000000000000000000000000000000000000000000000000001"
fi

echo "  Trust tree root: ${TRUST_ROOT_HEX:0:16}..."

# Encrypt exactly as a client does: to the G2 master public key read from
# the chain, with no secret material (the key file is only used to seed
# genesis). The payload is an inner MsgCreatePost fragment; this test build
# has no verifying key, so the fragment carries no proof.
echo "  Building encrypted payload for epoch $CURRENT_EPOCH..."

MPK_G2_B64=$($BINARY query shield tle-master-public-key --output json 2>/dev/null | jq -r '.master_public_key_g2 // empty')
if [ -z "$MPK_G2_B64" ]; then
    echo "  Chain reports no G2 master public key (tle-master-public-key)"
    record_result "Chain exposes G2 master public key" "FAIL"
else
    echo "  G2 master public key: ${MPK_G2_B64:0:20}..."
    record_result "Chain exposes G2 master public key" "PASS"
fi

# This test build has no verifying key, so neither the submission proof
# (cleartext, bound to the ciphertext) nor the operation proof (sealed) is
# verified; the ante handler still requires a proof-sized blob. The
# operation nullifier is sealed inside the payload; the cleartext nullifier
# is the per-payload submission nullifier.
DUMMY_PROOF_HEX=$(openssl rand -hex 128)
OP_NULLIFIER_HEX=$(openssl rand -hex 32)

# Build the payload tool once: `go run` can take longer than a test epoch
# (10 blocks), and a payload is sealed to the epoch it targets.
SEED_TLE="$(mktemp -d)/seed-tle"
(cd "$PROJECT_ROOT" && go build -o "$SEED_TLE" ./tools/zk/cmd/seed-tle) || { echo "  Failed to build seed-tle"; exit 1; }

# build_payload <op-nullifier-hex> <title> <body>: seals a payload to the
# current epoch, rebuilding if the epoch rolls over meanwhile. Sets
# PAYLOAD_B64 and PAYLOAD_EPOCH.
build_payload() {
    local attempt
    for attempt in 1 2 3; do
        PAYLOAD_EPOCH=$($BINARY query shield shield-epoch --output json 2>/dev/null | jq -r '.epoch_state.current_epoch // "0"')
        PAYLOAD_B64=$("$SEED_TLE" payload \
            --mpk-g2="$MPK_G2_B64" \
            --epoch="$PAYLOAD_EPOCH" \
            --shield-addr="$SHIELD_MODULE_ADDR" \
            --nullifier-hex="$1" \
            --title="$2" \
            --body="$3" 2>/dev/null)
        local now
        now=$($BINARY query shield shield-epoch --output json 2>/dev/null | jq -r '.epoch_state.current_epoch // "0"')
        if [ -n "$PAYLOAD_B64" ] && [ "$now" == "$PAYLOAD_EPOCH" ]; then
            return 0
        fi
        echo "  Epoch moved while sealing ($PAYLOAD_EPOCH -> $now); resealing"
    done
    return 1
}

build_payload "$OP_NULLIFIER_HEX" "EncBatchTest$(date +%s)" "Encrypted batch lifecycle test post"
ENCRYPTED_PAYLOAD_B64="$PAYLOAD_B64"
CURRENT_EPOCH="$PAYLOAD_EPOCH"

if [ -z "$ENCRYPTED_PAYLOAD_B64" ]; then
    echo "  Failed to generate encrypted payload"
    record_result "Build encrypted payload" "FAIL"
else
    echo "  Encrypted payload size: ${#ENCRYPTED_PAYLOAD_B64} chars (base64)"
    record_result "Build encrypted payload" "PASS"
fi

# Submission nullifier and rate-limit nullifier (32-byte random values; a
# real client takes both from the submission proof)
NULLIFIER_HEX=$(openssl rand -hex 32)
RATE_NULLIFIER_HEX=$(openssl rand -hex 32)

# The CLI takes hex for every byte field (payload included); exec-mode 1 = ENCRYPTED_BATCH.
ENCRYPTED_PAYLOAD_HEX=$(echo "$ENCRYPTED_PAYLOAD_B64" | base64 -d | od -An -tx1 | tr -d ' \n')

echo "  Nullifier: ${NULLIFIER_HEX:0:16}..."
echo "  Submitting encrypted batch tx..."

# Submit the encrypted batch via autocli
TX_RES=$($BINARY tx shield shielded-exec \
    --exec-mode=1 \
    --encrypted-payload="$ENCRYPTED_PAYLOAD_HEX" \
    --target-epoch="$CURRENT_EPOCH" \
    --proof="$DUMMY_PROOF_HEX" \
    --merkle-root="$TRUST_ROOT_HEX" \
    --nullifier="$NULLIFIER_HEX" \
    --rate-limit-nullifier="$RATE_NULLIFIER_HEX" \
    --proof-domain=1 \
    --min-trust-level=0 \
    --from submitter1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    --gas 500000 \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')
BROADCAST_CODE=$(echo "$TX_RES" | jq -r '.code // "0"' 2>/dev/null)

# Immediately resubmit under the same submission nullifier (the first one is
# pending in the mempool/check state): refused in CheckTx with
# ErrInvalidSubmissionNullifier (shield code 52). Sent before waiting for the
# first tx so both fall in the same epoch on a fast chain. A distinct memo
# keeps the bytes different, or CometBFT's tx cache would refuse it first.
DUP_TX_RES=$($BINARY tx shield shielded-exec \
    --note "dup-$(date +%s%N)" \
    --exec-mode=1 \
    --encrypted-payload="$ENCRYPTED_PAYLOAD_HEX" \
    --target-epoch="$CURRENT_EPOCH" \
    --proof="$DUMMY_PROOF_HEX" \
    --merkle-root="$TRUST_ROOT_HEX" \
    --nullifier="$NULLIFIER_HEX" \
    --rate-limit-nullifier="$RATE_NULLIFIER_HEX" \
    --proof-domain=1 \
    --min-trust-level=0 \
    --from submitter1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    --gas 500000 \
    -y \
    --output json 2>&1)

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to submit encrypted batch: no txhash"
    echo "  Response: $TX_RES"
    record_result "Submit encrypted batch tx" "FAIL"
elif [ "$BROADCAST_CODE" != "0" ]; then
    # Rejected in CheckTx (ante handler): never indexed, so report the
    # broadcast response instead of waiting for a tx that will not land.
    echo "  Rejected at broadcast (code $BROADCAST_CODE): $(echo "$TX_RES" | jq -r '.raw_log // empty')"
    record_result "Submit encrypted batch tx" "FAIL"
else
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        SUBMIT_HEIGHT=$(echo "$TX_RESULT" | jq -r '.height // "0"')
        PENDING_OP_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="shielded_queued") | .attributes[] | select(.key=="pending_op_id") | .value' 2>/dev/null | head -1)
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
# PART 5b: The same submission nullifier cannot be queued twice
# =========================================================================
echo "--- PART 5b: Duplicate submission nullifier rejected ---"

DUP_CODE=$(echo "$DUP_TX_RES" | jq -r '.code // empty' 2>/dev/null)
DUP_TXHASH=$(echo "$DUP_TX_RES" | jq -r '.txhash // empty' 2>/dev/null)
if [ "$DUP_CODE" == "0" ] && [ -n "$DUP_TXHASH" ]; then
    sleep 6
    DUP_CODE=$(wait_for_tx "$DUP_TXHASH" | jq -r '.code // "0"')
fi
if [ "$DUP_CODE" == "52" ]; then
    echo "  Duplicate rejected with code 52 (submission nullifier already pending)"
    record_result "Duplicate submission nullifier rejected" "PASS"
elif [ "$DUP_CODE" == "21" ]; then
    echo "  Epoch rolled over between the two submissions (code 21): the dedup was not exercised"
    record_result "Duplicate submission nullifier rejected" "SKIP"
else
    echo "  Expected code 52, got '${DUP_CODE:-none}': $(echo "$DUP_TX_RES" | head -c 200)"
    record_result "Duplicate submission nullifier rejected" "FAIL"
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
    # With seeded epoch keys a batch runs one block after its epoch closes,
    # so an op submitted at the end of an epoch may already be gone. That
    # only counts if a batch actually executed since the submission height;
    # an advanced epoch on its own proves nothing.
    echo "  No pending operations found; looking for a batch executed since height ${SUBMIT_HEIGHT:-?}"
    ALREADY_RAN=false
    if [ -n "$SUBMIT_HEIGHT" ] && [ "$SUBMIT_HEIGHT" -gt 0 ] 2>/dev/null; then
        for h in $(seq "$SUBMIT_HEIGHT" "$(get_block_height)"); do
            if $BINARY query block-results "$h" --output json 2>/dev/null | grep -q shield_batch_executed; then
                echo "  Batch executed at height $h"
                ALREADY_RAN=true
                BATCH_HEIGHT=$h
                break
            fi
        done
    fi
    if [ "$ALREADY_RAN" = true ]; then
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

BATCH_EXECUTED=${ALREADY_RAN:-false}
WAIT_ATTEMPT=0
MAX_WAIT_SECONDS=$((MAX_WAIT_BLOCKS * 7))  # ~7 seconds per block

[ "$BATCH_EXECUTED" = true ] && echo "  Batch already executed at height ${BATCH_HEIGHT:-?} (fast chain)"
while [ "$BATCH_EXECUTED" != true ] && [ $WAIT_ATTEMPT -lt $MAX_WAIT_SECONDS ]; do
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

    # Report epoch advancement, but only an executed batch (queue drained)
    # passes: the epoch rolling over on its own proves nothing.
    EPOCH_NOW=$($BINARY query shield shield-epoch --output json 2>&1)
    EPOCH_NUM=$(echo "$EPOCH_NOW" | jq -r '.epoch_state.current_epoch // "0"' 2>/dev/null)
    if [ "$EPOCH_NUM" -gt "$CURRENT_EPOCH" ] && [ "${EPOCH_REPORTED:-}" != "1" ]; then
        echo "  Epoch advanced: $CURRENT_EPOCH -> $EPOCH_NUM at height $CURRENT_HEIGHT; waiting for the batch"
        EPOCH_REPORTED=1
    fi

    WAIT_ATTEMPT=$((WAIT_ATTEMPT + 3))
    sleep 3
done

if [ "$BATCH_EXECUTED" = true ]; then
    echo "  Batch executed (pending queue drained)"
    record_result "Epoch advancement and batch processing" "PASS"
else
    echo "  Batch was not executed within $MAX_WAIT_BLOCKS blocks (pending: ${COUNT_NOW:-?})"
    # On a seeded (no-DKG) chain a batch can only run if genesis seeded a key
    # for the op's epoch; say so when that is the cause.
    if ! $BINARY query shield decryption-shares "$CURRENT_EPOCH" --output json >/dev/null 2>&1 || \
       [ "$(sparkdreamd query shield pending-op-count --output json 2>/dev/null | jq -r '.count // 0')" != "0" ]; then
        echo "  Hint: with patch_genesis_tle.sh the chain holds pre-seeded epoch keys only; epoch $CURRENT_EPOCH may be beyond the seeded range (NUM_EPOCHS)"
    fi
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
SEARCH_FROM=${SUBMIT_HEIGHT:-$((CURRENT_HEIGHT > 20 ? CURRENT_HEIGHT - 20 : 1))}
[ "$SEARCH_FROM" -lt 1 ] 2>/dev/null && SEARCH_FROM=1

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
# PART 10: Operation nullifier replay is dropped at execution
# =========================================================================
echo "--- PART 10: Operation nullifier replay ---"

# The operation nullifier travels inside the ciphertext, so a replay cannot
# be refused at submission (fresh submission nullifier, different payload):
# it queues, and the batch drops it because the nullifier is already in the
# permanent store for that scope. A second blog post must not appear.

echo "  Re-submitting a new payload sealed with the same operation nullifier..."

# Posts from earlier runs on the same chain must not count: compare deltas.
REPLAY_POSTS_BEFORE=$($BINARY query blog list-post --output json 2>/dev/null | jq -r '[.post[]? | select(.title | startswith("ReplayTest"))] | length' 2>/dev/null || echo 0)

build_payload "$OP_NULLIFIER_HEX" "ReplayTest$(date +%s)" "This should be dropped: operation nullifier replay"
NEW_PAYLOAD_B64="$PAYLOAD_B64"
REPLAY_EPOCH="$PAYLOAD_EPOCH"

# Blog posts are EPOCH-scoped with a 12-epoch window: the nullifier is only
# a replay if both submissions fall in the same window (a real prover's
# nullifier changes with the scope anyway). Across a window boundary this
# is a fresh action, not a replay, so the check cannot run.
POST_WINDOW=$($BINARY query shield shielded-op "/sparkdream.blog.v1.MsgCreatePost" --output json 2>/dev/null | jq -r '.registration.epoch_window // "1"')
[ "${POST_WINDOW:-0}" -lt 1 ] 2>/dev/null && POST_WINDOW=1
if [ $((CURRENT_EPOCH / POST_WINDOW)) -ne $((REPLAY_EPOCH / POST_WINDOW)) ]; then
    echo "  Original (epoch $CURRENT_EPOCH) and replay (epoch $REPLAY_EPOCH) fall in different $POST_WINDOW-epoch windows: not a replay"
    record_result "Operation nullifier replay dropped" "SKIP"
    SKIP_REPLAY=true
fi

if [ "${SKIP_REPLAY:-false}" != true ]; then
# The epoch can advance between sealing and CheckTx on a fast chain under
# load (code 21: target_epoch must be the current shield epoch). That is the
# submission guard working, not the replay check, so reseal against the
# current epoch and resubmit a bounded number of times.
for attempt in 1 2 3; do
    NEW_PAYLOAD_HEX=$(echo "$NEW_PAYLOAD_B64" | base64 -d | od -An -tx1 | tr -d ' \n')
    REPLAY_SUBMISSION_NULLIFIER=$(openssl rand -hex 32)
    REPLAY_TX_RES=$($BINARY tx shield shielded-exec \
        --exec-mode=1 \
        --encrypted-payload="$NEW_PAYLOAD_HEX" \
        --target-epoch="$REPLAY_EPOCH" \
        --proof="$DUMMY_PROOF_HEX" \
        --merkle-root="$TRUST_ROOT_HEX" \
        --nullifier="$REPLAY_SUBMISSION_NULLIFIER" \
        --rate-limit-nullifier="$RATE_NULLIFIER_HEX" \
        --proof-domain=1 \
        --min-trust-level=0 \
        --from submitter1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000${BOND_DENOM} \
        --gas 500000 \
        -y \
        --output json 2>&1)
    REPLAY_TXHASH=$(echo "$REPLAY_TX_RES" | jq -r '.txhash // empty')
    REPLAY_BROADCAST_CODE=$(echo "$REPLAY_TX_RES" | jq -r '.code // "0"' 2>/dev/null)
    if [ "$REPLAY_BROADCAST_CODE" == "21" ] && [ $attempt -lt 3 ]; then
        echo "  Epoch moved before broadcast (attempt $attempt); resealing against the current epoch"
        build_payload "$OP_NULLIFIER_HEX" "ReplayTest$(date +%s)" "This should be dropped: operation nullifier replay"
        NEW_PAYLOAD_B64="$PAYLOAD_B64"
        REPLAY_EPOCH="$PAYLOAD_EPOCH"
        if [ $((CURRENT_EPOCH / POST_WINDOW)) -ne $((REPLAY_EPOCH / POST_WINDOW)) ]; then
            echo "  Replay now falls in a different $POST_WINDOW-epoch window: not a replay"
            REPLAY_BROADCAST_CODE="window"
            break
        fi
        continue
    fi
    break
done
if [ "$REPLAY_BROADCAST_CODE" == "window" ]; then
    record_result "Operation nullifier replay dropped" "SKIP"
    SKIP_REPLAY=true
fi
fi

if [ "${SKIP_REPLAY:-false}" != true ]; then

if [ -z "$REPLAY_TXHASH" ] || [ "$REPLAY_BROADCAST_CODE" != "0" ]; then
    echo "  Replay submission unexpectedly refused at broadcast (code $REPLAY_BROADCAST_CODE): $(echo "$REPLAY_TX_RES" | jq -r '.raw_log // empty' | head -c 200)"
    record_result "Operation nullifier replay dropped" "FAIL"
else
    sleep 6
    REPLAY_RESULT=$(wait_for_tx $REPLAY_TXHASH)
    REPLAY_CODE=$(echo "$REPLAY_RESULT" | jq -r '.code')

    if [ "$REPLAY_CODE" != "0" ]; then
        echo "  Replay submission failed on-chain (code $REPLAY_CODE): $(echo "$REPLAY_RESULT" | jq -r '.raw_log // "unknown"' | head -c 200)"
        record_result "Operation nullifier replay dropped" "FAIL"
    else
        echo "  Replay queued (nullifier sealed until decryption); waiting for its batch to drop it..."
        EPOCH_INTERVAL=$($BINARY query shield params --output json 2>/dev/null | jq -r '.params.shield_epoch_interval // "10"')
        MAX_WAIT_BLOCKS=$((EPOCH_INTERVAL * 3 + 10))
        START_H=$(get_block_height)
        REPLAY_PENDING="1"
        while :; do
            CUR_H=$(get_block_height)
            if [ $((CUR_H - START_H)) -ge "$MAX_WAIT_BLOCKS" ]; then
                break
            fi
            REPLAY_PENDING=$($BINARY query shield pending-op-count --output json 2>/dev/null | jq -r '.count // "0"')
            if [ "$REPLAY_PENDING" == "0" ]; then
                break
            fi
            sleep 2
        done
        REPLAY_POSTS_AFTER=$($BINARY query blog list-post --output json 2>/dev/null | jq -r '[.post[]? | select(.title | startswith("ReplayTest"))] | length' 2>/dev/null || echo 0)
        REPLAY_POSTS=$(( ${REPLAY_POSTS_AFTER:-0} - ${REPLAY_POSTS_BEFORE:-0} ))
        echo "  Pending after wait: $REPLAY_PENDING; new posts from the replay: $REPLAY_POSTS"
        if [ "$REPLAY_PENDING" == "0" ] && [ "$REPLAY_POSTS" == "0" ]; then
            echo "  Replay dropped at execution (operation nullifier already used) — EXPECTED"
            record_result "Operation nullifier replay dropped" "PASS"
        else
            echo "  WARNING: replayed nullifier produced a post or never executed"
            record_result "Operation nullifier replay dropped" "FAIL"
        fi
    fi
fi

fi

# =========================================================================
# PART 11: Test submission with wrong target epoch
# =========================================================================
echo "--- PART 11: Wrong target epoch rejection ---"

# Submit with a wrong epoch number
WRONG_EPOCH=$((REPLAY_EPOCH + 999))
WRONG_NULLIFIER_HEX=$(openssl rand -hex 32)
WRONG_RATE_HEX=$(openssl rand -hex 32)

WRONG_TX_RES=$($BINARY tx shield shielded-exec \
    --exec-mode=1 \
    --encrypted-payload="$ENCRYPTED_PAYLOAD_HEX" \
    --target-epoch="$WRONG_EPOCH" \
    --proof="$DUMMY_PROOF_HEX" \
    --merkle-root="$TRUST_ROOT_HEX" \
    --nullifier="$WRONG_NULLIFIER_HEX" \
    --rate-limit-nullifier="$WRONG_RATE_HEX" \
    --proof-domain=1 \
    --min-trust-level=0 \
    --from submitter1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    --gas 500000 \
    -y \
    --output json 2>&1)

# ErrInvalidTargetEpoch is shield code 21; the ante handler returns it in
# CheckTx (broadcast code) before anything is paid.
WRONG_TXHASH=$(echo "$WRONG_TX_RES" | jq -r '.txhash // empty')
WRONG_CODE=$(echo "$WRONG_TX_RES" | jq -r '.code // empty')
if [ "$WRONG_CODE" == "0" ] && [ -n "$WRONG_TXHASH" ]; then
    sleep 6
    WRONG_CODE=$(wait_for_tx $WRONG_TXHASH | jq -r '.code // "0"')
fi
if [ "$WRONG_CODE" == "21" ]; then
    echo "  Wrong epoch tx rejected with code 21 (target_epoch must be the current shield epoch)"
    record_result "Wrong target epoch rejected" "PASS"
else
    echo "  Expected code 21, got '${WRONG_CODE:-none}': $(echo "$WRONG_TX_RES" | head -c 200)"
    record_result "Wrong target epoch rejected" "FAIL"
fi

# =========================================================================
# PART 12: Test submission with empty encrypted payload
# =========================================================================
echo "--- PART 12: Empty encrypted payload rejection ---"

EMPTY_NULLIFIER_HEX=$(openssl rand -hex 32)
EMPTY_RATE_HEX=$(openssl rand -hex 32)

EMPTY_TX_RES=$($BINARY tx shield shielded-exec \
    --exec-mode=1 \
    --target-epoch="$($BINARY query shield shield-epoch --output json 2>/dev/null | jq -r '.epoch_state.current_epoch // "0"')" \
    --proof="$DUMMY_PROOF_HEX" \
    --merkle-root="$TRUST_ROOT_HEX" \
    --nullifier="$EMPTY_NULLIFIER_HEX" \
    --rate-limit-nullifier="$EMPTY_RATE_HEX" \
    --proof-domain=1 \
    --min-trust-level=0 \
    --from submitter1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    --gas 500000 \
    -y \
    --output json 2>&1)

# ErrMissingEncryptedPayload is shield code 19.
EMPTY_TXHASH=$(echo "$EMPTY_TX_RES" | jq -r '.txhash // empty')
EMPTY_CODE=$(echo "$EMPTY_TX_RES" | jq -r '.code // empty')
if [ "$EMPTY_CODE" == "0" ] && [ -n "$EMPTY_TXHASH" ]; then
    sleep 6
    EMPTY_CODE=$(wait_for_tx $EMPTY_TXHASH | jq -r '.code // "0"')
fi
if [ "$EMPTY_CODE" == "19" ]; then
    echo "  Empty payload tx rejected with code 19 (encrypted_payload is required)"
    record_result "Empty encrypted payload rejected" "PASS"
else
    echo "  Expected code 19, got '${EMPTY_CODE:-none}': $(echo "$EMPTY_TX_RES" | head -c 200)"
    record_result "Empty encrypted payload rejected" "FAIL"
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
echo "  Tests Skipped: $SKIP_COUNT"
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
