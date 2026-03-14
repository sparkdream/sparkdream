#!/bin/bash

echo "--- TESTING: CONTENT STATUS GATES & VALIDATION (BLOG) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

echo "Blogger 1:  $BLOGGER1_ADDR"
echo "Blogger 2:  $BLOGGER2_ADDR"
echo "Reader 1:   $READER1_ADDR"
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
# TEST 1: React to deleted post (ErrPostDeleted)
# ========================================================================
echo "--- TEST 1: React to deleted post ---"

# Create a post, delete it, then react
TX_RES=$($BINARY tx blog create-post \
    "Post for delete+react test" \
    "Body of the post that will be deleted." \
    --from blogger2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

DELETED_POST_ID=""
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    DELETED_POST_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="blog.post.created") | .attributes[] | select(.key=="post_id") | .value' | tr -d '"' | head -1)
    echo "  Created post $DELETED_POST_ID, deleting it..."

    TX_RES=$($BINARY tx blog delete-post "$DELETED_POST_ID" \
        --from blogger2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
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
    TX_RES=$($BINARY tx blog react "$DELETED_POST_ID" "like" \
        --from blogger1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    expect_tx_failure "$TX_RES" "deleted" "React to deleted post"
else
    echo "  Skipped (no post created)"
    record_result "React to deleted post" "FAIL"
fi

# ========================================================================
# TEST 2: React to hidden post (ErrPostHidden)
# ========================================================================
echo "--- TEST 2: React to hidden post ---"

TX_RES=$($BINARY tx blog create-post \
    "Post for hide+react test" \
    "Body of the post that will be hidden." \
    --from blogger2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

HIDDEN_POST_ID=""
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    HIDDEN_POST_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="blog.post.created") | .attributes[] | select(.key=="post_id") | .value' | tr -d '"' | head -1)
    echo "  Created post $HIDDEN_POST_ID, hiding it..."

    TX_RES=$($BINARY tx blog hide-post "$HIDDEN_POST_ID" \
        --from blogger2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)
    submit_tx_and_wait "$TX_RES"
    if check_tx_success "$TX_RESULT"; then
        echo "  Post $HIDDEN_POST_ID hidden"
    else
        echo "  Failed to hide: $(echo "$TX_RESULT" | jq -r '.raw_log')"
        HIDDEN_POST_ID=""
    fi
fi

if [ -n "$HIDDEN_POST_ID" ]; then
    TX_RES=$($BINARY tx blog react "$HIDDEN_POST_ID" "like" \
        --from blogger1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    expect_tx_failure "$TX_RES" "hidden" "React to hidden post"
else
    echo "  Skipped (no post created)"
    record_result "React to hidden post" "FAIL"
fi

# ========================================================================
# TEST 3: Re-delete already deleted post (ErrPostDeleted)
# ========================================================================
echo "--- TEST 3: Re-delete already deleted post ---"

if [ -n "$DELETED_POST_ID" ]; then
    TX_RES=$($BINARY tx blog delete-post "$DELETED_POST_ID" \
        --from blogger2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    expect_tx_failure "$TX_RES" "deleted" "Re-delete already deleted post"
else
    echo "  Skipped (no deleted post)"
    record_result "Re-delete already deleted post" "FAIL"
fi

# ========================================================================
# TEST 4: Update hidden post (ErrPostHidden)
# ========================================================================
echo "--- TEST 4: Update hidden post ---"

if [ -n "$HIDDEN_POST_ID" ]; then
    TX_RES=$($BINARY tx blog update-post \
        "Updated title" "Updated body" "$HIDDEN_POST_ID" \
        --from blogger2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    expect_tx_failure "$TX_RES" "hidden" "Update hidden post"
else
    echo "  Skipped (no hidden post)"
    record_result "Update hidden post" "FAIL"
fi

# ========================================================================
# TEST 5: Non-creator unhide (ErrUnauthorized)
# ========================================================================
echo "--- TEST 5: Non-creator unhide ---"

if [ -n "$HIDDEN_POST_ID" ]; then
    TX_RES=$($BINARY tx blog unhide-post "$HIDDEN_POST_ID" \
        --from blogger1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    expect_tx_failure "$TX_RES" "unauthorized" "Non-creator unhide"
else
    echo "  Skipped (no hidden post)"
    record_result "Non-creator unhide" "FAIL"
fi

# ========================================================================
# TEST 6: Empty body on create post
# ========================================================================
echo "--- TEST 6: Empty body on create post ---"

TX_RES=$($BINARY tx blog create-post \
    "Title with empty body" \
    "" \
    --from blogger2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "invalid" "Empty body on create post"

# ========================================================================
# TEST 7: Title too long
# ========================================================================
echo "--- TEST 7: Title too long ---"

# Generate a title longer than max (default is usually 200)
LONG_TITLE=$(python3 -c "print('A' * 300)" 2>/dev/null || printf '%0.sA' $(seq 1 300))

TX_RES=$($BINARY tx blog create-post \
    "$LONG_TITLE" \
    "Normal body" \
    --from blogger2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

expect_tx_failure "$TX_RES" "invalid\|too long\|exceeds" "Title too long"

# ========================================================================
# SUMMARY
# ========================================================================
echo "============================================================================"
echo "  BLOG CONTENT STATUS & VALIDATION TEST SUMMARY"
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
