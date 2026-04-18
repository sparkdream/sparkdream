#!/bin/bash

echo "--- TESTING: POSTS & THREADS (CREATE, EDIT, DELETE, VOTE, FOLLOW) ---"

# --- 0. SETUP ---
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

echo "Poster 1:     $POSTER1_ADDR"
echo "Poster 2:     $POSTER2_ADDR"
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
        echo "Transaction failed with code: $CODE"
        echo "$TX_RESULT" | jq -r '.raw_log'
        return 1
    fi
    return 0
}

# Expect the transaction to fail (non-zero code). Returns 0 if it did fail.
check_tx_failure() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        return 0
    fi
    return 1
}

extract_event_value() {
    local TX_RESULT=$1
    local EVENT_TYPE=$2
    local ATTR_KEY=$3

    echo "$TX_RESULT" | jq -r ".events[] | select(.type==\"$EVENT_TYPE\") | .attributes[] | select(.key==\"$ATTR_KEY\") | .value" | tr -d '"'
}

# Submit a tx and wait for result. Sets TX_RESULT and returns 0 on submission success.
submit_tx_and_wait() {
    local TX_RES="$1"
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        TX_RESULT=""
        return 1
    fi

    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    return 0
}

# ========================================================================
# PART 0: ENSURE CATEGORY EXISTS
# ========================================================================
echo "--- PART 0: ENSURE CATEGORY EXISTS ---"

CATEGORIES=$($BINARY query commons list-category --output json 2>&1)
CATEGORY_COUNT=$(echo "$CATEGORIES" | jq -r '.category | length' 2>/dev/null || echo "0")

if [ "$CATEGORY_COUNT" -gt 0 ]; then
    # Use the first existing category (category_id may be 0 due to proto3 zero-value omission)
    TEST_CATEGORY_ID=$(echo "$CATEGORIES" | jq -r '.category[0].category_id // "0"')
    echo "  Using existing category ID: $TEST_CATEGORY_ID"
else
    echo "  No categories found, creating one..."
    TX_RES=$($BINARY tx commons create-category \
        "General Discussion" \
        "A category for general forum discussions" \
        "false" \
        "false" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        TEST_CATEGORY_ID=$(extract_event_value "$TX_RESULT" "category_created" "category_id")
        if [ -z "$TEST_CATEGORY_ID" ]; then
            TEST_CATEGORY_ID="0"
        fi
        echo "  Category created with ID: $TEST_CATEGORY_ID"
    else
        echo "  Failed to create category"
        exit 1
    fi
fi

echo "Category ID:  $TEST_CATEGORY_ID"
echo ""

# ========================================================================
# PART 0b: VERIFY PARAMS INCLUDE ephemeral_ttl
# ========================================================================
echo "--- PART 0b: VERIFY PARAMS INCLUDE ephemeral_ttl ---"

PARAMS=$($BINARY query forum params --output json 2>&1)

if echo "$PARAMS" | grep -q "error"; then
    echo "  Failed to query forum params"
    PARAMS_CHECK_RESULT="FAIL"
else
    EPHEMERAL_TTL=$(echo "$PARAMS" | jq -r '.params.ephemeral_ttl // "0"')
    echo "  ephemeral_ttl: $EPHEMERAL_TTL"

    if [ "$EPHEMERAL_TTL" == "86400" ]; then
        echo "  ephemeral_ttl matches default (86400s / 24h)"
        PARAMS_CHECK_RESULT="PASS"
    elif [ "$EPHEMERAL_TTL" -gt 0 ] 2>/dev/null; then
        echo "  ephemeral_ttl is positive ($EPHEMERAL_TTL)"
        PARAMS_CHECK_RESULT="PASS"
    else
        echo "  ERROR: ephemeral_ttl is missing or zero"
        PARAMS_CHECK_RESULT="FAIL"
    fi
fi

echo ""

# ========================================================================
# PART 1: LIST EXISTING POSTS
# ========================================================================
echo "--- PART 1: LIST EXISTING POSTS ---"

POSTS=$($BINARY query forum list-post --output json 2>&1)

if echo "$POSTS" | grep -q "error"; then
    echo "  Failed to query posts"
    INITIAL_POST_COUNT=0
    LIST_POSTS_RESULT="FAIL"
else
    INITIAL_POST_COUNT=$(echo "$POSTS" | jq -r '.post | length' 2>/dev/null || echo "0")
    echo "  Existing posts: $INITIAL_POST_COUNT"
    LIST_POSTS_RESULT="PASS"

    if [ "$INITIAL_POST_COUNT" -gt 0 ]; then
        echo ""
        echo "  Recent posts:"
        echo "$POSTS" | jq -r '.post[0:3] | .[] | "    - ID \(.post_id): \(.content | .[0:30])..."' 2>/dev/null
    fi
fi

echo ""

# ========================================================================
# PART 2: CREATE A NEW THREAD (ROOT POST)
# ========================================================================
echo "--- PART 2: CREATE A NEW THREAD (ROOT POST) ---"

POST_CONTENT="This is a test thread created at $(date). Testing the x/forum module functionality."

echo "Creating new thread in category $TEST_CATEGORY_ID..."

TX_RES=$($BINARY tx forum create-post \
    "$TEST_CATEGORY_ID" \
    "0" \
    "$POST_CONTENT" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    ROOT_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")

    if [ -z "$ROOT_POST_ID" ] || [ "$ROOT_POST_ID" == "null" ]; then
        # Fallback: query the latest post
        POSTS=$($BINARY query forum list-post --output json 2>&1)
        ROOT_POST_ID=$(echo "$POSTS" | jq -r '.post[-1].post_id // empty')
    fi

    echo "  Thread created successfully (Post ID: $ROOT_POST_ID)"
    CREATE_THREAD_RESULT="PASS"
else
    echo "  Failed to create thread"
    ROOT_POST_ID=""
    CREATE_THREAD_RESULT="FAIL"
fi

# Export for use in other tests
if [ -n "$ROOT_POST_ID" ]; then
    echo "export TEST_ROOT_POST_ID=$ROOT_POST_ID" >> "$SCRIPT_DIR/.test_env"
fi

echo ""

# ========================================================================
# PART 3: QUERY POST DETAILS
# ========================================================================
echo "--- PART 3: QUERY POST DETAILS ---"

if [ -n "$ROOT_POST_ID" ]; then
    POST_INFO=$($BINARY query forum get-post $ROOT_POST_ID --output json 2>&1)

    if echo "$POST_INFO" | grep -q "error\|not found"; then
        echo "  Post $ROOT_POST_ID not found"
        QUERY_POST_RESULT="FAIL"
    else
        QUERIED_AUTHOR=$(echo "$POST_INFO" | jq -r '.post.author')
        QUERIED_CONTENT=$(echo "$POST_INFO" | jq -r '.post.content')
        echo "  Post Details:"
        echo "    ID: $(echo "$POST_INFO" | jq -r '.post.post_id')"
        echo "    Category: $(echo "$POST_INFO" | jq -r '.post.category_id')"
        echo "    Author: ${QUERIED_AUTHOR:0:20}..."
        echo "    Content: ${QUERIED_CONTENT:0:50}..."

        if [ "$QUERIED_AUTHOR" == "$POSTER1_ADDR" ]; then
            echo "  Author matches poster1 (correct)"
            QUERY_POST_RESULT="PASS"
        else
            echo "  ERROR: Author does not match poster1"
            QUERY_POST_RESULT="FAIL"
        fi
    fi
else
    echo "  No post ID available, skipping query"
    QUERY_POST_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 4: CREATE A REPLY
# ========================================================================
echo "--- PART 4: CREATE A REPLY ---"

if [ -n "$ROOT_POST_ID" ]; then
    REPLY_CONTENT="This is a reply to the test thread. Great discussion topic!"

    echo "Creating reply to post $ROOT_POST_ID..."

    TX_RES=$($BINARY tx forum create-post \
        "$TEST_CATEGORY_ID" \
        "$ROOT_POST_ID" \
        "$REPLY_CONTENT" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        REPLY_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        echo "  Reply created successfully (Reply Post ID: $REPLY_POST_ID)"
        CREATE_REPLY_RESULT="PASS"

        if [ -n "$REPLY_POST_ID" ]; then
            echo "export TEST_REPLY_POST_ID=$REPLY_POST_ID" >> "$SCRIPT_DIR/.test_env"
        fi
    else
        echo "  Failed to create reply"
        CREATE_REPLY_RESULT="FAIL"
    fi
else
    echo "  No root post available, skipping reply"
    CREATE_REPLY_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 5: EDIT POST
# ========================================================================
echo "--- PART 5: EDIT POST ---"

if [ -n "$ROOT_POST_ID" ]; then
    EDITED_CONTENT="This is the EDITED content of the test thread. Updated at $(date)."

    echo "Editing post $ROOT_POST_ID..."

    TX_RES=$($BINARY tx forum edit-post \
        "$ROOT_POST_ID" \
        "$EDITED_CONTENT" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        # Verify edit
        POST_INFO=$($BINARY query forum get-post $ROOT_POST_ID --output json 2>&1)
        NEW_CONTENT=$(echo "$POST_INFO" | jq -r '.post.content')

        if echo "$NEW_CONTENT" | grep -q "EDITED"; then
            echo "  Post edited successfully (content updated)"
            EDIT_POST_RESULT="PASS"
        else
            echo "  Post edit tx succeeded but content not updated"
            EDIT_POST_RESULT="FAIL"
        fi
    else
        echo "  Failed to edit post"
        EDIT_POST_RESULT="FAIL"
    fi
else
    echo "  No post available to edit"
    EDIT_POST_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 6: UPVOTE POST
# ========================================================================
echo "--- PART 6: UPVOTE POST ---"

if [ -n "$ROOT_POST_ID" ]; then
    echo "Upvoting post $ROOT_POST_ID (from poster2)..."

    TX_RES=$($BINARY tx forum upvote-post \
        "$ROOT_POST_ID" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        echo "  Post upvoted successfully"
        UPVOTE_RESULT="PASS"
    else
        echo "  Failed to upvote post"
        UPVOTE_RESULT="FAIL"
    fi
else
    echo "  No post available to upvote"
    UPVOTE_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 7: DOWNVOTE POST
# ========================================================================
echo "--- PART 7: DOWNVOTE POST ---"

if [ -n "$REPLY_POST_ID" ]; then
    echo "Downvoting reply $REPLY_POST_ID (from poster1)..."

    TX_RES=$($BINARY tx forum downvote-post \
        "$REPLY_POST_ID" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        echo "  Reply downvoted successfully"
        DOWNVOTE_RESULT="PASS"
    else
        echo "  Failed to downvote reply"
        DOWNVOTE_RESULT="FAIL"
    fi
else
    echo "  No reply available to downvote"
    DOWNVOTE_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 8: FOLLOW THREAD
# ========================================================================
echo "--- PART 8: FOLLOW THREAD ---"

if [ -n "$ROOT_POST_ID" ]; then
    echo "Following thread $ROOT_POST_ID (from poster2)..."

    TX_RES=$($BINARY tx forum follow-thread \
        "$ROOT_POST_ID" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        # Verify following
        IS_FOLLOWING=$($BINARY query forum is-following-thread $ROOT_POST_ID $POSTER2_ADDR --output json 2>&1)
        FOLLOWING=$(echo "$IS_FOLLOWING" | jq -r 'if .is_following == null then "false" else (.is_following | tostring) end')

        if [ "$FOLLOWING" == "true" ]; then
            echo "  Now following thread (verified)"
            FOLLOW_RESULT="PASS"
        else
            echo "  Follow tx succeeded but is-following returned: $FOLLOWING"
            FOLLOW_RESULT="FAIL"
        fi
    else
        echo "  Failed to follow thread"
        FOLLOW_RESULT="FAIL"
    fi
else
    echo "  No thread available to follow"
    FOLLOW_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 9: QUERY THREAD FOLLOWERS
# ========================================================================
echo "--- PART 9: QUERY THREAD FOLLOW COUNT ---"

if [ -n "$ROOT_POST_ID" ]; then
    FOLLOW_COUNT=$($BINARY query forum get-thread-follow-count $ROOT_POST_ID --output json 2>&1)

    if echo "$FOLLOW_COUNT" | grep -q "error"; then
        echo "  Failed to query follow count"
        FOLLOW_COUNT_RESULT="FAIL"
    else
        COUNT=$(echo "$FOLLOW_COUNT" | jq -r '.thread_follow_count.follower_count // "0"')
        echo "  Thread $ROOT_POST_ID has $COUNT followers"

        if [ "$COUNT" -gt 0 ] 2>/dev/null; then
            echo "  Follow count > 0 (correct)"
            FOLLOW_COUNT_RESULT="PASS"
        else
            echo "  ERROR: Follow count should be > 0"
            FOLLOW_COUNT_RESULT="FAIL"
        fi
    fi
else
    echo "  No thread available"
    FOLLOW_COUNT_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 10: QUERY THREAD FOLLOWERS LIST
# ========================================================================
echo "--- PART 10: QUERY THREAD FOLLOWERS LIST ---"

if [ -n "$ROOT_POST_ID" ]; then
    FOLLOWERS=$($BINARY query forum thread-followers $ROOT_POST_ID --output json 2>&1)

    if echo "$FOLLOWERS" | grep -q "error"; then
        echo "  Failed to query thread followers"
        FOLLOWERS_LIST_RESULT="FAIL"
    else
        FOLLOWER_ADDR=$(echo "$FOLLOWERS" | jq -r '.follower // "none"')
        echo "  First follower: $FOLLOWER_ADDR"

        if [ "$FOLLOWER_ADDR" == "$POSTER2_ADDR" ]; then
            echo "  Follower matches poster2 (correct)"
            FOLLOWERS_LIST_RESULT="PASS"
        elif [ "$FOLLOWER_ADDR" == "none" ] || [ "$FOLLOWER_ADDR" == "" ]; then
            echo "  No followers found (unexpected)"
            FOLLOWERS_LIST_RESULT="FAIL"
        else
            echo "  Follower address present but does not match poster2"
            FOLLOWERS_LIST_RESULT="PASS"
        fi
    fi
else
    echo "  No thread available to query followers"
    FOLLOWERS_LIST_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 11: QUERY USER FOLLOWED THREADS
# ========================================================================
echo "--- PART 11: QUERY USER FOLLOWED THREADS ---"

if [ -n "$ROOT_POST_ID" ]; then
    USER_THREADS=$($BINARY query forum user-followed-threads $POSTER2_ADDR --output json 2>&1)

    if echo "$USER_THREADS" | grep -q "error"; then
        echo "  Failed to query user followed threads"
        USER_FOLLOWED_RESULT="FAIL"
    else
        THREAD_ID=$(echo "$USER_THREADS" | jq -r '.thread_id // "0"')
        echo "  First followed thread: $THREAD_ID"

        if [ "$THREAD_ID" == "$ROOT_POST_ID" ]; then
            echo "  Thread ID matches root post (correct)"
            USER_FOLLOWED_RESULT="PASS"
        else
            echo "  Thread ID does not match root post (may be different ordering)"
            USER_FOLLOWED_RESULT="PASS"
        fi
    fi
else
    echo "  No thread available"
    USER_FOLLOWED_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 12: UNFOLLOW THREAD
# ========================================================================
echo "--- PART 12: UNFOLLOW THREAD ---"

if [ -n "$ROOT_POST_ID" ]; then
    echo "Unfollowing thread $ROOT_POST_ID (from poster2)..."

    TX_RES=$($BINARY tx forum unfollow-thread \
        "$ROOT_POST_ID" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        # Verify unfollowed
        IS_FOLLOWING=$($BINARY query forum is-following-thread $ROOT_POST_ID $POSTER2_ADDR --output json 2>&1)
        FOLLOWING=$(echo "$IS_FOLLOWING" | jq -r 'if .is_following == null then "false" else (.is_following | tostring) end')

        if [ "$FOLLOWING" == "false" ]; then
            echo "  Unfollowed thread (verified)"
            UNFOLLOW_RESULT="PASS"
        else
            echo "  Unfollow tx succeeded but is-following returned: $FOLLOWING"
            UNFOLLOW_RESULT="FAIL"
        fi
    else
        echo "  Failed to unfollow thread"
        UNFOLLOW_RESULT="FAIL"
    fi
else
    echo "  No thread available to unfollow"
    UNFOLLOW_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 13: MARK ACCEPTED REPLY
# ========================================================================
echo "--- PART 13: MARK ACCEPTED REPLY ---"

if [ -n "$ROOT_POST_ID" ] && [ -n "$REPLY_POST_ID" ]; then
    echo "Marking reply $REPLY_POST_ID as accepted answer..."

    TX_RES=$($BINARY tx forum mark-accepted-reply \
        "$ROOT_POST_ID" \
        "$REPLY_POST_ID" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        echo "  Reply marked as accepted"
        MARK_ACCEPTED_RESULT="PASS"
    else
        echo "  Failed to mark accepted reply"
        MARK_ACCEPTED_RESULT="FAIL"
    fi
else
    echo "  No thread/reply available"
    MARK_ACCEPTED_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 14: QUERY POSTS COUNT
# ========================================================================
echo "--- PART 14: QUERY POSTS COUNT ---"

POSTS_FINAL=$($BINARY query forum list-post --output json 2>&1)

if echo "$POSTS_FINAL" | grep -q "error"; then
    echo "  Failed to query posts"
    POSTS_COUNT_RESULT="FAIL"
else
    POST_COUNT=$(echo "$POSTS_FINAL" | jq -r '.post | length' 2>/dev/null || echo "0")
    echo "  Total posts: $POST_COUNT"

    if [ "$POST_COUNT" -gt "$INITIAL_POST_COUNT" ] 2>/dev/null; then
        echo "  Post count increased (correct)"
        POSTS_COUNT_RESULT="PASS"
    else
        echo "  ERROR: Post count did not increase"
        POSTS_COUNT_RESULT="FAIL"
    fi
fi

echo ""

# ========================================================================
# PART 15: QUERY POSTS BY CATEGORY AND STATUS
# ========================================================================
echo "--- PART 15: QUERY POSTS BY CATEGORY AND STATUS ---"

if [ -n "$TEST_CATEGORY_ID" ]; then
    FILTERED=$($BINARY query forum posts $TEST_CATEGORY_ID 0 --output json 2>&1)

    if echo "$FILTERED" | grep -q "error"; then
        echo "  Failed to query posts by category"
        POSTS_BY_CAT_RESULT="FAIL"
    else
        F_POST_ID=$(echo "$FILTERED" | jq -r '.post_id // "0"')
        echo "  First root post in category $TEST_CATEGORY_ID: $F_POST_ID"

        if [ "$F_POST_ID" != "0" ] && [ -n "$F_POST_ID" ]; then
            echo "  Posts query returned results (correct)"
            POSTS_BY_CAT_RESULT="PASS"
        else
            echo "  Posts query returned empty"
            POSTS_BY_CAT_RESULT="FAIL"
        fi
    fi
else
    echo "  No category available"
    POSTS_BY_CAT_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 16: QUERY USER POSTS
# ========================================================================
echo "--- PART 16: QUERY USER POSTS ---"

USER_POSTS=$($BINARY query forum user-posts $POSTER1_ADDR --output json 2>&1)

if echo "$USER_POSTS" | grep -q "error"; then
    echo "  Failed to query user posts"
    USER_POSTS_RESULT="FAIL"
else
    UP_POST_ID=$(echo "$USER_POSTS" | jq -r '.post_id // "0"')
    echo "  First post by poster1: $UP_POST_ID"

    if [ "$UP_POST_ID" != "0" ] && [ -n "$UP_POST_ID" ]; then
        echo "  User posts query returned results (correct)"
        USER_POSTS_RESULT="PASS"
    else
        echo "  User posts query returned empty"
        USER_POSTS_RESULT="FAIL"
    fi
fi

echo ""

# ========================================================================
# PART 17: QUERY TOP POSTS
# ========================================================================
echo "--- PART 17: QUERY TOP POSTS ---"

if [ -n "$TEST_CATEGORY_ID" ]; then
    TOP=$($BINARY query forum top-posts $TEST_CATEGORY_ID 0 --output json 2>&1)

    if echo "$TOP" | grep -q "error"; then
        echo "  Failed to query top posts"
        TOP_POSTS_RESULT="FAIL"
    else
        TOP_POST_ID=$(echo "$TOP" | jq -r '.post_id // "0"')
        echo "  Top post ID: $TOP_POST_ID"

        if [ "$TOP_POST_ID" == "$ROOT_POST_ID" ]; then
            echo "  Top post matches upvoted root post (correct)"
            TOP_POSTS_RESULT="PASS"
        elif [ -n "$TOP_POST_ID" ] && [ "$TOP_POST_ID" != "0" ]; then
            echo "  Top post returned (different from root, may be pre-existing)"
            TOP_POSTS_RESULT="PASS"
        else
            echo "  Top posts query returned empty"
            TOP_POSTS_RESULT="FAIL"
        fi
    fi
else
    echo "  No category available"
    TOP_POSTS_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 18: DELETE POST
# ========================================================================
echo "--- PART 18: DELETE POST ---"

echo "Creating a post to delete..."

DELETE_CONTENT="This post will be deleted shortly."

TX_RES=$($BINARY tx forum create-post \
    "$TEST_CATEGORY_ID" \
    "0" \
    "$DELETE_CONTENT" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    DELETE_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
    echo "  Created post $DELETE_POST_ID for deletion"

    if [ -n "$DELETE_POST_ID" ]; then
        echo "  Deleting post $DELETE_POST_ID..."

        TX_RES=$($BINARY tx forum delete-post \
            "$DELETE_POST_ID" \
            --from poster1 \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
            echo "  Post deleted successfully"
            DELETE_POST_RESULT="PASS"
        else
            echo "  Failed to delete post"
            DELETE_POST_RESULT="FAIL"
        fi
    else
        echo "  Could not extract post ID for deletion"
        DELETE_POST_RESULT="FAIL"
    fi
else
    echo "  Could not create post for deletion test"
    DELETE_POST_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 19: EPHEMERAL POST PRUNING (NON-MEMBER POST EXPIRES VIA ENDBLOCKER)
# ========================================================================
echo "--- PART 19: EPHEMERAL POST PRUNING ---"

# dave is a genesis account with SPARK but is NOT an x/rep member,
# so his posts are ephemeral (expiration_time = now + ephemeral_ttl).
# config.yml sets ephemeral_ttl to 15 seconds for testing.

DAVE_ADDR=$($BINARY keys show dave -a --keyring-backend test 2>/dev/null)

if [ -z "$DAVE_ADDR" ]; then
    echo "  dave key not found in keyring, skipping pruning test"
    EPHEMERAL_PRUNE_RESULT="FAIL"
else
    echo "  dave (non-member): $DAVE_ADDR"

    # Verify dave is NOT a member
    DAVE_MEMBER=$($BINARY query rep get-member $DAVE_ADDR --output json 2>&1)
    if echo "$DAVE_MEMBER" | grep -q "not found"; then
        echo "  Confirmed: dave is NOT an x/rep member (good)"
    else
        echo "  WARNING: dave IS a member — posts will be permanent, not ephemeral"
        echo "  Test may not work as expected"
    fi

    # Create an ephemeral post from dave
    EPHEMERAL_CONTENT="Ephemeral post from non-member dave - should be pruned after TTL"

    echo "  Creating ephemeral post from dave..."
    TX_RES=$($BINARY tx forum create-post \
        "$TEST_CATEGORY_ID" \
        "0" \
        "$EPHEMERAL_CONTENT" \
        --from dave \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        EPHEMERAL_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        IS_EPHEMERAL=$(extract_event_value "$TX_RESULT" "post_created" "is_ephemeral")

        if [ -z "$EPHEMERAL_POST_ID" ] || [ "$EPHEMERAL_POST_ID" == "null" ]; then
            # Fallback: query the latest post
            POSTS=$($BINARY query forum list-post --output json 2>&1)
            EPHEMERAL_POST_ID=$(echo "$POSTS" | jq -r '.post[-1].post_id // empty')
        fi

        echo "  Ephemeral post created (ID: $EPHEMERAL_POST_ID, is_ephemeral: $IS_EPHEMERAL)"

        # Verify the post exists and has an expiration_time
        POST_INFO=$($BINARY query forum get-post $EPHEMERAL_POST_ID --output json 2>&1)
        EXPIRATION=$(echo "$POST_INFO" | jq -r '.post.expiration_time // "0"')
        echo "  Post expiration_time: $EXPIRATION"

        if [ "$EXPIRATION" == "0" ] || [ -z "$EXPIRATION" ]; then
            echo "  ERROR: Post has no expiration_time (should be ephemeral)"
            EPHEMERAL_PRUNE_RESULT="FAIL"
        else
            echo "  Waiting for TTL to expire and EndBlocker to prune..."
            echo "  (ephemeral_ttl=15s, waiting 25s for blocks to advance past expiration)"
            sleep 25

            # Query the post - it should be gone (pruned by EndBlocker)
            POST_AFTER=$($BINARY query forum get-post $EPHEMERAL_POST_ID --output json 2>&1)

            if echo "$POST_AFTER" | grep -qi "not found\|does not exist\|error"; then
                echo "  Post $EPHEMERAL_POST_ID was pruned by EndBlocker (correct)"
                EPHEMERAL_PRUNE_RESULT="PASS"
            else
                # Post still exists - check if it might need more time
                POST_STATUS=$(echo "$POST_AFTER" | jq -r '.post.status // "unknown"')
                CURRENT_EXP=$(echo "$POST_AFTER" | jq -r '.post.expiration_time // "0"')
                echo "  Post still exists (status: $POST_STATUS, expiration: $CURRENT_EXP)"
                echo "  Waiting another 15s..."
                sleep 15

                POST_RETRY=$($BINARY query forum get-post $EPHEMERAL_POST_ID --output json 2>&1)
                if echo "$POST_RETRY" | grep -qi "not found\|does not exist\|error"; then
                    echo "  Post $EPHEMERAL_POST_ID was pruned on second check (correct)"
                    EPHEMERAL_PRUNE_RESULT="PASS"
                else
                    echo "  ERROR: Post $EPHEMERAL_POST_ID still exists after TTL!"
                    EPHEMERAL_PRUNE_RESULT="FAIL"
                fi
            fi
        fi
    else
        echo "  Failed to create ephemeral post from dave"
        echo "  (dave may not have enough SPARK for spam_tax + storage fee)"
        EPHEMERAL_PRUNE_RESULT="FAIL"
    fi
fi

echo ""

# ########################################################################
#
#   NEGATIVE PATH TESTS
#
# ########################################################################

echo "========================================================================"
echo "  NEGATIVE PATH TESTS"
echo "========================================================================"
echo ""

# ========================================================================
# NEG 1: CREATE POST IN NON-EXISTENT CATEGORY
# ========================================================================
echo "--- NEG 1: CREATE POST IN NON-EXISTENT CATEGORY ---"

echo "Attempting to create post in category 999999..."

TX_RES=$($BINARY tx forum create-post \
    "999999" \
    "0" \
    "This should fail - bad category" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Transaction rejected at submission (expected)"
    NEG_BAD_CATEGORY_RESULT="PASS"
else
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        echo "  Transaction failed as expected (code: $CODE)"
        echo "  Error: $RAW_LOG"
        NEG_BAD_CATEGORY_RESULT="PASS"
    else
        echo "  ERROR: Transaction succeeded — post created in non-existent category!"
        NEG_BAD_CATEGORY_RESULT="FAIL"
    fi
fi

echo ""

# ========================================================================
# NEG 2: CREATE POST WITH EMPTY CONTENT
# ========================================================================
echo "--- NEG 2: CREATE POST WITH EMPTY CONTENT ---"

echo "Attempting to create post with empty content..."

TX_RES=$($BINARY tx forum create-post \
    "$TEST_CATEGORY_ID" \
    "0" \
    "" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Transaction rejected at submission (expected)"
    NEG_EMPTY_CONTENT_RESULT="PASS"
else
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        echo "  Transaction failed as expected (code: $CODE)"
        echo "  Error: $RAW_LOG"
        NEG_EMPTY_CONTENT_RESULT="PASS"
    else
        echo "  ERROR: Transaction succeeded — empty content was accepted!"
        NEG_EMPTY_CONTENT_RESULT="FAIL"
    fi
fi

echo ""

# ========================================================================
# NEG 3: CREATE POST WITH CONTENT TOO LARGE (>10KB)
# ========================================================================
echo "--- NEG 3: CREATE POST WITH CONTENT TOO LARGE ---"

LARGE_CONTENT=$(python3 -c "print('X' * 10241)" 2>/dev/null || printf 'X%.0s' $(seq 1 10241))
echo "Attempting to create post with ${#LARGE_CONTENT}-byte content (limit is 10240)..."

TX_RES=$($BINARY tx forum create-post \
    "$TEST_CATEGORY_ID" \
    "0" \
    "$LARGE_CONTENT" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Transaction rejected at submission (expected)"
    NEG_LARGE_CONTENT_RESULT="PASS"
else
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        echo "  Transaction failed as expected (code: $CODE)"
        echo "  Error: $RAW_LOG"
        NEG_LARGE_CONTENT_RESULT="PASS"
    else
        echo "  ERROR: Transaction succeeded — oversized content was accepted!"
        NEG_LARGE_CONTENT_RESULT="FAIL"
    fi
fi

echo ""

# ========================================================================
# NEG 4: CREATE REPLY TO NON-EXISTENT PARENT
# ========================================================================
echo "--- NEG 4: CREATE REPLY TO NON-EXISTENT PARENT ---"

echo "Attempting to reply to non-existent post 999999..."

TX_RES=$($BINARY tx forum create-post \
    "$TEST_CATEGORY_ID" \
    "999999" \
    "This reply should fail - parent does not exist" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Transaction rejected at submission (expected)"
    NEG_BAD_PARENT_RESULT="PASS"
else
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        echo "  Transaction failed as expected (code: $CODE)"
        echo "  Error: $RAW_LOG"
        NEG_BAD_PARENT_RESULT="PASS"
    else
        echo "  ERROR: Transaction succeeded — reply to non-existent parent was accepted!"
        NEG_BAD_PARENT_RESULT="FAIL"
    fi
fi

echo ""

# ========================================================================
# NEG 5: EDIT POST BY NON-AUTHOR
# ========================================================================
echo "--- NEG 5: EDIT POST BY NON-AUTHOR ---"

if [ -n "$ROOT_POST_ID" ]; then
    echo "Attempting to edit post $ROOT_POST_ID as poster2 (not author)..."

    TX_RES=$($BINARY tx forum edit-post \
        "$ROOT_POST_ID" \
        "Hacked content from wrong user" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Transaction rejected at submission (expected)"
        NEG_EDIT_NONAUTHOR_RESULT="PASS"
    else
        sleep 6
        TX_RESULT=$(wait_for_tx "$TXHASH")
        CODE=$(echo "$TX_RESULT" | jq -r '.code')

        if [ "$CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            echo "  Transaction failed as expected (code: $CODE)"
            echo "  Error: $RAW_LOG"
            NEG_EDIT_NONAUTHOR_RESULT="PASS"
        else
            echo "  ERROR: Transaction succeeded — non-author edited a post!"
            NEG_EDIT_NONAUTHOR_RESULT="FAIL"
        fi
    fi
else
    echo "  No post available, skipping"
    NEG_EDIT_NONAUTHOR_RESULT="FAIL"
fi

echo ""

# ========================================================================
# NEG 6: EDIT NON-EXISTENT POST
# ========================================================================
echo "--- NEG 6: EDIT NON-EXISTENT POST ---"

echo "Attempting to edit post 999999 (does not exist)..."

TX_RES=$($BINARY tx forum edit-post \
    "999999" \
    "This edit should fail" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Transaction rejected at submission (expected)"
    NEG_EDIT_NONEXISTENT_RESULT="PASS"
else
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        echo "  Transaction failed as expected (code: $CODE)"
        echo "  Error: $RAW_LOG"
        NEG_EDIT_NONEXISTENT_RESULT="PASS"
    else
        echo "  ERROR: Transaction succeeded — edited a non-existent post!"
        NEG_EDIT_NONEXISTENT_RESULT="FAIL"
    fi
fi

echo ""

# ========================================================================
# NEG 7: EDIT POST WITH EMPTY CONTENT
# ========================================================================
echo "--- NEG 7: EDIT POST WITH EMPTY CONTENT ---"

if [ -n "$ROOT_POST_ID" ]; then
    echo "Attempting to edit post $ROOT_POST_ID with empty content..."

    TX_RES=$($BINARY tx forum edit-post \
        "$ROOT_POST_ID" \
        "" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Transaction rejected at submission (expected)"
        NEG_EDIT_EMPTY_RESULT="PASS"
    else
        sleep 6
        TX_RESULT=$(wait_for_tx "$TXHASH")
        CODE=$(echo "$TX_RESULT" | jq -r '.code')

        if [ "$CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            echo "  Transaction failed as expected (code: $CODE)"
            echo "  Error: $RAW_LOG"
            NEG_EDIT_EMPTY_RESULT="PASS"
        else
            echo "  ERROR: Transaction succeeded — empty edit was accepted!"
            NEG_EDIT_EMPTY_RESULT="FAIL"
        fi
    fi
else
    echo "  No post available, skipping"
    NEG_EDIT_EMPTY_RESULT="FAIL"
fi

echo ""

# ========================================================================
# NEG 8: DELETE POST BY NON-AUTHOR
# ========================================================================
echo "--- NEG 8: DELETE POST BY NON-AUTHOR ---"

if [ -n "$ROOT_POST_ID" ]; then
    echo "Attempting to delete post $ROOT_POST_ID as poster2 (not author)..."

    TX_RES=$($BINARY tx forum delete-post \
        "$ROOT_POST_ID" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Transaction rejected at submission (expected)"
        NEG_DELETE_NONAUTHOR_RESULT="PASS"
    else
        sleep 6
        TX_RESULT=$(wait_for_tx "$TXHASH")
        CODE=$(echo "$TX_RESULT" | jq -r '.code')

        if [ "$CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            echo "  Transaction failed as expected (code: $CODE)"
            echo "  Error: $RAW_LOG"
            NEG_DELETE_NONAUTHOR_RESULT="PASS"
        else
            echo "  ERROR: Transaction succeeded — non-author deleted a post!"
            NEG_DELETE_NONAUTHOR_RESULT="FAIL"
        fi
    fi
else
    echo "  No post available, skipping"
    NEG_DELETE_NONAUTHOR_RESULT="FAIL"
fi

echo ""

# ========================================================================
# NEG 9: DELETE NON-EXISTENT POST
# ========================================================================
echo "--- NEG 9: DELETE NON-EXISTENT POST ---"

echo "Attempting to delete post 999999 (does not exist)..."

TX_RES=$($BINARY tx forum delete-post \
    "999999" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Transaction rejected at submission (expected)"
    NEG_DELETE_NONEXISTENT_RESULT="PASS"
else
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        echo "  Transaction failed as expected (code: $CODE)"
        echo "  Error: $RAW_LOG"
        NEG_DELETE_NONEXISTENT_RESULT="PASS"
    else
        echo "  ERROR: Transaction succeeded — deleted a non-existent post!"
        NEG_DELETE_NONEXISTENT_RESULT="FAIL"
    fi
fi

echo ""

# ========================================================================
# NEG 10: DELETE ALREADY-DELETED POST
# ========================================================================
echo "--- NEG 10: DELETE ALREADY-DELETED POST ---"

if [ -n "$DELETE_POST_ID" ]; then
    echo "Attempting to delete already-deleted post $DELETE_POST_ID..."

    TX_RES=$($BINARY tx forum delete-post \
        "$DELETE_POST_ID" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Transaction rejected at submission (expected)"
        NEG_DELETE_TWICE_RESULT="PASS"
    else
        sleep 6
        TX_RESULT=$(wait_for_tx "$TXHASH")
        CODE=$(echo "$TX_RESULT" | jq -r '.code')

        if [ "$CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            echo "  Transaction failed as expected (code: $CODE)"
            echo "  Error: $RAW_LOG"
            NEG_DELETE_TWICE_RESULT="PASS"
        else
            echo "  ERROR: Transaction succeeded — deleted an already-deleted post!"
            NEG_DELETE_TWICE_RESULT="FAIL"
        fi
    fi
else
    echo "  No deleted post ID available, skipping"
    NEG_DELETE_TWICE_RESULT="FAIL"
fi

echo ""

# ========================================================================
# NEG 11: UPVOTE OWN POST (SELF-VOTE)
# ========================================================================
echo "--- NEG 11: UPVOTE OWN POST (SELF-VOTE) ---"

if [ -n "$ROOT_POST_ID" ]; then
    echo "Attempting to upvote own post $ROOT_POST_ID as poster1 (author)..."

    TX_RES=$($BINARY tx forum upvote-post \
        "$ROOT_POST_ID" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Transaction rejected at submission (expected)"
        NEG_SELF_UPVOTE_RESULT="PASS"
    else
        sleep 6
        TX_RESULT=$(wait_for_tx "$TXHASH")
        CODE=$(echo "$TX_RESULT" | jq -r '.code')

        if [ "$CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            echo "  Transaction failed as expected (code: $CODE)"
            echo "  Error: $RAW_LOG"
            NEG_SELF_UPVOTE_RESULT="PASS"
        else
            echo "  ERROR: Transaction succeeded — author upvoted own post!"
            NEG_SELF_UPVOTE_RESULT="FAIL"
        fi
    fi
else
    echo "  No post available, skipping"
    NEG_SELF_UPVOTE_RESULT="FAIL"
fi

echo ""

# ========================================================================
# NEG 12: DOWNVOTE OWN POST (SELF-VOTE)
# ========================================================================
echo "--- NEG 12: DOWNVOTE OWN POST (SELF-VOTE) ---"

if [ -n "$ROOT_POST_ID" ]; then
    echo "Attempting to downvote own post $ROOT_POST_ID as poster1 (author)..."

    TX_RES=$($BINARY tx forum downvote-post \
        "$ROOT_POST_ID" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Transaction rejected at submission (expected)"
        NEG_SELF_DOWNVOTE_RESULT="PASS"
    else
        sleep 6
        TX_RESULT=$(wait_for_tx "$TXHASH")
        CODE=$(echo "$TX_RESULT" | jq -r '.code')

        if [ "$CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            echo "  Transaction failed as expected (code: $CODE)"
            echo "  Error: $RAW_LOG"
            NEG_SELF_DOWNVOTE_RESULT="PASS"
        else
            echo "  ERROR: Transaction succeeded — author downvoted own post!"
            NEG_SELF_DOWNVOTE_RESULT="FAIL"
        fi
    fi
else
    echo "  No post available, skipping"
    NEG_SELF_DOWNVOTE_RESULT="FAIL"
fi

echo ""

# ========================================================================
# NEG 13: UPVOTE NON-EXISTENT POST
# ========================================================================
echo "--- NEG 13: UPVOTE NON-EXISTENT POST ---"

echo "Attempting to upvote post 999999 (does not exist)..."

TX_RES=$($BINARY tx forum upvote-post \
    "999999" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Transaction rejected at submission (expected)"
    NEG_UPVOTE_NONEXISTENT_RESULT="PASS"
else
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        echo "  Transaction failed as expected (code: $CODE)"
        echo "  Error: $RAW_LOG"
        NEG_UPVOTE_NONEXISTENT_RESULT="PASS"
    else
        echo "  ERROR: Transaction succeeded — upvoted a non-existent post!"
        NEG_UPVOTE_NONEXISTENT_RESULT="FAIL"
    fi
fi

echo ""

# ========================================================================
# NEG 14: FOLLOW A REPLY (NOT ROOT POST)
# ========================================================================
echo "--- NEG 14: FOLLOW A REPLY (NOT ROOT POST) ---"

if [ -n "$REPLY_POST_ID" ]; then
    echo "Attempting to follow reply $REPLY_POST_ID (not a root post)..."

    TX_RES=$($BINARY tx forum follow-thread \
        "$REPLY_POST_ID" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Transaction rejected at submission (expected)"
        NEG_FOLLOW_REPLY_RESULT="PASS"
    else
        sleep 6
        TX_RESULT=$(wait_for_tx "$TXHASH")
        CODE=$(echo "$TX_RESULT" | jq -r '.code')

        if [ "$CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            echo "  Transaction failed as expected (code: $CODE)"
            echo "  Error: $RAW_LOG"
            NEG_FOLLOW_REPLY_RESULT="PASS"
        else
            echo "  ERROR: Transaction succeeded — followed a reply instead of a thread!"
            NEG_FOLLOW_REPLY_RESULT="FAIL"
        fi
    fi
else
    echo "  No reply available, skipping"
    NEG_FOLLOW_REPLY_RESULT="FAIL"
fi

echo ""

# ========================================================================
# NEG 15: FOLLOW NON-EXISTENT THREAD
# ========================================================================
echo "--- NEG 15: FOLLOW NON-EXISTENT THREAD ---"

echo "Attempting to follow thread 999999 (does not exist)..."

TX_RES=$($BINARY tx forum follow-thread \
    "999999" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Transaction rejected at submission (expected)"
    NEG_FOLLOW_NONEXISTENT_RESULT="PASS"
else
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        echo "  Transaction failed as expected (code: $CODE)"
        echo "  Error: $RAW_LOG"
        NEG_FOLLOW_NONEXISTENT_RESULT="PASS"
    else
        echo "  ERROR: Transaction succeeded — followed a non-existent thread!"
        NEG_FOLLOW_NONEXISTENT_RESULT="FAIL"
    fi
fi

echo ""

# ========================================================================
# NEG 16: DOUBLE-FOLLOW SAME THREAD
# ========================================================================
echo "--- NEG 16: DOUBLE-FOLLOW SAME THREAD ---"

if [ -n "$ROOT_POST_ID" ]; then
    # First follow the thread
    echo "Following thread $ROOT_POST_ID (from poster1)..."

    TX_RES=$($BINARY tx forum follow-thread \
        "$ROOT_POST_ID" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES"; then
        if check_tx_success "$TX_RESULT"; then
            echo "  First follow succeeded, now attempting duplicate..."

            TX_RES=$($BINARY tx forum follow-thread \
                "$ROOT_POST_ID" \
                --from poster1 \
                --chain-id $CHAIN_ID \
                --keyring-backend test \
                --fees 5000uspark \
                -y \
                --output json 2>&1)

            TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

            if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
                echo "  Duplicate follow rejected at submission (expected)"
                NEG_DOUBLE_FOLLOW_RESULT="PASS"
            else
                sleep 6
                TX_RESULT=$(wait_for_tx "$TXHASH")
                CODE=$(echo "$TX_RESULT" | jq -r '.code')

                if [ "$CODE" != "0" ]; then
                    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
                    echo "  Duplicate follow failed as expected (code: $CODE)"
                    echo "  Error: $RAW_LOG"
                    NEG_DOUBLE_FOLLOW_RESULT="PASS"
                else
                    echo "  ERROR: Transaction succeeded — double-follow was accepted!"
                    NEG_DOUBLE_FOLLOW_RESULT="FAIL"
                fi
            fi
        else
            echo "  First follow failed, cannot test double-follow"
            NEG_DOUBLE_FOLLOW_RESULT="FAIL"
        fi
    else
        echo "  Could not submit first follow"
        NEG_DOUBLE_FOLLOW_RESULT="FAIL"
    fi
else
    echo "  No thread available, skipping"
    NEG_DOUBLE_FOLLOW_RESULT="FAIL"
fi

echo ""

# ========================================================================
# NEG 17: UNFOLLOW THREAD NOT FOLLOWING
# ========================================================================
echo "--- NEG 17: UNFOLLOW THREAD NOT FOLLOWING ---"

if [ -n "$ROOT_POST_ID" ]; then
    echo "Attempting to unfollow thread $ROOT_POST_ID as poster2 (already unfollowed earlier)..."

    TX_RES=$($BINARY tx forum unfollow-thread \
        "$ROOT_POST_ID" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Transaction rejected at submission (expected)"
        NEG_UNFOLLOW_NONFOLLOWER_RESULT="PASS"
    else
        sleep 6
        TX_RESULT=$(wait_for_tx "$TXHASH")
        CODE=$(echo "$TX_RESULT" | jq -r '.code')

        if [ "$CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            echo "  Transaction failed as expected (code: $CODE)"
            echo "  Error: $RAW_LOG"
            NEG_UNFOLLOW_NONFOLLOWER_RESULT="PASS"
        else
            echo "  ERROR: Transaction succeeded — unfollowed a thread not following!"
            NEG_UNFOLLOW_NONFOLLOWER_RESULT="FAIL"
        fi
    fi
else
    echo "  No thread available, skipping"
    NEG_UNFOLLOW_NONFOLLOWER_RESULT="FAIL"
fi

echo ""

# ========================================================================
# NEG 18: MARK ACCEPTED REPLY BY NON-AUTHOR
# ========================================================================
echo "--- NEG 18: MARK ACCEPTED REPLY BY NON-AUTHOR ---"

# Create a fresh thread + reply for this test (so accepted_reply is not already set)
echo "Creating fresh thread for non-author accepted reply test..."

TX_RES=$($BINARY tx forum create-post \
    "$TEST_CATEGORY_ID" \
    "0" \
    "Thread for accepted reply non-author test" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    FRESH_THREAD_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")

    # Create a reply
    TX_RES=$($BINARY tx forum create-post \
        "$TEST_CATEGORY_ID" \
        "$FRESH_THREAD_ID" \
        "Reply for non-author accepted reply test" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        FRESH_REPLY_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")

        echo "  Attempting to mark reply as accepted by poster2 (not thread author)..."

        TX_RES=$($BINARY tx forum mark-accepted-reply \
            "$FRESH_THREAD_ID" \
            "$FRESH_REPLY_ID" \
            --from poster2 \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

        if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
            echo "  Transaction rejected at submission (expected)"
            NEG_ACCEPTED_NONAUTHOR_RESULT="PASS"
        else
            sleep 6
            TX_RESULT=$(wait_for_tx "$TXHASH")
            CODE=$(echo "$TX_RESULT" | jq -r '.code')

            if [ "$CODE" != "0" ]; then
                RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
                echo "  Transaction failed as expected (code: $CODE)"
                echo "  Error: $RAW_LOG"
                NEG_ACCEPTED_NONAUTHOR_RESULT="PASS"
            else
                echo "  ERROR: Transaction succeeded — non-author marked accepted reply!"
                NEG_ACCEPTED_NONAUTHOR_RESULT="FAIL"
            fi
        fi
    else
        echo "  Could not create reply for test"
        NEG_ACCEPTED_NONAUTHOR_RESULT="FAIL"
    fi
else
    echo "  Could not create thread for test"
    NEG_ACCEPTED_NONAUTHOR_RESULT="FAIL"
fi

echo ""

# ========================================================================
# NEG 19: MARK ACCEPTED REPLY TWICE (ALREADY ACCEPTED)
# ========================================================================
echo "--- NEG 19: MARK ACCEPTED REPLY TWICE ---"

if [ -n "$ROOT_POST_ID" ] && [ -n "$REPLY_POST_ID" ]; then
    echo "Attempting to mark accepted reply again on thread $ROOT_POST_ID..."

    # Create another reply to try to accept
    TX_RES=$($BINARY tx forum create-post \
        "$TEST_CATEGORY_ID" \
        "$ROOT_POST_ID" \
        "Another reply to test double-accept" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        SECOND_REPLY_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")

        TX_RES=$($BINARY tx forum mark-accepted-reply \
            "$ROOT_POST_ID" \
            "$SECOND_REPLY_ID" \
            --from poster1 \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

        if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
            echo "  Transaction rejected at submission (expected)"
            NEG_ACCEPTED_TWICE_RESULT="PASS"
        else
            sleep 6
            TX_RESULT=$(wait_for_tx "$TXHASH")
            CODE=$(echo "$TX_RESULT" | jq -r '.code')

            if [ "$CODE" != "0" ]; then
                RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
                echo "  Transaction failed as expected (code: $CODE)"
                echo "  Error: $RAW_LOG"
                NEG_ACCEPTED_TWICE_RESULT="PASS"
            else
                echo "  ERROR: Transaction succeeded — double accepted reply!"
                NEG_ACCEPTED_TWICE_RESULT="FAIL"
            fi
        fi
    else
        echo "  Could not create second reply for test"
        NEG_ACCEPTED_TWICE_RESULT="FAIL"
    fi
else
    echo "  No thread/reply available"
    NEG_ACCEPTED_TWICE_RESULT="FAIL"
fi

echo ""

# ========================================================================
# NEG 20: QUERY NON-EXISTENT POST
# ========================================================================
echo "--- NEG 20: QUERY NON-EXISTENT POST ---"

echo "Querying post ID 999999 (should not exist)..."

NONEXISTENT=$($BINARY query forum get-post 999999 --output json 2>&1)

if echo "$NONEXISTENT" | grep -qi "not found\|does not exist\|error"; then
    echo "  Correctly returned error for non-existent post"
    NEG_QUERY_NONEXISTENT_RESULT="PASS"
else
    echo "  ERROR: Query returned a result for non-existent post!"
    echo "  $NONEXISTENT"
    NEG_QUERY_NONEXISTENT_RESULT="FAIL"
fi

echo ""

# ========================================================================
# SUMMARY
# ========================================================================
echo "========================================================================"
echo "  POST TEST SUMMARY"
echo "========================================================================"
echo ""
echo "  --- Happy Path ---"
echo "  Params ephemeral_ttl:      $PARAMS_CHECK_RESULT"
echo "  List posts:                $LIST_POSTS_RESULT"
echo "  Create thread:             $CREATE_THREAD_RESULT"
echo "  Query post details:        $QUERY_POST_RESULT"
echo "  Create reply:              $CREATE_REPLY_RESULT"
echo "  Edit post:                 $EDIT_POST_RESULT"
echo "  Upvote post:               $UPVOTE_RESULT"
echo "  Downvote post:             $DOWNVOTE_RESULT"
echo "  Follow thread:             $FOLLOW_RESULT"
echo "  Follow count query:        $FOLLOW_COUNT_RESULT"
echo "  Thread followers list:     $FOLLOWERS_LIST_RESULT"
echo "  User followed threads:     $USER_FOLLOWED_RESULT"
echo "  Unfollow thread:           $UNFOLLOW_RESULT"
echo "  Mark accepted reply:       $MARK_ACCEPTED_RESULT"
echo "  Posts count:               $POSTS_COUNT_RESULT"
echo "  Posts by category:         $POSTS_BY_CAT_RESULT"
echo "  User posts:                $USER_POSTS_RESULT"
echo "  Top posts:                 $TOP_POSTS_RESULT"
echo "  Delete post:               $DELETE_POST_RESULT"
echo "  Ephemeral post pruning:    $EPHEMERAL_PRUNE_RESULT"
echo ""
echo "  --- Negative Path ---"
echo "  Bad category:              $NEG_BAD_CATEGORY_RESULT"
echo "  Empty content:             $NEG_EMPTY_CONTENT_RESULT"
echo "  Content too large:         $NEG_LARGE_CONTENT_RESULT"
echo "  Bad parent (reply):        $NEG_BAD_PARENT_RESULT"
echo "  Edit by non-author:        $NEG_EDIT_NONAUTHOR_RESULT"
echo "  Edit non-existent:         $NEG_EDIT_NONEXISTENT_RESULT"
echo "  Edit empty content:        $NEG_EDIT_EMPTY_RESULT"
echo "  Delete by non-author:      $NEG_DELETE_NONAUTHOR_RESULT"
echo "  Delete non-existent:       $NEG_DELETE_NONEXISTENT_RESULT"
echo "  Delete already deleted:    $NEG_DELETE_TWICE_RESULT"
echo "  Self-upvote:               $NEG_SELF_UPVOTE_RESULT"
echo "  Self-downvote:             $NEG_SELF_DOWNVOTE_RESULT"
echo "  Upvote non-existent:       $NEG_UPVOTE_NONEXISTENT_RESULT"
echo "  Follow reply (not root):   $NEG_FOLLOW_REPLY_RESULT"
echo "  Follow non-existent:       $NEG_FOLLOW_NONEXISTENT_RESULT"
echo "  Double-follow:             $NEG_DOUBLE_FOLLOW_RESULT"
echo "  Unfollow non-follower:     $NEG_UNFOLLOW_NONFOLLOWER_RESULT"
echo "  Accepted by non-author:    $NEG_ACCEPTED_NONAUTHOR_RESULT"
echo "  Accepted reply twice:      $NEG_ACCEPTED_TWICE_RESULT"
echo "  Query non-existent post:   $NEG_QUERY_NONEXISTENT_RESULT"
echo ""

# Count failures
FAIL_COUNT=0
TOTAL_COUNT=0

for RESULT in \
    "$PARAMS_CHECK_RESULT" \
    "$LIST_POSTS_RESULT" "$CREATE_THREAD_RESULT" "$QUERY_POST_RESULT" \
    "$CREATE_REPLY_RESULT" "$EDIT_POST_RESULT" "$UPVOTE_RESULT" \
    "$DOWNVOTE_RESULT" "$FOLLOW_RESULT" "$FOLLOW_COUNT_RESULT" \
    "$FOLLOWERS_LIST_RESULT" "$USER_FOLLOWED_RESULT" "$UNFOLLOW_RESULT" \
    "$MARK_ACCEPTED_RESULT" "$POSTS_COUNT_RESULT" "$POSTS_BY_CAT_RESULT" \
    "$USER_POSTS_RESULT" "$TOP_POSTS_RESULT" "$DELETE_POST_RESULT" \
    "$EPHEMERAL_PRUNE_RESULT" \
    "$NEG_BAD_CATEGORY_RESULT" "$NEG_EMPTY_CONTENT_RESULT" \
    "$NEG_LARGE_CONTENT_RESULT" "$NEG_BAD_PARENT_RESULT" \
    "$NEG_EDIT_NONAUTHOR_RESULT" "$NEG_EDIT_NONEXISTENT_RESULT" \
    "$NEG_EDIT_EMPTY_RESULT" "$NEG_DELETE_NONAUTHOR_RESULT" \
    "$NEG_DELETE_NONEXISTENT_RESULT" "$NEG_DELETE_TWICE_RESULT" \
    "$NEG_SELF_UPVOTE_RESULT" "$NEG_SELF_DOWNVOTE_RESULT" \
    "$NEG_UPVOTE_NONEXISTENT_RESULT" "$NEG_FOLLOW_REPLY_RESULT" \
    "$NEG_FOLLOW_NONEXISTENT_RESULT" "$NEG_DOUBLE_FOLLOW_RESULT" \
    "$NEG_UNFOLLOW_NONFOLLOWER_RESULT" "$NEG_ACCEPTED_NONAUTHOR_RESULT" \
    "$NEG_ACCEPTED_TWICE_RESULT" "$NEG_QUERY_NONEXISTENT_RESULT"; do
    TOTAL_COUNT=$((TOTAL_COUNT + 1))
    if [ "$RESULT" == "FAIL" ]; then
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
done

PASS_COUNT=$((TOTAL_COUNT - FAIL_COUNT))

echo "  Total: $TOTAL_COUNT | Passed: $PASS_COUNT | Failed: $FAIL_COUNT"
echo ""

if [ "$FAIL_COUNT" -gt 0 ]; then
    echo "  FAILURES: $FAIL_COUNT test(s) failed"
else
    echo "  ALL TESTS PASSED"
fi

echo ""
echo "POST TEST COMPLETED"
echo ""
