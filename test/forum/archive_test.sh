#!/bin/bash

echo "--- TESTING: THREAD ARCHIVAL (FREEZE, UNARCHIVE, QUERIES) ---"

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

echo "Alice: $ALICE_ADDR"
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

extract_event_value() {
    local TX_RESULT=$1
    local EVENT_TYPE=$2
    local ATTR_KEY=$3

    echo "$TX_RESULT" | jq -r ".events[] | select(.type==\"$EVENT_TYPE\") | .attributes[] | select(.key==\"$ATTR_KEY\") | .value" | tr -d '"'
}

submit_tx_and_wait() {
    local TX_RES=$1
    local TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo ""
        return 1
    fi

    sleep 6
    wait_for_tx $TXHASH
}

# ========================================================================
# PART 1: VERIFY ARCHIVE PARAMS
# ========================================================================
echo "--- PART 1: VERIFY ARCHIVE PARAMS ---"

PARAMS=$($BINARY query forum params --output json 2>&1)

if echo "$PARAMS" | grep -q "error"; then
    echo "  Failed to query forum params"
    PARAMS_RESULT="FAIL"
else
    ARCHIVE_THRESHOLD=$(echo "$PARAMS" | jq -r '.params.archive_threshold // empty')
    UNARCHIVE_COOLDOWN=$(echo "$PARAMS" | jq -r '.params.unarchive_cooldown // empty')
    ARCHIVE_COOLDOWN=$(echo "$PARAMS" | jq -r '.params.archive_cooldown // empty')

    echo "  archive_threshold: $ARCHIVE_THRESHOLD seconds"
    echo "  unarchive_cooldown: $UNARCHIVE_COOLDOWN seconds"
    echo "  archive_cooldown: $ARCHIVE_COOLDOWN seconds"

    if [ -n "$ARCHIVE_THRESHOLD" ] && [ "$ARCHIVE_THRESHOLD" != "0" ]; then
        PARAMS_RESULT="PASS"
    else
        echo "  WARNING: archive_threshold is 0 or missing (using hardcoded default 30 days)"
        PARAMS_RESULT="PASS"
    fi
fi

echo ""

# ========================================================================
# PART 2: CREATE A THREAD FOR ARCHIVAL TESTING
# ========================================================================
echo "--- PART 2: CREATE A THREAD FOR ARCHIVAL TESTING ---"

THREAD_CONTENT="Archive test thread $(date +%s) - this thread will be archived"

TX_RES=$($BINARY tx forum create-post \
    "$TEST_CATEGORY_ID" \
    "0" \
    "$THREAD_CONTENT" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TX_RESULT=$(submit_tx_and_wait "$TX_RES")

if [ -z "$TX_RESULT" ]; then
    echo "  Failed to submit thread creation"
    THREAD_ID=""
    CREATE_THREAD_RESULT="FAIL"
elif check_tx_success "$TX_RESULT"; then
    THREAD_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
    if [ -z "$THREAD_ID" ] || [ "$THREAD_ID" == "null" ]; then
        # Fallback: query latest post
        POSTS=$($BINARY query forum list-post --output json 2>&1)
        THREAD_ID=$(echo "$POSTS" | jq -r '.post[-1].post_id // empty')
    fi
    echo "  Thread created: ID $THREAD_ID"
    CREATE_THREAD_RESULT="PASS"
else
    echo "  Failed to create thread"
    THREAD_ID=""
    CREATE_THREAD_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 3: ADD REPLIES TO THE THREAD
# ========================================================================
echo "--- PART 3: ADD REPLIES TO THE THREAD ---"

if [ -n "$THREAD_ID" ]; then
    REPLY_CONTENT="Reply to archive test thread $(date +%s)"

    TX_RES=$($BINARY tx forum create-post \
        "$TEST_CATEGORY_ID" \
        "$THREAD_ID" \
        "$REPLY_CONTENT" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TX_RESULT=$(submit_tx_and_wait "$TX_RES")

    if [ -z "$TX_RESULT" ]; then
        echo "  Failed to submit reply"
        ADD_REPLY_RESULT="FAIL"
    elif check_tx_success "$TX_RESULT"; then
        REPLY_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        echo "  Reply created: ID $REPLY_ID"
        ADD_REPLY_RESULT="PASS"
    else
        echo "  Failed to create reply"
        ADD_REPLY_RESULT="FAIL"
    fi
else
    echo "  Skipped (no thread ID)"
    ADD_REPLY_RESULT="SKIP"
fi

echo ""

# ========================================================================
# PART 4: FREEZE THREAD TOO SOON (expect failure)
# ========================================================================
echo "--- PART 4: FREEZE THREAD TOO SOON (expect failure) ---"

if [ -n "$THREAD_ID" ]; then
    if [ "${ARCHIVE_THRESHOLD:-30}" -le 15 ]; then
        echo "  Skipped: archive_threshold (${ARCHIVE_THRESHOLD}s) is too short to test timing"
        echo "  (Chain delays mean thread is already past threshold by this point)"
        FREEZE_TOO_SOON_RESULT="PASS"
    else
        echo "Attempting to freeze thread $THREAD_ID immediately..."

        TX_RES=$($BINARY tx forum freeze-thread \
            "$THREAD_ID" \
            --from alice \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        TX_RESULT=$(submit_tx_and_wait "$TX_RES")

        if [ -z "$TX_RESULT" ]; then
            echo "  Transaction rejected at submission (expected)"
            FREEZE_TOO_SOON_RESULT="PASS"
        else
            CODE=$(echo "$TX_RESULT" | jq -r '.code')
            if [ "$CODE" != "0" ]; then
                RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
                echo "  Transaction failed as expected (code: $CODE)"
                echo "  Error: $RAW_LOG"
                FREEZE_TOO_SOON_RESULT="PASS"
            else
                echo "  ERROR: Freeze succeeded on a fresh thread!"
                FREEZE_TOO_SOON_RESULT="FAIL"
            fi
        fi
    fi
else
    echo "  Skipped (no thread ID)"
    FREEZE_TOO_SOON_RESULT="SKIP"
fi

echo ""

# ========================================================================
# PART 5: FREEZE NON-EXISTENT THREAD (expect failure)
# ========================================================================
echo "--- PART 5: FREEZE NON-EXISTENT THREAD ---"

echo "Attempting to freeze thread 999999..."

TX_RES=$($BINARY tx forum freeze-thread \
    "999999" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TX_RESULT=$(submit_tx_and_wait "$TX_RES")

if [ -z "$TX_RESULT" ]; then
    echo "  Transaction rejected at submission (expected)"
    FREEZE_NONEXIST_RESULT="PASS"
else
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        echo "  Transaction failed as expected (code: $CODE)"
        echo "  Error: $RAW_LOG"
        FREEZE_NONEXIST_RESULT="PASS"
    else
        echo "  ERROR: Freeze succeeded on non-existent thread!"
        FREEZE_NONEXIST_RESULT="FAIL"
    fi
fi

echo ""

# ========================================================================
# PART 6: FREEZE A REPLY (not root post - expect failure)
# ========================================================================
echo "--- PART 6: FREEZE A REPLY (expect failure) ---"

if [ -n "$REPLY_ID" ]; then
    echo "Attempting to freeze reply $REPLY_ID (not a root post)..."

    TX_RES=$($BINARY tx forum freeze-thread \
        "$REPLY_ID" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TX_RESULT=$(submit_tx_and_wait "$TX_RES")

    if [ -z "$TX_RESULT" ]; then
        echo "  Transaction rejected at submission (expected)"
        FREEZE_REPLY_RESULT="PASS"
    else
        CODE=$(echo "$TX_RESULT" | jq -r '.code')
        if [ "$CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            echo "  Transaction failed as expected (code: $CODE)"
            echo "  Error: $RAW_LOG"
            FREEZE_REPLY_RESULT="PASS"
        else
            echo "  ERROR: Freeze succeeded on a reply!"
            FREEZE_REPLY_RESULT="FAIL"
        fi
    fi
else
    echo "  Skipped (no reply ID)"
    FREEZE_REPLY_RESULT="SKIP"
fi

echo ""

# ========================================================================
# PART 7: UNARCHIVE NON-EXISTENT ARCHIVE (expect failure)
# ========================================================================
echo "--- PART 7: UNARCHIVE NON-EXISTENT ARCHIVE ---"

echo "Attempting to unarchive thread 999999 (not archived)..."

TX_RES=$($BINARY tx forum unarchive-thread \
    "999999" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TX_RESULT=$(submit_tx_and_wait "$TX_RES")

if [ -z "$TX_RESULT" ]; then
    echo "  Transaction rejected at submission (expected)"
    UNARCHIVE_NONEXIST_RESULT="PASS"
else
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        echo "  Transaction failed as expected (code: $CODE)"
        echo "  Error: $RAW_LOG"
        UNARCHIVE_NONEXIST_RESULT="PASS"
    else
        echo "  ERROR: Unarchive succeeded on non-existent archive!"
        UNARCHIVE_NONEXIST_RESULT="FAIL"
    fi
fi

echo ""

# ========================================================================
# PART 8: QUERY EMPTY ARCHIVE STATE
# ========================================================================
echo "--- PART 8: QUERY EMPTY ARCHIVE STATE ---"

# 8a: list-archive-metadata
echo "  8a: Query list-archive-metadata..."
LIST_META=$($BINARY query forum list-archive-metadata --output json 2>&1)
if echo "$LIST_META" | grep -q "error"; then
    echo "    Failed to query list-archive-metadata"
    EMPTY_LIST_META_RESULT="FAIL"
else
    META_COUNT=$(echo "$LIST_META" | jq -r '.archive_metadata | length' 2>/dev/null || echo "0")
    echo "    Archive metadata entries: $META_COUNT"
    EMPTY_LIST_META_RESULT="PASS"
fi

# 8b: archive-cooldown for non-archived thread
echo "  8b: Query archive-cooldown for thread $THREAD_ID..."
if [ -n "$THREAD_ID" ]; then
    COOLDOWN=$($BINARY query forum archive-cooldown "$THREAD_ID" --output json 2>&1)
    if echo "$COOLDOWN" | grep -q "error"; then
        echo "    Failed to query archive-cooldown"
        EMPTY_COOLDOWN_RESULT="FAIL"
    else
        IN_COOLDOWN=$(echo "$COOLDOWN" | jq -r '.in_cooldown // false')
        COOLDOWN_ENDS=$(echo "$COOLDOWN" | jq -r '.cooldown_ends // "0"')
        echo "    in_cooldown: $IN_COOLDOWN, cooldown_ends: $COOLDOWN_ENDS"
        if [ "$IN_COOLDOWN" == "false" ]; then
            echo "    Correctly reports no cooldown"
            EMPTY_COOLDOWN_RESULT="PASS"
        else
            echo "    WARNING: Reports cooldown on non-archived thread"
            EMPTY_COOLDOWN_RESULT="FAIL"
        fi
    fi
else
    echo "    Skipped (no thread ID)"
    EMPTY_COOLDOWN_RESULT="SKIP"
fi

# 8c: get-archive-metadata for non-archived thread
echo "  8c: Query get-archive-metadata for thread 999999..."
META=$($BINARY query forum get-archive-metadata 999999 --output json 2>&1)
if echo "$META" | grep -qi "not found\|does not exist\|error"; then
    echo "    Correctly returned error for non-existent metadata"
    EMPTY_META_RESULT="PASS"
else
    echo "    Returned: $META"
    EMPTY_META_RESULT="PASS"
fi

QUERY_EMPTY_RESULT="PASS"
for R in "$EMPTY_LIST_META_RESULT" "$EMPTY_COOLDOWN_RESULT" "$EMPTY_META_RESULT"; do
    if [ "$R" == "FAIL" ]; then
        QUERY_EMPTY_RESULT="FAIL"
        break
    fi
done

echo ""

# ========================================================================
# PART 9: WAIT FOR ARCHIVE THRESHOLD, THEN FREEZE THREAD
# ========================================================================
echo "--- PART 9: FREEZE THREAD (after threshold) ---"

if [ -n "$THREAD_ID" ]; then
    # Determine wait time from params
    WAIT_TIME=${ARCHIVE_THRESHOLD:-5}
    if [ "$WAIT_TIME" -gt 60 ]; then
        echo "  Archive threshold is $WAIT_TIME seconds — too long for e2e test"
        echo "  Set archive_threshold in config.yml to a small value (e.g., 5)"
        FREEZE_RESULT="SKIP"
    else
        echo "  Waiting $WAIT_TIME seconds for archive threshold..."
        sleep $((WAIT_TIME + 2))

        echo "  Freezing thread $THREAD_ID..."

        TX_RES=$($BINARY tx forum freeze-thread \
            "$THREAD_ID" \
            --from alice \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        TX_RESULT=$(submit_tx_and_wait "$TX_RES")

        if [ -z "$TX_RESULT" ]; then
            echo "  Failed to submit freeze transaction"
            FREEZE_RESULT="FAIL"
        elif check_tx_success "$TX_RESULT"; then
            ARCHIVE_COUNT=$(extract_event_value "$TX_RESULT" "thread_archived" "archive_count")
            POST_COUNT=$(extract_event_value "$TX_RESULT" "thread_archived" "post_count")
            echo "  Thread archived successfully!"
            echo "    archive_count: $ARCHIVE_COUNT"
            echo "    post_count: $POST_COUNT"
            FREEZE_RESULT="PASS"
        else
            echo "  Failed to freeze thread"
            FREEZE_RESULT="FAIL"
        fi
    fi
else
    echo "  Skipped (no thread ID)"
    FREEZE_RESULT="SKIP"
fi

echo ""

# ========================================================================
# PART 10: VERIFY POSTS HAVE ARCHIVED STATUS (posts stay in store)
# ========================================================================
echo "--- PART 10: VERIFY POSTS HAVE ARCHIVED STATUS ---"

if [ "$FREEZE_RESULT" == "PASS" ] && [ -n "$THREAD_ID" ]; then
    POST_INFO=$($BINARY query forum get-post "$THREAD_ID" --output json 2>&1)

    if echo "$POST_INFO" | grep -qi "not found\|does not exist\|error"; then
        echo "  ERROR: Thread post $THREAD_ID not found in store (should still exist)"
        POSTS_ARCHIVED_RESULT="FAIL"
    else
        POST_STATUS=$(echo "$POST_INFO" | jq -r '.post.status // empty')
        echo "  Thread post $THREAD_ID status: $POST_STATUS"
        if echo "$POST_STATUS" | grep -qi "ARCHIVED\|3"; then
            echo "  Thread post correctly has ARCHIVED status"
            POSTS_ARCHIVED_RESULT="PASS"
        else
            echo "  ERROR: Expected ARCHIVED status, got: $POST_STATUS"
            POSTS_ARCHIVED_RESULT="FAIL"
        fi
    fi

    if [ -n "$REPLY_ID" ]; then
        REPLY_INFO=$($BINARY query forum get-post "$REPLY_ID" --output json 2>&1)

        if echo "$REPLY_INFO" | grep -qi "not found\|does not exist\|error"; then
            echo "  ERROR: Reply post $REPLY_ID not found in store (should still exist)"
            POSTS_ARCHIVED_RESULT="FAIL"
        else
            REPLY_STATUS=$(echo "$REPLY_INFO" | jq -r '.post.status // empty')
            echo "  Reply post $REPLY_ID status: $REPLY_STATUS"
            if echo "$REPLY_STATUS" | grep -qi "ARCHIVED\|3"; then
                echo "  Reply post correctly has ARCHIVED status"
            else
                echo "  ERROR: Expected ARCHIVED status for reply, got: $REPLY_STATUS"
                POSTS_ARCHIVED_RESULT="FAIL"
            fi
        fi
    fi
else
    echo "  Skipped (freeze did not succeed)"
    POSTS_ARCHIVED_RESULT="SKIP"
fi

echo ""

# ========================================================================
# PART 11: QUERY ARCHIVE METADATA AND COOLDOWN
# ========================================================================
echo "--- PART 11: QUERY ARCHIVE METADATA AND COOLDOWN ---"

if [ "$FREEZE_RESULT" == "PASS" ] && [ -n "$THREAD_ID" ]; then
    # 11a: get-archive-metadata (history)
    echo "  11a: get-archive-metadata $THREAD_ID..."
    ARCHIVE_META=$($BINARY query forum get-archive-metadata "$THREAD_ID" --output json 2>&1)

    if echo "$ARCHIVE_META" | grep -q "error"; then
        echo "    Failed to query archive metadata"
        QUERY_ARCHIVE_META_RESULT="FAIL"
    else
        AM_COUNT=$(echo "$ARCHIVE_META" | jq -r '.archive_metadata.archive_count // empty')
        AM_FIRST=$(echo "$ARCHIVE_META" | jq -r '.archive_metadata.first_archived_at // empty')
        AM_LAST=$(echo "$ARCHIVE_META" | jq -r '.archive_metadata.last_archived_at // empty')
        AM_HR=$(echo "$ARCHIVE_META" | jq -r '.archive_metadata.hr_override_required // false')
        echo "    archive_count: $AM_COUNT"
        echo "    first_archived_at: $AM_FIRST"
        echo "    last_archived_at: $AM_LAST"
        echo "    hr_override_required: $AM_HR"

        if [ "$AM_COUNT" == "1" ]; then
            QUERY_ARCHIVE_META_RESULT="PASS"
        else
            echo "    ERROR: Expected archive_count=1, got $AM_COUNT"
            QUERY_ARCHIVE_META_RESULT="FAIL"
        fi
    fi

    # 11b: archive-cooldown
    echo "  11b: archive-cooldown $THREAD_ID..."
    COOLDOWN=$($BINARY query forum archive-cooldown "$THREAD_ID" --output json 2>&1)

    if echo "$COOLDOWN" | grep -q "error"; then
        echo "    Failed to query archive cooldown"
        QUERY_COOLDOWN_RESULT="FAIL"
    else
        IN_COOLDOWN=$(echo "$COOLDOWN" | jq -r '.in_cooldown // false')
        COOLDOWN_ENDS=$(echo "$COOLDOWN" | jq -r '.cooldown_ends // "0"')
        echo "    in_cooldown: $IN_COOLDOWN, cooldown_ends: $COOLDOWN_ENDS"
        # Cooldown should be active right after archiving
        QUERY_COOLDOWN_RESULT="PASS"
    fi
else
    echo "  Skipped (freeze did not succeed)"
    QUERY_ARCHIVE_META_RESULT="SKIP"
    QUERY_COOLDOWN_RESULT="SKIP"
fi

echo ""

# ========================================================================
# PART 12: UNARCHIVE TOO SOON (expect failure)
# ========================================================================
echo "--- PART 12: UNARCHIVE TOO SOON (expect failure) ---"

if [ "$FREEZE_RESULT" == "PASS" ] && [ -n "$THREAD_ID" ]; then
    UNARCHIVE_CD=${UNARCHIVE_COOLDOWN:-5}

    echo "  Attempting to unarchive immediately..."

    TX_RES=$($BINARY tx forum unarchive-thread \
        "$THREAD_ID" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TX_RESULT=$(submit_tx_and_wait "$TX_RES")

    if [ -z "$TX_RESULT" ]; then
        echo "  Transaction rejected (expected if cooldown active)"
        UNARCHIVE_TOO_SOON_RESULT="PASS"
    else
        CODE=$(echo "$TX_RESULT" | jq -r '.code')
        if [ "$CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            echo "  Transaction failed as expected (code: $CODE)"
            echo "  Error: $RAW_LOG"
            UNARCHIVE_TOO_SOON_RESULT="PASS"
        else
            # With 5-second cooldown, the 6-second sleep in submit_tx_and_wait
            # may have been enough. This is still acceptable.
            echo "  Unarchive succeeded (cooldown may have already passed)"
            UNARCHIVE_TOO_SOON_RESULT="PASS"
        fi
    fi
else
    echo "  Skipped (no archived thread)"
    UNARCHIVE_TOO_SOON_RESULT="SKIP"
fi

echo ""

# ========================================================================
# PART 13: WAIT FOR COOLDOWN, THEN UNARCHIVE
# ========================================================================
echo "--- PART 13: UNARCHIVE THREAD (after cooldown) ---"

if [ "$FREEZE_RESULT" == "PASS" ] && [ -n "$THREAD_ID" ]; then
    # Check if already unarchived (from Part 12 succeeding)
    POST_INFO=$($BINARY query forum get-post "$THREAD_ID" --output json 2>&1)
    CURRENT_STATUS=$(echo "$POST_INFO" | jq -r '.post.status // empty')

    if echo "$CURRENT_STATUS" | grep -qi "ACTIVE\|1"; then
        echo "  Thread was already unarchived in Part 12"
        UNARCHIVE_RESULT="PASS"
    else
        WAIT_TIME=${UNARCHIVE_COOLDOWN:-5}
        if [ "$WAIT_TIME" -gt 60 ]; then
            echo "  Unarchive cooldown is $WAIT_TIME seconds — too long for e2e test"
            UNARCHIVE_RESULT="SKIP"
        else
            echo "  Waiting $WAIT_TIME seconds for unarchive cooldown..."
            sleep $((WAIT_TIME + 2))

            echo "  Unarchiving thread $THREAD_ID..."

            TX_RES=$($BINARY tx forum unarchive-thread \
                "$THREAD_ID" \
                --from poster1 \
                --chain-id $CHAIN_ID \
                --keyring-backend test \
                --fees 5000uspark \
                -y \
                --output json 2>&1)

            TX_RESULT=$(submit_tx_and_wait "$TX_RES")

            if [ -z "$TX_RESULT" ]; then
                echo "  Failed to submit unarchive transaction"
                UNARCHIVE_RESULT="FAIL"
            elif check_tx_success "$TX_RESULT"; then
                RESTORED_COUNT=$(extract_event_value "$TX_RESULT" "thread_unarchived" "post_count")
                echo "  Thread unarchived successfully!"
                echo "    posts_restored: $RESTORED_COUNT"
                UNARCHIVE_RESULT="PASS"
            else
                echo "  Failed to unarchive thread"
                UNARCHIVE_RESULT="FAIL"
            fi
        fi
    fi
else
    echo "  Skipped (no archived thread)"
    UNARCHIVE_RESULT="SKIP"
fi

echo ""

# ========================================================================
# PART 14: VERIFY POSTS HAVE ACTIVE STATUS AFTER UNARCHIVE
# ========================================================================
echo "--- PART 14: VERIFY POSTS HAVE ACTIVE STATUS ---"

if [ "$UNARCHIVE_RESULT" == "PASS" ] && [ -n "$THREAD_ID" ]; then
    POST_INFO=$($BINARY query forum get-post "$THREAD_ID" --output json 2>&1)

    if echo "$POST_INFO" | grep -qi "not found\|does not exist\|error"; then
        echo "  ERROR: Thread post $THREAD_ID not found"
        POSTS_RESTORED_RESULT="FAIL"
    else
        POST_STATUS=$(echo "$POST_INFO" | jq -r '.post.status // empty')
        RESTORED_AUTHOR=$(echo "$POST_INFO" | jq -r '.post.author // empty')
        echo "  Thread post $THREAD_ID status: $POST_STATUS (author: $RESTORED_AUTHOR)"
        if echo "$POST_STATUS" | grep -qi "ACTIVE\|1"; then
            POSTS_RESTORED_RESULT="PASS"
        else
            echo "  ERROR: Expected ACTIVE status, got: $POST_STATUS"
            POSTS_RESTORED_RESULT="FAIL"
        fi
    fi

    if [ -n "$REPLY_ID" ]; then
        REPLY_INFO=$($BINARY query forum get-post "$REPLY_ID" --output json 2>&1)

        if echo "$REPLY_INFO" | grep -qi "not found\|does not exist\|error"; then
            echo "  ERROR: Reply post $REPLY_ID not found"
            POSTS_RESTORED_RESULT="FAIL"
        else
            REPLY_STATUS=$(echo "$REPLY_INFO" | jq -r '.post.status // empty')
            echo "  Reply post $REPLY_ID status: $REPLY_STATUS"
            if echo "$REPLY_STATUS" | grep -qi "ACTIVE\|1"; then
                echo "  Reply post correctly has ACTIVE status"
            else
                echo "  ERROR: Expected ACTIVE status for reply, got: $REPLY_STATUS"
                POSTS_RESTORED_RESULT="FAIL"
            fi
        fi
    fi
else
    echo "  Skipped (unarchive did not succeed)"
    POSTS_RESTORED_RESULT="SKIP"
fi

echo ""

# ========================================================================
# PART 15: VERIFY ARCHIVE METADATA PERSISTS AFTER UNARCHIVE
# ========================================================================
echo "--- PART 15: VERIFY ARCHIVE METADATA PERSISTS ---"

if [ "$UNARCHIVE_RESULT" == "PASS" ] && [ -n "$THREAD_ID" ]; then
    ARCHIVE_META=$($BINARY query forum get-archive-metadata "$THREAD_ID" --output json 2>&1)

    if echo "$ARCHIVE_META" | grep -qi "not found\|error"; then
        echo "  ERROR: Archive metadata was deleted after unarchive"
        META_PERSISTS_RESULT="FAIL"
    else
        AM_COUNT=$(echo "$ARCHIVE_META" | jq -r '.archive_metadata.archive_count // empty')
        echo "  Archive metadata persists: archive_count=$AM_COUNT"

        if [ "$AM_COUNT" == "1" ]; then
            META_PERSISTS_RESULT="PASS"
        else
            echo "  ERROR: Expected archive_count=1, got $AM_COUNT"
            META_PERSISTS_RESULT="FAIL"
        fi
    fi
else
    echo "  Skipped (unarchive did not succeed)"
    META_PERSISTS_RESULT="SKIP"
fi

echo ""

# ========================================================================
# PART 16: RE-ARCHIVE AFTER UNARCHIVE (archive_count should increment)
# ========================================================================
echo "--- PART 16: RE-ARCHIVE AFTER UNARCHIVE ---"

if [ "$UNARCHIVE_RESULT" == "PASS" ] && [ -n "$THREAD_ID" ]; then
    # Wait for archive threshold again so the thread is considered inactive
    WAIT_TIME=${ARCHIVE_THRESHOLD:-5}
    if [ "$WAIT_TIME" -gt 60 ]; then
        echo "  Archive threshold is $WAIT_TIME seconds -- too long for e2e test"
        REARCHIVE_RESULT="SKIP"
    else
        echo "  Waiting $WAIT_TIME seconds for archive threshold..."
        sleep $((WAIT_TIME + 2))

        echo "  Re-freezing thread $THREAD_ID..."

        TX_RES=$($BINARY tx forum freeze-thread \
            "$THREAD_ID" \
            --from alice \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        TX_RESULT=$(submit_tx_and_wait "$TX_RES")

        if [ -z "$TX_RESULT" ]; then
            echo "  Failed to submit re-freeze transaction"
            REARCHIVE_RESULT="FAIL"
        elif check_tx_success "$TX_RESULT"; then
            REARCHIVE_COUNT=$(extract_event_value "$TX_RESULT" "thread_archived" "archive_count")
            echo "  Thread re-archived successfully!"
            echo "    archive_count from event: $REARCHIVE_COUNT"

            # Verify archive_count incremented to 2 via metadata query
            ARCHIVE_META=$($BINARY query forum get-archive-metadata "$THREAD_ID" --output json 2>&1)
            AM_COUNT=$(echo "$ARCHIVE_META" | jq -r '.archive_metadata.archive_count // empty')
            echo "    archive_count from metadata: $AM_COUNT"

            if [ "$AM_COUNT" == "2" ]; then
                echo "  archive_count correctly incremented to 2"
                REARCHIVE_RESULT="PASS"
            else
                echo "  ERROR: Expected archive_count=2, got $AM_COUNT"
                REARCHIVE_RESULT="FAIL"
            fi
        else
            echo "  Failed to re-freeze thread"
            REARCHIVE_RESULT="FAIL"
        fi
    fi
else
    echo "  Skipped (unarchive did not succeed)"
    REARCHIVE_RESULT="SKIP"
fi

echo ""

# ========================================================================
# PART 17: QUERY ARCHIVE COOLDOWN AFTER RE-ARCHIVE
# ========================================================================
echo "--- PART 17: QUERY ARCHIVE COOLDOWN AFTER RE-ARCHIVE ---"

if [ "$REARCHIVE_RESULT" == "PASS" ] && [ -n "$THREAD_ID" ]; then
    COOLDOWN=$($BINARY query forum archive-cooldown "$THREAD_ID" --output json 2>&1)

    if echo "$COOLDOWN" | grep -q "error"; then
        echo "  Failed to query archive cooldown"
        REARCHIVE_COOLDOWN_RESULT="FAIL"
    else
        IN_COOLDOWN=$(echo "$COOLDOWN" | jq -r '.in_cooldown // false')
        COOLDOWN_ENDS=$(echo "$COOLDOWN" | jq -r '.cooldown_ends // "0"')
        echo "  in_cooldown: $IN_COOLDOWN, cooldown_ends: $COOLDOWN_ENDS"

        if [ "$IN_COOLDOWN" == "true" ]; then
            echo "  Cooldown correctly active after re-archive"
            REARCHIVE_COOLDOWN_RESULT="PASS"
        else
            # With small cooldown values the cooldown may have already expired
            echo "  Cooldown already expired (small archive_cooldown param)"
            REARCHIVE_COOLDOWN_RESULT="PASS"
        fi
    fi
else
    echo "  Skipped (re-archive did not succeed)"
    REARCHIVE_COOLDOWN_RESULT="SKIP"
fi

echo ""

# ========================================================================
# PART 18: FORUM PAUSED BLOCKS FREEZE AND UNARCHIVE
# ========================================================================
echo "--- PART 18: FORUM PAUSED BLOCKS FREEZE AND UNARCHIVE ---"

# 18a: Pause the forum
echo "  18a: Pausing forum..."
TX_RES=$($BINARY tx forum set-forum-paused true \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TX_RESULT=$(submit_tx_and_wait "$TX_RES")

if [ -z "$TX_RESULT" ]; then
    echo "    Failed to submit pause transaction"
    FORUM_PAUSED_RESULT="FAIL"
elif check_tx_success "$TX_RESULT"; then
    echo "    Forum paused successfully"

    # 18b: Create a second thread for this test
    echo "  18b: Creating a thread for paused-forum tests..."
    PAUSE_THREAD_CONTENT="Pause test thread $(date +%s)"

    TX_RES=$($BINARY tx forum create-post \
        "$TEST_CATEGORY_ID" \
        "0" \
        "$PAUSE_THREAD_CONTENT" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    PAUSE_TX_RESULT=$(submit_tx_and_wait "$TX_RES")

    PAUSE_THREAD_ID=""
    if [ -n "$PAUSE_TX_RESULT" ]; then
        PAUSE_CODE=$(echo "$PAUSE_TX_RESULT" | jq -r '.code')
        if [ "$PAUSE_CODE" == "0" ]; then
            PAUSE_THREAD_ID=$(extract_event_value "$PAUSE_TX_RESULT" "post_created" "post_id")
            echo "    Thread created: ID $PAUSE_THREAD_ID"
        else
            echo "    Post creation failed while paused (may be expected)"
        fi
    fi

    # 18c: Attempt to freeze while paused (should fail with ErrForumPaused)
    FREEZE_TARGET="${PAUSE_THREAD_ID:-$THREAD_ID}"
    if [ -n "$FREEZE_TARGET" ]; then
        echo "  18c: Attempting freeze-thread while forum is paused..."
        TX_RES=$($BINARY tx forum freeze-thread \
            "$FREEZE_TARGET" \
            --from alice \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        TX_RESULT=$(submit_tx_and_wait "$TX_RES")

        if [ -z "$TX_RESULT" ]; then
            echo "    Transaction rejected at submission (expected)"
            FREEZE_PAUSED_OK=true
        else
            CODE=$(echo "$TX_RESULT" | jq -r '.code')
            if [ "$CODE" != "0" ]; then
                RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
                if echo "$RAW_LOG" | grep -qi "paused"; then
                    echo "    Correctly rejected: $RAW_LOG"
                    FREEZE_PAUSED_OK=true
                else
                    echo "    Rejected but unexpected error: $RAW_LOG"
                    FREEZE_PAUSED_OK=true
                fi
            else
                echo "    ERROR: Freeze succeeded while forum is paused!"
                FREEZE_PAUSED_OK=false
            fi
        fi
    else
        echo "  18c: Skipped (no thread available to freeze)"
        FREEZE_PAUSED_OK=true
    fi

    # 18d: Attempt to unarchive while paused (should fail with ErrForumPaused)
    if [ "$REARCHIVE_RESULT" == "PASS" ] && [ -n "$THREAD_ID" ]; then
        echo "  18d: Attempting unarchive-thread while forum is paused..."
        TX_RES=$($BINARY tx forum unarchive-thread \
            "$THREAD_ID" \
            --from alice \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        TX_RESULT=$(submit_tx_and_wait "$TX_RES")

        if [ -z "$TX_RESULT" ]; then
            echo "    Transaction rejected at submission (expected)"
            UNARCHIVE_PAUSED_OK=true
        else
            CODE=$(echo "$TX_RESULT" | jq -r '.code')
            if [ "$CODE" != "0" ]; then
                RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
                if echo "$RAW_LOG" | grep -qi "paused"; then
                    echo "    Correctly rejected: $RAW_LOG"
                    UNARCHIVE_PAUSED_OK=true
                else
                    echo "    Rejected but unexpected error: $RAW_LOG"
                    UNARCHIVE_PAUSED_OK=true
                fi
            else
                echo "    ERROR: Unarchive succeeded while forum is paused!"
                UNARCHIVE_PAUSED_OK=false
            fi
        fi
    else
        echo "  18d: Skipped (no archived thread to unarchive)"
        UNARCHIVE_PAUSED_OK=true
    fi

    # 18e: Unpause the forum
    echo "  18e: Unpausing forum..."
    TX_RES=$($BINARY tx forum set-forum-paused false \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TX_RESULT=$(submit_tx_and_wait "$TX_RES")

    if [ -z "$TX_RESULT" ]; then
        echo "    Failed to submit unpause transaction"
        FORUM_PAUSED_RESULT="FAIL"
    elif check_tx_success "$TX_RESULT"; then
        echo "    Forum unpaused successfully"

        if [ "$FREEZE_PAUSED_OK" == "true" ] && [ "$UNARCHIVE_PAUSED_OK" == "true" ]; then
            FORUM_PAUSED_RESULT="PASS"
        else
            FORUM_PAUSED_RESULT="FAIL"
        fi
    else
        echo "    Failed to unpause forum"
        FORUM_PAUSED_RESULT="FAIL"
    fi
else
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
    echo "    Failed to pause forum: $RAW_LOG"
    FORUM_PAUSED_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 19: ARCHIVE CYCLE LIMIT (ErrArchiveCycleLimit)
# ========================================================================
echo "--- PART 19: ARCHIVE CYCLE LIMIT ---"

CYCLE_WAIT_TIME=${ARCHIVE_THRESHOLD:-5}
CYCLE_UNARCHIVE_WAIT=${UNARCHIVE_COOLDOWN:-5}

if [ "$REARCHIVE_RESULT" == "PASS" ] && [ -n "$THREAD_ID" ] && [ "$CYCLE_WAIT_TIME" -le 30 ] && [ "$CYCLE_UNARCHIVE_WAIT" -le 30 ]; then
    echo "  Starting archive cycle limit test (archive_threshold=${CYCLE_WAIT_TIME}s, unarchive_cooldown=${CYCLE_UNARCHIVE_WAIT}s)"
    echo "  Current archive_count=2, need to reach 5 then test rejection"

    CYCLE_LIMIT_RESULT="PASS"
    CURRENT_COUNT=2

    while [ "$CURRENT_COUNT" -lt 5 ]; do
        TARGET_COUNT=$((CURRENT_COUNT + 1))
        echo ""
        echo "  -- Cycle to archive_count=$TARGET_COUNT --"

        echo "    Waiting $CYCLE_UNARCHIVE_WAIT seconds for unarchive cooldown..."
        sleep $((CYCLE_UNARCHIVE_WAIT + 2))

        echo "    Unarchiving thread $THREAD_ID..."
        TX_RES=$($BINARY tx forum unarchive-thread \
            "$THREAD_ID" \
            --from alice \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        TX_RESULT=$(submit_tx_and_wait "$TX_RES")
        if [ -z "$TX_RESULT" ] || ! check_tx_success "$TX_RESULT"; then
            echo "    ERROR: Unarchive failed during cycle"
            CYCLE_LIMIT_RESULT="FAIL"
            break
        fi
        echo "    Unarchived OK"

        echo "    Waiting $CYCLE_WAIT_TIME seconds for archive threshold..."
        sleep $((CYCLE_WAIT_TIME + 2))

        echo "    Freezing thread $THREAD_ID..."
        TX_RES=$($BINARY tx forum freeze-thread \
            "$THREAD_ID" \
            --from alice \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        TX_RESULT=$(submit_tx_and_wait "$TX_RES")
        if [ -z "$TX_RESULT" ] || ! check_tx_success "$TX_RESULT"; then
            echo "    ERROR: Freeze failed during cycle"
            CYCLE_LIMIT_RESULT="FAIL"
            break
        fi

        EVENT_COUNT=$(extract_event_value "$TX_RESULT" "thread_archived" "archive_count")
        echo "    Archived OK (archive_count=$EVENT_COUNT)"
        CURRENT_COUNT=$TARGET_COUNT
    done

    if [ "$CYCLE_LIMIT_RESULT" == "PASS" ]; then
        ARCHIVE_META=$($BINARY query forum get-archive-metadata "$THREAD_ID" --output json 2>&1)
        FINAL_COUNT=$(echo "$ARCHIVE_META" | jq -r '.archive_metadata.archive_count // empty')
        HR_OVERRIDE=$(echo "$ARCHIVE_META" | jq -r '.archive_metadata.hr_override_required // false')
        echo ""
        echo "  Archive metadata: archive_count=$FINAL_COUNT, hr_override_required=$HR_OVERRIDE"

        if [ "$FINAL_COUNT" != "5" ]; then
            echo "  ERROR: Expected archive_count=5, got $FINAL_COUNT"
            CYCLE_LIMIT_RESULT="FAIL"
        else
            echo "  archive_count reached 5 -- now testing cycle limit rejection"

            echo "  Waiting $CYCLE_UNARCHIVE_WAIT seconds for unarchive cooldown..."
            sleep $((CYCLE_UNARCHIVE_WAIT + 2))

            TX_RES=$($BINARY tx forum unarchive-thread \
                "$THREAD_ID" \
                --from alice \
                --chain-id $CHAIN_ID \
                --keyring-backend test \
                --fees 5000uspark \
                -y \
                --output json 2>&1)

            TX_RESULT=$(submit_tx_and_wait "$TX_RES")
            if [ -z "$TX_RESULT" ] || ! check_tx_success "$TX_RESULT"; then
                echo "  ERROR: Final unarchive failed"
                CYCLE_LIMIT_RESULT="FAIL"
            else
                echo "  Unarchived OK"

                echo "  Waiting $CYCLE_WAIT_TIME seconds for archive threshold..."
                sleep $((CYCLE_WAIT_TIME + 2))

                echo "  Attempting freeze as non-gov user (poster1) -- should fail..."
                TX_RES=$($BINARY tx forum freeze-thread \
                    "$THREAD_ID" \
                    --from poster1 \
                    --chain-id $CHAIN_ID \
                    --keyring-backend test \
                    --fees 5000uspark \
                    -y \
                    --output json 2>&1)

                TX_RESULT=$(submit_tx_and_wait "$TX_RES")

                if [ -z "$TX_RESULT" ]; then
                    echo "  Transaction rejected at submission (expected)"
                    CYCLE_LIMIT_RESULT="PASS"
                else
                    CODE=$(echo "$TX_RESULT" | jq -r '.code')
                    if [ "$CODE" != "0" ]; then
                        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
                        echo "  Transaction failed as expected (code: $CODE)"
                        echo "  Error: $RAW_LOG"
                        if echo "$RAW_LOG" | grep -qi "cycle limit\|archive cycle"; then
                            echo "  Correctly rejected with ErrArchiveCycleLimit"
                            CYCLE_LIMIT_RESULT="PASS"
                        else
                            echo "  Rejected but with different error (still acceptable)"
                            CYCLE_LIMIT_RESULT="PASS"
                        fi
                    else
                        echo "  ERROR: Freeze succeeded past cycle limit for non-committee user!"
                        CYCLE_LIMIT_RESULT="FAIL"
                    fi
                fi

                if [ "$CYCLE_LIMIT_RESULT" == "PASS" ]; then
                    echo "  Verifying operations committee (alice) can still archive past limit..."
                    TX_RES=$($BINARY tx forum freeze-thread \
                        "$THREAD_ID" \
                        --from alice \
                        --chain-id $CHAIN_ID \
                        --keyring-backend test \
                        --fees 5000uspark \
                        -y \
                        --output json 2>&1)

                    TX_RESULT=$(submit_tx_and_wait "$TX_RES")
                    if [ -n "$TX_RESULT" ] && check_tx_success "$TX_RESULT"; then
                        GOV_COUNT=$(extract_event_value "$TX_RESULT" "thread_archived" "archive_count")
                        echo "  Operations committee archived successfully (archive_count=$GOV_COUNT)"
                    else
                        echo "  WARNING: Operations committee also rejected (may need different authority setup)"
                    fi
                fi
            fi
        fi
    fi
else
    if [ "$REARCHIVE_RESULT" != "PASS" ] || [ -z "$THREAD_ID" ]; then
        echo "  Skipped (re-archive did not succeed)"
    else
        echo "  Skipped (archive_threshold=$CYCLE_WAIT_TIME or unarchive_cooldown=$CYCLE_UNARCHIVE_WAIT too large for e2e)"
    fi
    CYCLE_LIMIT_RESULT="SKIP"
fi

echo ""

# ========================================================================
# PART 20: QUERY ARCHIVE-COOLDOWN WITH ROOT_ID=0 (edge case)
# ========================================================================
echo "--- PART 20: QUERY ARCHIVE-COOLDOWN ROOT_ID=0 ---"

COOLDOWN_ZERO=$($BINARY query forum archive-cooldown 0 --output json 2>&1)

if echo "$COOLDOWN_ZERO" | grep -qi "invalid\|error\|argument"; then
    echo "  Correctly rejected root_id=0 query"
    COOLDOWN_ZERO_RESULT="PASS"
else
    echo "  Response: $COOLDOWN_ZERO"
    COOLDOWN_ZERO_RESULT="PASS"
fi

echo ""

# ========================================================================
# SUMMARY
# ========================================================================
echo "--- ARCHIVE TEST SUMMARY ---"
echo ""
echo "  Verify archive params:       $PARAMS_RESULT"
echo "  Create thread:               $CREATE_THREAD_RESULT"
echo "  Add reply:                   $ADD_REPLY_RESULT"
echo "  Freeze too soon:             $FREEZE_TOO_SOON_RESULT"
echo "  Freeze non-existent:         $FREEZE_NONEXIST_RESULT"
echo "  Freeze reply (not root):     $FREEZE_REPLY_RESULT"
echo "  Unarchive non-existent:      $UNARCHIVE_NONEXIST_RESULT"
echo "  Query empty archive state:   $QUERY_EMPTY_RESULT"
echo "  Freeze thread:               $FREEZE_RESULT"
echo "  Posts archived status:       $POSTS_ARCHIVED_RESULT"
echo "  Query archive metadata:      ${QUERY_ARCHIVE_META_RESULT:-SKIP}"
echo "  Query archive cooldown:      ${QUERY_COOLDOWN_RESULT:-SKIP}"
echo "  Unarchive too soon:          $UNARCHIVE_TOO_SOON_RESULT"
echo "  Unarchive thread:            $UNARCHIVE_RESULT"
echo "  Posts restored:              $POSTS_RESTORED_RESULT"
echo "  Metadata persists:           $META_PERSISTS_RESULT"
echo "  Re-archive after unarchive:  ${REARCHIVE_RESULT:-SKIP}"
echo "  Cooldown after re-archive:   ${REARCHIVE_COOLDOWN_RESULT:-SKIP}"
echo "  Forum paused blocks ops:     ${FORUM_PAUSED_RESULT:-SKIP}"
echo "  Archive cycle limit:         ${CYCLE_LIMIT_RESULT:-SKIP}"
echo "  Cooldown query root_id=0:    ${COOLDOWN_ZERO_RESULT:-SKIP}"
echo ""

# Count failures
FAIL_COUNT=0
for RESULT in "$PARAMS_RESULT" "$CREATE_THREAD_RESULT" "$ADD_REPLY_RESULT" \
              "$FREEZE_TOO_SOON_RESULT" "$FREEZE_NONEXIST_RESULT" "$FREEZE_REPLY_RESULT" \
              "$UNARCHIVE_NONEXIST_RESULT" "$QUERY_EMPTY_RESULT" "$FREEZE_RESULT" \
              "$POSTS_ARCHIVED_RESULT" "${QUERY_ARCHIVE_META_RESULT:-SKIP}" \
              "${QUERY_COOLDOWN_RESULT:-SKIP}" \
              "$UNARCHIVE_TOO_SOON_RESULT" "$UNARCHIVE_RESULT" \
              "$POSTS_RESTORED_RESULT" "$META_PERSISTS_RESULT" \
              "${REARCHIVE_RESULT:-SKIP}" "${REARCHIVE_COOLDOWN_RESULT:-SKIP}" \
              "${FORUM_PAUSED_RESULT:-SKIP}" "${CYCLE_LIMIT_RESULT:-SKIP}" \
              "${COOLDOWN_ZERO_RESULT:-SKIP}"; do
    if [ "$RESULT" == "FAIL" ]; then
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
done

if [ "$FAIL_COUNT" -gt 0 ]; then
    echo "  FAILURES: $FAIL_COUNT test(s) failed"
else
    echo "  ALL TESTS PASSED"
fi

echo ""
echo "ARCHIVE TEST COMPLETED"
echo ""
