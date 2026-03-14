#!/bin/bash

echo "--- TESTING: REP MODULE VALIDATION ERROR PATHS ---"

# ========================================================================
# 0. SETUP
# ========================================================================
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

echo ""
echo "=== TEST ACTORS ==="
echo "Alice:      $ALICE_ADDR"
echo "Bob:        $BOB_ADDR"
echo "Assignee:   $ASSIGNEE_ADDR"
echo "Project:    $TEST_PROJECT_ID"
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
# TEST 1: Invite already-existing member (ErrMemberAlreadyExists)
# ========================================================================
echo "--- TEST 1: Invite already-existing member ---"
echo "  Alice invites Bob, who is already a member..."

TX_RES=$($BINARY tx rep invite-member \
    "$BOB_ADDR" "1000000" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "already.*member\|member already exists\|already exists" "Invite already-existing member"

# ========================================================================
# TEST 2: Accept non-existent invitation (ErrInvitationNotFound)
# ========================================================================
echo "--- TEST 2: Accept non-existent invitation ---"
echo "  Trying to accept invitation ID 99999..."

TX_RES=$($BINARY tx rep accept-invitation \
    99999 \
    --from bob \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "invitation.*not found\|not found\|does not exist" "Accept non-existent invitation"

# ========================================================================
# TEST 3: Stake on non-existent content (ErrStakeNotFound / not found)
# ========================================================================
echo "--- TEST 3: Stake on non-existent initiative ---"
echo "  Alice stakes on initiative ID 99999 (does not exist)..."

TX_RES=$($BINARY tx rep stake \
    "stake-target-initiative" 99999 "1000000" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "not found\|does not exist\|invalid" "Stake on non-existent initiative"

# ========================================================================
# TEST 4: Insufficient balance for DREAM transfer
# ========================================================================
echo "--- TEST 4: DREAM transfer exceeding balance ---"
echo "  Carol transfers 999999999 DREAM to Bob (should exceed balance)..."

CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)

TX_RES=$($BINARY tx rep transfer-dream \
    "$BOB_ADDR" "999999999000000" "tip" "huge tip test" \
    --from carol \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "insufficient\|not enough\|exceeds" "DREAM transfer exceeding balance"

# ========================================================================
# TEST 5: Non-member creating initiative (should fail)
# ========================================================================
echo "--- TEST 5: Non-member creating initiative ---"

DAVE_ADDR=$($BINARY keys show dave -a --keyring-backend test 2>/dev/null)

if [ -n "$DAVE_ADDR" ]; then
    echo "  Dave ($DAVE_ADDR) is not a member, attempting to create initiative..."

    TX_RES=$($BINARY tx rep create-initiative \
        $TEST_PROJECT_ID \
        "Non-member initiative" \
        "This should fail because dave is not a member" \
        "1" \
        "0" \
        "0" \
        "5000000" \
        --tags "backend" \
        --from dave \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    expect_tx_failure "$TX_RES" "not a member\|member not found\|not found\|unauthorized" "Non-member creating initiative"
else
    echo "  Dave account not available, skipping"
    record_result "Non-member creating initiative" "FAIL"
fi

# ========================================================================
# TEST 6: Zero-amount stake (ErrInvalidAmount)
# ========================================================================
echo "--- TEST 6: Zero-amount stake ---"
echo "  Alice attempts to stake with amount=0..."

TX_RES=$($BINARY tx rep stake \
    "stake-target-tag" 0 "0" \
    --target-identifier "backend" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "invalid.*amount\|amount must be\|zero\|must be positive\|non-zero" "Zero-amount stake"

# ========================================================================
# SUMMARY
# ========================================================================
echo "============================================================================"
echo "  REP VALIDATION ERROR PATHS TEST SUMMARY"
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
