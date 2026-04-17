#!/bin/bash

echo "--- TESTING: SEASON MODULE VALIDATION ERROR PATHS ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Source test env if available
if [ -f "$SCRIPT_DIR/.test_env" ]; then
    source "$SCRIPT_DIR/.test_env"
fi

ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

echo "Alice: $ALICE_ADDR"
echo "Bob:   $BOB_ADDR"
echo ""

# ========================================================================
# Result Tracking & Helpers
# ========================================================================
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
    return 1
}

submit_tx_and_wait() {
    local TX_RES="$1"
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        TX_RESULT="$TX_RES"
        return 1
    fi
    local BROADCAST_CODE=$(echo "$TX_RES" | jq -r '.code // "0"')
    if [ "$BROADCAST_CODE" != "0" ]; then
        TX_RESULT="$TX_RES"
        return 0
    fi
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    return 0
}

check_tx_success() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    [ "$CODE" == "0" ]
}

expect_tx_failure() {
    local TX_RES="$1"
    local EXPECTED_ERR="$2"
    local TEST_NAME="$3"

    if ! submit_tx_and_wait "$TX_RES"; then
        if echo "$TX_RES" | grep -qi "$EXPECTED_ERR"; then
            record_result "$TEST_NAME" "PASS"
        else
            echo "  Broadcast rejection did not contain expected error: $EXPECTED_ERR"
            echo "  Response: $(echo "$TX_RES" | head -c 300)"
            record_result "$TEST_NAME" "FAIL"
        fi
        return
    fi

    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        local RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        if echo "$RAW_LOG" | grep -qi "$EXPECTED_ERR"; then
            echo "  Failed as expected (code: $CODE)"
            record_result "$TEST_NAME" "PASS"
        else
            echo "  Failed but unexpected error: $RAW_LOG"
            echo "  Expected: $EXPECTED_ERR"
            record_result "$TEST_NAME" "FAIL"
        fi
    else
        echo "  ERROR: Transaction succeeded when it should have failed!"
        record_result "$TEST_NAME" "FAIL"
    fi
}

# ========================================================================
# TEST 1: Create guild with empty name (ErrGuildNameTooShort)
# ========================================================================
echo "--- TEST 1: Create guild with empty name ---"

TX_RES=$($BINARY tx season create-guild \
    "" "A guild with no name" "false" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "too short\|empty\|invalid\|required" "Create guild with empty name"

# ========================================================================
# TEST 2: Create guild with very long name (ErrGuildNameTooLong)
# ========================================================================
echo "--- TEST 2: Create guild with very long name ---"

LONG_NAME=$(python3 -c "print('X' * 300)" 2>/dev/null || printf '%0.sX' $(seq 1 300))

TX_RES=$($BINARY tx season create-guild \
    "$LONG_NAME" "A guild with a too-long name" "false" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "too long\|exceeds\|invalid\|max" "Create guild with very long name"

# ========================================================================
# TEST 3: Join non-existent guild (ErrGuildNotFound)
# ========================================================================
echo "--- TEST 3: Join non-existent guild ---"

TX_RES=$($BINARY tx season join-guild \
    99999 \
    --from bob \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "not found\|does not exist\|invalid" "Join non-existent guild"

# ========================================================================
# TEST 4: Report own display name (ErrCannotReportOwnDisplayName)
# ========================================================================
echo "--- TEST 4: Report own display name ---"

TX_RES=$($BINARY tx season report-display-name \
    "$ALICE_ADDR" "offensive" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "cannot report own\|own display name\|self" "Report own display name"

# ========================================================================
# TEST 5: Display name too long (ErrDisplayNameTooLong)
# ========================================================================
echo "--- TEST 5: Display name too long ---"

LONG_DISPLAY=$(python3 -c "print('Z' * 200)" 2>/dev/null || printf '%0.sZ' $(seq 1 200))

TX_RES=$($BINARY tx season set-display-name \
    "$LONG_DISPLAY" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "too long\|exceeds\|invalid\|max" "Display name too long"

# ========================================================================
# TEST 6a: Display name contains blocked impersonation term (ErrDisplayNameBlocked)
# ========================================================================
echo "--- TEST 6a: Display name containing 'Admin' is blocked ---"

TX_RES=$($BINARY tx season set-display-name \
    "Admin_Bob" \
    --from bob \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "blocked\|impersonation\|1242" "Display name contains 'Admin'"

# ========================================================================
# TEST 6b: Display name containing 'moderator' is blocked (case-insensitive)
# ========================================================================
echo "--- TEST 6b: Display name 'MODERATOR' is blocked ---"

TX_RES=$($BINARY tx season set-display-name \
    "MODERATOR" \
    --from bob \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "blocked\|impersonation\|1242" "Display name 'MODERATOR' is blocked"

# ========================================================================
# TEST 7: Start non-existent quest (ErrQuestNotFound)
# ========================================================================
echo "--- TEST 7: Start non-existent quest ---"

TX_RES=$($BINARY tx season start-quest \
    99999 \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "not found\|does not exist\|invalid" "Start non-existent quest"

# ========================================================================
# SUMMARY
# ========================================================================
echo "============================================================================"
echo "  SEASON VALIDATION ERROR PATHS TEST SUMMARY"
echo "============================================================================"
echo ""
echo "  Tests Run:    $((PASS_COUNT + FAIL_COUNT))"
echo "  Tests Passed: $PASS_COUNT"
echo "  Tests Failed: $FAIL_COUNT"
echo ""

for i in "${!TEST_NAMES[@]}"; do
    echo "  ${RESULTS[$i]}: ${TEST_NAMES[$i]}"
done

echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL TESTS PASSED <<<"
fi
