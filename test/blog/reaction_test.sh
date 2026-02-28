#!/bin/bash

echo "--- TESTING: BLOG REACTIONS (REACT, REMOVE-REACTION, QUERIES) ---"

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

    # Check if tx was rejected at broadcast time (code != 0 in broadcast response)
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
# PART 0: Create a post for reaction tests
# ========================================================================
echo "--- PART 0: Create a parent post ---"

TX_RES=$($BINARY tx blog create-post \
    "Post for Reaction Tests" \
    "This post will receive reactions during testing." \
    --from blogger2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    REACT_POST_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
    echo "  Parent post created with ID: $REACT_POST_ID"
else
    echo "  Failed to create parent post"
    exit 1
fi

echo ""

# Also create a reply on that post (for reply-reaction tests)
TX_RES=$($BINARY tx blog create-reply \
    $REACT_POST_ID \
    "Reply for reaction testing." \
    --from blogger2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    REACT_REPLY_ID=$(extract_event_value "$TX_RESULT" "blog.reply.created" "reply_id")
    echo "  Reply created with ID: $REACT_REPLY_ID"
else
    echo "  Failed to create reply for reaction tests"
    exit 1
fi

echo ""

# ========================================================================
# TEST 1: React to a post (LIKE)
# ========================================================================
echo "--- TEST 1: React to a post (LIKE) ---"

TX_RES=$($BINARY tx blog react \
    $REACT_POST_ID \
    like \
    --from blogger1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    echo "  Blogger1 reacted LIKE on post $REACT_POST_ID"
    record_result "React to post (LIKE)" "PASS"
else
    echo "  Failed to react"
    echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
    record_result "React to post (LIKE)" "FAIL"
fi

# ========================================================================
# TEST 2: Query reaction counts
# ========================================================================
echo "--- TEST 2: Query reaction counts ---"

COUNTS=$($BINARY query blog reaction-counts $REACT_POST_ID --output json 2>&1)
LIKE_COUNT=$(echo "$COUNTS" | jq -r '.counts.like_count // "0"')

if [ "$LIKE_COUNT" == "1" ]; then
    echo "  like_count: $LIKE_COUNT"
    record_result "Query reaction counts" "PASS"
else
    echo "  Expected like_count=1, got: $LIKE_COUNT"
    echo "  Full response: $COUNTS"
    record_result "Query reaction counts" "FAIL"
fi

# ========================================================================
# TEST 3: Query user reaction
# ========================================================================
echo "--- TEST 3: Query user reaction ---"

USER_REACT=$($BINARY query blog user-reaction $BLOGGER1_ADDR $REACT_POST_ID --output json 2>&1)
REACT_TYPE=$(echo "$USER_REACT" | jq -r '.reaction.reaction_type // empty')

if [ "$REACT_TYPE" == "REACTION_TYPE_LIKE" ]; then
    echo "  Reaction type: $REACT_TYPE"
    record_result "Query user reaction" "PASS"
else
    echo "  Expected REACTION_TYPE_LIKE, got: $REACT_TYPE"
    echo "  Full response: $USER_REACT"
    record_result "Query user reaction" "FAIL"
fi

# ========================================================================
# TEST 4: Second user reacts (INSIGHTFUL)
# ========================================================================
echo "--- TEST 4: Second user reacts (INSIGHTFUL) ---"

TX_RES=$($BINARY tx blog react \
    $REACT_POST_ID \
    insightful \
    --from blogger2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    echo "  Blogger2 reacted INSIGHTFUL on post $REACT_POST_ID"
    record_result "Second user reacts (INSIGHTFUL)" "PASS"
else
    echo "  Failed to react"
    record_result "Second user reacts (INSIGHTFUL)" "FAIL"
fi

# ========================================================================
# TEST 5: List reactions for post
# ========================================================================
echo "--- TEST 5: List reactions for post ---"

REACTIONS=$($BINARY query blog list-reactions $REACT_POST_ID --output json 2>&1)
REACT_COUNT=$(echo "$REACTIONS" | jq -r '.reactions | length' 2>/dev/null || echo "0")

if [ "$REACT_COUNT" -ge 2 ]; then
    echo "  Reactions found: $REACT_COUNT"
    record_result "List reactions for post" "PASS"
else
    echo "  Expected >= 2 reactions, got: $REACT_COUNT"
    echo "  Full response: $REACTIONS"
    record_result "List reactions for post" "FAIL"
fi

# ========================================================================
# TEST 6: Change reaction (LIKE -> FUNNY)
# ========================================================================
echo "--- TEST 6: Change reaction (LIKE -> FUNNY) ---"

TX_RES=$($BINARY tx blog react \
    $REACT_POST_ID \
    funny \
    --from blogger1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    # Verify the change
    USER_REACT=$($BINARY query blog user-reaction $BLOGGER1_ADDR $REACT_POST_ID --output json 2>&1)
    NEW_TYPE=$(echo "$USER_REACT" | jq -r '.reaction.reaction_type // empty')

    if [ "$NEW_TYPE" == "REACTION_TYPE_FUNNY" ]; then
        echo "  Reaction changed to: $NEW_TYPE"
        record_result "Change reaction" "PASS"
    else
        echo "  Expected REACTION_TYPE_FUNNY, got: $NEW_TYPE"
        record_result "Change reaction" "FAIL"
    fi
else
    echo "  Failed to change reaction"
    record_result "Change reaction" "FAIL"
fi

# ========================================================================
# TEST 7: Verify counts after change (like=0, insightful=1, funny=1)
# ========================================================================
echo "--- TEST 7: Verify counts after change ---"

COUNTS=$($BINARY query blog reaction-counts $REACT_POST_ID --output json 2>&1)
LIKE_COUNT=$(echo "$COUNTS" | jq -r '.counts.like_count // "0"')
INSIGHTFUL_COUNT=$(echo "$COUNTS" | jq -r '.counts.insightful_count // "0"')
FUNNY_COUNT=$(echo "$COUNTS" | jq -r '.counts.funny_count // "0"')

if [ "$LIKE_COUNT" == "0" ] && [ "$INSIGHTFUL_COUNT" == "1" ] && [ "$FUNNY_COUNT" == "1" ]; then
    echo "  like=$LIKE_COUNT insightful=$INSIGHTFUL_COUNT funny=$FUNNY_COUNT"
    record_result "Verify counts after change" "PASS"
else
    echo "  Expected like=0,insightful=1,funny=1 got like=$LIKE_COUNT,insightful=$INSIGHTFUL_COUNT,funny=$FUNNY_COUNT"
    record_result "Verify counts after change" "FAIL"
fi

# ========================================================================
# TEST 8: React to a reply
# ========================================================================
echo "--- TEST 8: React to a reply ---"

TX_RES=$($BINARY tx blog react \
    $REACT_POST_ID \
    disagree \
    --reply-id $REACT_REPLY_ID \
    --from blogger1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    # Verify reply reaction counts
    COUNTS=$($BINARY query blog reaction-counts $REACT_POST_ID --reply-id $REACT_REPLY_ID --output json 2>&1)
    DISAGREE_COUNT=$(echo "$COUNTS" | jq -r '.counts.disagree_count // "0"')

    if [ "$DISAGREE_COUNT" == "1" ]; then
        echo "  Reacted DISAGREE on reply $REACT_REPLY_ID (disagree_count=$DISAGREE_COUNT)"
        record_result "React to a reply" "PASS"
    else
        echo "  Expected disagree_count=1, got: $DISAGREE_COUNT"
        record_result "React to a reply" "FAIL"
    fi
else
    echo "  Failed to react to reply"
    echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
    record_result "React to a reply" "FAIL"
fi

# ========================================================================
# TEST 9: Remove reaction from post
# ========================================================================
echo "--- TEST 9: Remove reaction from post ---"

TX_RES=$($BINARY tx blog remove-reaction \
    $REACT_POST_ID \
    --from blogger1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    # Verify removal - user reaction should be nil
    USER_REACT=$($BINARY query blog user-reaction $BLOGGER1_ADDR $REACT_POST_ID --output json 2>&1)
    REACT_TYPE=$(echo "$USER_REACT" | jq -r '.reaction.reaction_type // empty')

    if [ -z "$REACT_TYPE" ] || [ "$REACT_TYPE" == "null" ]; then
        echo "  Reaction removed successfully"
        record_result "Remove reaction from post" "PASS"
    else
        echo "  Reaction still present: $REACT_TYPE"
        record_result "Remove reaction from post" "FAIL"
    fi
else
    echo "  Failed to remove reaction"
    echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
    record_result "Remove reaction from post" "FAIL"
fi

# ========================================================================
# TEST 10: Verify counts after removal (funny=0, insightful=1)
# ========================================================================
echo "--- TEST 10: Verify counts after removal ---"

COUNTS=$($BINARY query blog reaction-counts $REACT_POST_ID --output json 2>&1)
FUNNY_COUNT=$(echo "$COUNTS" | jq -r '.counts.funny_count // "0"')
INSIGHTFUL_COUNT=$(echo "$COUNTS" | jq -r '.counts.insightful_count // "0"')

if [ "$FUNNY_COUNT" == "0" ] && [ "$INSIGHTFUL_COUNT" == "1" ]; then
    echo "  funny=$FUNNY_COUNT insightful=$INSIGHTFUL_COUNT"
    record_result "Verify counts after removal" "PASS"
else
    echo "  Expected funny=0,insightful=1 got funny=$FUNNY_COUNT,insightful=$INSIGHTFUL_COUNT"
    record_result "Verify counts after removal" "FAIL"
fi

# ========================================================================
# TEST 11: Remove reaction - FAIL: no reaction to remove
# ========================================================================
echo "--- TEST 11: Remove reaction - FAIL: no reaction to remove ---"

TX_RES=$($BINARY tx blog remove-reaction \
    $REACT_POST_ID \
    --from blogger1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    echo "  Correctly rejected: no reaction to remove"
    record_result "Remove non-existent reaction rejected" "PASS"
else
    echo "  Should have been rejected"
    record_result "Remove non-existent reaction rejected" "FAIL"
fi

# ========================================================================
# TEST 12: React - FAIL: invalid reaction type (0 = unspecified)
# ========================================================================
echo "--- TEST 12: React - FAIL: invalid reaction type ---"

TX_RES=$($BINARY tx blog react \
    $REACT_POST_ID \
    unspecified \
    --from blogger1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    echo "  Correctly rejected: invalid reaction type"
    record_result "Invalid reaction type rejected" "PASS"
else
    echo "  Should have been rejected"
    record_result "Invalid reaction type rejected" "FAIL"
fi

# ========================================================================
# TEST 13: React - FAIL: react to non-existent post
# ========================================================================
echo "--- TEST 13: React - FAIL: react to non-existent post ---"

TX_RES=$($BINARY tx blog react \
    99999 \
    like \
    --from blogger1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    echo "  Correctly rejected: post not found"
    record_result "React to non-existent post rejected" "PASS"
else
    echo "  Should have been rejected"
    record_result "React to non-existent post rejected" "FAIL"
fi

# ========================================================================
# TEST 14: List reactions by creator
# ========================================================================
echo "--- TEST 14: List reactions by creator ---"

# blogger2 still has an INSIGHTFUL reaction on the post
CREATOR_REACTIONS=$($BINARY query blog list-reactions-by-creator $BLOGGER2_ADDR --output json 2>&1)
CREATOR_REACT_COUNT=$(echo "$CREATOR_REACTIONS" | jq -r '.reactions | length' 2>/dev/null || echo "0")

if [ "$CREATOR_REACT_COUNT" -ge 1 ]; then
    echo "  Reactions by blogger2: $CREATOR_REACT_COUNT"
    record_result "List reactions by creator" "PASS"
else
    echo "  Expected >= 1 reactions, got: $CREATOR_REACT_COUNT"
    echo "  Full response: $CREATOR_REACTIONS"
    record_result "List reactions by creator" "FAIL"
fi

# ========================================================================
# SUMMARY
# ========================================================================
echo "============================================"
echo "REACTION TEST RESULTS"
echo "============================================"

for i in "${!TEST_NAMES[@]}"; do
    printf "  %-45s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done

echo ""
echo "  Passed: $PASS_COUNT / $((PASS_COUNT + FAIL_COUNT))"
echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL REACTION TESTS PASSED <<<"
    exit 0
fi
