#!/bin/bash

echo "--- TESTING: BLOG REPLIES (CREATE, UPDATE, DELETE, HIDE, UNHIDE) ---"

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
# PART 0: Create a post for reply tests
# ========================================================================
echo "--- PART 0: Create a parent post ---"

# Use blogger2 as post author (blogger1 may be rate-limited from post_test.sh run)
TX_RES=$($BINARY tx blog create-post \
    "Post for Reply Tests" \
    "This post will receive replies during testing." \
    --from blogger2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    PARENT_POST_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
    echo "  Parent post created with ID: $PARENT_POST_ID"
else
    echo "  Failed to create parent post"
    exit 1
fi

echo ""

# ========================================================================
# TEST 1: Create a reply (happy path)
# ========================================================================
echo "--- TEST 1: Create a reply (happy path) ---"

# blogger1 creates the reply; blogger2 is the post author
TX_RES=$($BINARY tx blog create-reply \
    $PARENT_POST_ID \
    "This is my first reply to the blog post." \
    --from blogger1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    REPLY1_ID=$(extract_event_value "$TX_RESULT" "blog.reply.created" "reply_id")
    if [ -z "$REPLY1_ID" ]; then
        REPLY1_ID="0"
    fi
    echo "  Reply created with ID: $REPLY1_ID"
    record_result "Create a reply" "PASS"
else
    echo "  Failed to create reply"
    echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
    REPLY1_ID=""
    record_result "Create a reply" "FAIL"
fi

# ========================================================================
# TEST 2: Query the reply
# ========================================================================
echo "--- TEST 2: Query the reply ---"

if [ -n "$REPLY1_ID" ]; then
    REPLY_DATA=$($BINARY query blog show-reply $REPLY1_ID --output json 2>&1)
    BODY=$(echo "$REPLY_DATA" | jq -r '.reply.body // empty')
    CREATOR=$(echo "$REPLY_DATA" | jq -r '.reply.creator // empty')
    POST_ID=$(echo "$REPLY_DATA" | jq -r '.reply.post_id // empty')

    if [ "$CREATOR" == "$BLOGGER1_ADDR" ] && [ "$POST_ID" == "$PARENT_POST_ID" ]; then
        echo "  Body: $(echo "$BODY" | head -c 40)..."
        echo "  Creator: $CREATOR"
        echo "  PostId: $POST_ID"
        record_result "Query the reply" "PASS"
    else
        echo "  Unexpected reply data"
        record_result "Query the reply" "FAIL"
    fi
else
    echo "  Skipped (no reply ID)"
    record_result "Query the reply" "FAIL"
fi

# ========================================================================
# TEST 3: List replies for post
# ========================================================================
echo "--- TEST 3: List replies for post ---"

LIST=$($BINARY query blog list-replies $PARENT_POST_ID --output json 2>&1)
REPLY_COUNT=$(echo "$LIST" | jq -r '.replies | length' 2>/dev/null || echo "0")

if [ "$REPLY_COUNT" -gt 0 ]; then
    echo "  Replies found: $REPLY_COUNT"
    record_result "List replies for post" "PASS"
else
    echo "  No replies found"
    record_result "List replies for post" "FAIL"
fi

# ========================================================================
# TEST 4: Verify post reply count incremented
# ========================================================================
echo "--- TEST 4: Verify post reply count ---"

POST_DATA=$($BINARY query blog show-post $PARENT_POST_ID --output json 2>&1)
REPLY_CNT=$(echo "$POST_DATA" | jq -r '.post.reply_count // "0"')

if [ "$REPLY_CNT" -gt 0 ]; then
    echo "  Post reply_count: $REPLY_CNT"
    record_result "Post reply count incremented" "PASS"
else
    echo "  Post reply_count: $REPLY_CNT (expected > 0)"
    record_result "Post reply count incremented" "FAIL"
fi

# ========================================================================
# TEST 5: Create reply - FAIL: non-existent post
# ========================================================================
echo "--- TEST 5: Create reply - FAIL: non-existent post ---"

TX_RES=$($BINARY tx blog create-reply \
    99999 \
    "Reply to non-existent post" \
    --from blogger2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    echo "  Correctly rejected: post not found"
    record_result "Reply to non-existent post rejected" "PASS"
else
    echo "  Should have been rejected"
    record_result "Reply to non-existent post rejected" "FAIL"
fi

# ========================================================================
# TEST 6: Create reply - FAIL: empty body
# ========================================================================
echo "--- TEST 6: Create reply - FAIL: empty body ---"

TX_RES=$($BINARY tx blog create-reply \
    $PARENT_POST_ID \
    "" \
    --from blogger2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    echo "  Correctly rejected: empty body"
    record_result "Reply empty body rejected" "PASS"
else
    echo "  Should have been rejected"
    record_result "Reply empty body rejected" "FAIL"
fi

# ========================================================================
# TEST 7: Update a reply (happy path)
# ========================================================================
echo "--- TEST 7: Update a reply (happy path) ---"

if [ -n "$REPLY1_ID" ]; then
    # blogger1 is the reply creator - they can update their own reply
    TX_RES=$($BINARY tx blog update-reply \
        $REPLY1_ID \
        "This is my updated reply body." \
        --from blogger1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        REPLY_DATA=$($BINARY query blog show-reply $REPLY1_ID --output json 2>&1)
        NEW_BODY=$(echo "$REPLY_DATA" | jq -r '.reply.body // empty')
        EDITED=$(echo "$REPLY_DATA" | jq -r '.reply.edited // "false"')

        if [ "$NEW_BODY" == "This is my updated reply body." ]; then
            echo "  Reply updated. Edited: $EDITED"
            record_result "Update a reply" "PASS"
        else
            echo "  Body not updated: $NEW_BODY"
            record_result "Update a reply" "FAIL"
        fi
    else
        echo "  Failed to update reply"
        record_result "Update a reply" "FAIL"
    fi
else
    echo "  Skipped (no reply ID)"
    record_result "Update a reply" "FAIL"
fi

# ========================================================================
# TEST 8: Update reply - FAIL: non-creator
# ========================================================================
echo "--- TEST 8: Update reply - FAIL: non-creator ---"

if [ -n "$REPLY1_ID" ]; then
    # blogger2 is the post author but NOT the reply creator (blogger1 is) - should be rejected
    TX_RES=$($BINARY tx blog update-reply \
        $REPLY1_ID \
        "Hacked reply body" \
        --from blogger2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: non-creator cannot update reply"
        record_result "Update reply - non-creator rejected" "PASS"
    else
        echo "  Should have been rejected"
        record_result "Update reply - non-creator rejected" "FAIL"
    fi
else
    echo "  Skipped"
    record_result "Update reply - non-creator rejected" "FAIL"
fi

# ========================================================================
# TEST 9: Hide reply (happy path - POST author hides reply)
# ========================================================================
echo "--- TEST 9: Hide reply (post author hides reply) ---"

if [ -n "$REPLY1_ID" ]; then
    # blogger2 is the post author - they can hide any reply on their post
    TX_RES=$($BINARY tx blog hide-reply \
        $REPLY1_ID \
        --from blogger2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        REPLY_DATA=$($BINARY query blog show-reply $REPLY1_ID --output json 2>&1)
        STATUS=$(echo "$REPLY_DATA" | jq -r '.reply.status // empty')

        if [ "$STATUS" == "REPLY_STATUS_HIDDEN" ]; then
            echo "  Reply hidden. Status: $STATUS"
            record_result "Hide reply" "PASS"
        else
            echo "  Reply status unexpected: $STATUS"
            record_result "Hide reply" "FAIL"
        fi
    else
        echo "  Failed to hide reply"
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Hide reply" "FAIL"
    fi
else
    echo "  Skipped"
    record_result "Hide reply" "FAIL"
fi

# ========================================================================
# TEST 10: Hide reply - FAIL: reply creator cannot hide (only post author)
# ========================================================================
echo "--- TEST 10: Hide reply - FAIL: reply creator cannot hide ---"

# blogger1 creates a reply; blogger1 is the reply author (not the post author)
TX_RES=$($BINARY tx blog create-reply \
    $PARENT_POST_ID \
    "Another reply for hide test." \
    --from blogger1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    HIDE_REPLY_ID=$(extract_event_value "$TX_RESULT" "blog.reply.created" "reply_id")

    # blogger1 (reply author, NOT post author) tries to hide - should fail; only post author (blogger2) can hide
    TX_RES=$($BINARY tx blog hide-reply \
        $HIDE_REPLY_ID \
        --from blogger1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: reply creator cannot hide (only post author)"
        record_result "Hide reply - reply creator rejected" "PASS"
    else
        echo "  Should have been rejected"
        record_result "Hide reply - reply creator rejected" "FAIL"
    fi
else
    echo "  Failed to create test reply"
    record_result "Hide reply - reply creator rejected" "FAIL"
fi

# ========================================================================
# TEST 11: Unhide reply (happy path)
# ========================================================================
echo "--- TEST 11: Unhide reply (happy path) ---"

if [ -n "$REPLY1_ID" ]; then
    # blogger2 is the post author - only they can unhide a reply they hid
    TX_RES=$($BINARY tx blog unhide-reply \
        $REPLY1_ID \
        --from blogger2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        REPLY_DATA=$($BINARY query blog show-reply $REPLY1_ID --output json 2>&1)
        STATUS=$(echo "$REPLY_DATA" | jq -r '.reply.status // empty')

        if [ "$STATUS" == "REPLY_STATUS_ACTIVE" ]; then
            echo "  Reply unhidden. Status: $STATUS"
            record_result "Unhide reply" "PASS"
        else
            echo "  Reply status unexpected: $STATUS"
            record_result "Unhide reply" "FAIL"
        fi
    else
        echo "  Failed to unhide reply"
        record_result "Unhide reply" "FAIL"
    fi
else
    echo "  Skipped"
    record_result "Unhide reply" "FAIL"
fi

# ========================================================================
# TEST 12: Delete reply (happy path - reply creator deletes)
# ========================================================================
echo "--- TEST 12: Delete reply (happy path) ---"

# Create a reply to delete; blogger1 is the reply creator
TX_RES=$($BINARY tx blog create-reply \
    $PARENT_POST_ID \
    "Reply to be deleted." \
    --from blogger1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    DEL_REPLY_ID=$(extract_event_value "$TX_RESULT" "blog.reply.created" "reply_id")

    TX_RES=$($BINARY tx blog delete-reply \
        $DEL_REPLY_ID \
        --from blogger1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        REPLY_DATA=$($BINARY query blog show-reply $DEL_REPLY_ID --output json 2>&1)
        STATUS=$(echo "$REPLY_DATA" | jq -r '.reply.status // empty')

        if [ "$STATUS" == "REPLY_STATUS_DELETED" ]; then
            echo "  Reply deleted. Status: $STATUS"
            record_result "Delete reply" "PASS"
        else
            echo "  Reply status unexpected: $STATUS"
            record_result "Delete reply" "FAIL"
        fi
    else
        echo "  Failed to delete reply"
        record_result "Delete reply" "FAIL"
    fi
else
    echo "  Failed to create reply for deletion"
    record_result "Delete reply" "FAIL"
fi

# ========================================================================
# TEST 13: Delete reply (post author can delete others' replies)
# ========================================================================
echo "--- TEST 13: Delete reply (post author deletes other's reply) ---"

# Create a reply by blogger1 (the reply creator, not the post author)
TX_RES=$($BINARY tx blog create-reply \
    $PARENT_POST_ID \
    "Reply that post author will delete." \
    --from blogger1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    PA_DEL_REPLY_ID=$(extract_event_value "$TX_RESULT" "blog.reply.created" "reply_id")

    # blogger2 (post author) deletes blogger1's reply
    TX_RES=$($BINARY tx blog delete-reply \
        $PA_DEL_REPLY_ID \
        --from blogger2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        echo "  Post author successfully deleted other's reply"
        record_result "Post author deletes reply" "PASS"
    else
        echo "  Failed"
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Post author deletes reply" "FAIL"
    fi
else
    echo "  Failed to create reply"
    record_result "Post author deletes reply" "FAIL"
fi

# ========================================================================
# TEST 14: Create reply on deleted post
# ========================================================================
echo "--- TEST 14: Reply on deleted post ---"

# Create a post with blogger2, delete it, then try to reply
TX_RES=$($BINARY tx blog create-post \
    "Post to Delete Then Reply" \
    "This post will be deleted before reply attempt." \
    --from blogger2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    DEL_POST_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")

    # Delete the post (blogger2 is the creator)
    TX_RES=$($BINARY tx blog delete-post \
        $DEL_POST_ID \
        --from blogger2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    submit_tx_and_wait "$TX_RES"

    TX_RES=$($BINARY tx blog create-reply \
        $DEL_POST_ID \
        "Trying to reply on deleted post." \
        --from blogger1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: cannot reply to deleted post"
        record_result "Reply on deleted post rejected" "PASS"
    else
        echo "  Should have been rejected"
        record_result "Reply on deleted post rejected" "FAIL"
    fi
else
    echo "  Failed to create test post"
    record_result "Reply on deleted post rejected" "FAIL"
fi

# ========================================================================
# SUMMARY
# ========================================================================
echo "============================================"
echo "REPLY TEST RESULTS"
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
    echo ">>> ALL REPLY TESTS PASSED <<<"
    exit 0
fi
