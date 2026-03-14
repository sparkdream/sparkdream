#!/bin/bash

echo "--- TESTING: RATE LIMITS (BLOG) ---"

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
# TEST 1: Post rate limit (ErrRateLimitExceeded)
# ========================================================================
echo "--- TEST 1: Post rate limit enforcement ---"
echo "  Using reader1 account to avoid interference with other tests."

# Query max_posts_per_day from params
MAX_POSTS=$($BINARY q blog params --output json 2>/dev/null | jq -r '.params.max_posts_per_day // "10"')
echo "  max_posts_per_day = $MAX_POSTS"

# Create posts sequentially up to the limit using reader1.
# Each post requires ~6s for block confirmation.
# We track successes and stop as soon as we hit a rate limit error.
SUCCESS_COUNT=0
RATE_LIMITED=false

for i in $(seq 1 $((MAX_POSTS + 1))); do
    echo "  Submitting post $i / $((MAX_POSTS + 1))..."

    TX_RES=$($BINARY tx blog create-post \
        "Rate limit test post $i" \
        "Body for rate limit test post number $i." \
        --from reader1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    # Check for broadcast-level rejection (rate limit can be caught here)
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash' 2>/dev/null)
    BROADCAST_CODE=$(echo "$TX_RES" | jq -r '.code // "0"' 2>/dev/null)

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        # Broadcast rejected entirely
        if echo "$TX_RES" | grep -qi "rate limit"; then
            echo "  Post $i: rate limited at broadcast (expected)"
            RATE_LIMITED=true
            break
        else
            echo "  Post $i: broadcast failed unexpectedly: $TX_RES"
            break
        fi
    fi

    if [ "$BROADCAST_CODE" != "0" ]; then
        if echo "$TX_RES" | jq -r '.raw_log // ""' | grep -qi "rate limit"; then
            echo "  Post $i: rate limited at broadcast (expected)"
            RATE_LIMITED=true
            break
        else
            echo "  Post $i: broadcast failed (code $BROADCAST_CODE)"
            break
        fi
    fi

    # Wait for tx to be included
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" == "0" ]; then
        SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
        echo "  Post $i: success (total: $SUCCESS_COUNT)"
    else
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        if echo "$RAW_LOG" | grep -qi "rate limit"; then
            echo "  Post $i: rate limited as expected (total successes: $SUCCESS_COUNT)"
            RATE_LIMITED=true
            break
        else
            echo "  Post $i: unexpected error: $RAW_LOG"
            break
        fi
    fi
done

if [ "$RATE_LIMITED" = true ]; then
    echo "  Rate limit hit after $SUCCESS_COUNT successful posts (limit: $MAX_POSTS)"
    record_result "Post rate limit enforcement" "PASS"
else
    echo "  ERROR: Created $SUCCESS_COUNT posts without hitting rate limit (limit: $MAX_POSTS)"
    record_result "Post rate limit enforcement" "FAIL"
fi

# ========================================================================
# TEST 2: Verify rate limit params exist and are valid
# ========================================================================
echo "--- TEST 2: Verify rate limit params in module params ---"

PARAMS_JSON=$($BINARY q blog params --output json 2>/dev/null)

MAX_POSTS_VAL=$(echo "$PARAMS_JSON" | jq -r '.params.max_posts_per_day // "0"')
MAX_REPLIES_VAL=$(echo "$PARAMS_JSON" | jq -r '.params.max_replies_per_day // "0"')
MAX_REACTIONS_VAL=$(echo "$PARAMS_JSON" | jq -r '.params.max_reactions_per_day // "0"')
MAX_PINS_VAL=$(echo "$PARAMS_JSON" | jq -r '.params.max_pins_per_day // "0"')

echo "  max_posts_per_day:     $MAX_POSTS_VAL"
echo "  max_replies_per_day:   $MAX_REPLIES_VAL"
echo "  max_reactions_per_day: $MAX_REACTIONS_VAL"
echo "  max_pins_per_day:      $MAX_PINS_VAL"

if [ "$MAX_POSTS_VAL" -gt 0 ] && [ "$MAX_REPLIES_VAL" -gt 0 ] && \
   [ "$MAX_REACTIONS_VAL" -gt 0 ] && [ "$MAX_PINS_VAL" -gt 0 ]; then
    record_result "Rate limit params present and positive" "PASS"
else
    echo "  ERROR: One or more rate limit params are missing or zero"
    record_result "Rate limit params present and positive" "FAIL"
fi

# ========================================================================
# TEST 3: Replies disabled (ErrRepliesDisabled)
# ========================================================================
echo "--- TEST 3: Replies disabled on post ---"
echo "  Creating a post, then disabling replies via update-post, then attempting a reply."

# Create a post with blogger1
TX_RES=$($BINARY tx blog create-post \
    "Post with replies to disable" \
    "This post will have replies disabled after creation." \
    --from blogger1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

DISABLE_REPLIES_POST_ID=""
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    DISABLE_REPLIES_POST_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="blog.post.created") | .attributes[] | select(.key=="post_id") | .value' | tr -d '"' | head -1)
    echo "  Created post $DISABLE_REPLIES_POST_ID"
else
    echo "  Failed to create post: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
fi

if [ -n "$DISABLE_REPLIES_POST_ID" ]; then
    # Disable replies via update-post
    TX_RES=$($BINARY tx blog update-post \
        "Post with replies to disable" \
        "This post will have replies disabled after creation." \
        "$DISABLE_REPLIES_POST_ID" \
        --replies-enabled=false \
        --from blogger1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        echo "  Replies disabled on post $DISABLE_REPLIES_POST_ID"

        # Verify via query
        POST_JSON=$($BINARY q blog show-post "$DISABLE_REPLIES_POST_ID" --output json 2>/dev/null)
        REPLIES_ENABLED=$(echo "$POST_JSON" | jq -r '.post.replies_enabled // "true"')
        echo "  Queried replies_enabled = $REPLIES_ENABLED"

        # Now try to reply - should fail
        TX_RES=$($BINARY tx blog create-reply \
            "$DISABLE_REPLIES_POST_ID" \
            "This reply should be rejected." \
            --from blogger2 \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 50000uspark \
            -y \
            --output json 2>&1)

        expect_tx_failure "$TX_RES" "disabled" "Reply to post with replies disabled"
    else
        echo "  Failed to disable replies: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Reply to post with replies disabled" "FAIL"
    fi
else
    echo "  Skipped (no post created)"
    record_result "Reply to post with replies disabled" "FAIL"
fi

# ========================================================================
# TEST 4: Max reply depth (ErrMaxReplyDepth)
# ========================================================================
echo "--- TEST 4: Maximum reply nesting depth ---"
echo "  DefaultMaxReplyDepth = 5. Creating a chain of nested replies to exceed it."

# Query actual max reply depth from params
MAX_DEPTH=$($BINARY q blog params --output json 2>/dev/null | jq -r '.params.max_reply_depth // "5"')
echo "  max_reply_depth = $MAX_DEPTH"

# Create a post for the nesting test
TX_RES=$($BINARY tx blog create-post \
    "Post for reply depth test" \
    "Testing maximum reply nesting depth enforcement." \
    --from blogger2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

DEPTH_POST_ID=""
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    DEPTH_POST_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="blog.post.created") | .attributes[] | select(.key=="post_id") | .value' | tr -d '"' | head -1)
    echo "  Created post $DEPTH_POST_ID"
else
    echo "  Failed to create post: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
fi

if [ -n "$DEPTH_POST_ID" ]; then
    # Create a chain of nested replies up to max depth.
    # Top-level reply = depth 0, then each nested reply increments depth by 1.
    # We need MAX_DEPTH + 1 replies total: depths 0..MAX_DEPTH are valid,
    # depth MAX_DEPTH+1 should fail.
    PARENT_REPLY_ID=0
    DEPTH_REACHED=0
    DEPTH_TEST_PASS=false

    for d in $(seq 0 $MAX_DEPTH); do
        echo "  Creating reply at depth $d (parent_reply_id=$PARENT_REPLY_ID)..."

        if [ "$PARENT_REPLY_ID" -eq 0 ]; then
            TX_RES=$($BINARY tx blog create-reply \
                "$DEPTH_POST_ID" \
                "Reply at depth $d for nesting test." \
                --from blogger2 \
                --chain-id $CHAIN_ID \
                --keyring-backend test \
                --fees 50000uspark \
                -y \
                --output json 2>&1)
        else
            TX_RES=$($BINARY tx blog create-reply \
                "$DEPTH_POST_ID" \
                "Reply at depth $d for nesting test." \
                --parent-reply-id "$PARENT_REPLY_ID" \
                --from blogger2 \
                --chain-id $CHAIN_ID \
                --keyring-backend test \
                --fees 50000uspark \
                -y \
                --output json 2>&1)
        fi

        if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
            REPLY_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="blog.reply.created") | .attributes[] | select(.key=="reply_id") | .value' | tr -d '"' | head -1)
            echo "  Reply created: id=$REPLY_ID, depth=$d"
            PARENT_REPLY_ID=$REPLY_ID
            DEPTH_REACHED=$d
        else
            echo "  Failed to create reply at depth $d: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
            break
        fi
    done

    if [ "$DEPTH_REACHED" -eq "$MAX_DEPTH" ]; then
        echo "  Successfully created replies at depths 0..$MAX_DEPTH"
        echo "  Now attempting reply at depth $((MAX_DEPTH + 1)) (should fail)..."

        TX_RES=$($BINARY tx blog create-reply \
            "$DEPTH_POST_ID" \
            "This reply at depth $((MAX_DEPTH + 1)) should be rejected." \
            --parent-reply-id "$PARENT_REPLY_ID" \
            --from blogger2 \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 50000uspark \
            -y \
            --output json 2>&1)

        expect_tx_failure "$TX_RES" "depth" "Max reply depth exceeded"
    else
        echo "  ERROR: Only reached depth $DEPTH_REACHED, expected $MAX_DEPTH before testing overflow"
        record_result "Max reply depth exceeded" "FAIL"
    fi
else
    echo "  Skipped (no post created)"
    record_result "Max reply depth exceeded" "FAIL"
fi

# ========================================================================
# SUMMARY
# ========================================================================
echo "============================================================================"
echo "  BLOG RATE LIMIT TEST SUMMARY"
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
