#!/bin/bash

echo "--- TESTING: TLE Query Tests (x/shield) ---"
echo ""
echo "NOTE: TLE validator registration and decryption share submission are done"
echo "      via ABCI vote extensions, not user-submitted transactions."
echo "      This test verifies query endpoints only."
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

echo "Alice:     $ALICE_ADDR"
echo ""

# === RESULT TRACKING ===

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

# =========================================================================
# PART 1: Check shield module status (params query)
# =========================================================================
echo "--- PART 1: Check shield module status ---"

PARAMS=$($BINARY query shield params --output json 2>&1)

if echo "$PARAMS" | jq -e '.params' > /dev/null 2>&1; then
    ENABLED=$(echo "$PARAMS" | jq -r '.params.enabled // "null"')
    BATCH_ENABLED=$(echo "$PARAMS" | jq -r '.params.encrypted_batch_enabled // "null"')
    EPOCH_INTERVAL=$(echo "$PARAMS" | jq -r '.params.shield_epoch_interval // "0"')

    echo "  Enabled: $ENABLED"
    echo "  Encrypted batch enabled: $BATCH_ENABLED"
    echo "  Shield epoch interval: $EPOCH_INTERVAL blocks"

    if [ "$ENABLED" == "true" ]; then
        record_result "Shield module params query" "PASS"
    else
        echo "  Shield module is disabled. Skipping remaining TLE tests."
        record_result "Shield module params query" "PASS"
        echo ""
        echo "--- FINAL RESULTS ---"
        for i in "${!TEST_NAMES[@]}"; do
            printf "  %-50s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
        done
        echo ""
        echo "Total: $PASS_COUNT passed, $FAIL_COUNT failed out of $((PASS_COUNT + FAIL_COUNT))"
        echo "  (TLE tests skipped — shield not enabled)"
        exit 0
    fi
else
    echo "  Failed to query shield params"
    echo "  Response: $PARAMS"
    record_result "Shield module params query" "FAIL"
fi

# =========================================================================
# PART 2: Query current shield epoch state
# =========================================================================
echo "--- PART 2: Query shield epoch state ---"

EPOCH_STATE=$($BINARY query shield shield-epoch --output json 2>&1)

if echo "$EPOCH_STATE" | jq -e '.epoch_state' > /dev/null 2>&1; then
    CURRENT_EPOCH=$(echo "$EPOCH_STATE" | jq -r '.epoch_state.current_epoch // "0"')
    EPOCH_START=$(echo "$EPOCH_STATE" | jq -r '.epoch_state.epoch_start_height // "0"')

    echo "  Current epoch: $CURRENT_EPOCH"
    echo "  Epoch start height: $EPOCH_START"
    record_result "Shield epoch state query" "PASS"
else
    echo "  Failed to query shield epoch state"
    echo "  Response: $EPOCH_STATE"
    CURRENT_EPOCH="0"
    record_result "Shield epoch state query" "FAIL"
fi

# =========================================================================
# PART 3: Query TLE key set (verify empty/present)
# =========================================================================
echo "--- PART 3: Query TLE key set ---"

KEYSET=$($BINARY query shield tle-key-set --output json 2>&1)

if echo "$KEYSET" | jq -e '.' > /dev/null 2>&1; then
    THRESHOLD_NUM=$(echo "$KEYSET" | jq -r '.key_set.threshold_numerator // "0"')
    THRESHOLD_DEN=$(echo "$KEYSET" | jq -r '.key_set.threshold_denominator // "0"')
    SHARE_COUNT=$(echo "$KEYSET" | jq -r '.key_set.validator_shares | length' 2>/dev/null || echo "0")
    MPK=$(echo "$KEYSET" | jq -r '.key_set.master_public_key // ""')

    echo "  Threshold: $THRESHOLD_NUM/$THRESHOLD_DEN"
    echo "  Registered shares: $SHARE_COUNT"
    if [ -n "$MPK" ] && [ "$MPK" != "null" ] && [ "$MPK" != "" ]; then
        echo "  Master public key: ${MPK:0:20}..."
    else
        echo "  Master public key: not yet computed (expected before DKG)"
    fi
    record_result "TLE key set query" "PASS"
else
    # "not found" is also acceptable before DKG
    if echo "$KEYSET" | grep -qi "not found"; then
        echo "  TLE key set: not available (expected before DKG)"
        record_result "TLE key set query" "PASS"
    else
        echo "  Failed to query TLE key set"
        echo "  Response: $KEYSET"
        record_result "TLE key set query" "FAIL"
    fi
fi

# =========================================================================
# PART 4: Query DKG state (should show INACTIVE on single-validator chain)
# =========================================================================
echo "--- PART 4: Query DKG state ---"

DKG_STATE=$($BINARY query shield dkg-state --output json 2>&1)

if echo "$DKG_STATE" | jq -e '.' > /dev/null 2>&1; then
    PHASE=$(echo "$DKG_STATE" | jq -r '.state.phase // .phase // "unknown"')
    ROUND=$(echo "$DKG_STATE" | jq -r '.state.round // .round // "0"')

    echo "  DKG phase: $PHASE"
    echo "  DKG round: $ROUND"

    # On a single-validator chain, DKG should be inactive
    if echo "$PHASE" | grep -qi "inactive\|INACTIVE\|DKG_PHASE_INACTIVE\|0"; then
        echo "  DKG is inactive (expected on single-validator chain)"
    else
        echo "  DKG is in phase: $PHASE"
    fi
    record_result "DKG state query" "PASS"
else
    if echo "$DKG_STATE" | grep -qi "not found"; then
        echo "  DKG state: not found (expected — no DKG initiated)"
        record_result "DKG state query" "PASS"
    else
        echo "  Failed to query DKG state"
        echo "  Response: $DKG_STATE"
        record_result "DKG state query" "FAIL"
    fi
fi

# =========================================================================
# PART 5: Query DKG contributions (should be empty)
# =========================================================================
echo "--- PART 5: Query DKG contributions ---"

DKG_CONTRIBS=$($BINARY query shield dkg-contributions --output json 2>&1)

if echo "$DKG_CONTRIBS" | jq -e '.' > /dev/null 2>&1; then
    CONTRIB_COUNT=$(echo "$DKG_CONTRIBS" | jq -r '.contributions | length' 2>/dev/null || echo "0")
    echo "  DKG contributions: $CONTRIB_COUNT"

    if [ "$CONTRIB_COUNT" == "0" ] || [ "$CONTRIB_COUNT" == "null" ]; then
        echo "  No contributions (expected — no DKG ceremony active)"
    fi
    record_result "DKG contributions query" "PASS"
else
    if echo "$DKG_CONTRIBS" | grep -qi "not found"; then
        echo "  DKG contributions: none (expected — no DKG initiated)"
        record_result "DKG contributions query" "PASS"
    else
        echo "  Failed to query DKG contributions"
        echo "  Response: $DKG_CONTRIBS"
        record_result "DKG contributions query" "FAIL"
    fi
fi

# =========================================================================
# PART 6: Query TLE miss count for alice
# =========================================================================
echo "--- PART 6: Query TLE miss count for alice ---"

MISS_RESULT=$($BINARY query shield tle-miss-count "$ALICE_ADDR" --output json 2>&1)

if echo "$MISS_RESULT" | jq -e '.' > /dev/null 2>&1; then
    MISS_COUNT=$(echo "$MISS_RESULT" | jq -r '.miss_count // "0"')
    echo "  Alice TLE miss count: $MISS_COUNT"
    record_result "TLE miss count query" "PASS"
else
    if echo "$MISS_RESULT" | grep -qi "not found"; then
        echo "  TLE miss count: not found (expected — no TLE participation tracking yet)"
        record_result "TLE miss count query" "PASS"
    else
        echo "  Failed to query TLE miss count"
        echo "  Response: $MISS_RESULT"
        record_result "TLE miss count query" "FAIL"
    fi
fi

# =========================================================================
# PART 7: Query decryption shares for current epoch
# =========================================================================
echo "--- PART 7: Query decryption shares ---"

DEC_SHARES=$($BINARY query shield decryption-shares "$CURRENT_EPOCH" --output json 2>&1)

if echo "$DEC_SHARES" | jq -e '.' > /dev/null 2>&1; then
    SHARE_COUNT=$(echo "$DEC_SHARES" | jq -r '.shares | length' 2>/dev/null || echo "0")
    echo "  Epoch $CURRENT_EPOCH decryption shares: $SHARE_COUNT"

    if [ "$SHARE_COUNT" == "0" ] || [ "$SHARE_COUNT" == "null" ]; then
        echo "  No shares (expected — no DKG completed, threshold not met)"
    fi
    record_result "Decryption shares query" "PASS"
else
    if echo "$DEC_SHARES" | grep -qi "not found"; then
        echo "  No decryption shares for epoch $CURRENT_EPOCH (expected)"
        record_result "Decryption shares query" "PASS"
    else
        echo "  Failed to query decryption shares"
        echo "  Response: $DEC_SHARES"
        record_result "Decryption shares query" "FAIL"
    fi
fi

# =========================================================================
# PART 8: Query TLE master public key (should be empty — no DKG)
# =========================================================================
echo "--- PART 8: Query TLE master public key ---"

MPK_RESULT=$($BINARY query shield tle-master-public-key --output json 2>&1)

if echo "$MPK_RESULT" | jq -e '.' > /dev/null 2>&1; then
    MPK_KEY=$(echo "$MPK_RESULT" | jq -r '.master_public_key // ""')
    if [ -n "$MPK_KEY" ] && [ "$MPK_KEY" != "null" ] && [ "$MPK_KEY" != "" ]; then
        echo "  Master public key available: ${MPK_KEY:0:20}..."
    else
        echo "  Master public key: empty (DKG not completed — expected)"
    fi
    record_result "TLE master public key query" "PASS"
else
    if echo "$MPK_RESULT" | grep -qi "not found"; then
        echo "  Master public key: not available (DKG not completed — expected)"
        record_result "TLE master public key query" "PASS"
    else
        echo "  Failed to query TLE master public key"
        echo "  Response: $MPK_RESULT"
        record_result "TLE master public key query" "FAIL"
    fi
fi

# =========================================================================
# PART 9: Verify pending ops count (should be 0)
# =========================================================================
echo "--- PART 9: Verify pending operations count ---"

PENDING_COUNT=$($BINARY query shield pending-op-count --output json 2>&1)

if echo "$PENDING_COUNT" | jq -e '.' > /dev/null 2>&1; then
    COUNT=$(echo "$PENDING_COUNT" | jq -r '.count // "0"')
    echo "  Pending operations: $COUNT"

    if [ "$COUNT" == "0" ] || [ "$COUNT" == "null" ]; then
        record_result "Pending ops count query" "PASS"
    else
        echo "  WARNING: Expected 0 pending operations, got $COUNT"
        record_result "Pending ops count query" "FAIL"
    fi
else
    echo "  Failed to query pending op count"
    echo "  Response: $PENDING_COUNT"
    record_result "Pending ops count query" "FAIL"
fi

# =========================================================================
# PART 10: Verify TLE params (miss window, tolerance, jail duration, min validators)
# =========================================================================
echo "--- PART 10: Verify TLE params ---"

# Re-use the params already queried in Part 1
TLE_MISS_WINDOW=$(echo "$PARAMS" | jq -r '.params.tle_miss_window // "0"')
TLE_MISS_TOLERANCE=$(echo "$PARAMS" | jq -r '.params.tle_miss_tolerance // "0"')
TLE_JAIL_DURATION=$(echo "$PARAMS" | jq -r '.params.tle_jail_duration // "0"')
MIN_TLE_VALIDATORS=$(echo "$PARAMS" | jq -r '.params.min_tle_validators // "0"')

echo "  TLE miss window:     $TLE_MISS_WINDOW epochs"
echo "  TLE miss tolerance:  $TLE_MISS_TOLERANCE"
echo "  TLE jail duration:   $TLE_JAIL_DURATION seconds"
echo "  Min TLE validators:  $MIN_TLE_VALIDATORS"

ALL_OK=true

if [ "$TLE_MISS_WINDOW" == "0" ] || [ "$TLE_MISS_WINDOW" == "null" ]; then
    echo "  ERROR: tle_miss_window should be > 0"
    ALL_OK=false
fi

if [ "$TLE_MISS_TOLERANCE" == "null" ]; then
    echo "  ERROR: tle_miss_tolerance should be set"
    ALL_OK=false
fi

if [ "$TLE_JAIL_DURATION" == "0" ] || [ "$TLE_JAIL_DURATION" == "null" ]; then
    echo "  ERROR: tle_jail_duration should be > 0"
    ALL_OK=false
fi

# Proto3 omits 0 from JSON; min_tle_validators=0 means DKG won't auto-trigger
# This is valid in a test chain where DKG is not expected
if [ "$MIN_TLE_VALIDATORS" == "0" ] || [ "$MIN_TLE_VALIDATORS" == "null" ]; then
    echo "  min_tle_validators is 0 (DKG auto-trigger disabled — expected in test chain)"
fi

# Verify tolerance <= window
if [ "$TLE_MISS_TOLERANCE" != "null" ] && [ "$TLE_MISS_WINDOW" != "null" ] && \
   [ "$TLE_MISS_TOLERANCE" != "0" ] && [ "$TLE_MISS_WINDOW" != "0" ]; then
    if [ "$TLE_MISS_TOLERANCE" -gt "$TLE_MISS_WINDOW" ] 2>/dev/null; then
        echo "  ERROR: tle_miss_tolerance ($TLE_MISS_TOLERANCE) > tle_miss_window ($TLE_MISS_WINDOW)"
        ALL_OK=false
    else
        echo "  Tolerance <= window: OK"
    fi
fi

if [ "$ALL_OK" == "true" ]; then
    record_result "TLE params verification" "PASS"
else
    record_result "TLE params verification" "FAIL"
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
