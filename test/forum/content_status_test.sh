#!/bin/bash

echo "--- TESTING: CONTENT STATUS GATES (REACT TO DELETED/HIDDEN, SELF-VOTE, LOCKED THREAD) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

echo "Poster 1:       $POSTER1_ADDR"
echo "Poster 2:       $POSTER2_ADDR"
echo "Sentinel 1:     $SENTINEL1_ADDR"
echo "Category ID:    $TEST_CATEGORY_ID"
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
            echo "  Response: $(echo "$TX_RES" | head -c 200)"
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
# TEST 1: Cannot upvote own post (ErrCannotVoteOwnPost)
# ========================================================================
echo "--- TEST 1: Cannot upvote own post ---"

# Create a post by poster1
TX_RES=$($BINARY tx forum create-post \
    "$TEST_CATEGORY_ID" 0 "Post for self-vote test" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

SELF_POST_ID=""
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    SELF_POST_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="post_created") | .attributes[] | select(.key=="post_id") | .value' | tr -d '"' | head -1)
    echo "  Created post $SELF_POST_ID by poster1"
fi

if [ -n "$SELF_POST_ID" ]; then
    # poster1 tries to upvote their own post
    TX_RES=$($BINARY tx forum upvote-post \
        "$SELF_POST_ID" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    expect_tx_failure "$TX_RES" "cannot vote on your own post" "Cannot upvote own post"
else
    echo "  Skipped (no post created)"
    record_result "Cannot upvote own post" "FAIL"
fi

# ========================================================================
# TEST 2: Cannot downvote own post
# ========================================================================
echo "--- TEST 2: Cannot downvote own post ---"

if [ -n "$SELF_POST_ID" ]; then
    TX_RES=$($BINARY tx forum downvote-post \
        "$SELF_POST_ID" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    expect_tx_failure "$TX_RES" "cannot vote on your own post" "Cannot downvote own post"
else
    echo "  Skipped (no post created)"
    record_result "Cannot downvote own post" "FAIL"
fi

# ========================================================================
# TEST 3: Cannot upvote deleted post
# ========================================================================
echo "--- TEST 3: Cannot upvote deleted post ---"

# Create and delete a post
TX_RES=$($BINARY tx forum create-post \
    "$TEST_CATEGORY_ID" 0 "Post to be deleted for status gate test" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

DELETED_POST_ID=""
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    DELETED_POST_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="post_created") | .attributes[] | select(.key=="post_id") | .value' | tr -d '"' | head -1)
    echo "  Created post $DELETED_POST_ID, deleting it..."

    TX_RES=$($BINARY tx forum delete-post \
        "$DELETED_POST_ID" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    submit_tx_and_wait "$TX_RES"
    if check_tx_success "$TX_RESULT"; then
        echo "  Post $DELETED_POST_ID deleted"
    else
        echo "  Failed to delete: $(echo "$TX_RESULT" | jq -r '.raw_log')"
        DELETED_POST_ID=""
    fi
fi

if [ -n "$DELETED_POST_ID" ]; then
    TX_RES=$($BINARY tx forum upvote-post \
        "$DELETED_POST_ID" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    expect_tx_failure "$TX_RES" "deleted" "Cannot upvote deleted post"
else
    echo "  Skipped (no post created)"
    record_result "Cannot upvote deleted post" "FAIL"
fi

# ========================================================================
# TEST 4: Cannot reply to locked thread (ErrThreadLocked)
# ========================================================================
echo "--- TEST 4: Cannot reply to locked thread ---"

# Create a post, lock it, then try to reply
TX_RES=$($BINARY tx forum create-post \
    "$TEST_CATEGORY_ID" 0 "Thread to be locked for reply test" \
    --from poster2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

LOCKED_POST_ID=""
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    LOCKED_POST_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="post_created") | .attributes[] | select(.key=="post_id") | .value' | tr -d '"' | head -1)
    echo "  Created post $LOCKED_POST_ID, locking it..."

    TX_RES=$($BINARY tx forum lock-thread \
        "$LOCKED_POST_ID" "test lock for reply gate" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    submit_tx_and_wait "$TX_RES"
    if check_tx_success "$TX_RESULT"; then
        echo "  Thread $LOCKED_POST_ID locked"
    else
        echo "  Failed to lock: $(echo "$TX_RESULT" | jq -r '.raw_log')"
        LOCKED_POST_ID=""
    fi
fi

if [ -n "$LOCKED_POST_ID" ]; then
    TX_RES=$($BINARY tx forum create-post \
        "$TEST_CATEGORY_ID" "$LOCKED_POST_ID" "Reply to locked thread" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    expect_tx_failure "$TX_RES" "thread is locked" "Cannot reply to locked thread"
else
    echo "  Skipped (could not lock thread)"
    record_result "Cannot reply to locked thread" "FAIL"
fi

# ========================================================================
# SUMMARY
# ========================================================================
echo "============================================================================"
echo "  CONTENT STATUS GATES TEST SUMMARY"
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
