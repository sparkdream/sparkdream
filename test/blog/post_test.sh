#!/bin/bash

echo "--- TESTING: BLOG POSTS (CREATE, UPDATE, DELETE, HIDE, UNHIDE) ---"

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
echo "Note: blogger2 is used as primary post creator (blogger1 may be rate-limited by other tests)."
echo "      blogger1 is used as the unauthorized non-creator for negative tests."
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
# TEST 1: Query initial params
# ========================================================================
echo "--- TEST 1: Query blog params ---"

PARAMS=$($BINARY query blog params --output json 2>&1)
MAX_TITLE=$(echo "$PARAMS" | jq -r '.params.max_title_length // "0"')
MAX_BODY=$(echo "$PARAMS" | jq -r '.params.max_body_length // "0"')

if [ "$MAX_TITLE" -gt 0 ] 2>/dev/null && [ "$MAX_BODY" -gt 0 ] 2>/dev/null; then
    echo "  max_title_length: $MAX_TITLE"
    echo "  max_body_length: $MAX_BODY"
    record_result "Query blog params" "PASS"
else
    echo "  Failed to query params or invalid values"
    record_result "Query blog params" "FAIL"
fi

# ========================================================================
# TEST 2: Create a post (happy path) - blogger2 is the primary creator
# ========================================================================
echo "--- TEST 2: Create a post (happy path) ---"
echo "  (Using blogger2 as creator — blogger1 may be rate-limited by other test scripts)"

TX_RES=$($BINARY tx blog create-post \
    "My First Blog Post" \
    "This is the body of my first blog post for e2e testing." \
    --from blogger2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    POST1_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
    if [ -z "$POST1_ID" ]; then
        POST1_ID="0"
    fi
    echo "  Post created with ID: $POST1_ID"
    record_result "Create a post" "PASS"
else
    echo "  Failed to create post"
    echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
    POST1_ID=""
    record_result "Create a post" "FAIL"
fi

# ========================================================================
# TEST 3: Query the created post
# ========================================================================
echo "--- TEST 3: Query the created post ---"

if [ -n "$POST1_ID" ]; then
    POST_DATA=$($BINARY query blog show-post $POST1_ID --output json 2>&1)
    TITLE=$(echo "$POST_DATA" | jq -r '.post.title // empty')
    CREATOR=$(echo "$POST_DATA" | jq -r '.post.creator // empty')
    STATUS=$(echo "$POST_DATA" | jq -r '.post.status // empty')

    if [ "$TITLE" == "My First Blog Post" ] && [ "$CREATOR" == "$BLOGGER2_ADDR" ]; then
        echo "  Title: $TITLE"
        echo "  Creator: $CREATOR"
        echo "  Status: $STATUS"
        record_result "Query the created post" "PASS"
    else
        echo "  Unexpected post data: title=$TITLE creator=$CREATOR"
        record_result "Query the created post" "FAIL"
    fi
else
    echo "  Skipped (no post ID)"
    record_result "Query the created post" "FAIL"
fi

# ========================================================================
# TEST 4: List posts
# ========================================================================
echo "--- TEST 4: List posts ---"

LIST=$($BINARY query blog list-post --output json 2>&1)
# list-post response field is named 'post' (repeated Post post = 1)
POST_COUNT=$(echo "$LIST" | jq -r '.post | length' 2>/dev/null || echo "0")

if [ "$POST_COUNT" -gt 0 ]; then
    echo "  Total posts: $POST_COUNT"
    record_result "List posts" "PASS"
else
    echo "  No posts found"
    record_result "List posts" "FAIL"
fi

# ========================================================================
# TEST 5: List posts by creator
# ========================================================================
echo "--- TEST 5: List posts by creator ---"

CREATOR_LIST=$($BINARY query blog list-posts-by-creator $BLOGGER2_ADDR --output json 2>&1)
# list-posts-by-creator response field is named 'posts' (repeated Post posts = 1)
CREATOR_COUNT=$(echo "$CREATOR_LIST" | jq -r '.posts | length' 2>/dev/null || echo "0")

if [ "$CREATOR_COUNT" -gt 0 ]; then
    echo "  Posts by blogger2: $CREATOR_COUNT"
    record_result "List posts by creator" "PASS"
else
    echo "  No posts found for blogger2"
    record_result "List posts by creator" "FAIL"
fi

# ========================================================================
# TEST 6: Update a post (happy path - creator updates own post)
# ========================================================================
echo "--- TEST 6: Update a post (happy path) ---"

if [ -n "$POST1_ID" ]; then
    TX_RES=$($BINARY tx blog update-post \
        "Updated Blog Title" \
        "This is the updated body of my first blog post." \
        $POST1_ID \
        --from blogger2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        # Verify the update
        POST_DATA=$($BINARY query blog show-post $POST1_ID --output json 2>&1)
        NEW_TITLE=$(echo "$POST_DATA" | jq -r '.post.title // empty')
        EDITED=$(echo "$POST_DATA" | jq -r '.post.edited // "false"')

        if [ "$NEW_TITLE" == "Updated Blog Title" ]; then
            echo "  Title updated to: $NEW_TITLE"
            echo "  Edited flag: $EDITED"
            record_result "Update a post" "PASS"
        else
            echo "  Title not updated: $NEW_TITLE"
            record_result "Update a post" "FAIL"
        fi
    else
        echo "  Failed to update post"
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Update a post" "FAIL"
    fi
else
    echo "  Skipped (no post ID)"
    record_result "Update a post" "FAIL"
fi

# ========================================================================
# TEST 7: Update post - FAIL: non-creator cannot update
# ========================================================================
echo "--- TEST 7: Update post - FAIL: non-creator cannot update ---"

if [ -n "$POST1_ID" ]; then
    TX_RES=$($BINARY tx blog update-post \
        "Hacked Title" \
        "Hacked body" \
        $POST1_ID \
        --from blogger1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: non-creator cannot update"
        record_result "Update post - non-creator rejected" "PASS"
    else
        echo "  Should have been rejected but was not"
        echo "  Code: $(echo "$TX_RESULT" | jq -r '.code' 2>/dev/null)"
        record_result "Update post - non-creator rejected" "FAIL"
    fi
else
    echo "  Skipped (no post ID)"
    record_result "Update post - non-creator rejected" "FAIL"
fi

# ========================================================================
# TEST 8: Create post - FAIL: empty title
# ========================================================================
echo "--- TEST 8: Create post - FAIL: empty title ---"

TX_RES=$($BINARY tx blog create-post \
    "" \
    "Body with empty title" \
    --from blogger2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    echo "  Correctly rejected: empty title"
    record_result "Create post - empty title rejected" "PASS"
else
    echo "  Should have been rejected but was not"
    record_result "Create post - empty title rejected" "FAIL"
fi

# ========================================================================
# TEST 9: Hide a post (happy path - creator hides own post)
# ========================================================================
echo "--- TEST 9: Hide a post (happy path) ---"

if [ -n "$POST1_ID" ]; then
    TX_RES=$($BINARY tx blog hide-post \
        $POST1_ID \
        --from blogger2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        # Verify hidden
        POST_DATA=$($BINARY query blog show-post $POST1_ID --output json 2>&1)
        STATUS=$(echo "$POST_DATA" | jq -r '.post.status // empty')

        if [ "$STATUS" == "POST_STATUS_HIDDEN" ]; then
            echo "  Post hidden. Status: $STATUS"
            record_result "Hide a post" "PASS"
        else
            echo "  Post status unexpected: $STATUS"
            record_result "Hide a post" "FAIL"
        fi
    else
        echo "  Failed to hide post"
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Hide a post" "FAIL"
    fi
else
    echo "  Skipped (no post ID)"
    record_result "Hide a post" "FAIL"
fi

# ========================================================================
# TEST 10: Hide post - FAIL: non-creator cannot hide
# ========================================================================
echo "--- TEST 10: Hide post - FAIL: non-creator cannot hide ---"

# Create a new post by blogger2 to try hiding from blogger1 (non-creator)
TX_RES=$($BINARY tx blog create-post \
    "Post for hide test" \
    "This post tests that non-creators cannot hide it." \
    --from blogger2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    HIDE_TEST_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")

    TX_RES=$($BINARY tx blog hide-post \
        $HIDE_TEST_ID \
        --from blogger1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: non-creator cannot hide"
        record_result "Hide post - non-creator rejected" "PASS"
    else
        echo "  Should have been rejected but was not"
        echo "  Code: $(echo "$TX_RESULT" | jq -r '.code' 2>/dev/null)"
        record_result "Hide post - non-creator rejected" "FAIL"
    fi
else
    echo "  Failed to create test post"
    echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
    record_result "Hide post - non-creator rejected" "FAIL"
fi

# ========================================================================
# TEST 11: Hide post - FAIL: already hidden
# ========================================================================
echo "--- TEST 11: Hide post - FAIL: already hidden ---"

if [ -n "$POST1_ID" ]; then
    TX_RES=$($BINARY tx blog hide-post \
        $POST1_ID \
        --from blogger2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: post already hidden"
        record_result "Hide post - already hidden rejected" "PASS"
    else
        echo "  Should have been rejected but was not"
        echo "  Code: $(echo "$TX_RESULT" | jq -r '.code' 2>/dev/null)"
        record_result "Hide post - already hidden rejected" "FAIL"
    fi
else
    echo "  Skipped (no post ID)"
    record_result "Hide post - already hidden rejected" "FAIL"
fi

# ========================================================================
# TEST 12: Unhide a post (happy path)
# ========================================================================
echo "--- TEST 12: Unhide a post (happy path) ---"

if [ -n "$POST1_ID" ]; then
    TX_RES=$($BINARY tx blog unhide-post \
        $POST1_ID \
        --from blogger2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        # Verify active
        POST_DATA=$($BINARY query blog show-post $POST1_ID --output json 2>&1)
        STATUS=$(echo "$POST_DATA" | jq -r '.post.status // empty')

        if [ "$STATUS" == "POST_STATUS_ACTIVE" ]; then
            echo "  Post unhidden. Status: $STATUS"
            record_result "Unhide a post" "PASS"
        else
            echo "  Post status unexpected: $STATUS"
            record_result "Unhide a post" "FAIL"
        fi
    else
        echo "  Failed to unhide post"
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Unhide a post" "FAIL"
    fi
else
    echo "  Skipped (no post ID)"
    record_result "Unhide a post" "FAIL"
fi

# ========================================================================
# TEST 13: Unhide post - FAIL: post not hidden
# ========================================================================
echo "--- TEST 13: Unhide post - FAIL: post not hidden ---"

if [ -n "$POST1_ID" ]; then
    TX_RES=$($BINARY tx blog unhide-post \
        $POST1_ID \
        --from blogger2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: post not hidden"
        record_result "Unhide post - not hidden rejected" "PASS"
    else
        echo "  Should have been rejected but was not"
        echo "  Code: $(echo "$TX_RESULT" | jq -r '.code' 2>/dev/null)"
        record_result "Unhide post - not hidden rejected" "FAIL"
    fi
else
    echo "  Skipped (no post ID)"
    record_result "Unhide post - not hidden rejected" "FAIL"
fi

# ========================================================================
# TEST 14: Delete a post (happy path)
# ========================================================================
echo "--- TEST 14: Delete a post (happy path) ---"

# Create a post specifically for deletion
TX_RES=$($BINARY tx blog create-post \
    "Post to Delete" \
    "This post will be deleted." \
    --from blogger2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    DELETE_POST_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")

    TX_RES=$($BINARY tx blog delete-post \
        $DELETE_POST_ID \
        --from blogger2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        # Verify deleted
        POST_DATA=$($BINARY query blog show-post $DELETE_POST_ID --output json 2>&1)
        STATUS=$(echo "$POST_DATA" | jq -r '.post.status // empty')
        TITLE=$(echo "$POST_DATA" | jq -r '.post.title // empty')

        if [ "$STATUS" == "POST_STATUS_DELETED" ]; then
            echo "  Post deleted. Status: $STATUS, Title cleared: $([ -z "$TITLE" ] && echo 'yes' || echo 'no')"
            record_result "Delete a post" "PASS"
        else
            echo "  Post status unexpected: $STATUS"
            record_result "Delete a post" "FAIL"
        fi
    else
        echo "  Failed to delete post"
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Delete a post" "FAIL"
    fi
else
    echo "  Failed to create post for deletion"
    echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
    record_result "Delete a post" "FAIL"
fi

# ========================================================================
# TEST 15: Delete post - FAIL: non-creator cannot delete
# ========================================================================
echo "--- TEST 15: Delete post - FAIL: non-creator cannot delete ---"

if [ -n "$POST1_ID" ]; then
    TX_RES=$($BINARY tx blog delete-post \
        $POST1_ID \
        --from blogger1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: non-creator cannot delete"
        record_result "Delete post - non-creator rejected" "PASS"
    else
        echo "  Should have been rejected but was not"
        echo "  Code: $(echo "$TX_RESULT" | jq -r '.code' 2>/dev/null)"
        record_result "Delete post - non-creator rejected" "FAIL"
    fi
else
    echo "  Skipped (no post ID)"
    record_result "Delete post - non-creator rejected" "FAIL"
fi

# ========================================================================
# TEST 16: Update post - FAIL: cannot update deleted post
# ========================================================================
echo "--- TEST 16: Update post - FAIL: cannot update deleted post ---"

if [ -n "$DELETE_POST_ID" ]; then
    TX_RES=$($BINARY tx blog update-post \
        "Revived Title" \
        "Revived body" \
        $DELETE_POST_ID \
        --from blogger2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: cannot update deleted post"
        record_result "Update deleted post rejected" "PASS"
    else
        echo "  Should have been rejected but was not"
        echo "  Code: $(echo "$TX_RESULT" | jq -r '.code' 2>/dev/null)"
        record_result "Update deleted post rejected" "FAIL"
    fi
else
    echo "  Skipped (no delete post ID)"
    record_result "Update deleted post rejected" "FAIL"
fi

# ========================================================================
# TEST 17: Query non-existent post
# ========================================================================
echo "--- TEST 17: Query non-existent post ---"

POST_DATA=$($BINARY query blog show-post 99999 --output json 2>&1)

if echo "$POST_DATA" | grep -qi "not found\|error"; then
    echo "  Correctly returned error for non-existent post"
    record_result "Query non-existent post" "PASS"
else
    echo "  Should have returned error"
    record_result "Query non-existent post" "FAIL"
fi

# ========================================================================
# SUMMARY
# ========================================================================
echo "============================================"
echo "POST TEST RESULTS"
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
    echo ">>> ALL POST TESTS PASSED <<<"
    exit 0
fi
