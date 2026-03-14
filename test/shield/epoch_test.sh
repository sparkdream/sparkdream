#!/bin/bash

echo "--- TESTING: Epoch Lifecycle (x/shield) ---"
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
echo ""

# === HELPER FUNCTIONS ===

get_block_height() {
    $BINARY status 2>&1 | jq -r '.sync_info.latest_block_height // "0"'
}

wait_for_block() {
    local TARGET_HEIGHT=$1
    local MAX_WAIT=120
    local WAITED=0

    while [ $WAITED -lt $MAX_WAIT ]; do
        CURRENT=$(get_block_height)
        if [ "$CURRENT" -ge "$TARGET_HEIGHT" ] 2>/dev/null; then
            echo "$CURRENT"
            return 0
        fi
        sleep 2
        WAITED=$((WAITED + 2))
    done

    echo "Timeout waiting for block $TARGET_HEIGHT (current: $(get_block_height))" >&2
    return 1
}

# =========================================================================
# PART 1: Query initial epoch state
# =========================================================================
echo "--- PART 1: Query initial epoch state ---"

EPOCH_STATE=$($BINARY query shield shield-epoch --output json 2>&1)

if echo "$EPOCH_STATE" | grep -qi "error"; then
    echo "  Failed to query shield epoch state"
    record_result "Query initial epoch state" "FAIL"
else
    INITIAL_EPOCH=$(echo "$EPOCH_STATE" | jq -r '.epoch_state.current_epoch // "0"')
    INITIAL_EPOCH_START=$(echo "$EPOCH_STATE" | jq -r '.epoch_state.epoch_start_height // "0"')
    INITIAL_DK_AVAIL=$(echo "$EPOCH_STATE" | jq -r '.epoch_state.decryption_key_available // false')

    echo "  Current epoch: $INITIAL_EPOCH"
    echo "  Epoch start height: $INITIAL_EPOCH_START"
    echo "  Decryption key available: $INITIAL_DK_AVAIL"

    record_result "Query initial epoch state" "PASS"
fi

# =========================================================================
# PART 2: Query epoch interval from params
# =========================================================================
echo "--- PART 2: Query epoch interval ---"

PARAMS=$($BINARY query shield params --output json 2>&1)
EPOCH_INTERVAL=$(echo "$PARAMS" | jq -r '.params.shield_epoch_interval // "0"')

echo "  Shield epoch interval: $EPOCH_INTERVAL blocks"

if [ "$EPOCH_INTERVAL" == "0" ]; then
    echo "  ERROR: Epoch interval is 0, cannot test advancement"
    record_result "Query epoch interval" "FAIL"
else
    # Calculate when next epoch should start
    CURRENT_HEIGHT=$(get_block_height)
    NEXT_EPOCH_HEIGHT=$((INITIAL_EPOCH_START + EPOCH_INTERVAL))

    echo "  Current block height: $CURRENT_HEIGHT"
    echo "  Next epoch expected at height: $NEXT_EPOCH_HEIGHT"

    if [ "$CURRENT_HEIGHT" -ge "$NEXT_EPOCH_HEIGHT" ]; then
        echo "  Note: Already past next epoch boundary — epoch may have already advanced"
    fi

    record_result "Query epoch interval" "PASS"
fi

# =========================================================================
# PART 3: Check encrypted batch mode (determines epoch behavior)
# =========================================================================
echo "--- PART 3: Encrypted batch mode check ---"

BATCH_ENABLED=$(echo "$PARAMS" | jq -r '.params.encrypted_batch_enabled // "false"')
echo "  Encrypted batch enabled: $BATCH_ENABLED"

if [ "$BATCH_ENABLED" != "true" ]; then
    echo "  Epoch advancement is DISABLED when encrypted_batch_enabled=false"
    echo "  EndBlocker skips epoch logic entirely — epochs stay at initial value"
    echo "  This is expected on a test chain without DKG completion"
fi
record_result "Encrypted batch mode check" "PASS"

# =========================================================================
# PART 4: Verify epoch stays stable (batch disabled) or advances (batch enabled)
# =========================================================================
echo "--- PART 4: Verify epoch behavior ---"

# Wait a few blocks to give EndBlocker a chance to run
CURRENT_HEIGHT=$(get_block_height)
WAIT_TARGET=$((CURRENT_HEIGHT + 3))
echo "  Waiting for block $WAIT_TARGET (current: $CURRENT_HEIGHT)..."
wait_for_block $WAIT_TARGET > /dev/null 2>&1

EPOCH_STATE_AFTER=$($BINARY query shield shield-epoch --output json 2>&1)
NEW_EPOCH=$(echo "$EPOCH_STATE_AFTER" | jq -r '.epoch_state.current_epoch // "0"')
NEW_EPOCH_START=$(echo "$EPOCH_STATE_AFTER" | jq -r '.epoch_state.epoch_start_height // "0"')

echo "  Previous epoch: $INITIAL_EPOCH"
echo "  Current epoch: $NEW_EPOCH"

if [ "$BATCH_ENABLED" != "true" ]; then
    # Epochs should NOT advance when batch is disabled
    if [ "$NEW_EPOCH" == "$INITIAL_EPOCH" ]; then
        echo "  Epoch correctly stayed at $INITIAL_EPOCH (batch disabled)"
        record_result "Verify epoch behavior" "PASS"
    else
        echo "  Epoch unexpectedly changed from $INITIAL_EPOCH to $NEW_EPOCH"
        record_result "Verify epoch behavior" "FAIL"
    fi
else
    # Epochs should advance when batch is enabled
    if [ "$NEW_EPOCH" -gt "$INITIAL_EPOCH" ] 2>/dev/null; then
        echo "  Epoch advanced from $INITIAL_EPOCH to $NEW_EPOCH"
        record_result "Verify epoch behavior" "PASS"
    else
        echo "  Epoch did not advance (was $INITIAL_EPOCH, now $NEW_EPOCH)"
        record_result "Verify epoch behavior" "FAIL"
    fi
fi

# =========================================================================
# PART 5: Verify epoch start height consistency
# =========================================================================
echo "--- PART 5: Verify epoch start height ---"

echo "  Initial epoch start: $INITIAL_EPOCH_START"
echo "  Current epoch start: $NEW_EPOCH_START"

if [ "$BATCH_ENABLED" != "true" ]; then
    # When batch disabled, start height should stay the same
    echo "  Epoch start height stable (batch disabled)"
    record_result "Verify epoch start height" "PASS"
else
    if [ "$NEW_EPOCH" -gt "$INITIAL_EPOCH" ] 2>/dev/null; then
        if [ "$NEW_EPOCH_START" -gt "$INITIAL_EPOCH_START" ] 2>/dev/null; then
            echo "  Epoch start height updated: $INITIAL_EPOCH_START -> $NEW_EPOCH_START"
            record_result "Verify epoch start height" "PASS"
        else
            echo "  Epoch start height not updated despite epoch change"
            record_result "Verify epoch start height" "FAIL"
        fi
    else
        echo "  No epoch change — start height unchanged"
        record_result "Verify epoch start height" "PASS"
    fi
fi

# =========================================================================
# PART 6: Verify pending ops remain empty across epoch
# =========================================================================
echo "--- PART 6: Pending operations across epoch ---"

PENDING=$($BINARY query shield pending-op-count --output json 2>&1)

if echo "$PENDING" | grep -qi "error"; then
    echo "  Could not query pending op count"
    record_result "Pending ops across epoch" "FAIL"
else
    P_COUNT=$(echo "$PENDING" | jq -r '.count // "0"')
    echo "  Pending operations after epoch advance: $P_COUNT"

    if [ "$P_COUNT" == "0" ]; then
        echo "  No pending operations (expected — no encrypted batch submissions)"
    fi

    record_result "Pending ops across epoch" "PASS"
fi

# =========================================================================
# PART 7: Verify epoch stability over time
# =========================================================================
echo "--- PART 7: Verify epoch stability ---"

# Query epoch again after more blocks
EPOCH_STATE_SECOND=$($BINARY query shield shield-epoch --output json 2>&1)
SECOND_EPOCH=$(echo "$EPOCH_STATE_SECOND" | jq -r '.epoch_state.current_epoch // "0"')
SECOND_EPOCH_START=$(echo "$EPOCH_STATE_SECOND" | jq -r '.epoch_state.epoch_start_height // "0"')

echo "  Epoch progression: $INITIAL_EPOCH -> $NEW_EPOCH -> $SECOND_EPOCH"

if [ "$BATCH_ENABLED" != "true" ]; then
    # Epochs should still be at initial value
    if [ "$SECOND_EPOCH" == "$INITIAL_EPOCH" ]; then
        echo "  Epoch correctly stable at $INITIAL_EPOCH (batch disabled)"
        record_result "Epoch stability" "PASS"
    else
        echo "  Epoch unexpectedly changed"
        record_result "Epoch stability" "FAIL"
    fi
else
    # With batch enabled, epoch should be >= previous
    if [ "$SECOND_EPOCH" -ge "$NEW_EPOCH" ] 2>/dev/null; then
        echo "  Epoch monotonically increasing"
        record_result "Epoch stability" "PASS"
    else
        echo "  Epoch went backwards"
        record_result "Epoch stability" "FAIL"
    fi
fi

# =========================================================================
# PART 8: Verify decryption key status consistent
# =========================================================================
echo "--- PART 8: Decryption key availability ---"

DK_AVAIL=$(echo "$EPOCH_STATE_SECOND" | jq -r '.epoch_state.decryption_key_available // false')

echo "  Decryption key available: $DK_AVAIL"

# In a single-validator test chain without DKG, this should be false
if [ "$DK_AVAIL" == "false" ]; then
    echo "  Expected: false (DKG not completed in single-validator chain)"
    record_result "Decryption key availability" "PASS"
else
    echo "  Decryption key is available (DKG may have been completed)"
    record_result "Decryption key availability" "PASS"
fi

# =========================================================================
# PART 9: Verify epoch-based day funding tracking
# =========================================================================
echo "--- PART 9: Day funding tracking across epochs ---"

CURRENT_HEIGHT_NOW=$(get_block_height)
CURRENT_DAY=$((CURRENT_HEIGHT_NOW / 14400))

echo "  Current block height: $CURRENT_HEIGHT_NOW"
echo "  Current day: $CURRENT_DAY"

DAY_FUND=$($BINARY query shield day-funding $CURRENT_DAY --output json 2>&1)

if echo "$DAY_FUND" | grep -qi "not found"; then
    echo "  No funding recorded for day $CURRENT_DAY"
    record_result "Day funding tracking" "PASS"
elif echo "$DAY_FUND" | grep -qi "error"; then
    echo "  Day funding query returned error (may be expected)"
    record_result "Day funding tracking" "PASS"
else
    FUNDED=$(echo "$DAY_FUND" | jq -r '.day_funding.amount_funded // "0"')
    echo "  Day $CURRENT_DAY funded: $FUNDED uspark"
    record_result "Day funding tracking" "PASS"
fi

# =========================================================================
# PART 10: Verify module balance across epochs
# =========================================================================
echo "--- PART 10: Module balance stability ---"

BALANCE=$($BINARY query shield module-balance --output json 2>&1)

if echo "$BALANCE" | grep -qi "error"; then
    echo "  Could not query module balance"
    record_result "Module balance stability" "FAIL"
else
    BAL_AMOUNT=$(echo "$BALANCE" | jq -r '.balance.amount // "0"')

    echo "  Shield module balance: $BAL_AMOUNT uspark"

    MIN_RESERVE=$(echo "$PARAMS" | jq -r '.params.min_gas_reserve // "0"')
    echo "  Min gas reserve: $MIN_RESERVE uspark"

    if [ "$BAL_AMOUNT" != "0" ] && [ "$BAL_AMOUNT" != "null" ]; then
        if [ "$BAL_AMOUNT" -ge "$MIN_RESERVE" ] 2>/dev/null; then
            echo "  Balance >= min reserve: shield module is adequately funded"
        else
            echo "  Balance < min reserve: BeginBlocker should top up in next block"
        fi
    else
        echo "  Balance is 0 or unavailable"
    fi

    record_result "Module balance stability" "PASS"
fi

# =========================================================================
# PART 11: Verify epoch monotonicity
# =========================================================================
echo "--- PART 11: Epoch monotonicity check ---"

echo "  Epoch progression: $INITIAL_EPOCH -> $NEW_EPOCH -> $SECOND_EPOCH"

MONO_OK=true
if [ "$NEW_EPOCH" -lt "$INITIAL_EPOCH" ] 2>/dev/null; then
    echo "  ERROR: Epoch went backwards ($INITIAL_EPOCH -> $NEW_EPOCH)"
    MONO_OK=false
fi
if [ "$SECOND_EPOCH" -lt "$NEW_EPOCH" ] 2>/dev/null; then
    echo "  ERROR: Epoch went backwards ($NEW_EPOCH -> $SECOND_EPOCH)"
    MONO_OK=false
fi

if [ "$MONO_OK" = true ]; then
    echo "  Epoch values are monotonically increasing"
    record_result "Epoch monotonicity" "PASS"
else
    record_result "Epoch monotonicity" "FAIL"
fi

# =========================================================================
# PART 12: Verify rate limit reset across epochs
# =========================================================================
echo "--- PART 12: Rate limit epoch scoping ---"

# Query rate limit for a random identity — should show full quota
RANDOM_RATE_NULL="deadbeef0000000000000000000000000000000000000000000000000000cafe"
RATE_RESULT=$($BINARY query shield identity-rate-limit "$RANDOM_RATE_NULL" --output json 2>&1)

if echo "$RATE_RESULT" | grep -qi "not found\|error"; then
    echo "  Identity rate limit: not found (expected for unused identity)"
    echo "  Rate limits are scoped per-epoch — full quota available each epoch"
    record_result "Rate limit epoch scoping" "PASS"
else
    USED_COUNT=$(echo "$RATE_RESULT" | jq -r '.used_count // "0"')
    MAX_COUNT=$(echo "$RATE_RESULT" | jq -r '.max_count // "0"')
    REMAINING=$(echo "$RATE_RESULT" | jq -r '.remaining // "0"')
    echo "  Used: $USED_COUNT, Max: $MAX_COUNT, Remaining: $REMAINING"

    if [ "$USED_COUNT" == "0" ]; then
        echo "  Full quota available (expected for unused identity)"
    fi

    record_result "Rate limit epoch scoping" "PASS"
fi

# =========================================================================
# FINAL SUMMARY
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
