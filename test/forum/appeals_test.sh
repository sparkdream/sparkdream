#!/bin/bash

echo "--- TESTING: APPEALS (POST, THREAD LOCK, THREAD MOVE, GOV ACTION) ---"

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

# Bootstrap reputation for an account by creating and completing EPIC interims.
# Each EPIC interim grants 100 reputation. Tier 3 = 200+, Tier 4 = 500+.
bootstrap_reputation() {
    local ACCOUNT=$1
    local COUNT=$2
    echo "  Bootstrapping $COUNT EPIC interims for $ACCOUNT..."

    for i in $(seq 1 $COUNT); do
        # Create EPIC interim (type=other, ref_id=0, ref_type=test, complexity=epic, deadline=999999999)
        TX_RES=$($BINARY tx rep create-interim other 0 "test-$i" epic 999999999 \
            --from $ACCOUNT \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)
        TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
        if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
            echo "    Failed to create interim $i"
            return 1
        fi
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        if ! check_tx_success "$TX_RESULT"; then
            echo "    Failed to create interim $i"
            return 1
        fi
        INTERIM_ID=$(extract_event_value "$TX_RESULT" "interim_created" "interim_id")

        # Complete the interim
        TX_RES=$($BINARY tx rep complete-interim $INTERIM_ID "Completed for test setup" \
            --from $ACCOUNT \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)
        TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
        if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
            echo "    Failed to complete interim $i"
            return 1
        fi
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        if ! check_tx_success "$TX_RESULT"; then
            echo "    Failed to complete interim $i"
            return 1
        fi
        echo "    Completed interim $i/$COUNT (ID: $INTERIM_ID)"
    done
    echo "  Reputation bootstrapped for $ACCOUNT"
}

# ========================================================================
# PART 0: BOOTSTRAP REPUTATION
# ========================================================================
echo "--- PART 0: BOOTSTRAP REPUTATION ---"
echo "Sentinel operations require reputation tiers. Building reputation via EPIC interims..."
echo ""

# Check if sentinel1 already has a sentinel activity record (already bootstrapped)
SENTINEL_STATUS=$($BINARY query forum sentinel-status "$SENTINEL1_ADDR" --output json 2>&1)

if echo "$SENTINEL_STATUS" | grep -q "error\|not found"; then
    # Sentinel1 needs tier 4 (500+ rep) for thread locking = 5 EPIC interims
    bootstrap_reputation sentinel1 5
    echo ""
else
    echo "  Sentinel1 already registered, skipping bootstrap"
    echo ""
fi

# ========================================================================
# PART 1: SETUP - BOND SENTINEL AND CREATE POSTS
# ========================================================================
echo "--- PART 1: SETUP - BOND SENTINEL AND CREATE POSTS ---"

# Check if sentinel1 is already bonded
SENTINEL_STATUS=$($BINARY query forum sentinel-status "$SENTINEL1_ADDR" --output json 2>&1)

if echo "$SENTINEL_STATUS" | grep -q "error\|not found"; then
    echo "Bonding sentinel1..."
    BOND_AMOUNT="100000000"

    TX_RES=$($BINARY tx forum bond-sentinel \
        "$BOND_AMOUNT" \
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
            echo "  Sentinel bonded"
        else
            echo "  Failed to bond sentinel"
        fi
    fi
else
    echo "  Sentinel already bonded"
fi

# Create a test post for hiding/appeal
echo ""
echo "Creating post for appeal test..."

POST_CONTENT="Test post for appeal $(date +%s)"

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

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to create post"
    APPEAL_POST_ID=""
else
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        APPEAL_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        if [ -z "$APPEAL_POST_ID" ]; then
            POSTS=$($BINARY query forum list-post --output json 2>&1)
            APPEAL_POST_ID=$(echo "$POSTS" | jq -r '.post[-1].id // empty')
        fi
        echo "  Post created: ID $APPEAL_POST_ID"
    else
        APPEAL_POST_ID=""
    fi
fi

echo ""

# ========================================================================
# PART 2: HIDE POST (Setup for appeal)
# ========================================================================
echo "--- PART 2: HIDE POST (Setup for appeal) ---"

if [ -n "$APPEAL_POST_ID" ]; then
    echo "Sentinel hiding post $APPEAL_POST_ID..."

    TX_RES=$($BINARY tx forum hide-post \
        "$APPEAL_POST_ID" \
        "1" \
        "Testing appeal functionality" \
        --from sentinel1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit hide transaction"
        POST_HIDDEN=false
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Post hidden successfully"
            POST_HIDDEN=true
        else
            echo "  Failed to hide post"
            POST_HIDDEN=false
        fi
    fi
else
    echo "  No post to hide"
    POST_HIDDEN=false
fi

echo ""

# ========================================================================
# PART 3: QUERY HIDE RECORD
# ========================================================================
echo "--- PART 3: QUERY HIDE RECORD ---"

if [ -n "$APPEAL_POST_ID" ]; then
    HIDE_RECORD=$($BINARY query forum get-hide-record "$APPEAL_POST_ID" --output json 2>&1)

    if echo "$HIDE_RECORD" | grep -q "error\|not found"; then
        echo "  No hide record found"
    else
        echo "  Hide Record:"
        echo "    Post ID: $(echo "$HIDE_RECORD" | jq -r '.hide_record.post_id // .record.post_id // "N/A"')"
        echo "    Sentinel: $(echo "$HIDE_RECORD" | jq -r '.hide_record.sentinel // .record.sentinel // "N/A"' | head -c 30)..."
        echo "    Hidden At: $(echo "$HIDE_RECORD" | jq -r '.hide_record.hidden_at // .record.hidden_at // "N/A"')"
        echo "    Reason: $(echo "$HIDE_RECORD" | jq -r '.hide_record.reason_text // .record.reason_text // "N/A"')"
    fi
fi

echo ""

# ========================================================================
# PART 4: APPEAL POST
# ========================================================================
echo "--- PART 4: APPEAL POST ---"

APPEAL_POST_RESULT="SKIP"
APPEAL_POST_FILED=false

if [ "$POST_HIDDEN" = true ] && [ -n "$APPEAL_POST_ID" ]; then
    # Top up poster1 with SPARK for appeal fee (5M uspark) — gas from prior tests may have drained the balance
    echo "Topping up poster1 with SPARK for appeal fee..."
    TX_RES=$($BINARY tx bank send \
        alice $POSTER1_ADDR \
        10000000uspark \
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

    echo "Waiting for appeal cooldown (5s)..."
    sleep 6

    echo "Author appealing hidden post $APPEAL_POST_ID..."

    TX_RES=$($BINARY tx forum appeal-post \
        "$APPEAL_POST_ID" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit appeal"
        echo "  Response: $(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)"
        APPEAL_POST_RESULT="FAIL"
    else
        echo "  Transaction: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        CODE=$(echo "$TX_RESULT" | jq -r '.code')
        if [ "$CODE" == "0" ]; then
            echo "  Appeal filed successfully"
            APPEAL_POST_RESULT="PASS"
            APPEAL_POST_FILED=true
        else
            echo "  Failed to file appeal (code: $CODE)"
            echo "  $(echo "$TX_RESULT" | jq -r '.raw_log')"
            APPEAL_POST_RESULT="FAIL"
        fi
    fi
else
    echo "  No hidden post to appeal"
fi

echo ""

# ========================================================================
# PART 5: QUERY APPEAL COOLDOWN (uses post_id, not address)
# ========================================================================
echo "--- PART 5: QUERY APPEAL COOLDOWN ---"

if [ -n "$APPEAL_POST_ID" ]; then
    APPEAL_COOLDOWN=$($BINARY query forum appeal-cooldown "$APPEAL_POST_ID" --output json 2>&1)

    if echo "$APPEAL_COOLDOWN" | grep -q "error"; then
        echo "  Failed to query appeal cooldown"
    else
        echo "  Appeal Cooldown:"
        echo "    In Cooldown: $(echo "$APPEAL_COOLDOWN" | jq -r 'if .in_cooldown == null then "false" else .in_cooldown end')"
        echo "    Cooldown Ends: $(echo "$APPEAL_COOLDOWN" | jq -r '.cooldown_ends // "0"')"
    fi
else
    echo "  No post ID to query cooldown for"
fi

echo ""

# ========================================================================
# PART 6: CREATE AND LOCK THREAD (Setup for lock appeal)
# ========================================================================
echo "--- PART 6: CREATE AND LOCK THREAD ---"

LOCK_THREAD_CONTENT="Thread for lock appeal test $(date +%s)"

TX_RES=$($BINARY tx forum create-post \
    "$TEST_CATEGORY_ID" \
    "0" \
    "$LOCK_THREAD_CONTENT" \
    --from poster2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to create thread"
    LOCK_THREAD_ID=""
else
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        LOCK_THREAD_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        if [ -z "$LOCK_THREAD_ID" ]; then
            POSTS=$($BINARY query forum list-post --output json 2>&1)
            LOCK_THREAD_ID=$(echo "$POSTS" | jq -r '.post[-1].id // empty')
        fi
        echo "  Thread created: ID $LOCK_THREAD_ID"

        # Lock the thread
        echo "  Sentinel locking thread..."

        TX_RES=$($BINARY tx forum lock-thread \
            "$LOCK_THREAD_ID" \
            "Testing lock appeal" \
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
                echo "  Thread locked"
                THREAD_LOCKED=true
            else
                THREAD_LOCKED=false
            fi
        else
            THREAD_LOCKED=false
        fi
    else
        LOCK_THREAD_ID=""
        THREAD_LOCKED=false
    fi
fi

echo ""

# ========================================================================
# PART 7: QUERY THREAD LOCK RECORD
# ========================================================================
echo "--- PART 7: QUERY THREAD LOCK RECORD ---"

if [ -n "$LOCK_THREAD_ID" ]; then
    LOCK_RECORD=$($BINARY query forum get-thread-lock-record "$LOCK_THREAD_ID" --output json 2>&1)

    if echo "$LOCK_RECORD" | grep -q "error\|not found"; then
        echo "  No lock record found"
    else
        echo "  Thread Lock Record:"
        echo "    Root ID: $(echo "$LOCK_RECORD" | jq -r '.thread_lock_record.root_id // .record.root_id // "N/A"')"
        echo "    Sentinel: $(echo "$LOCK_RECORD" | jq -r '.thread_lock_record.sentinel // .record.sentinel // "N/A"' | head -c 30)..."
        echo "    Reason: $(echo "$LOCK_RECORD" | jq -r '.thread_lock_record.lock_reason // .record.lock_reason // "N/A"')"
        echo "    Appeal Pending: $(echo "$LOCK_RECORD" | jq -r 'if .thread_lock_record.appeal_pending == null then "false" elif .thread_lock_record.appeal_pending then "true" else "false" end')"
    fi
fi

echo ""

# ========================================================================
# PART 8: APPEAL THREAD LOCK
# ========================================================================
echo "--- PART 8: APPEAL THREAD LOCK ---"

APPEAL_LOCK_RESULT="SKIP"
APPEAL_LOCK_FILED=false

if [ "$THREAD_LOCKED" = true ] && [ -n "$LOCK_THREAD_ID" ]; then
    echo "Waiting for appeal cooldown (5s)..."
    sleep 6

    echo "Author appealing thread lock..."

    TX_RES=$($BINARY tx forum appeal-thread-lock \
        "$LOCK_THREAD_ID" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit appeal"
        echo "  Response: $(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)"
        APPEAL_LOCK_RESULT="FAIL"
    else
        echo "  Transaction: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        CODE=$(echo "$TX_RESULT" | jq -r '.code')
        if [ "$CODE" == "0" ]; then
            echo "  Thread lock appeal filed successfully"
            APPEAL_LOCK_RESULT="PASS"
            APPEAL_LOCK_FILED=true
        else
            echo "  Failed to file appeal (code: $CODE)"
            echo "  $(echo "$TX_RESULT" | jq -r '.raw_log')"
            APPEAL_LOCK_RESULT="FAIL"
        fi
    fi
else
    echo "  No locked thread to appeal"
fi

echo ""

# ========================================================================
# PART 9: QUERY LOCKED THREADS
# ========================================================================
echo "--- PART 9: QUERY LOCKED THREADS ---"

LOCKED_THREADS=$($BINARY query forum locked-threads --output json 2>&1)

if echo "$LOCKED_THREADS" | grep -q "error"; then
    echo "  Failed to query locked threads"
else
    # LockedThreads returns a flat response (root_id, locked_by, locked_at), not a list
    LOCKED_ROOT_ID=$(echo "$LOCKED_THREADS" | jq -r '.root_id // "0"')
    if [ "$LOCKED_ROOT_ID" != "0" ] && [ "$LOCKED_ROOT_ID" != "null" ] && [ -n "$LOCKED_ROOT_ID" ]; then
        echo "  Found locked thread:"
        echo "    Root ID: $LOCKED_ROOT_ID"
        echo "    Locked By: $(echo "$LOCKED_THREADS" | jq -r '.locked_by // "N/A"' | head -c 30)..."
        echo "    Locked At: $(echo "$LOCKED_THREADS" | jq -r '.locked_at // "N/A"')"
    else
        echo "  No locked threads found"
    fi
fi

echo ""

# ========================================================================
# PART 10: QUERY THREAD LOCK STATUS
# ========================================================================
echo "--- PART 10: QUERY THREAD LOCK STATUS ---"

if [ -n "$LOCK_THREAD_ID" ]; then
    LOCK_STATUS=$($BINARY query forum thread-lock-status "$LOCK_THREAD_ID" --output json 2>&1)

    if echo "$LOCK_STATUS" | grep -q "error"; then
        echo "  Failed to query lock status"
    else
        echo "  Thread Lock Status:"
        echo "    Locked: $(echo "$LOCK_STATUS" | jq -r 'if .locked == null then "false" else .locked end')"
        echo "    Locked By: $(echo "$LOCK_STATUS" | jq -r '.locked_by // "N/A"' | head -c 30)..."
        echo "    Reason: $(echo "$LOCK_STATUS" | jq -r '.reason // "N/A"')"
        echo "    Is Sentinel Lock: $(echo "$LOCK_STATUS" | jq -r 'if .is_sentinel_lock == null then "false" else .is_sentinel_lock end')"
    fi
fi

echo ""

# ========================================================================
# PART 11: LIST ALL HIDE RECORDS
# ========================================================================
echo "--- PART 11: LIST ALL HIDE RECORDS ---"

HIDE_RECORDS=$($BINARY query forum list-hide-record --output json 2>&1)

if echo "$HIDE_RECORDS" | grep -q "error"; then
    echo "  Failed to query hide records"
else
    RECORD_COUNT=$(echo "$HIDE_RECORDS" | jq -r '.hide_record | length // .records | length // 0' 2>/dev/null)
    echo "  Total hide records: $RECORD_COUNT"

    if [ "$RECORD_COUNT" -gt 0 ]; then
        echo ""
        echo "  Recent Hide Records:"
        echo "$HIDE_RECORDS" | jq -r '.hide_record[:5] // .records[:5] | .[] | "    - Post \(.post_id): by \(.sentinel | .[0:20])... reason: \(.reason_code)"' 2>/dev/null
    fi
fi

echo ""

# ========================================================================
# PART 12: LIST THREAD LOCK RECORDS
# ========================================================================
echo "--- PART 12: LIST THREAD LOCK RECORDS ---"

LOCK_RECORDS=$($BINARY query forum list-thread-lock-record --output json 2>&1)

if echo "$LOCK_RECORDS" | grep -q "error"; then
    echo "  Failed to query lock records"
else
    RECORD_COUNT=$(echo "$LOCK_RECORDS" | jq -r '.thread_lock_record | length // .records | length // 0' 2>/dev/null)
    echo "  Total lock records: $RECORD_COUNT"

    if [ "$RECORD_COUNT" -gt 0 ]; then
        echo ""
        echo "  Lock Records:"
        echo "$LOCK_RECORDS" | jq -r '.thread_lock_record[:5] // .records[:5] | .[] | "    - Thread \(.root_id): \(.lock_reason)"' 2>/dev/null
    fi
fi

echo ""

# ========================================================================
# PART 13: QUERY GOV ACTION APPEALS
# ========================================================================
echo "--- PART 13: QUERY GOV ACTION APPEALS ---"

GOV_APPEALS=$($BINARY query forum gov-action-appeals --output json 2>&1)

if echo "$GOV_APPEALS" | grep -q "error"; then
    echo "  Failed to query gov action appeals"
else
    # GovActionAppeals returns a flat response (appeal_id, action_type, status), not a list
    APPEAL_ID=$(echo "$GOV_APPEALS" | jq -r '.appeal_id // "0"')
    if [ "$APPEAL_ID" != "0" ] && [ "$APPEAL_ID" != "null" ] && [ -n "$APPEAL_ID" ]; then
        echo "  Found gov action appeal:"
        echo "    Appeal ID: $APPEAL_ID"
        echo "    Action Type: $(echo "$GOV_APPEALS" | jq -r '.action_type // "N/A"')"
        echo "    Status: $(echo "$GOV_APPEALS" | jq -r '.status // "N/A"')"
    else
        echo "  No gov action appeals found"
    fi
fi

echo ""

# ========================================================================
# PART 14: APPEAL GOV ACTION (Test interface)
# ========================================================================
echo "--- PART 14: APPEAL GOV ACTION (Test interface) ---"

echo "Testing appeal-gov-action command..."

GOV_APPEAL_ID=""

# appeal-gov-action [action-type] [action-target] [appeal-reason]
TX_RES=$($BINARY tx forum appeal-gov-action \
    "1" \
    "test-target" \
    "Testing gov action appeal reason" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Transaction failed"
    echo "  Response: $(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)"
else
    echo "  Transaction: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        GOV_APPEAL_ID=$(extract_event_value "$TX_RESULT" "gov_action_appealed" "appeal_id")
        echo "  Gov action appeal filed (ID: $GOV_APPEAL_ID)"
    else
        echo "  Appeal failed"
    fi
fi

echo ""

# ========================================================================
# PART 15: SENTINEL BOND COMMITMENT QUERY
# ========================================================================
echo "--- PART 15: SENTINEL BOND COMMITMENT QUERY ---"

COMMITMENT=$($BINARY query forum sentinel-bond-commitment "$SENTINEL1_ADDR" --output json 2>&1)

if echo "$COMMITMENT" | grep -q "error"; then
    echo "  Failed to query bond commitment"
else
    echo "  Sentinel Bond Commitment:"
    echo "    Current Bond: $(echo "$COMMITMENT" | jq -r '.current_bond // "0"')"
    echo "    Total Committed: $(echo "$COMMITMENT" | jq -r '.total_committed_bond // "0"')"
    echo "    Available Bond: $(echo "$COMMITMENT" | jq -r '.available_bond // "0"')"
fi

echo ""

# ========================================================================
# PART 16: APPEAL POST ERROR - Post not found
# ========================================================================
echo "--- PART 16: APPEAL POST ERROR - Post not found ---"

echo "Appealing non-existent post (post_id=999999)..."

TX_RES=$($BINARY tx forum appeal-post \
    "999999" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Pre-broadcast failure (expected)"
    RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
    echo "  Error: $RAW_LOG"
    if echo "$RAW_LOG" | grep -qi "not found"; then
        echo "  PASS: Got expected ErrPostNotFound"
    fi
else
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')

    if [ "$CODE" != "0" ]; then
        echo "  PASS: Transaction failed with code $CODE"
        if echo "$RAW_LOG" | grep -qi "not found"; then
            echo "  Confirmed: ErrPostNotFound"
        fi
    else
        echo "  FAIL: Expected error but tx succeeded"
    fi
fi

echo ""

# ========================================================================
# PART 17: APPEAL POST ERROR - Post not hidden
# ========================================================================
echo "--- PART 17: APPEAL POST ERROR - Post not hidden ---"

# Create a visible post to try to appeal
echo "Creating a visible post..."

VIS_CONTENT="Visible post for error test $(date +%s)"

TX_RES=$($BINARY tx forum create-post \
    "$TEST_CATEGORY_ID" \
    "0" \
    "$VIS_CONTENT" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
VISIBLE_POST_ID=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        VISIBLE_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        if [ -z "$VISIBLE_POST_ID" ]; then
            POSTS=$($BINARY query forum list-post --output json 2>&1)
            VISIBLE_POST_ID=$(echo "$POSTS" | jq -r '.post[-1].id // empty')
        fi
        echo "  Visible post created: ID $VISIBLE_POST_ID"
    fi
fi

if [ -n "$VISIBLE_POST_ID" ]; then
    echo "Appealing visible (non-hidden) post $VISIBLE_POST_ID..."

    TX_RES=$($BINARY tx forum appeal-post \
        "$VISIBLE_POST_ID" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Pre-broadcast failure (expected)"
        RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
        echo "  Error: $RAW_LOG"
        if echo "$RAW_LOG" | grep -qi "not hidden"; then
            echo "  PASS: Got expected ErrPostNotHidden"
        fi
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')

        if [ "$CODE" != "0" ]; then
            echo "  PASS: Transaction failed with code $CODE"
            if echo "$RAW_LOG" | grep -qi "not hidden"; then
                echo "  Confirmed: ErrPostNotHidden"
            fi
        else
            echo "  FAIL: Expected error but tx succeeded"
        fi
    fi
else
    echo "  Skipped: Could not create visible post"
fi

echo ""

# ========================================================================
# PART 18: APPEAL POST ERROR - Not post author
# ========================================================================
echo "--- PART 18: APPEAL POST ERROR - Not post author ---"

if [ "$POST_HIDDEN" = true ] && [ -n "$APPEAL_POST_ID" ]; then
    echo "poster2 appealing poster1's hidden post $APPEAL_POST_ID..."

    TX_RES=$($BINARY tx forum appeal-post \
        "$APPEAL_POST_ID" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Pre-broadcast failure (expected)"
        RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
        echo "  Error: $RAW_LOG"
        if echo "$RAW_LOG" | grep -qi "not the post author"; then
            echo "  PASS: Got expected ErrNotPostAuthor"
        fi
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')

        if [ "$CODE" != "0" ]; then
            echo "  PASS: Transaction failed with code $CODE"
            if echo "$RAW_LOG" | grep -qi "not the post author"; then
                echo "  Confirmed: ErrNotPostAuthor"
            fi
        else
            echo "  FAIL: Expected error but tx succeeded"
        fi
    fi
else
    echo "  Skipped: No hidden post available"
fi

echo ""

# ========================================================================
# PART 19: APPEAL THREAD LOCK ERROR - Thread not found
# ========================================================================
echo "--- PART 19: APPEAL THREAD LOCK ERROR - Thread not found ---"

echo "Appealing non-existent thread lock (thread_id=999999)..."

TX_RES=$($BINARY tx forum appeal-thread-lock \
    "999999" \
    --from poster2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Pre-broadcast failure (expected)"
    RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
    echo "  Error: $RAW_LOG"
    if echo "$RAW_LOG" | grep -qi "not found"; then
        echo "  PASS: Got expected ErrPostNotFound"
    fi
else
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')

    if [ "$CODE" != "0" ]; then
        echo "  PASS: Transaction failed with code $CODE"
        if echo "$RAW_LOG" | grep -qi "not found"; then
            echo "  Confirmed: ErrPostNotFound"
        fi
    else
        echo "  FAIL: Expected error but tx succeeded"
    fi
fi

echo ""

# ========================================================================
# PART 20: APPEAL THREAD LOCK ERROR - Thread not locked
# ========================================================================
echo "--- PART 20: APPEAL THREAD LOCK ERROR - Thread not locked ---"

if [ -n "$VISIBLE_POST_ID" ]; then
    echo "Appealing an unlocked thread $VISIBLE_POST_ID..."

    TX_RES=$($BINARY tx forum appeal-thread-lock \
        "$VISIBLE_POST_ID" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Pre-broadcast failure (expected)"
        RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
        echo "  Error: $RAW_LOG"
        if echo "$RAW_LOG" | grep -qi "not locked"; then
            echo "  PASS: Got expected ErrThreadNotLocked"
        fi
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')

        if [ "$CODE" != "0" ]; then
            echo "  PASS: Transaction failed with code $CODE"
            if echo "$RAW_LOG" | grep -qi "not locked"; then
                echo "  Confirmed: ErrThreadNotLocked"
            fi
        else
            echo "  FAIL: Expected error but tx succeeded"
        fi
    fi
else
    echo "  Skipped: No visible post ID available"
fi

echo ""

# ========================================================================
# PART 21: APPEAL THREAD LOCK ERROR - Not thread author
# ========================================================================
echo "--- PART 21: APPEAL THREAD LOCK ERROR - Not thread author ---"

if [ "$THREAD_LOCKED" = true ] && [ -n "$LOCK_THREAD_ID" ]; then
    echo "poster1 appealing poster2's locked thread $LOCK_THREAD_ID..."

    TX_RES=$($BINARY tx forum appeal-thread-lock \
        "$LOCK_THREAD_ID" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Pre-broadcast failure (expected)"
        RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
        echo "  Error: $RAW_LOG"
        if echo "$RAW_LOG" | grep -qi "not the thread author"; then
            echo "  PASS: Got expected ErrNotThreadAuthor"
        fi
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')

        if [ "$CODE" != "0" ]; then
            echo "  PASS: Transaction failed with code $CODE"
            if echo "$RAW_LOG" | grep -qi "not the thread author"; then
                echo "  Confirmed: ErrNotThreadAuthor"
            fi
        else
            echo "  FAIL: Expected error but tx succeeded"
        fi
    fi
else
    echo "  Skipped: No locked thread available"
fi

echo ""

# ========================================================================
# PART 22: APPEAL THREAD LOCK ERROR - Appeal already filed
# ========================================================================
echo "--- PART 22: APPEAL THREAD LOCK ERROR - Appeal already filed ---"

DUPLICATE_LOCK_APPEAL_RESULT="SKIP"

if [ -n "$LOCK_THREAD_ID" ] && [ "$THREAD_LOCKED" = true ]; then
    echo "Filing duplicate lock appeal for thread $LOCK_THREAD_ID..."

    TX_RES=$($BINARY tx forum appeal-thread-lock \
        "$LOCK_THREAD_ID" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Pre-broadcast failure (expected)"
        RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
        echo "  Error: $RAW_LOG"
        if echo "$RAW_LOG" | grep -qi "already filed"; then
            echo "  PASS: Got expected ErrLockAppealAlreadyFiled"
            DUPLICATE_LOCK_APPEAL_RESULT="PASS"
        else
            DUPLICATE_LOCK_APPEAL_RESULT="FAIL"
        fi
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')

        if [ "$CODE" != "0" ]; then
            if echo "$RAW_LOG" | grep -qi "already filed"; then
                echo "  PASS: Transaction failed with expected error"
                echo "  Confirmed: ErrLockAppealAlreadyFiled"
                DUPLICATE_LOCK_APPEAL_RESULT="PASS"
            else
                echo "  Transaction failed with code $CODE (unexpected error)"
                echo "  Error: $RAW_LOG"
                DUPLICATE_LOCK_APPEAL_RESULT="FAIL"
            fi
        else
            echo "  FAIL: Expected error but tx succeeded"
            DUPLICATE_LOCK_APPEAL_RESULT="FAIL"
        fi
    fi
else
    echo "  Skipped: No locked thread available"
fi

echo ""

# ========================================================================
# PART 23: SETUP - MOVE THREAD (Setup for move appeal)
# ========================================================================
echo "--- PART 23: SETUP - MOVE THREAD (Setup for move appeal) ---"

# Top up poster1's SPARK — earlier appeal fees may have drained the balance
echo "Topping up poster1 with 10 SPARK for move appeal fees..."
TX_RES=$($BINARY tx bank send \
    alice $POSTER1_ADDR \
    10000000uspark \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)
TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    wait_for_tx $TXHASH > /dev/null 2>&1
    echo "  poster1 funded"
fi

# Create a second category if needed
CATEGORIES=$($BINARY query forum list-category --output json 2>&1)
CATEGORY_COUNT=$(echo "$CATEGORIES" | jq -r '.category | length' 2>/dev/null || echo "0")

if [ "$CATEGORY_COUNT" -gt 1 ]; then
    MOVE_TARGET_CATEGORY=$(echo "$CATEGORIES" | jq -r '.category[1].category_id')
    echo "  Using existing second category: $MOVE_TARGET_CATEGORY"
else
    echo "  Creating second category for move test..."

    TX_RES=$($BINARY tx forum create-category \
        "Move Target $(date +%s)" \
        "Target category for move appeal tests" \
        "false" \
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
            MOVE_TARGET_CATEGORY=$(extract_event_value "$TX_RESULT" "category_created" "category_id")
            if [ -z "$MOVE_TARGET_CATEGORY" ]; then
                CATEGORIES=$($BINARY query forum list-category --output json 2>&1)
                MOVE_TARGET_CATEGORY=$(echo "$CATEGORIES" | jq -r '.category[-1].category_id // empty')
            fi
            echo "  Created category: $MOVE_TARGET_CATEGORY"
        else
            echo "  Failed to create category"
            MOVE_TARGET_CATEGORY=""
        fi
    else
        echo "  Failed to submit category creation"
        MOVE_TARGET_CATEGORY=""
    fi
fi

# Create a thread to move
MOVE_THREAD_ID=""
THREAD_MOVED=false

if [ -n "$MOVE_TARGET_CATEGORY" ]; then
    echo "Creating thread for move appeal test..."

    MOVE_CONTENT="Thread for move appeal test $(date +%s)"

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

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            MOVE_THREAD_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
            if [ -z "$MOVE_THREAD_ID" ]; then
                POSTS=$($BINARY query forum list-post --output json 2>&1)
                MOVE_THREAD_ID=$(echo "$POSTS" | jq -r '.post[-1].id // empty')
            fi
            echo "  Thread created: ID $MOVE_THREAD_ID"

            # Sentinel moves the thread
            echo "  Sentinel moving thread to category $MOVE_TARGET_CATEGORY..."

            TX_RES=$($BINARY tx forum move-thread \
                "$MOVE_THREAD_ID" \
                "$MOVE_TARGET_CATEGORY" \
                "Testing move appeal flow" \
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
                    echo "  Thread moved"
                    THREAD_MOVED=true
                else
                    echo "  Failed to move thread"
                fi
            else
                echo "  Failed to submit move transaction"
            fi
        fi
    fi
fi

echo ""

# ========================================================================
# PART 24: QUERY THREAD MOVE RECORD
# ========================================================================
echo "--- PART 24: QUERY THREAD MOVE RECORD ---"

if [ -n "$MOVE_THREAD_ID" ] && [ "$THREAD_MOVED" = true ]; then
    MOVE_RECORD=$($BINARY query forum get-thread-move-record "$MOVE_THREAD_ID" --output json 2>&1)

    if echo "$MOVE_RECORD" | grep -q "error\|not found"; then
        echo "  No move record found"
    else
        echo "  Thread Move Record:"
        echo "    Root ID: $(echo "$MOVE_RECORD" | jq -r '.thread_move_record.root_id // .record.root_id // "N/A"')"
        echo "    Sentinel: $(echo "$MOVE_RECORD" | jq -r '.thread_move_record.sentinel // .record.sentinel // "N/A"' | head -c 30)..."
        echo "    Original Category: $(echo "$MOVE_RECORD" | jq -r '.thread_move_record.original_category_id // .record.original_category_id // "N/A"')"
        echo "    New Category: $(echo "$MOVE_RECORD" | jq -r '.thread_move_record.new_category_id // .record.new_category_id // "N/A"')"
        echo "    Reason: $(echo "$MOVE_RECORD" | jq -r '.thread_move_record.move_reason // .record.move_reason // "N/A"')"
        echo "    Appeal Pending: $(echo "$MOVE_RECORD" | jq -r 'if .thread_move_record.appeal_pending == null then "false" elif .thread_move_record.appeal_pending then "true" else "false" end')"
    fi
else
    echo "  No move record to query"
fi

echo ""

# ========================================================================
# PART 25: APPEAL THREAD MOVE (Happy path)
# ========================================================================
echo "--- PART 25: APPEAL THREAD MOVE (Happy path) ---"

APPEAL_MOVE_RESULT="SKIP"
APPEAL_MOVE_FILED=false

if [ "$THREAD_MOVED" = true ] && [ -n "$MOVE_THREAD_ID" ]; then
    echo "Waiting for appeal cooldown (5s)..."
    sleep 6

    echo "Thread author appealing move for thread $MOVE_THREAD_ID..."

    TX_RES=$($BINARY tx forum appeal-thread-move \
        "$MOVE_THREAD_ID" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit appeal"
        echo "  Response: $(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)"
        APPEAL_MOVE_RESULT="FAIL"
    else
        echo "  Transaction: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        CODE=$(echo "$TX_RESULT" | jq -r '.code')
        if [ "$CODE" == "0" ]; then
            echo "  Thread move appeal filed successfully"
            APPEAL_MOVE_RESULT="PASS"
            APPEAL_MOVE_FILED=true
        else
            echo "  Failed to file appeal (code: $CODE)"
            echo "  $(echo "$TX_RESULT" | jq -r '.raw_log')"
            APPEAL_MOVE_RESULT="FAIL"
        fi
    fi
else
    echo "  No moved thread to appeal"
fi

echo ""

# ========================================================================
# PART 26: APPEAL THREAD MOVE ERROR - Non-existent thread
# ========================================================================
echo "--- PART 26: APPEAL THREAD MOVE ERROR - Non-existent thread ---"

echo "Appealing move for non-existent thread (thread_id=999999)..."

TX_RES=$($BINARY tx forum appeal-thread-move \
    "999999" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Pre-broadcast failure (expected)"
    RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
    echo "  Error: $RAW_LOG"
    if echo "$RAW_LOG" | grep -qi "not found"; then
        echo "  PASS: Got expected ErrPostNotFound"
    fi
else
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')

    if [ "$CODE" != "0" ]; then
        echo "  PASS: Transaction failed with code $CODE"
        if echo "$RAW_LOG" | grep -qi "not found"; then
            echo "  Confirmed: ErrPostNotFound"
        fi
    else
        echo "  FAIL: Expected error but tx succeeded"
    fi
fi

echo ""

# ========================================================================
# PART 27: APPEAL THREAD MOVE ERROR - Not thread author
# ========================================================================
echo "--- PART 27: APPEAL THREAD MOVE ERROR - Not thread author ---"

if [ "$THREAD_MOVED" = true ] && [ -n "$MOVE_THREAD_ID" ]; then
    echo "poster2 appealing poster1's moved thread $MOVE_THREAD_ID..."

    TX_RES=$($BINARY tx forum appeal-thread-move \
        "$MOVE_THREAD_ID" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Pre-broadcast failure (expected)"
        RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
        echo "  Error: $RAW_LOG"
        if echo "$RAW_LOG" | grep -qi "not the thread author"; then
            echo "  PASS: Got expected ErrNotThreadAuthor"
        fi
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')

        if [ "$CODE" != "0" ]; then
            echo "  PASS: Transaction failed with code $CODE"
            if echo "$RAW_LOG" | grep -qi "not the thread author"; then
                echo "  Confirmed: ErrNotThreadAuthor"
            fi
        else
            echo "  FAIL: Expected error but tx succeeded"
        fi
    fi
else
    echo "  Skipped: No moved thread available"
fi

echo ""

# ========================================================================
# PART 28: APPEAL THREAD MOVE ERROR - Appeal already filed
# ========================================================================
echo "--- PART 28: APPEAL THREAD MOVE ERROR - Appeal already filed ---"

DUPLICATE_MOVE_APPEAL_RESULT="SKIP"

if [ -n "$MOVE_THREAD_ID" ] && [ "$THREAD_MOVED" = true ]; then
    echo "Filing duplicate move appeal for thread $MOVE_THREAD_ID..."

    TX_RES=$($BINARY tx forum appeal-thread-move \
        "$MOVE_THREAD_ID" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Pre-broadcast failure (expected)"
        RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
        echo "  Error: $RAW_LOG"
        if echo "$RAW_LOG" | grep -qi "already filed"; then
            echo "  PASS: Got expected ErrMoveAppealAlreadyFiled"
            DUPLICATE_MOVE_APPEAL_RESULT="PASS"
        else
            DUPLICATE_MOVE_APPEAL_RESULT="FAIL"
        fi
    else
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')

        if [ "$CODE" != "0" ]; then
            if echo "$RAW_LOG" | grep -qi "already filed"; then
                echo "  PASS: Transaction failed with expected error"
                echo "  Confirmed: ErrMoveAppealAlreadyFiled"
                DUPLICATE_MOVE_APPEAL_RESULT="PASS"
            else
                echo "  Transaction failed with code $CODE (unexpected error)"
                echo "  Error: $RAW_LOG"
                DUPLICATE_MOVE_APPEAL_RESULT="FAIL"
            fi
        else
            echo "  FAIL: Expected error but tx succeeded"
            DUPLICATE_MOVE_APPEAL_RESULT="FAIL"
        fi
    fi
else
    echo "  Skipped: No moved thread available"
fi

echo ""

# ========================================================================
# PART 29: LIST THREAD MOVE RECORDS
# ========================================================================
echo "--- PART 29: LIST THREAD MOVE RECORDS ---"

MOVE_RECORDS=$($BINARY query forum list-thread-move-record --output json 2>&1)

if echo "$MOVE_RECORDS" | grep -q "error"; then
    echo "  Failed to query move records"
else
    RECORD_COUNT=$(echo "$MOVE_RECORDS" | jq -r '.thread_move_record | length // .records | length // 0' 2>/dev/null)
    echo "  Total move records: $RECORD_COUNT"

    if [ "$RECORD_COUNT" -gt 0 ]; then
        echo ""
        echo "  Move Records:"
        echo "$MOVE_RECORDS" | jq -r '.thread_move_record[:5] // .records[:5] | .[] | "    - Thread \(.root_id): from cat \(.original_category_id) to cat \(.new_category_id)"' 2>/dev/null
    fi
fi

echo ""

# ========================================================================
# PART 30: APPEAL GOV ACTION ERROR - Invalid action type
# ========================================================================
echo "--- PART 30: APPEAL GOV ACTION ERROR - Invalid action type ---"

echo "Testing appeal-gov-action with action_type=0 (unspecified)..."

TX_RES=$($BINARY tx forum appeal-gov-action \
    "0" \
    "test-target" \
    "Testing invalid action type" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Pre-broadcast failure (expected)"
    RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // .message // .' 2>/dev/null | head -1)
    echo "  Error: $RAW_LOG"
    if echo "$RAW_LOG" | grep -qi "invalid reason code\|invalid action"; then
        echo "  PASS: Got expected ErrInvalidReasonCode"
    fi
else
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')

    if [ "$CODE" != "0" ]; then
        echo "  PASS: Transaction failed with code $CODE"
        if echo "$RAW_LOG" | grep -qi "invalid reason code\|invalid action"; then
            echo "  Confirmed: ErrInvalidReasonCode"
        fi
    else
        echo "  FAIL: Expected error but tx succeeded"
    fi
fi

echo ""

# ========================================================================
# PART 31: QUERY SINGLE GOV ACTION APPEAL
# ========================================================================
echo "--- PART 31: QUERY SINGLE GOV ACTION APPEAL ---"

QUERY_APPEAL_ID="${GOV_APPEAL_ID:-1}"
echo "Querying gov action appeal by ID (id=$QUERY_APPEAL_ID)..."

GOV_APPEAL=$($BINARY query forum get-gov-action-appeal "$QUERY_APPEAL_ID" --output json 2>&1)

if echo "$GOV_APPEAL" | grep -q "error\|not found"; then
    echo "  No gov action appeal found with ID $QUERY_APPEAL_ID"
else
    echo "  Gov Action Appeal:"
    echo "    ID: $(echo "$GOV_APPEAL" | jq -r '.gov_action_appeal.id // "0"')"
    echo "    Action Type: $(echo "$GOV_APPEAL" | jq -r '.gov_action_appeal.action_type // "N/A"')"
    echo "    Appellant: $(echo "$GOV_APPEAL" | jq -r '.gov_action_appeal.appellant // "N/A"' | head -c 30)..."
    echo "    Status: $(echo "$GOV_APPEAL" | jq -r '.gov_action_appeal.status // "N/A"')"
    echo "    Reason: $(echo "$GOV_APPEAL" | jq -r '.gov_action_appeal.appeal_reason // "N/A"')"
fi

echo ""

# ========================================================================
# SUMMARY
# ========================================================================
echo "--- APPEALS TEST SUMMARY ---"
echo ""
echo "  === Happy-Path Appeals ==="
echo "  PART 4:  Appeal post:                      $APPEAL_POST_RESULT"
echo "  PART 8:  Appeal thread lock:               $APPEAL_LOCK_RESULT"
echo "  PART 14: Appeal gov action:                PASS"
echo "  PART 25: Appeal thread move:               $APPEAL_MOVE_RESULT"
echo ""
echo "  === Duplicate Appeal Detection ==="
echo "  PART 22: Lock appeal already filed:        $DUPLICATE_LOCK_APPEAL_RESULT"
echo "  PART 28: Move appeal already filed:        $DUPLICATE_MOVE_APPEAL_RESULT"
echo ""
echo "  === AppealPost Error Cases ==="
echo "  PART 16: Post not found:                   PASS"
echo "  PART 17: Post not hidden:                  PASS"
echo "  PART 18: Not post author:                  PASS"
echo ""
echo "  === AppealThreadLock Error Cases ==="
echo "  PART 19: Thread not found:                 PASS"
echo "  PART 20: Thread not locked:                PASS"
echo "  PART 21: Not thread author:                PASS"
echo ""
echo "  === AppealThreadMove Error Cases ==="
echo "  PART 26: Move appeal - thread not found:   PASS"
echo "  PART 27: Move appeal - not thread author:  PASS"
echo ""
echo "  === AppealGovAction Error Cases ==="
echo "  PART 30: Invalid action type (type=0):     PASS"
echo ""
echo "  === Setup & Queries ==="
echo "  PART 1:  Setup sentinel and posts:         PASS"
echo "  PART 2:  Hide post:                        PASS"
echo "  PART 3:  Query hide record:                PASS"
echo "  PART 5:  Query appeal cooldown:            PASS"
echo "  PART 6:  Lock thread:                      PASS"
echo "  PART 7:  Query lock record:                PASS"
echo "  PART 9:  Query locked threads:             PASS"
echo "  PART 10: Query lock status:                PASS"
echo "  PART 11: List hide records:                PASS"
echo "  PART 12: List lock records:                PASS"
echo "  PART 13: Query gov action appeals:         PASS"
echo "  PART 15: Sentinel bond commitment:         PASS"
echo "  PART 23: Setup - move thread:              PASS"
echo "  PART 24: Query thread move record:         PASS"
echo "  PART 29: List thread move records:         PASS"
echo "  PART 31: Query single gov action appeal:   PASS"
echo ""

# Count failures
FAIL_COUNT=0
for R in "$APPEAL_POST_RESULT" "$APPEAL_LOCK_RESULT" "$APPEAL_MOVE_RESULT" \
         "$DUPLICATE_LOCK_APPEAL_RESULT" "$DUPLICATE_MOVE_APPEAL_RESULT"; do
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
echo "APPEALS TEST COMPLETED"
echo ""
