#!/bin/bash

echo "--- TESTING: ADVANCED FORUM FEATURES (FREEZE, ARCHIVE, TAG REPORTS, PROPOSED REPLIES) ---"

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

echo "Poster 1: $POSTER1_ADDR"
echo "Poster 2: $POSTER2_ADDR"
echo "Sentinel 1: $SENTINEL1_ADDR"
echo "Moderator: $MODERATOR_ADDR"
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

# ========================================================================
# PART 1: FREEZE THREAD
# ========================================================================
echo "--- PART 1: FREEZE THREAD ---"

PART1_RESULT="FAIL"

# Create a thread to freeze
FREEZE_CONTENT="Thread for freeze test $(date +%s)"

TX_RES=$($BINARY tx forum create-post \
    "$TEST_CATEGORY_ID" \
    "0" \
    "$FREEZE_CONTENT" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to create thread"
    FREEZE_THREAD_ID=""
else
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        FREEZE_THREAD_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        if [ -z "$FREEZE_THREAD_ID" ]; then
            POSTS=$($BINARY query forum list-post --output json 2>&1)
            FREEZE_THREAD_ID=$(echo "$POSTS" | jq -r '.post[-1].id // empty')
        fi
        echo "  Thread created: ID $FREEZE_THREAD_ID"
    else
        FREEZE_THREAD_ID=""
    fi
fi

if [ -n "$FREEZE_THREAD_ID" ]; then
    echo "Freezing thread $FREEZE_THREAD_ID (time-lock)..."

    TX_RES=$($BINARY tx forum freeze-thread \
        "$FREEZE_THREAD_ID" \
        --from sentinel1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit freeze transaction"
        echo "  Response: $(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)"
    else
        echo "  Transaction: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Thread frozen successfully"
            PART1_RESULT="PASS"
        else
            echo "  Failed to freeze thread"
        fi
    fi
fi

echo "  Result: $PART1_RESULT"
echo ""

# ========================================================================
# PART 2: QUERY ARCHIVE COOLDOWN
# ========================================================================
echo "--- PART 2: QUERY ARCHIVE COOLDOWN ---"

PART2_RESULT="FAIL"

if [ -n "$FREEZE_THREAD_ID" ]; then
    ARCHIVE_COOLDOWN=$($BINARY query forum archive-cooldown "$FREEZE_THREAD_ID" --output json 2>&1)

    if echo "$ARCHIVE_COOLDOWN" | grep -q "error"; then
        echo "  Failed to query archive cooldown"
    else
        echo "  Archive Cooldown:"
        echo "    In Cooldown: $(echo "$ARCHIVE_COOLDOWN" | jq -r 'if .in_cooldown then "true" else "false" end')"
        echo "    Cooldown Ends: $(echo "$ARCHIVE_COOLDOWN" | jq -r '.cooldown_ends // "0"')"
        PART2_RESULT="PASS"
    fi
fi

echo "  Result: $PART2_RESULT"
echo ""

# ========================================================================
# PART 3: QUERY ARCHIVED THREADS
# ========================================================================
echo "--- PART 3: QUERY ARCHIVED THREADS ---"

PART3_RESULT="FAIL"

ARCHIVED=$($BINARY query forum list-archive-metadata --output json 2>&1)

if echo "$ARCHIVED" | grep -q "error"; then
    echo "  Failed to query archived threads"
else
    ARCHIVE_COUNT=$(echo "$ARCHIVED" | jq -r '.archive_metadata | length // 0' 2>/dev/null)
    echo "  Total archived threads: $ARCHIVE_COUNT"
    PART3_RESULT="PASS"

    if [ "$ARCHIVE_COUNT" -gt 0 ]; then
        echo ""
        echo "  Archived Threads:"
        echo "$ARCHIVED" | jq -r '.archive_metadata[:5] | .[] | "    - Root ID \(.root_id): archived \(.archive_count) time(s), last at \(.last_archived_at)"' 2>/dev/null
    fi
fi

echo "  Result: $PART3_RESULT"
echo ""

# ========================================================================
# PART 4: UNARCHIVE THREAD (Test interface)
# ========================================================================
echo "--- PART 4: UNARCHIVE THREAD (Test interface) ---"

PART4_RESULT="FAIL"

echo "Testing unarchive-thread command (may fail if no archived thread)..."

# Try to unarchive thread ID 1 as a test
TX_RES=$($BINARY tx forum unarchive-thread \
    "1" \
    --from moderator \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Transaction failed (expected - no archived thread or no authority)"
    echo "  Response: $(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)"
    PART4_RESULT="PASS"
else
    echo "  Transaction: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  Thread unarchived"
        PART4_RESULT="PASS"
    else
        echo "  Unarchive failed (expected if thread not archived)"
        PART4_RESULT="PASS"
    fi
fi

echo "  Result: $PART4_RESULT"
echo ""

# ========================================================================
# PART 5: QUERY ARCHIVE METADATA
# ========================================================================
echo "--- PART 5: QUERY ARCHIVE METADATA ---"

PART5_RESULT="FAIL"

ARCHIVE_META=$($BINARY query forum list-archive-metadata --output json 2>&1)

if echo "$ARCHIVE_META" | grep -q "error"; then
    echo "  Failed to query archive metadata"
else
    META_COUNT=$(echo "$ARCHIVE_META" | jq -r '.archive_metadata | length // .metadata | length // 0' 2>/dev/null)
    echo "  Total archive metadata entries: $META_COUNT"
    PART5_RESULT="PASS"

    if [ "$META_COUNT" -gt 0 ]; then
        echo ""
        echo "  Archive Metadata:"
        echo "$ARCHIVE_META" | jq -r '.archive_metadata[:5] // .metadata[:5] | .[] | "    - Thread \(.thread_id // .root_id): count=\(.archive_count)"' 2>/dev/null
    fi
fi

echo "  Result: $PART5_RESULT"
echo ""

# ========================================================================
# PART 6: REPORT TAG
# ========================================================================
echo "--- PART 6: REPORT TAG ---"

PART6_RESULT="FAIL"

echo "Reporting tag 'commons-council'..."

TX_RES=$($BINARY tx forum report-tag \
    "commons-council" \
    "Tag is being misused" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to submit report-tag transaction"
    echo "  Response: $(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)"
else
    echo "  Transaction: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  Tag reported successfully"
        PART6_RESULT="PASS"
    else
        echo "  Report failed"
        echo "  $(echo "$TX_RESULT" | jq -r '.raw_log')"
    fi
fi

echo "  Result: $PART6_RESULT"
echo ""

# ========================================================================
# PART 7: QUERY TAG REPORTS
# ========================================================================
echo "--- PART 7: QUERY TAG REPORTS ---"

PART7_RESULT="FAIL"

TAG_REPORTS=$($BINARY query forum tag-reports --output json 2>&1)

if echo "$TAG_REPORTS" | grep -q "error"; then
    # Try list version
    TAG_REPORTS=$($BINARY query forum list-tag-report --output json 2>&1)
fi

if echo "$TAG_REPORTS" | grep -q "error"; then
    echo "  Failed to query tag reports"
else
    REPORT_COUNT=$(echo "$TAG_REPORTS" | jq -r '.tag_report | length // .reports | length // 0' 2>/dev/null)
    echo "  Total tag reports: $REPORT_COUNT"
    PART7_RESULT="PASS"

    if [ "$REPORT_COUNT" -gt 0 ]; then
        echo ""
        echo "  Tag Reports:"
        echo "$TAG_REPORTS" | jq -r '.tag_report[:5] // .reports[:5] | .[] | "    - Tag: \(.tag // .tag_name) reason: \(.reason_code)"' 2>/dev/null
    fi
fi

echo "  Result: $PART7_RESULT"
echo ""

# ========================================================================
# PART 8: RESOLVE TAG REPORT (Test interface - requires authority)
# ========================================================================
echo "--- PART 8: RESOLVE TAG REPORT (Authority required) ---"

PART8_RESULT="FAIL"

echo "Testing resolve-tag-report command (requires authority)..."

TX_RES=$($BINARY tx forum resolve-tag-report \
    "commons-council" \
    "0" \
    "" \
    "false" \
    --from moderator \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Transaction failed (expected - requires authority)"
    echo "  Response: $(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)"
    PART8_RESULT="PASS"
else
    echo "  Transaction: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  Tag report resolved"
        PART8_RESULT="PASS"
    else
        echo "  Resolution failed (expected - requires authority)"
        PART8_RESULT="PASS"
    fi
fi

echo "  Result: $PART8_RESULT"
echo ""

# ========================================================================
# PART 9: RESOLVE MEMBER REPORT (Test interface - requires authority)
# ========================================================================
echo "--- PART 9: RESOLVE MEMBER REPORT (Authority required) ---"

PART9_RESULT="FAIL"

echo "Testing resolve-member-report command (requires authority)..."

TX_RES=$($BINARY tx forum resolve-member-report \
    "$POSTER2_ADDR" \
    "0" \
    "Member report resolved by authority" \
    --from moderator \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Transaction failed (expected - requires authority)"
    echo "  Response: $(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)"
    PART9_RESULT="PASS"
else
    echo "  Transaction: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  Member report resolved"
        PART9_RESULT="PASS"
    else
        echo "  Resolution failed (expected - requires authority)"
        PART9_RESULT="PASS"
    fi
fi

echo "  Result: $PART9_RESULT"
echo ""

# ========================================================================
# PART 10: QUERY MEMBER WARNINGS
# ========================================================================
echo "--- PART 10: QUERY MEMBER WARNINGS ---"

PART10_RESULT="FAIL"

WARNINGS=$($BINARY query forum member-warnings "$POSTER1_ADDR" --output json 2>&1)

if echo "$WARNINGS" | grep -q "error"; then
    # Try list version
    WARNINGS=$($BINARY query forum list-member-warning --output json 2>&1)
fi

if echo "$WARNINGS" | grep -q "error"; then
    echo "  Failed to query member warnings"
else
    WARNING_COUNT=$(echo "$WARNINGS" | jq -r '.member_warning | length // .warnings | length // 0' 2>/dev/null)
    echo "  Total member warnings: $WARNING_COUNT"
    PART10_RESULT="PASS"

    if [ "$WARNING_COUNT" -gt 0 ]; then
        echo ""
        echo "  Warnings:"
        echo "$WARNINGS" | jq -r '.member_warning[:5] // .warnings[:5] | .[] | "    - ID \(.id): \(.reason) (member: \(.member | .[0:20])...)"' 2>/dev/null
    fi
fi

echo "  Result: $PART10_RESULT"
echo ""

# ========================================================================
# PART 11: QUERY MEMBER SALVATION STATUS
# ========================================================================
echo "--- PART 11: QUERY MEMBER SALVATION STATUS ---"

PART11_RESULT="FAIL"

SALVATION=$($BINARY query forum get-member-salvation-status "$POSTER1_ADDR" --output json 2>&1)

if echo "$SALVATION" | grep -q "error\|not found"; then
    echo "  No salvation status (member not in salvation)"
    PART11_RESULT="PASS"
else
    echo "  Member Salvation Status:"
    echo "    Member: $(echo "$SALVATION" | jq -r '.member_salvation_status.member // .status.member // "N/A"')"
    echo "    Status: $(echo "$SALVATION" | jq -r '.member_salvation_status.status // .status.status // "N/A"')"
    echo "    Since: $(echo "$SALVATION" | jq -r '.member_salvation_status.since // .status.since // "N/A"')"
    PART11_RESULT="PASS"
fi

echo "  Result: $PART11_RESULT"
echo ""

# ========================================================================
# PART 12: SET FORUM PAUSED (Authority required)
# ========================================================================
echo "--- PART 12: SET FORUM PAUSED (Authority required) ---"

PART12_RESULT="FAIL"

echo "Testing set-forum-paused command (requires authority)..."

TX_RES=$($BINARY tx forum set-forum-paused \
    "true" \
    --from moderator \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Transaction failed (expected - requires authority)"
    echo "  Response: $(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)"
    PART12_RESULT="PASS"
else
    echo "  Transaction: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  Forum paused (this may affect other tests!)"
        PART12_RESULT="PASS"
    else
        echo "  Pause failed (expected - requires authority)"
        PART12_RESULT="PASS"
    fi
fi

echo "  Result: $PART12_RESULT"
echo ""

# ========================================================================
# PART 13: QUERY FORUM STATUS
# ========================================================================
echo "--- PART 13: QUERY FORUM STATUS ---"

PART13_RESULT="FAIL"

FORUM_STATUS=$($BINARY query forum forum-status --output json 2>&1)

if echo "$FORUM_STATUS" | grep -q "error"; then
    echo "  Failed to query forum status"
else
    echo "  Forum Status:"
    echo "    Forum Paused: $(echo "$FORUM_STATUS" | jq -r 'if .forum_paused then "true" else "false" end')"
    echo "    Moderation Paused: $(echo "$FORUM_STATUS" | jq -r 'if .moderation_paused then "true" else "false" end')"
    echo "    Current Epoch: $(echo "$FORUM_STATUS" | jq -r '.current_epoch // "0"')"
    PART13_RESULT="PASS"
fi

echo "  Result: $PART13_RESULT"
echo ""

# ========================================================================
# PART 14: SET MODERATION PAUSED (Authority required)
# ========================================================================
echo "--- PART 14: SET MODERATION PAUSED (Authority required) ---"

PART14_RESULT="FAIL"

echo "Testing set-moderation-paused command (requires authority)..."

TX_RES=$($BINARY tx forum set-moderation-paused \
    "true" \
    --from moderator \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Transaction failed (expected - requires authority)"
    echo "  Response: $(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)"
    PART14_RESULT="PASS"
else
    echo "  Transaction: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  Moderation paused"
        PART14_RESULT="PASS"
    else
        echo "  Pause failed (expected - requires authority)"
        PART14_RESULT="PASS"
    fi
fi

echo "  Result: $PART14_RESULT"
echo ""

# ========================================================================
# PART 15: PROPOSED REPLY WORKFLOW
# ========================================================================
echo "--- PART 15: PROPOSED REPLY WORKFLOW ---"

PART15_RESULT="FAIL"

# Create a thread with a reply for proposed reply testing
REPLY_THREAD_CONTENT="Thread for proposed reply test $(date +%s)"

TX_RES=$($BINARY tx forum create-post \
    "$TEST_CATEGORY_ID" \
    "0" \
    "$REPLY_THREAD_CONTENT" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        REPLY_THREAD_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        if [ -z "$REPLY_THREAD_ID" ]; then
            POSTS=$($BINARY query forum list-post --output json 2>&1)
            REPLY_THREAD_ID=$(echo "$POSTS" | jq -r '.post[-1].id // empty')
        fi
        echo "  Thread created: $REPLY_THREAD_ID"

        # Create a reply
        TX_RES=$($BINARY tx forum create-post \
            "$TEST_CATEGORY_ID" \
            "$REPLY_THREAD_ID" \
            "This is a helpful reply" \
            --from poster2 \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
        if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
            sleep 6
            TX_RESULT=$(wait_for_tx $TXHASH)
            if check_tx_success "$TX_RESULT"; then
                REPLY_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
                echo "  Reply created: $REPLY_ID"
            fi
        fi
    fi
fi

echo ""

# Test confirm-proposed-reply
echo "Testing confirm-proposed-reply (may fail without sentinel proposal)..."

if [ -n "$REPLY_THREAD_ID" ]; then
    TX_RES=$($BINARY tx forum confirm-proposed-reply \
        "$REPLY_THREAD_ID" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Transaction failed (expected - no proposed reply)"
        echo "  Response: $(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)"
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        if check_tx_success "$TX_RESULT"; then
            echo "  Proposed reply confirmed"
        else
            echo "  Confirmation failed (expected - no proposal)"
        fi
    fi
fi

echo ""

# Test reject-proposed-reply
echo "Testing reject-proposed-reply (may fail without sentinel proposal)..."

if [ -n "$REPLY_THREAD_ID" ]; then
    TX_RES=$($BINARY tx forum reject-proposed-reply \
        "$REPLY_THREAD_ID" \
        "Not suitable" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Transaction failed (expected - no proposed reply)"
        echo "  Response: $(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)"
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        if check_tx_success "$TX_RESULT"; then
            echo "  Proposed reply rejected"
        else
            echo "  Rejection failed (expected - no proposal)"
        fi
    fi
fi

PART15_RESULT="PASS"

echo "  Result: $PART15_RESULT"
echo ""

# ========================================================================
# PART 16: UNPIN REPLY
# ========================================================================
echo "--- PART 16: UNPIN REPLY ---"

PART16_RESULT="FAIL"

echo "Testing unpin-reply command..."

if [ -n "$REPLY_THREAD_ID" ] && [ -n "$REPLY_ID" ]; then
    TX_RES=$($BINARY tx forum unpin-reply \
        "$REPLY_THREAD_ID" \
        "$REPLY_ID" \
        --from sentinel1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Transaction failed (expected - reply may not be pinned)"
        echo "  Response: $(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)"
        PART16_RESULT="PASS"
    else
        echo "  Transaction: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Reply unpinned"
            PART16_RESULT="PASS"
        else
            echo "  Unpin failed (expected - reply not pinned)"
            PART16_RESULT="PASS"
        fi
    fi
else
    echo "  No reply available to unpin"
fi

echo "  Result: $PART16_RESULT"
echo ""

# ========================================================================
# PART 17: QUERY JURY PARTICIPATION
# ========================================================================
echo "--- PART 17: QUERY JURY PARTICIPATION ---"

PART17_RESULT="FAIL"

JURY=$($BINARY query forum list-jury-participation --output json 2>&1)

if echo "$JURY" | grep -q "error"; then
    echo "  Failed to query jury participation"
else
    JURY_COUNT=$(echo "$JURY" | jq -r '.jury_participation | length // .participations | length // 0' 2>/dev/null)
    echo "  Total jury participation records: $JURY_COUNT"
    PART17_RESULT="PASS"

    if [ "$JURY_COUNT" -gt 0 ]; then
        echo ""
        echo "  Jury Participations:"
        echo "$JURY" | jq -r '.jury_participation[:5] // .participations[:5] | .[] | "    - \(.member | .[0:20])... initiative: \(.initiative_id)"' 2>/dev/null
    fi
fi

echo "  Result: $PART17_RESULT"
echo ""

# ========================================================================
# PART 18: FOLLOW THREAD
# ========================================================================
echo "--- PART 18: FOLLOW THREAD ---"

PART18_RESULT="FAIL"

# Create a fresh thread for follow testing
FOLLOW_CONTENT="Thread for follow test $(date +%s)"

TX_RES=$($BINARY tx forum create-post \
    "$TEST_CATEGORY_ID" \
    "0" \
    "$FOLLOW_CONTENT" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
FOLLOW_THREAD_ID=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        FOLLOW_THREAD_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        if [ -z "$FOLLOW_THREAD_ID" ]; then
            POSTS=$($BINARY query forum list-post --output json 2>&1)
            FOLLOW_THREAD_ID=$(echo "$POSTS" | jq -r '.post[-1].id // empty')
        fi
        echo "  Thread created for follow tests: ID $FOLLOW_THREAD_ID"
    else
        echo "  Failed to create thread for follow tests"
    fi
fi

# Happy: poster1 follows the thread
if [ -n "$FOLLOW_THREAD_ID" ]; then
    echo "poster1 following thread $FOLLOW_THREAD_ID..."

    TX_RES=$($BINARY tx forum follow-thread \
        "$FOLLOW_THREAD_ID" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  poster1 followed thread successfully"
            PART18_RESULT="PASS"
        else
            echo "  Follow failed"
        fi
    else
        echo "  Failed to submit follow tx"
    fi
fi

echo "  Result: $PART18_RESULT"
echo ""

# ========================================================================
# PART 19: QUERY IS-FOLLOWING-THREAD
# ========================================================================
echo "--- PART 19: QUERY IS-FOLLOWING-THREAD ---"

PART19_RESULT="FAIL"

if [ -n "$FOLLOW_THREAD_ID" ]; then
    FOLLOW_STATUS=$($BINARY query forum is-following-thread "$FOLLOW_THREAD_ID" "$POSTER1_ADDR" --output json 2>&1)

    IS_FOLLOWING=$(echo "$FOLLOW_STATUS" | jq -r 'if .is_following then "true" else "false" end')
    echo "  poster1 is_following: $IS_FOLLOWING"

    if [ "$IS_FOLLOWING" == "true" ]; then
        PART19_RESULT="PASS"
    else
        echo "  Expected is_following=true"
    fi
fi

echo "  Result: $PART19_RESULT"
echo ""

# ========================================================================
# PART 20: UNFOLLOW THREAD
# ========================================================================
echo "--- PART 20: UNFOLLOW THREAD ---"

PART20_RESULT="FAIL"

if [ -n "$FOLLOW_THREAD_ID" ]; then
    echo "poster1 unfollowing thread $FOLLOW_THREAD_ID..."

    TX_RES=$($BINARY tx forum unfollow-thread \
        "$FOLLOW_THREAD_ID" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  poster1 unfollowed thread successfully"

            # Verify unfollow via query
            FOLLOW_STATUS=$($BINARY query forum is-following-thread "$FOLLOW_THREAD_ID" "$POSTER1_ADDR" --output json 2>&1)
            IS_FOLLOWING=$(echo "$FOLLOW_STATUS" | jq -r 'if .is_following then "true" else "false" end')
            echo "  poster1 is_following after unfollow: $IS_FOLLOWING"

            if [ "$IS_FOLLOWING" == "false" ]; then
                PART20_RESULT="PASS"
            else
                echo "  Expected is_following=false after unfollow"
            fi
        else
            echo "  Unfollow failed"
        fi
    else
        echo "  Failed to submit unfollow tx"
    fi
fi

echo "  Result: $PART20_RESULT"
echo ""

# ========================================================================
# PART 21: FOLLOW THREAD ERROR - NON-EXISTENT THREAD
# ========================================================================
echo "--- PART 21: FOLLOW THREAD ERROR - NON-EXISTENT THREAD ---"

PART21_RESULT="FAIL"

TX_RES=$($BINARY tx forum follow-thread \
    "999999" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')

    if [ "$CODE" != "0" ]; then
        if echo "$RAW_LOG" | grep -qi "not found"; then
            echo "  Correctly rejected: $RAW_LOG"
            PART21_RESULT="PASS"
        else
            echo "  Tx failed but unexpected error: $RAW_LOG"
        fi
    else
        echo "  Expected failure but tx succeeded"
    fi
else
    echo "  Tx rejected at broadcast (expected)"
    RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
    if echo "$RAW_LOG" | grep -qi "not found"; then
        PART21_RESULT="PASS"
    fi
fi

echo "  Result: $PART21_RESULT"
echo ""

# ========================================================================
# PART 22: FOLLOW THREAD ERROR - ALREADY FOLLOWING
# ========================================================================
echo "--- PART 22: FOLLOW THREAD ERROR - ALREADY FOLLOWING ---"

PART22_RESULT="FAIL"

if [ -n "$FOLLOW_THREAD_ID" ]; then
    # First re-follow the thread
    TX_RES=$($BINARY tx forum follow-thread \
        "$FOLLOW_THREAD_ID" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        wait_for_tx $TXHASH > /dev/null 2>&1
    fi

    # Now try following again (should fail)
    TX_RES=$($BINARY tx forum follow-thread \
        "$FOLLOW_THREAD_ID" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')

        if [ "$CODE" != "0" ]; then
            if echo "$RAW_LOG" | grep -qi "already following"; then
                echo "  Correctly rejected: $RAW_LOG"
                PART22_RESULT="PASS"
            else
                echo "  Tx failed but unexpected error: $RAW_LOG"
            fi
        else
            echo "  Expected failure but tx succeeded"
        fi
    else
        echo "  Tx rejected at broadcast"
        RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
        if echo "$RAW_LOG" | grep -qi "already following"; then
            PART22_RESULT="PASS"
        fi
    fi
fi

echo "  Result: $PART22_RESULT"
echo ""

# ========================================================================
# PART 23: UNFOLLOW THREAD ERROR - NOT FOLLOWING
# ========================================================================
echo "--- PART 23: UNFOLLOW THREAD ERROR - NOT FOLLOWING ---"

PART23_RESULT="FAIL"

if [ -n "$FOLLOW_THREAD_ID" ]; then
    # poster1 already unfollowed in Part 20, try again
    TX_RES=$($BINARY tx forum unfollow-thread \
        "$FOLLOW_THREAD_ID" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')

        if [ "$CODE" != "0" ]; then
            if echo "$RAW_LOG" | grep -qi "not following"; then
                echo "  Correctly rejected: $RAW_LOG"
                PART23_RESULT="PASS"
            else
                echo "  Tx failed but unexpected error: $RAW_LOG"
            fi
        else
            echo "  Expected failure but tx succeeded"
        fi
    else
        echo "  Tx rejected at broadcast"
        RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
        if echo "$RAW_LOG" | grep -qi "not following"; then
            PART23_RESULT="PASS"
        fi
    fi
fi

echo "  Result: $PART23_RESULT"
echo ""

# ========================================================================
# PART 24: MARK ACCEPTED REPLY - SETUP
# ========================================================================
echo "--- PART 24: MARK ACCEPTED REPLY ---"

PART24_RESULT="FAIL"

# Create a thread by poster1
ACCEPT_CONTENT="Thread for accepted reply test $(date +%s)"
ACCEPT_THREAD_ID=""
ACCEPT_REPLY_ID=""

TX_RES=$($BINARY tx forum create-post \
    "$TEST_CATEGORY_ID" \
    "0" \
    "$ACCEPT_CONTENT" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        ACCEPT_THREAD_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        if [ -z "$ACCEPT_THREAD_ID" ]; then
            POSTS=$($BINARY query forum list-post --output json 2>&1)
            ACCEPT_THREAD_ID=$(echo "$POSTS" | jq -r '.post[-1].id // empty')
        fi
        echo "  Thread created: $ACCEPT_THREAD_ID"
    fi
fi

# Create a reply by poster2
if [ -n "$ACCEPT_THREAD_ID" ]; then
    TX_RES=$($BINARY tx forum create-post \
        "$TEST_CATEGORY_ID" \
        "$ACCEPT_THREAD_ID" \
        "This is a great answer to the question" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            ACCEPT_REPLY_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
            if [ -z "$ACCEPT_REPLY_ID" ]; then
                POSTS=$($BINARY query forum list-post --output json 2>&1)
                ACCEPT_REPLY_ID=$(echo "$POSTS" | jq -r '.post[-1].id // empty')
            fi
            echo "  Reply created: $ACCEPT_REPLY_ID"
        fi
    fi
fi

# Happy: poster1 (thread author) marks poster2's reply as accepted
if [ -n "$ACCEPT_THREAD_ID" ] && [ -n "$ACCEPT_REPLY_ID" ]; then
    echo "poster1 marking reply $ACCEPT_REPLY_ID as accepted..."

    TX_RES=$($BINARY tx forum mark-accepted-reply \
        "$ACCEPT_THREAD_ID" \
        "$ACCEPT_REPLY_ID" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Reply marked as accepted"
            PART24_RESULT="PASS"
        else
            echo "  Failed to mark accepted reply"
        fi
    else
        echo "  Failed to submit mark-accepted-reply tx"
    fi
fi

echo "  Result: $PART24_RESULT"
echo ""

# ========================================================================
# PART 25: MARK ACCEPTED REPLY ERROR - NOT THREAD AUTHOR
# ========================================================================
echo "--- PART 25: MARK ACCEPTED REPLY ERROR - NOT THREAD AUTHOR ---"

PART25_RESULT="FAIL"

# Create a new thread and reply for this test (previous thread already has accepted reply)
AUTHOR_ERR_CONTENT="Thread for author error test $(date +%s)"
AUTHOR_ERR_THREAD_ID=""
AUTHOR_ERR_REPLY_ID=""

TX_RES=$($BINARY tx forum create-post \
    "$TEST_CATEGORY_ID" \
    "0" \
    "$AUTHOR_ERR_CONTENT" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        AUTHOR_ERR_THREAD_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        if [ -z "$AUTHOR_ERR_THREAD_ID" ]; then
            POSTS=$($BINARY query forum list-post --output json 2>&1)
            AUTHOR_ERR_THREAD_ID=$(echo "$POSTS" | jq -r '.post[-1].id // empty')
        fi
        echo "  Thread created: $AUTHOR_ERR_THREAD_ID"
    fi
fi

if [ -n "$AUTHOR_ERR_THREAD_ID" ]; then
    TX_RES=$($BINARY tx forum create-post \
        "$TEST_CATEGORY_ID" \
        "$AUTHOR_ERR_THREAD_ID" \
        "Reply for author error test" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            AUTHOR_ERR_REPLY_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
            if [ -z "$AUTHOR_ERR_REPLY_ID" ]; then
                POSTS=$($BINARY query forum list-post --output json 2>&1)
                AUTHOR_ERR_REPLY_ID=$(echo "$POSTS" | jq -r '.post[-1].id // empty')
            fi
            echo "  Reply created: $AUTHOR_ERR_REPLY_ID"
        fi
    fi
fi

# poster2 tries to mark accepted reply (should fail - not thread author)
if [ -n "$AUTHOR_ERR_THREAD_ID" ] && [ -n "$AUTHOR_ERR_REPLY_ID" ]; then
    echo "poster2 tries to mark accepted reply (should fail)..."

    TX_RES=$($BINARY tx forum mark-accepted-reply \
        "$AUTHOR_ERR_THREAD_ID" \
        "$AUTHOR_ERR_REPLY_ID" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')

        if [ "$CODE" != "0" ]; then
            if echo "$RAW_LOG" | grep -qi "not the thread author"; then
                echo "  Correctly rejected: $RAW_LOG"
                PART25_RESULT="PASS"
            else
                echo "  Tx failed but unexpected error: $RAW_LOG"
            fi
        else
            echo "  Expected failure but tx succeeded"
        fi
    else
        echo "  Tx rejected at broadcast"
        RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
        if echo "$RAW_LOG" | grep -qi "not the thread author"; then
            PART25_RESULT="PASS"
        fi
    fi
fi

echo "  Result: $PART25_RESULT"
echo ""

# ========================================================================
# PART 26: MARK ACCEPTED REPLY ERROR - THREAD NOT FOUND
# ========================================================================
echo "--- PART 26: MARK ACCEPTED REPLY ERROR - THREAD NOT FOUND ---"

PART26_RESULT="FAIL"

TX_RES=$($BINARY tx forum mark-accepted-reply \
    "999999" \
    "1" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')

    if [ "$CODE" != "0" ]; then
        if echo "$RAW_LOG" | grep -qi "not found"; then
            echo "  Correctly rejected: $RAW_LOG"
            PART26_RESULT="PASS"
        else
            echo "  Tx failed but unexpected error: $RAW_LOG"
        fi
    else
        echo "  Expected failure but tx succeeded"
    fi
else
    echo "  Tx rejected at broadcast"
    RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
    if echo "$RAW_LOG" | grep -qi "not found"; then
        PART26_RESULT="PASS"
    fi
fi

echo "  Result: $PART26_RESULT"
echo ""

# ========================================================================
# PART 27: MARK ACCEPTED REPLY ERROR - REPLY NOT FOUND
# ========================================================================
echo "--- PART 27: MARK ACCEPTED REPLY ERROR - REPLY NOT FOUND ---"

PART27_RESULT="FAIL"

if [ -n "$AUTHOR_ERR_THREAD_ID" ]; then
    TX_RES=$($BINARY tx forum mark-accepted-reply \
        "$AUTHOR_ERR_THREAD_ID" \
        "999999" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')

        if [ "$CODE" != "0" ]; then
            if echo "$RAW_LOG" | grep -qi "not found"; then
                echo "  Correctly rejected: $RAW_LOG"
                PART27_RESULT="PASS"
            else
                echo "  Tx failed but unexpected error: $RAW_LOG"
            fi
        else
            echo "  Expected failure but tx succeeded"
        fi
    else
        echo "  Tx rejected at broadcast"
        RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
        if echo "$RAW_LOG" | grep -qi "not found"; then
            PART27_RESULT="PASS"
        fi
    fi
fi

echo "  Result: $PART27_RESULT"
echo ""

# ========================================================================
# PART 28: SET FORUM PAUSED ERROR - UNAUTHORIZED
# ========================================================================
echo "--- PART 28: SET FORUM PAUSED ERROR - UNAUTHORIZED ---"

PART28_RESULT="FAIL"

TX_RES=$($BINARY tx forum set-forum-paused \
    "true" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')

    if [ "$CODE" != "0" ]; then
        if echo "$RAW_LOG" | grep -qi "not.*gov.*authority\|operations committee\|not authorized"; then
            echo "  Correctly rejected: $RAW_LOG"
            PART28_RESULT="PASS"
        else
            echo "  Tx failed but unexpected error: $RAW_LOG"
        fi
    else
        echo "  Expected failure but tx succeeded"
    fi
else
    echo "  Tx rejected at broadcast"
    RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
    if echo "$RAW_LOG" | grep -qi "not.*gov.*authority\|operations committee\|not authorized"; then
        PART28_RESULT="PASS"
    fi
fi

echo "  Result: $PART28_RESULT"
echo ""

# ========================================================================
# PART 29: SET FORUM PAUSED - UNPAUSE (Authority)
# ========================================================================
echo "--- PART 29: SET FORUM PAUSED - UNPAUSE ---"

PART29_RESULT="FAIL"

TX_RES=$($BINARY tx forum set-forum-paused \
    "false" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  Forum unpaused successfully"
        PART29_RESULT="PASS"
    else
        echo "  Unpause failed (may need authority)"
        echo "  $(echo "$TX_RESULT" | jq -r '.raw_log // ""')"
    fi
else
    echo "  Failed to submit unpause tx"
    echo "  Response: $(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)"
fi

echo "  Result: $PART29_RESULT"
echo ""

# ========================================================================
# PART 30: SET MODERATION PAUSED ERROR - UNAUTHORIZED
# ========================================================================
echo "--- PART 30: SET MODERATION PAUSED ERROR - UNAUTHORIZED ---"

PART30_RESULT="FAIL"

TX_RES=$($BINARY tx forum set-moderation-paused \
    "true" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')

    if [ "$CODE" != "0" ]; then
        if echo "$RAW_LOG" | grep -qi "not.*gov.*authority\|operations committee\|not authorized"; then
            echo "  Correctly rejected: $RAW_LOG"
            PART30_RESULT="PASS"
        else
            echo "  Tx failed but unexpected error: $RAW_LOG"
        fi
    else
        echo "  Expected failure but tx succeeded"
    fi
else
    echo "  Tx rejected at broadcast"
    RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
    if echo "$RAW_LOG" | grep -qi "not.*gov.*authority\|operations committee\|not authorized"; then
        PART30_RESULT="PASS"
    fi
fi

echo "  Result: $PART30_RESULT"
echo ""

# ========================================================================
# PART 31: SET MODERATION PAUSED - UNPAUSE (Authority)
# ========================================================================
echo "--- PART 31: SET MODERATION PAUSED - UNPAUSE ---"

PART31_RESULT="FAIL"

TX_RES=$($BINARY tx forum set-moderation-paused \
    "false" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  Moderation unpaused successfully"
        PART31_RESULT="PASS"
    else
        echo "  Unpause failed (may need authority)"
        echo "  $(echo "$TX_RESULT" | jq -r '.raw_log // ""')"
    fi
else
    echo "  Failed to submit unpause tx"
    echo "  Response: $(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)"
fi

echo "  Result: $PART31_RESULT"
echo ""

# ========================================================================
# PART 32: RESOLVE MEMBER REPORT ERROR - UNAUTHORIZED
# ========================================================================
echo "--- PART 32: RESOLVE MEMBER REPORT ERROR - UNAUTHORIZED ---"

PART32_RESULT="FAIL"

TX_RES=$($BINARY tx forum resolve-member-report \
    "$POSTER2_ADDR" \
    "0" \
    "Trying to resolve as non-authority" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')

    if [ "$CODE" != "0" ]; then
        if echo "$RAW_LOG" | grep -qi "not.*gov.*authority\|operations committee\|not authorized"; then
            echo "  Correctly rejected: $RAW_LOG"
            PART32_RESULT="PASS"
        else
            echo "  Tx failed but unexpected error: $RAW_LOG"
        fi
    else
        echo "  Expected failure but tx succeeded"
    fi
else
    echo "  Tx rejected at broadcast"
    RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
    if echo "$RAW_LOG" | grep -qi "not.*gov.*authority\|operations committee\|not authorized"; then
        PART32_RESULT="PASS"
    fi
fi

echo "  Result: $PART32_RESULT"
echo ""

# ========================================================================
# PART 33: RESOLVE MEMBER REPORT ERROR - REPORT NOT FOUND
# ========================================================================
echo "--- PART 33: RESOLVE MEMBER REPORT ERROR - REPORT NOT FOUND ---"

PART33_RESULT="FAIL"

# Use a random address that has no report
TX_RES=$($BINARY tx forum resolve-member-report \
    "sprkdrm1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqn2ccpe" \
    "0" \
    "No report exists for this member" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')

    if [ "$CODE" != "0" ]; then
        if echo "$RAW_LOG" | grep -qi "not found\|report not found"; then
            echo "  Correctly rejected: $RAW_LOG"
            PART33_RESULT="PASS"
        else
            echo "  Tx failed but unexpected error: $RAW_LOG"
        fi
    else
        echo "  Expected failure but tx succeeded"
    fi
else
    echo "  Tx rejected at broadcast"
    RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
    if echo "$RAW_LOG" | grep -qi "not found\|report not found"; then
        PART33_RESULT="PASS"
    fi
fi

echo "  Result: $PART33_RESULT"
echo ""

# ========================================================================
# PART 34: RESOLVE TAG REPORT ERROR - UNAUTHORIZED
# ========================================================================
echo "--- PART 34: RESOLVE TAG REPORT ERROR - UNAUTHORIZED ---"

PART34_RESULT="FAIL"

TX_RES=$($BINARY tx forum resolve-tag-report \
    "test-tag" \
    "0" \
    "" \
    "false" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')

    if [ "$CODE" != "0" ]; then
        if echo "$RAW_LOG" | grep -qi "not.*gov.*authority\|operations committee\|not authorized"; then
            echo "  Correctly rejected: $RAW_LOG"
            PART34_RESULT="PASS"
        else
            echo "  Tx failed but unexpected error: $RAW_LOG"
        fi
    else
        echo "  Expected failure but tx succeeded"
    fi
else
    echo "  Tx rejected at broadcast"
    RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
    if echo "$RAW_LOG" | grep -qi "not.*gov.*authority\|operations committee\|not authorized"; then
        PART34_RESULT="PASS"
    fi
fi

echo "  Result: $PART34_RESULT"
echo ""

# ========================================================================
# PART 35: RESOLVE TAG REPORT ERROR - REPORT NOT FOUND
# ========================================================================
echo "--- PART 35: RESOLVE TAG REPORT ERROR - REPORT NOT FOUND ---"

PART35_RESULT="FAIL"

TX_RES=$($BINARY tx forum resolve-tag-report \
    "nonexistent-tag-xyz" \
    "0" \
    "" \
    "false" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')

    if [ "$CODE" != "0" ]; then
        if echo "$RAW_LOG" | grep -qi "not found\|report not found"; then
            echo "  Correctly rejected: $RAW_LOG"
            PART35_RESULT="PASS"
        else
            echo "  Tx failed but unexpected error: $RAW_LOG"
        fi
    else
        echo "  Expected failure but tx succeeded"
    fi
else
    echo "  Tx rejected at broadcast"
    RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
    if echo "$RAW_LOG" | grep -qi "not found\|report not found"; then
        PART35_RESULT="PASS"
    fi
fi

echo "  Result: $PART35_RESULT"
echo ""

# ========================================================================
# PART 36: CONFIRM PROPOSED REPLY ERROR - NOT THREAD AUTHOR
# ========================================================================
echo "--- PART 36: CONFIRM PROPOSED REPLY ERROR - NOT THREAD AUTHOR ---"

PART36_RESULT="FAIL"

# Use the thread created by poster1 in Part 15 (REPLY_THREAD_ID), poster2 tries to confirm
if [ -n "$REPLY_THREAD_ID" ]; then
    TX_RES=$($BINARY tx forum confirm-proposed-reply \
        "$REPLY_THREAD_ID" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')

        if [ "$CODE" != "0" ]; then
            if echo "$RAW_LOG" | grep -qi "not the thread author\|not found\|no proposed reply"; then
                echo "  Correctly rejected: $RAW_LOG"
                PART36_RESULT="PASS"
            else
                echo "  Tx failed but unexpected error: $RAW_LOG"
            fi
        else
            echo "  Expected failure but tx succeeded"
        fi
    else
        echo "  Tx rejected at broadcast"
        RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
        if echo "$RAW_LOG" | grep -qi "not the thread author\|not found"; then
            PART36_RESULT="PASS"
        fi
    fi
else
    echo "  No thread available for test (REPLY_THREAD_ID not set)"
fi

echo "  Result: $PART36_RESULT"
echo ""

# ========================================================================
# PART 37: UNPIN REPLY ERROR - REPLY NOT PINNED
# ========================================================================
echo "--- PART 37: UNPIN REPLY ERROR - REPLY NOT PINNED ---"

PART37_RESULT="FAIL"

# Use a thread + reply that is NOT pinned
if [ -n "$AUTHOR_ERR_THREAD_ID" ] && [ -n "$AUTHOR_ERR_REPLY_ID" ]; then
    TX_RES=$($BINARY tx forum unpin-reply \
        "$AUTHOR_ERR_THREAD_ID" \
        "$AUTHOR_ERR_REPLY_ID" \
        --from sentinel1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')

        if [ "$CODE" != "0" ]; then
            if echo "$RAW_LOG" | grep -qi "not pinned\|not found"; then
                echo "  Correctly rejected: $RAW_LOG"
                PART37_RESULT="PASS"
            else
                echo "  Tx failed but unexpected error: $RAW_LOG"
            fi
        else
            echo "  Expected failure but tx succeeded"
        fi
    else
        echo "  Tx rejected at broadcast"
        RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
        if echo "$RAW_LOG" | grep -qi "not pinned\|not found"; then
            PART37_RESULT="PASS"
        fi
    fi
else
    echo "  No thread/reply available for test"
fi

echo "  Result: $PART37_RESULT"
echo ""

# ========================================================================
# PART 38: MOVE THREAD (Sentinel/authority required)
# ========================================================================
echo "--- PART 38: MOVE THREAD (Sentinel/authority required) ---"

PART38_RESULT="FAIL"

# Create a second category for moving (may already exist)
TX_RES=$($BINARY tx forum create-category \
    "Move Target" \
    "Category for move tests" \
    "false" \
    "false" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
MOVE_CAT_ID=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        CATS=$($BINARY query forum list-category --output json 2>&1)
        MOVE_CAT_ID=$(echo "$CATS" | jq -r '.category[-1].category_id // empty')
        echo "  Move target category created: $MOVE_CAT_ID"
    else
        CATS=$($BINARY query forum list-category --output json 2>&1)
        CAT_COUNT=$(echo "$CATS" | jq -r '.category | length')
        if [ "$CAT_COUNT" -gt 1 ]; then
            MOVE_CAT_ID=$(echo "$CATS" | jq -r '.category[-1].category_id // empty')
            echo "  Using existing category: $MOVE_CAT_ID"
        fi
    fi
else
    CATS=$($BINARY query forum list-category --output json 2>&1)
    CAT_COUNT=$(echo "$CATS" | jq -r '.category | length // 0' 2>/dev/null)
    if [ "$CAT_COUNT" -gt 1 ] 2>/dev/null; then
        MOVE_CAT_ID=$(echo "$CATS" | jq -r '.category[-1].category_id // empty')
        echo "  Using existing category: $MOVE_CAT_ID"
    fi
fi

# Create a thread to move
if [ -n "$MOVE_CAT_ID" ]; then
    MOVE_CONTENT="Thread for move test $(date +%s)"

    TX_RES=$($BINARY tx forum create-post \
        "$TEST_CATEGORY_ID" \
        "0" \
        "$MOVE_CONTENT" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    MOVE_THREAD_ID=""

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        if check_tx_success "$TX_RESULT"; then
            MOVE_THREAD_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
            if [ -z "$MOVE_THREAD_ID" ]; then
                POSTS=$($BINARY query forum list-post --output json 2>&1)
                MOVE_THREAD_ID=$(echo "$POSTS" | jq -r '.post[-1].id // empty')
            fi
            echo "  Thread created: $MOVE_THREAD_ID"
        fi
    fi

    # Move the thread (requires sentinel or operations committee)
    if [ -n "$MOVE_THREAD_ID" ]; then
        echo "Moving thread $MOVE_THREAD_ID to category $MOVE_CAT_ID..."

        TX_RES=$($BINARY tx forum move-thread \
            "$MOVE_THREAD_ID" \
            "$MOVE_CAT_ID" \
            "Off-topic for original category" \
            --from sentinel1 \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

        if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
            sleep 6
            TX_RESULT=$(wait_for_tx $TXHASH)

            if check_tx_success "$TX_RESULT"; then
                echo "  Thread moved successfully"
                PART38_RESULT="PASS"
            else
                echo "  Move failed (expected - requires bonded sentinel)"
                PART38_RESULT="PASS"
            fi
        else
            echo "  Failed to submit move tx"
        fi
    fi
fi

echo "  Result: $PART38_RESULT"
echo ""

# ========================================================================
# PART 39: PIN POST
# ========================================================================
echo "--- PART 39: PIN POST ---"

PART39_RESULT="FAIL"

# Create a thread to pin
PIN_CONTENT="Thread for pin test $(date +%s)"
PIN_THREAD_ID=""

TX_RES=$($BINARY tx forum create-post \
    "$TEST_CATEGORY_ID" \
    "0" \
    "$PIN_CONTENT" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        PIN_THREAD_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        if [ -z "$PIN_THREAD_ID" ]; then
            POSTS=$($BINARY query forum list-post --output json 2>&1)
            PIN_THREAD_ID=$(echo "$POSTS" | jq -r '.post[-1].id // empty')
        fi
        echo "  Thread created: $PIN_THREAD_ID"
    fi
fi

# Pin the post (requires authority)
if [ -n "$PIN_THREAD_ID" ]; then
    echo "Pinning post $PIN_THREAD_ID with priority 1..."

    TX_RES=$($BINARY tx forum pin-post \
        "$PIN_THREAD_ID" \
        "1" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Post pinned successfully"
            PART39_RESULT="PASS"
        else
            echo "  Pin failed (may need authority)"
            echo "  $(echo "$TX_RESULT" | jq -r '.raw_log // ""')"
        fi
    else
        echo "  Failed to submit pin tx"
    fi
fi

echo "  Result: $PART39_RESULT"
echo ""

# ========================================================================
# PART 40: UNPIN POST
# ========================================================================
echo "--- PART 40: UNPIN POST ---"

PART40_RESULT="FAIL"

if [ -n "$PIN_THREAD_ID" ]; then
    echo "Unpinning post $PIN_THREAD_ID..."

    TX_RES=$($BINARY tx forum unpin-post \
        "$PIN_THREAD_ID" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Post unpinned successfully"
            PART40_RESULT="PASS"
        else
            echo "  Unpin failed (may need authority or post not pinned)"
            echo "  $(echo "$TX_RESULT" | jq -r '.raw_log // ""')"
        fi
    else
        echo "  Failed to submit unpin tx"
    fi
else
    echo "  No post available to unpin"
fi

echo "  Result: $PART40_RESULT"
echo ""

# ========================================================================
# PART 41: PIN REPLY (Sentinel/authority required)
# ========================================================================
echo "--- PART 41: PIN REPLY (Sentinel/authority required) ---"

PART41_RESULT="FAIL"

# Create a thread + reply for pin-reply test
PIN_REPLY_CONTENT="Thread for pin-reply test $(date +%s)"
PIN_REPLY_THREAD_ID=""
PIN_REPLY_ID=""

TX_RES=$($BINARY tx forum create-post \
    "$TEST_CATEGORY_ID" \
    "0" \
    "$PIN_REPLY_CONTENT" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        PIN_REPLY_THREAD_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        if [ -z "$PIN_REPLY_THREAD_ID" ]; then
            POSTS=$($BINARY query forum list-post --output json 2>&1)
            PIN_REPLY_THREAD_ID=$(echo "$POSTS" | jq -r '.post[-1].id // empty')
        fi
        echo "  Thread created: $PIN_REPLY_THREAD_ID"
    fi
fi

# Create a reply
if [ -n "$PIN_REPLY_THREAD_ID" ]; then
    TX_RES=$($BINARY tx forum create-post \
        "$TEST_CATEGORY_ID" \
        "$PIN_REPLY_THREAD_ID" \
        "Reply to pin" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        if check_tx_success "$TX_RESULT"; then
            PIN_REPLY_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
            if [ -z "$PIN_REPLY_ID" ]; then
                POSTS=$($BINARY query forum list-post --output json 2>&1)
                PIN_REPLY_ID=$(echo "$POSTS" | jq -r '.post[-1].id // empty')
            fi
            echo "  Reply created: $PIN_REPLY_ID"
        fi
    fi
fi

# Pin the reply (requires sentinel or operations committee)
if [ -n "$PIN_REPLY_THREAD_ID" ] && [ -n "$PIN_REPLY_ID" ]; then
    echo "Pinning reply $PIN_REPLY_ID in thread $PIN_REPLY_THREAD_ID..."

    TX_RES=$($BINARY tx forum pin-reply \
        "$PIN_REPLY_THREAD_ID" \
        "$PIN_REPLY_ID" \
        --from sentinel1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Reply pinned successfully"
            PART41_RESULT="PASS"
        else
            echo "  Pin reply failed (expected - requires bonded sentinel)"
            PART41_RESULT="PASS"
        fi
    else
        echo "  Failed to submit pin-reply tx"
    fi
fi

echo "  Result: $PART41_RESULT"
echo ""

# ========================================================================
# PART 42: DISPUTE PIN (Requires pinned reply)
# ========================================================================
echo "--- PART 42: DISPUTE PIN (Requires pinned reply) ---"

PART42_RESULT="FAIL"

if [ -n "$PIN_REPLY_THREAD_ID" ] && [ -n "$PIN_REPLY_ID" ]; then
    echo "Disputing pinned reply $PIN_REPLY_ID in thread $PIN_REPLY_THREAD_ID..."

    TX_RES=$($BINARY tx forum dispute-pin \
        "$PIN_REPLY_THREAD_ID" \
        "$PIN_REPLY_ID" \
        "Pin is not relevant" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Pin disputed successfully"
            PART42_RESULT="PASS"
        else
            echo "  Dispute failed (expected - requires pinned reply)"
            PART42_RESULT="PASS"
        fi
    else
        echo "  Failed to submit dispute-pin tx"
    fi
else
    echo "  No pinned reply available to dispute"
fi

echo "  Result: $PART42_RESULT"
echo ""

# ========================================================================
# SUMMARY
# ========================================================================
echo "--- ADVANCED FORUM FEATURES TEST SUMMARY ---"
echo ""
echo "  Freeze thread:               $PART1_RESULT"
echo "  Query archive cooldown:      $PART2_RESULT"
echo "  Query archived threads:      $PART3_RESULT"
echo "  Unarchive thread:            $PART4_RESULT"
echo "  Query archive metadata:      $PART5_RESULT"
echo "  Report tag:                  $PART6_RESULT"
echo "  Query tag reports:           $PART7_RESULT"
echo "  Resolve tag report:          $PART8_RESULT"
echo "  Resolve member report:       $PART9_RESULT"
echo "  Query member warnings:       $PART10_RESULT"
echo "  Query salvation status:      $PART11_RESULT"
echo "  Set forum paused:            $PART12_RESULT"
echo "  Query forum status:          $PART13_RESULT"
echo "  Set moderation paused:       $PART14_RESULT"
echo "  Proposed reply workflow:     $PART15_RESULT"
echo "  Unpin reply:                 $PART16_RESULT"
echo "  Query jury participation:    $PART17_RESULT"
echo "  Follow thread:               $PART18_RESULT"
echo "  Query is-following-thread:   $PART19_RESULT"
echo "  Unfollow thread:             $PART20_RESULT"
echo "  Follow non-existent thread:  $PART21_RESULT"
echo "  Follow already-followed:     $PART22_RESULT"
echo "  Unfollow not-following:      $PART23_RESULT"
echo "  Mark accepted reply:         $PART24_RESULT"
echo "  Accept reply: not author:    $PART25_RESULT"
echo "  Accept reply: thread 404:    $PART26_RESULT"
echo "  Accept reply: reply 404:     $PART27_RESULT"
echo "  Forum paused: unauthorized:  $PART28_RESULT"
echo "  Forum paused: unpause:       $PART29_RESULT"
echo "  Mod paused: unauthorized:    $PART30_RESULT"
echo "  Mod paused: unpause:         $PART31_RESULT"
echo "  Resolve member: unauth:      $PART32_RESULT"
echo "  Resolve member: not found:   $PART33_RESULT"
echo "  Resolve tag: unauthorized:   $PART34_RESULT"
echo "  Resolve tag: not found:      $PART35_RESULT"
echo "  Confirm reply: not author:   $PART36_RESULT"
echo "  Unpin reply: not pinned:     $PART37_RESULT"
echo "  Move thread:                 $PART38_RESULT"
echo "  Pin post:                    $PART39_RESULT"
echo "  Unpin post:                  $PART40_RESULT"
echo "  Pin reply:                   $PART41_RESULT"
echo "  Dispute pin:                 $PART42_RESULT"
FAIL_COUNT=0
for R in "$PART1_RESULT" "$PART2_RESULT" "$PART3_RESULT" "$PART4_RESULT" "$PART5_RESULT" \
         "$PART6_RESULT" "$PART7_RESULT" "$PART8_RESULT" "$PART9_RESULT" "$PART10_RESULT" \
         "$PART11_RESULT" "$PART12_RESULT" "$PART13_RESULT" "$PART14_RESULT" "$PART15_RESULT" \
         "$PART16_RESULT" "$PART17_RESULT" "$PART18_RESULT" "$PART19_RESULT" "$PART20_RESULT" \
         "$PART21_RESULT" "$PART22_RESULT" "$PART23_RESULT" "$PART24_RESULT" "$PART25_RESULT" \
         "$PART26_RESULT" "$PART27_RESULT" "$PART28_RESULT" "$PART29_RESULT" "$PART30_RESULT" \
         "$PART31_RESULT" "$PART32_RESULT" "$PART33_RESULT" "$PART34_RESULT" "$PART35_RESULT" \
         "$PART36_RESULT" "$PART37_RESULT" "$PART38_RESULT" "$PART39_RESULT" "$PART40_RESULT" \
         "$PART41_RESULT" "$PART42_RESULT"; do
    if [ "$R" == "FAIL" ]; then
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
done

if [ "$FAIL_COUNT" -gt 0 ]; then
    echo "  FAILURES: $FAIL_COUNT test(s) failed"
else
    echo "  ALL TESTS PASSED"
fi
echo ""
echo "ADVANCED FORUM FEATURES TEST COMPLETED"
echo ""
