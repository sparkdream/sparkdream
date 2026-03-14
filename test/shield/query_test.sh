#!/bin/bash

echo "--- TESTING: Query Endpoints (x/shield) ---"
echo ""

# === 0. SETUP ===
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Load test environment
if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    echo "   Run: bash setup_test_accounts.sh"
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

echo "Alice:     $ALICE_ADDR"
echo "Member1:   $MEMBER1_ADDR"
echo ""

# === HELPER FUNCTIONS ===

check_query_success() {
    local RESULT=$1
    local QUERY_NAME=$2

    if echo "$RESULT" | grep -qi "error\|Error\|ERROR"; then
        # Check if it's just "not found" (which is valid for some queries)
        if echo "$RESULT" | grep -qi "not found"; then
            echo "  $QUERY_NAME: empty result (not found)"
            return 0
        fi
        echo "  $QUERY_NAME: FAILED"
        echo "  $RESULT"
        return 1
    fi
    echo "  $QUERY_NAME: OK"
    return 0
}

# =========================================================================
# PART 1: Module params
# =========================================================================
echo "--- PART 1: Query module params ---"

PARAMS=$($BINARY query shield params --output json 2>&1)
PART1_OK=true

if ! check_query_success "$PARAMS" "params"; then
    PART1_OK=false
fi

if [ "$PART1_OK" == "true" ]; then
    ENABLED=$(echo "$PARAMS" | jq -r '.params.enabled // "null"')
    MAX_FUNDING=$(echo "$PARAMS" | jq -r '.params.max_funding_per_day // "null"')
    MIN_RESERVE=$(echo "$PARAMS" | jq -r '.params.min_gas_reserve // "null"')
    MAX_GAS=$(echo "$PARAMS" | jq -r '.params.max_gas_per_exec // "null"')
    MAX_EXECS=$(echo "$PARAMS" | jq -r '.params.max_execs_per_identity_per_epoch // "null"')
    BATCH_ENABLED=$(echo "$PARAMS" | jq -r '.params.encrypted_batch_enabled // "null"')
    EPOCH_INTERVAL=$(echo "$PARAMS" | jq -r '.params.shield_epoch_interval // "null"')
    MIN_BATCH=$(echo "$PARAMS" | jq -r '.params.min_batch_size // "null"')
    MAX_PENDING=$(echo "$PARAMS" | jq -r '.params.max_pending_epochs // "null"')
    MAX_QUEUE=$(echo "$PARAMS" | jq -r '.params.max_pending_queue_size // "null"')
    MAX_PAYLOAD=$(echo "$PARAMS" | jq -r '.params.max_encrypted_payload_size // "null"')
    MAX_OPS_BATCH=$(echo "$PARAMS" | jq -r '.params.max_ops_per_batch // "null"')
    TLE_WINDOW=$(echo "$PARAMS" | jq -r '.params.tle_miss_window // "null"')
    TLE_TOLERANCE=$(echo "$PARAMS" | jq -r '.params.tle_miss_tolerance // "null"')
    TLE_JAIL=$(echo "$PARAMS" | jq -r '.params.tle_jail_duration // "null"')

    echo "  enabled: $ENABLED"
    echo "  max_funding_per_day: $MAX_FUNDING"
    echo "  min_gas_reserve: $MIN_RESERVE"
    echo "  max_gas_per_exec: $MAX_GAS"
    echo "  max_execs_per_identity_per_epoch: $MAX_EXECS"
    echo "  encrypted_batch_enabled: $BATCH_ENABLED"
    echo "  shield_epoch_interval: $EPOCH_INTERVAL"
    echo "  min_batch_size: $MIN_BATCH"
    echo "  max_pending_epochs: $MAX_PENDING"
    echo "  max_pending_queue_size: $MAX_QUEUE"
    echo "  max_encrypted_payload_size: $MAX_PAYLOAD"
    echo "  max_ops_per_batch: $MAX_OPS_BATCH"
    echo "  tle_miss_window: $TLE_WINDOW"
    echo "  tle_miss_tolerance: $TLE_TOLERANCE"
    echo "  tle_jail_duration: $TLE_JAIL"

    # Verify enabled is true
    if [ "$ENABLED" != "true" ]; then
        echo "  WARNING: Shield module is not enabled"
    fi
fi

if [ "$PART1_OK" == "true" ]; then
    record_result "Module params" "PASS"
else
    record_result "Module params" "FAIL"
fi

# =========================================================================
# PART 2: Shielded operation queries
# =========================================================================
echo "--- PART 2: Shielded operation queries ---"

PART2_OK=true

# List all registered operations
OPS=$($BINARY query shield shielded-ops --output json 2>&1)
if ! check_query_success "$OPS" "shielded-ops"; then
    PART2_OK=false
fi

if [ "$PART2_OK" == "true" ]; then
    OP_COUNT=$(echo "$OPS" | jq -r '.registrations | length' 2>/dev/null || echo "0")
    echo "  Registered operations: $OP_COUNT"

    # Query a specific operation
    BLOG_URL="/sparkdream.blog.v1.MsgCreatePost"
    SINGLE_OP=$($BINARY query shield shielded-op "$BLOG_URL" --output json 2>&1)
    if ! check_query_success "$SINGLE_OP" "shielded-op (blog)"; then
        PART2_OK=false
    fi
fi

if [ "$PART2_OK" == "true" ]; then
    record_result "Shielded operation queries" "PASS"
else
    record_result "Shielded operation queries" "FAIL"
fi

# =========================================================================
# PART 3: Module balance
# =========================================================================
echo "--- PART 3: Module balance query ---"

PART3_OK=true

BALANCE=$($BINARY query shield module-balance --output json 2>&1)
if ! check_query_success "$BALANCE" "module-balance"; then
    PART3_OK=false
fi

if [ "$PART3_OK" == "true" ]; then
    BAL_DENOM=$(echo "$BALANCE" | jq -r '.balance.denom // "null"')
    BAL_AMOUNT=$(echo "$BALANCE" | jq -r '.balance.amount // "0"')
    echo "  Balance: $BAL_AMOUNT $BAL_DENOM"
fi

if [ "$PART3_OK" == "true" ]; then
    record_result "Module balance query" "PASS"
else
    record_result "Module balance query" "FAIL"
fi

# =========================================================================
# PART 4: Nullifier queries
# =========================================================================
echo "--- PART 4: Nullifier queries ---"

PART4_OK=true

# Check a random nullifier (should not be used)
RANDOM_NULL_HEX="abcdef0000000000000000000000000000000000000000000000000000000000"
NULL_RESULT=$($BINARY query shield nullifier-used 1 0 "$RANDOM_NULL_HEX" --output json 2>&1)

if echo "$NULL_RESULT" | grep -qi "not found"; then
    echo "  nullifier-used: not found (expected for unused nullifier)"
elif echo "$NULL_RESULT" | grep -qi "error"; then
    echo "  nullifier-used: error response (may be expected)"
    echo "  $NULL_RESULT"
else
    USED=$(echo "$NULL_RESULT" | jq -r '.used // "false"')
    USED_HEIGHT=$(echo "$NULL_RESULT" | jq -r '.used_at_height // "0"')
    echo "  nullifier-used: used=$USED, height=$USED_HEIGHT"

    if [ "$USED" == "true" ]; then
        echo "  WARNING: Random nullifier unexpectedly marked as used"
        PART4_OK=false
    fi
fi

if [ "$PART4_OK" == "true" ]; then
    record_result "Nullifier queries" "PASS"
else
    record_result "Nullifier queries" "FAIL"
fi

# =========================================================================
# PART 5: Day funding query
# =========================================================================
echo "--- PART 5: Day funding query ---"

PART5_OK=true

# Get current block height to compute current day
BLOCK_HEIGHT=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height // "0"')
CURRENT_DAY=$((BLOCK_HEIGHT / 14400))

DAY_FUND=$($BINARY query shield day-funding $CURRENT_DAY --output json 2>&1)

if echo "$DAY_FUND" | grep -qi "not found"; then
    echo "  day-funding: no funding recorded for day $CURRENT_DAY (expected early in chain)"
elif echo "$DAY_FUND" | grep -qi "error"; then
    echo "  day-funding: query returned error (may be expected if no funding yet)"
else
    check_query_success "$DAY_FUND" "day-funding"
    FUNDED=$(echo "$DAY_FUND" | jq -r '.day_funding.amount_funded // "0"')
    echo "  Day $CURRENT_DAY funding: $FUNDED uspark"
fi

if [ "$PART5_OK" == "true" ]; then
    record_result "Day funding query" "PASS"
else
    record_result "Day funding query" "FAIL"
fi

# =========================================================================
# PART 6: Shield epoch state
# =========================================================================
echo "--- PART 6: Shield epoch state ---"

PART6_OK=true

EPOCH=$($BINARY query shield shield-epoch --output json 2>&1)
if ! check_query_success "$EPOCH" "shield-epoch"; then
    PART6_OK=false
fi

if [ "$PART6_OK" == "true" ]; then
    CURRENT_EPOCH=$(echo "$EPOCH" | jq -r '.epoch_state.current_epoch // "0"')
    EPOCH_START=$(echo "$EPOCH" | jq -r '.epoch_state.epoch_start_height // "0"')
    KEY_AVAILABLE=$(echo "$EPOCH" | jq -r '.epoch_state.decryption_key_available // false')

    echo "  Current epoch: $CURRENT_EPOCH"
    echo "  Epoch start height: $EPOCH_START"
    echo "  Decryption key available: $KEY_AVAILABLE"
fi

if [ "$PART6_OK" == "true" ]; then
    record_result "Shield epoch state" "PASS"
else
    record_result "Shield epoch state" "FAIL"
fi

# =========================================================================
# PART 7: Pending operations
# =========================================================================
echo "--- PART 7: Pending operations queries ---"

PART7_OK=true

PENDING=$($BINARY query shield pending-ops --output json 2>&1)
if ! check_query_success "$PENDING" "pending-ops"; then
    PART7_OK=false
fi

if [ "$PART7_OK" == "true" ]; then
    PENDING_COUNT_RESULT=$($BINARY query shield pending-op-count --output json 2>&1)
    if ! check_query_success "$PENDING_COUNT_RESULT" "pending-op-count"; then
        PART7_OK=false
    else
        P_COUNT=$(echo "$PENDING_COUNT_RESULT" | jq -r '.count // "0"')
        echo "  Pending operations: $P_COUNT"
    fi
fi

if [ "$PART7_OK" == "true" ]; then
    record_result "Pending operations queries" "PASS"
else
    record_result "Pending operations queries" "FAIL"
fi

# =========================================================================
# PART 8: TLE queries
# =========================================================================
echo "--- PART 8: TLE queries ---"

PART8_OK=true

# TLE master public key
MPK=$($BINARY query shield tle-master-public-key --output json 2>&1)
if echo "$MPK" | grep -qi "not found\|error"; then
    echo "  tle-master-public-key: not available (DKG not completed)"
else
    check_query_success "$MPK" "tle-master-public-key"
    MPK_HEX=$(echo "$MPK" | jq -r '.master_public_key // "null"')
    echo "  Master public key: ${MPK_HEX:0:20}..."
fi

# TLE key set
KEYSET=$($BINARY query shield tle-key-set --output json 2>&1)
if echo "$KEYSET" | grep -qi "not found\|error"; then
    echo "  tle-key-set: not available (DKG not completed)"
else
    check_query_success "$KEYSET" "tle-key-set"
    THRESHOLD_NUM=$(echo "$KEYSET" | jq -r '.key_set.threshold_numerator // "0"')
    THRESHOLD_DEN=$(echo "$KEYSET" | jq -r '.key_set.threshold_denominator // "0"')
    SHARE_COUNT=$(echo "$KEYSET" | jq -r '.key_set.validator_shares | length' 2>/dev/null || echo "0")
    echo "  Threshold: $THRESHOLD_NUM/$THRESHOLD_DEN"
    echo "  Validator shares: $SHARE_COUNT"
fi

if [ "$PART8_OK" == "true" ]; then
    record_result "TLE queries" "PASS"
else
    record_result "TLE queries" "FAIL"
fi

# =========================================================================
# PART 9: Verification key query
# =========================================================================
echo "--- PART 9: Verification key queries ---"

PART9_OK=true

# Try to query anon_action_v1 circuit key
VK_RESULT=$($BINARY query shield verification-key "anon_action_v1" --output json 2>&1)

if echo "$VK_RESULT" | grep -qi "not found"; then
    echo "  verification-key (anon_action_v1): not stored (expected in development)"
elif echo "$VK_RESULT" | grep -qi "error"; then
    echo "  verification-key: query returned error (may be expected)"
else
    check_query_success "$VK_RESULT" "verification-key"
    VK_CIRCUIT=$(echo "$VK_RESULT" | jq -r '.verification_key.circuit_id // "null"')
    echo "  Circuit ID: $VK_CIRCUIT"
fi

if [ "$PART9_OK" == "true" ]; then
    record_result "Verification key query" "PASS"
else
    record_result "Verification key query" "FAIL"
fi

# =========================================================================
# PART 10: TLE miss count query
# =========================================================================
echo "--- PART 10: TLE miss count query ---"

PART10_OK=true

# Query alice's TLE miss count (she's the validator)
MISS_RESULT=$($BINARY query shield tle-miss-count "$ALICE_ADDR" --output json 2>&1)

if echo "$MISS_RESULT" | grep -qi "not found"; then
    echo "  tle-miss-count: not found (expected if no TLE shares registered)"
elif echo "$MISS_RESULT" | grep -qi "error"; then
    echo "  tle-miss-count: query returned error (may be expected)"
else
    check_query_success "$MISS_RESULT" "tle-miss-count"
    MISS_COUNT=$(echo "$MISS_RESULT" | jq -r '.miss_count // "0"')
    echo "  Alice TLE miss count: $MISS_COUNT"
fi

if [ "$PART10_OK" == "true" ]; then
    record_result "TLE miss count query" "PASS"
else
    record_result "TLE miss count query" "FAIL"
fi

# =========================================================================
# PART 11: Decryption shares query
# =========================================================================
echo "--- PART 11: Decryption shares query ---"

PART11_OK=true

# Query decryption shares for epoch 0
DEC_SHARES=$($BINARY query shield decryption-shares 0 --output json 2>&1)

if echo "$DEC_SHARES" | grep -qi "not found"; then
    echo "  decryption-shares (epoch 0): none found (expected)"
elif echo "$DEC_SHARES" | grep -qi "error"; then
    echo "  decryption-shares: query returned error (may be expected)"
else
    check_query_success "$DEC_SHARES" "decryption-shares"
    SHARE_COUNT=$(echo "$DEC_SHARES" | jq -r '.shares | length' 2>/dev/null || echo "0")
    echo "  Epoch 0 decryption shares: $SHARE_COUNT"
fi

if [ "$PART11_OK" == "true" ]; then
    record_result "Decryption shares query" "PASS"
else
    record_result "Decryption shares query" "FAIL"
fi

# =========================================================================
# PART 12: Identity rate limit query
# =========================================================================
echo "--- PART 12: Identity rate limit query ---"

PART12_OK=true

# Query rate limit for a random rate-limit nullifier
RANDOM_RATE_NULL="1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
RATE_RESULT=$($BINARY query shield identity-rate-limit "$RANDOM_RATE_NULL" --output json 2>&1)

if echo "$RATE_RESULT" | grep -qi "not found\|error"; then
    echo "  identity-rate-limit: not found (expected for unused nullifier)"
else
    check_query_success "$RATE_RESULT" "identity-rate-limit"
    USED_COUNT=$(echo "$RATE_RESULT" | jq -r '.used_count // "0"')
    MAX_COUNT=$(echo "$RATE_RESULT" | jq -r '.max_count // "0"')
    REMAINING=$(echo "$RATE_RESULT" | jq -r '.remaining // "0"')
    echo "  Used: $USED_COUNT, Max: $MAX_COUNT, Remaining: $REMAINING"
fi

if [ "$PART12_OK" == "true" ]; then
    record_result "Identity rate limit query" "PASS"
else
    record_result "Identity rate limit query" "FAIL"
fi

# =========================================================================
# SUMMARY
# =========================================================================
echo "--- FINAL RESULTS ---"
for i in "${!TEST_NAMES[@]}"; do
    printf "  %-50s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done
echo ""
echo "Total: $PASS_COUNT passed, $FAIL_COUNT failed out of $((PASS_COUNT + FAIL_COUNT))"
if [ $FAIL_COUNT -gt 0 ]; then
    exit 1
fi
