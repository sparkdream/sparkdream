#!/bin/bash

echo "--- TESTING: BLOG PIN POST & PIN REPLY (strict separation) ---"

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
echo "Alice:      $ALICE_ADDR"
echo ""

# ========================================================================
# Helper Functions
# ========================================================================

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

check_tx_failure() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        return 0
    fi
    return 1
}

submit_tx_and_wait() {
    local TX_RES="$1"
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        TX_RESULT=""
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

extract_event_value() {
    local TX_RESULT=$1
    local EVENT_TYPE=$2
    local ATTR_KEY=$3
    echo "$TX_RESULT" | jq -r ".events[] | select(.type==\"$EVENT_TYPE\") | .attributes[] | select(.key==\"$ATTR_KEY\") | .value" | tr -d '"'
}

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

# ========================================================================
# PREREQUISITE: Create posts/replies in known lifecycle states.
# ========================================================================
# Under the post-rework strict separation:
#   - Pin requires the post to already be permanent (expires_at == 0).
#   - Ephemeral content is rejected with ErrCannotPinEphemeral.
# A separate test (make_permanent_test.sh) covers the MsgMakePostPermanent
# lifecycle change. This script focuses only on Pin/Unpin marker semantics.

echo "=== PREREQUISITE: Create ephemeral + permanent fixtures ==="
echo ""

# Non-member account for ephemeral content fixtures.
NONMEMBER_ACCOUNT="pintest_nonmember"
if ! $BINARY keys show $NONMEMBER_ACCOUNT --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add $NONMEMBER_ACCOUNT --keyring-backend test --output json > /dev/null 2>&1
    echo "  Created non-member key: $NONMEMBER_ACCOUNT"
fi
NONMEMBER_ADDR=$($BINARY keys show $NONMEMBER_ACCOUNT -a --keyring-backend test)
echo "  Non-member account: $NONMEMBER_ADDR"

# Fund the non-member account with SPARK (for gas)
echo "  Funding non-member account..."
TX_RES=$($BINARY tx bank send \
    alice $NONMEMBER_ADDR \
    10000000${BOND_DENOM} \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)
sleep 6

# Ephemeral post — for "Pin rejects ephemeral" test.
EPHEMERAL_POST_ID=""
echo "  Creating ephemeral post from non-member..."
TX_RES=$($BINARY tx blog create-post \
    "Ephemeral Post (Pin should reject)" \
    "Non-member content stays ephemeral until promoted." \
    --min-reply-trust-level=-1 \
    --from $NONMEMBER_ACCOUNT \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    EPHEMERAL_POST_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
    POST_Q=$($BINARY query blog show-post $EPHEMERAL_POST_ID --output json 2>&1)
    EXPIRES_AT=$(echo "$POST_Q" | jq -r '(.post.expires_at // 0)')
    echo "  Ephemeral post created: ID=$EPHEMERAL_POST_ID (expires_at=$EXPIRES_AT)"
else
    RAW_LOG=$(echo "${TX_RESULT:-$TX_RES}" | jq -r '.raw_log // .message // "unknown error"' 2>/dev/null)
    echo "  Failed to create ephemeral post: $RAW_LOG"
fi

# Ephemeral reply — for "Pin rejects ephemeral reply".
EPHEMERAL_REPLY_ID=""
if [ -n "$EPHEMERAL_POST_ID" ]; then
    echo "  Creating ephemeral reply from non-member..."
    TX_RES=$($BINARY tx blog create-reply \
        $EPHEMERAL_POST_ID \
        "Ephemeral reply." \
        --from $NONMEMBER_ACCOUNT \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        EPHEMERAL_REPLY_ID=$(extract_event_value "$TX_RESULT" "blog.reply.created" "reply_id")
        echo "  Ephemeral reply created: ID=$EPHEMERAL_REPLY_ID"
    else
        RAW_LOG=$(echo "${TX_RESULT:-$TX_RES}" | jq -r '.raw_log // .message // "unknown error"' 2>/dev/null)
        echo "  Failed to create ephemeral reply: $RAW_LOG"
    fi
fi

# Permanent post for the happy-path Pin (and the already-pinned test).
TX_RES=$($BINARY tx blog create-post \
    "Permanent Post for Pin" \
    "Pin works on permanent posts only." \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)

PERMANENT_POST_ID=""
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    PERMANENT_POST_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
    echo "  Permanent post created: ID=$PERMANENT_POST_ID"
else
    RAW_LOG=$(echo "${TX_RESULT:-$TX_RES}" | jq -r '.raw_log // .message // "unknown error"' 2>/dev/null)
    echo "  Failed to create permanent post: $RAW_LOG"
fi

# Permanent reply for the happy-path Pin reply test.
PERMANENT_REPLY_ID=""
if [ -n "$PERMANENT_POST_ID" ]; then
    TX_RES=$($BINARY tx blog create-reply \
        $PERMANENT_POST_ID \
        "Permanent reply for pin test." \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        PERMANENT_REPLY_ID=$(extract_event_value "$TX_RESULT" "blog.reply.created" "reply_id")
        echo "  Permanent reply created: ID=$PERMANENT_REPLY_ID"
    fi
fi

# A post we'll hide for the pin-hidden test.
TX_RES=$($BINARY tx blog create-post \
    "Post to Hide for Pin Test" \
    "Pinning a hidden post must be rejected." \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)

HIDDEN_POST_FOR_PIN_ID=""
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    HIDDEN_POST_FOR_PIN_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
    TX_RES=$($BINARY tx blog hide-post \
        $HIDDEN_POST_FOR_PIN_ID \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        echo "  Post $HIDDEN_POST_FOR_PIN_ID hidden (for pin-hidden test)"
    else
        echo "  Failed to hide post $HIDDEN_POST_FOR_PIN_ID"
        HIDDEN_POST_FOR_PIN_ID=""
    fi
fi

# A post we'll delete for the pin-deleted test.
TX_RES=$($BINARY tx blog create-post \
    "Post to Delete for Pin Test" \
    "Pinning a deleted post must be rejected." \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)

DELETED_POST_ID=""
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    DELETED_POST_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
    TX_RES=$($BINARY tx blog delete-post $DELETED_POST_ID \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        echo "  Post $DELETED_POST_ID deleted"
    fi
fi

echo ""
echo "=== PIN TESTS ==="
echo ""

# ========================================================================
# TEST 1: Pin a permanent post (happy path — display marker only)
# ========================================================================
echo "--- TEST 1: Pin a permanent post (happy path) ---"

if [ -n "$PERMANENT_POST_ID" ]; then
    TX_RES=$($BINARY tx blog pin-post \
        $PERMANENT_POST_ID \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        PINNED_BY=$(extract_event_value "$TX_RESULT" "blog.post.pinned" "pinned_by")
        echo "  Post $PERMANENT_POST_ID pinned by: $PINNED_BY"
        record_result "Pin permanent post (happy path)" "PASS"
    else
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Pin permanent post (happy path)" "FAIL"
    fi
else
    record_result "Pin permanent post (happy path)" "FAIL"
fi

# ========================================================================
# TEST 2: Query pinned post — verify expires_at=0 + pinned_by populated.
# ========================================================================
echo "--- TEST 2: Query pinned post — verify display marker ---"

if [ -n "$PERMANENT_POST_ID" ]; then
    POST_Q=$($BINARY query blog show-post $PERMANENT_POST_ID --output json 2>&1)
    EXPIRES_AT=$(echo "$POST_Q" | jq -r '(.post.expires_at // 0)')
    PINNED_BY=$(echo "$POST_Q" | jq -r '.post.pinned_by // ""')

    if [ "$EXPIRES_AT" == "0" ] && [ -n "$PINNED_BY" ] && [ "$PINNED_BY" != "null" ]; then
        echo "  expires_at=$EXPIRES_AT (permanent), pinned_by=${PINNED_BY:0:20}..."
        record_result "Query pinned post display marker" "PASS"
    else
        echo "  Expected expires_at=0 and pinned_by set, got expires_at=$EXPIRES_AT, pinned_by=$PINNED_BY"
        record_result "Query pinned post display marker" "FAIL"
    fi
else
    record_result "Query pinned post display marker" "FAIL"
fi

# ========================================================================
# TEST 3: Pin a permanent reply (happy path)
# ========================================================================
echo "--- TEST 3: Pin a permanent reply (happy path) ---"

if [ -n "$PERMANENT_REPLY_ID" ]; then
    TX_RES=$($BINARY tx blog pin-reply \
        $PERMANENT_REPLY_ID \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        PINNED_BY=$(extract_event_value "$TX_RESULT" "blog.reply.pinned" "pinned_by")
        echo "  Reply $PERMANENT_REPLY_ID pinned by: $PINNED_BY"
        record_result "Pin permanent reply (happy path)" "PASS"
    else
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Pin permanent reply (happy path)" "FAIL"
    fi
else
    record_result "Pin permanent reply (happy path)" "FAIL"
fi

# ========================================================================
# TEST 4: Pin an ephemeral post is REJECTED with ErrCannotPinEphemeral.
# ========================================================================
echo "--- TEST 4: Pin ephemeral post rejected (strict separation) ---"

if [ -n "$EPHEMERAL_POST_ID" ]; then
    TX_RES=$($BINARY tx blog pin-post \
        $EPHEMERAL_POST_ID \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)
        if echo "$RAW_LOG" | grep -q "ephemeral"; then
            echo "  Correctly rejected: $RAW_LOG"
            record_result "Pin ephemeral post rejected" "PASS"
        else
            echo "  Rejected but message did not mention ephemeral: $RAW_LOG"
            record_result "Pin ephemeral post rejected" "FAIL"
        fi
    else
        echo "  Should have been rejected"
        record_result "Pin ephemeral post rejected" "FAIL"
    fi

    # Verify the ephemeral post is still ephemeral and has no pin marker.
    POST_Q=$($BINARY query blog show-post $EPHEMERAL_POST_ID --output json 2>&1)
    EXPIRES_AT=$(echo "$POST_Q" | jq -r '(.post.expires_at // 0)')
    PINNED_BY=$(echo "$POST_Q" | jq -r '.post.pinned_by // ""')
    if [ "$EXPIRES_AT" != "0" ] && [ -z "$PINNED_BY" -o "$PINNED_BY" == "null" ]; then
        echo "  Post unchanged: still ephemeral (expires_at=$EXPIRES_AT), unpinned"
    else
        echo "  WARNING: rejection should leave post unchanged"
    fi
else
    record_result "Pin ephemeral post rejected" "FAIL"
fi

# ========================================================================
# TEST 5: Pin an ephemeral reply is REJECTED.
# ========================================================================
echo "--- TEST 5: Pin ephemeral reply rejected ---"

if [ -n "$EPHEMERAL_REPLY_ID" ]; then
    TX_RES=$($BINARY tx blog pin-reply \
        $EPHEMERAL_REPLY_ID \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)
        if echo "$RAW_LOG" | grep -q "ephemeral"; then
            record_result "Pin ephemeral reply rejected" "PASS"
        else
            echo "  Rejected but message did not mention ephemeral: $RAW_LOG"
            record_result "Pin ephemeral reply rejected" "FAIL"
        fi
    else
        record_result "Pin ephemeral reply rejected" "FAIL"
    fi
else
    record_result "Pin ephemeral reply rejected" "FAIL"
fi

# ========================================================================
# TEST 6: Pin non-existent post.
# ========================================================================
echo "--- TEST 6: Pin non-existent post rejected ---"

TX_RES=$($BINARY tx blog pin-post \
    999999 \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    record_result "Pin non-existent post rejected" "PASS"
else
    record_result "Pin non-existent post rejected" "FAIL"
fi

# ========================================================================
# TEST 7: Pin a deleted post.
# ========================================================================
echo "--- TEST 7: Pin deleted post rejected ---"

if [ -n "$DELETED_POST_ID" ]; then
    TX_RES=$($BINARY tx blog pin-post \
        $DELETED_POST_ID \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        record_result "Pin deleted post rejected" "PASS"
    else
        record_result "Pin deleted post rejected" "FAIL"
    fi
else
    record_result "Pin deleted post rejected" "FAIL"
fi

# ========================================================================
# TEST 8: Pin a hidden post.
# ========================================================================
echo "--- TEST 8: Pin hidden post rejected ---"

if [ -n "$HIDDEN_POST_FOR_PIN_ID" ]; then
    TX_RES=$($BINARY tx blog pin-post \
        $HIDDEN_POST_FOR_PIN_ID \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        record_result "Pin hidden post rejected" "PASS"
    else
        record_result "Pin hidden post rejected" "FAIL"
    fi
else
    record_result "Pin hidden post rejected" "FAIL"
fi

# ========================================================================
# TEST 9: Pin an already-pinned post.
# ========================================================================
echo "--- TEST 9: Pin already-pinned post rejected ---"

if [ -n "$PERMANENT_POST_ID" ]; then
    TX_RES=$($BINARY tx blog pin-post \
        $PERMANENT_POST_ID \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        record_result "Pin already-pinned post rejected" "PASS"
    else
        record_result "Pin already-pinned post rejected" "FAIL"
    fi
else
    record_result "Pin already-pinned post rejected" "FAIL"
fi

# ========================================================================
# TEST 10: Pin non-existent reply.
# ========================================================================
echo "--- TEST 10: Pin non-existent reply rejected ---"

TX_RES=$($BINARY tx blog pin-reply \
    999999 \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    record_result "Pin non-existent reply rejected" "PASS"
else
    record_result "Pin non-existent reply rejected" "FAIL"
fi

# ========================================================================
# TEST 11: Pin an already-pinned reply.
# ========================================================================
echo "--- TEST 11: Pin already-pinned reply rejected ---"

if [ -n "$PERMANENT_REPLY_ID" ]; then
    TX_RES=$($BINARY tx blog pin-reply \
        $PERMANENT_REPLY_ID \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        record_result "Pin already-pinned reply rejected" "PASS"
    else
        record_result "Pin already-pinned reply rejected" "FAIL"
    fi
else
    record_result "Pin already-pinned reply rejected" "FAIL"
fi

# ========================================================================
# TEST 12: Unpin the previously-pinned permanent post — stays permanent,
# marker cleared.
# ========================================================================
echo "--- TEST 12: Unpin permanent post (stays permanent) ---"

if [ -n "$PERMANENT_POST_ID" ]; then
    TX_RES=$($BINARY tx blog unpin-post \
        $PERMANENT_POST_ID \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        POST_Q=$($BINARY query blog show-post $PERMANENT_POST_ID --output json 2>&1)
        EXPIRES_AT=$(echo "$POST_Q" | jq -r '(.post.expires_at // 0)')
        PINNED_BY=$(echo "$POST_Q" | jq -r '.post.pinned_by // ""')

        if [ "$EXPIRES_AT" == "0" ] && { [ -z "$PINNED_BY" ] || [ "$PINNED_BY" == "null" ]; }; then
            echo "  After unpin: expires_at=0 (still permanent), pinned_by cleared"
            record_result "Unpin permanent post (stays permanent)" "PASS"
        else
            echo "  Expected expires_at=0 + cleared pinned_by, got expires_at=$EXPIRES_AT pinned_by=$PINNED_BY"
            record_result "Unpin permanent post (stays permanent)" "FAIL"
        fi
    else
        record_result "Unpin permanent post (stays permanent)" "FAIL"
    fi
else
    record_result "Unpin permanent post (stays permanent)" "FAIL"
fi

# ========================================================================
# SUMMARY
# ========================================================================
echo "============================================"
echo "PIN TEST RESULTS"
echo "============================================"

for i in "${!TEST_NAMES[@]}"; do
    printf "  %-45s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done

echo ""
echo "  Passed: $PASS_COUNT / $((PASS_COUNT + FAIL_COUNT))"
echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME PIN TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL PIN TESTS PASSED <<<"
    exit 0
fi
