#!/bin/bash

echo "--- TESTING: SENTINELS (BOND, UNBOND, MODERATION ACTIONS) ---"

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

echo "Sentinel 1: $SENTINEL1_ADDR"
echo "Sentinel 2: $SENTINEL2_ADDR"
echo "Poster 1:   $POSTER1_ADDR"
echo ""

# ========================================================================
# Result Tracking
# ========================================================================

BOOTSTRAP_RESULT="FAIL"
STATUS_CHECK_RESULT="FAIL"
BOND_RESULT="FAIL"
BOND_COMMITMENT_RESULT="FAIL"
LIST_ACTIVITIES_RESULT="FAIL"
CREATE_POST_RESULT="FAIL"
FLAG_POST_RESULT="FAIL"
QUERY_FLAGS_RESULT="FAIL"
HIDE_POST_RESULT="FAIL"
LOCK_THREAD_RESULT="FAIL"
QUERY_LOCKED_RESULT="FAIL"
UNLOCK_RESULT="FAIL"
DISMISS_FLAGS_RESULT="FAIL"
UNBOND_RESULT="FAIL"
GET_ACTIVITY_RESULT="FAIL"
BOND_NO_REP_RESULT="FAIL"
BOND_BELOW_MIN_RESULT="FAIL"
HIDE_NOT_SENTINEL_RESULT="FAIL"
LOCK_NOT_SENTINEL_RESULT="FAIL"
HIDE_ALREADY_HIDDEN_RESULT="FAIL"
LOCK_ALREADY_LOCKED_RESULT="FAIL"
UNLOCK_NOT_LOCKED_RESULT="FAIL"
UNBOND_NOT_SENTINEL_RESULT="FAIL"
DISMISS_NO_FLAGS_RESULT="FAIL"

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
# PART 0: BOOTSTRAP REPUTATION FOR SENTINEL ACCOUNTS
# ========================================================================
echo "--- PART 0: BOOTSTRAP REPUTATION ---"
echo "Sentinel operations require reputation tiers. Building reputation via EPIC interims..."
echo ""

# Sentinel1 needs tier 4 (500+ rep) for thread locking = 5 EPIC interims
bootstrap_reputation sentinel1 5
BOOTSTRAP1_OK=$?
echo ""

# Sentinel2 needs tier 3 (200+ rep) for bonding = 2 EPIC interims
bootstrap_reputation sentinel2 2
BOOTSTRAP2_OK=$?
echo ""

if [ "$BOOTSTRAP1_OK" -eq 0 ] && [ "$BOOTSTRAP2_OK" -eq 0 ]; then
    BOOTSTRAP_RESULT="PASS"
fi

# ========================================================================
# PART 1: CHECK SENTINEL STATUS
# ========================================================================
echo "--- PART 1: CHECK SENTINEL STATUS ---"

SENTINEL_STATUS=$($BINARY query forum sentinel-status $SENTINEL1_ADDR --output json 2>&1)

if echo "$SENTINEL_STATUS" | grep -q "error\|not found"; then
    echo "  Sentinel1 is not yet a sentinel"
    # Expected before bonding - this is informational, always passes
    STATUS_CHECK_RESULT="PASS"
else
    echo "  Sentinel1 Status:"
    echo "    Address: $(echo "$SENTINEL_STATUS" | jq -r '.address // "unknown"')"
    echo "    Bond: $(echo "$SENTINEL_STATUS" | jq -r '.current_bond // "0"')"
    echo "    Bond Status: $(echo "$SENTINEL_STATUS" | jq -r '.bond_status // "unknown"')"
    STATUS_CHECK_RESULT="PASS"
fi

echo ""

# ========================================================================
# PART 2: BOND SENTINEL
# ========================================================================
echo "--- PART 2: BOND SENTINEL ---"

BOND_AMOUNT="100000000"  # 100 DREAM

echo "Bonding $BOND_AMOUNT micro-DREAM as sentinel1..."

TX_RES=$($BINARY tx forum bond-sentinel \
    "$BOND_AMOUNT" \
    --from sentinel1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to submit transaction"
    echo "  $TX_RES"
else
    echo "  Transaction: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  Sentinel bonded successfully"

        # Verify sentinel status
        SENTINEL_STATUS=$($BINARY query forum sentinel-status $SENTINEL1_ADDR --output json 2>&1)
        echo "  New sentinel status:"
        echo "    Bond: $(echo "$SENTINEL_STATUS" | jq -r '.current_bond // "unknown"')"
        echo "    Bond Status: $(echo "$SENTINEL_STATUS" | jq -r '.bond_status // "unknown"')"
        BOND_RESULT="PASS"
    else
        echo "  Failed to bond sentinel"
    fi
fi

echo ""

# ========================================================================
# PART 3: QUERY SENTINEL BOND COMMITMENT
# ========================================================================
echo "--- PART 3: QUERY SENTINEL BOND COMMITMENT ---"

BOND_COMMITMENT=$($BINARY query forum sentinel-bond-commitment $SENTINEL1_ADDR --output json 2>&1)

if echo "$BOND_COMMITMENT" | grep -q "error"; then
    echo "  Failed to query bond commitment"
else
    echo "  Bond Commitment:"
    echo "    Current Bond: $(echo "$BOND_COMMITMENT" | jq -r '.current_bond // "unknown"')"
    echo "    Available Bond: $(echo "$BOND_COMMITMENT" | jq -r '.available_bond // "unknown"')"
    BOND_COMMITMENT_RESULT="PASS"
fi

echo ""

# ========================================================================
# PART 4: LIST SENTINEL ACTIVITIES
# ========================================================================
echo "--- PART 4: LIST SENTINEL ACTIVITIES ---"

ACTIVITIES=$($BINARY query forum list-sentinel-activity --output json 2>&1)

if echo "$ACTIVITIES" | grep -q "error"; then
    echo "  Failed to query sentinel activities"
else
    ACTIVITY_COUNT=$(echo "$ACTIVITIES" | jq -r '.sentinel_activity | length' 2>/dev/null || echo "0")
    echo "  Sentinel activities: $ACTIVITY_COUNT"

    if [ "$ACTIVITY_COUNT" -gt 0 ]; then
        echo "$ACTIVITIES" | jq -r '.sentinel_activity[0:3] | .[] | "    - \(.address | .[0:20])...: bond=\(.current_bond // "0")"' 2>/dev/null
        LIST_ACTIVITIES_RESULT="PASS"
    fi
fi

echo ""

# ========================================================================
# PART 5: CREATE POST FOR MODERATION TEST
# ========================================================================
echo "--- PART 5: CREATE POST FOR MODERATION TEST ---"

# Create a post that will be moderated
TEST_CONTENT="This is a test post for moderation testing at $(date)"

TX_RES=$($BINARY tx forum create-post \
    "${TEST_CATEGORY_ID:-1}" \
    "0" \
    "$TEST_CONTENT" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
MOD_POST_ID=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        MOD_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        echo "  Created post $MOD_POST_ID for moderation tests"
        CREATE_POST_RESULT="PASS"
    else
        echo "  Failed to create post"
    fi
else
    echo "  Failed to submit create post transaction"
fi

echo ""

# ========================================================================
# PART 6: FLAG POST
# ========================================================================
echo "--- PART 6: FLAG POST ---"

if [ -n "$MOD_POST_ID" ]; then
    FLAG_SUCCESS_COUNT=0
    # Flag from 3 members (weight=2 each, total=6) to exceed review threshold (5)
    for FLAGGER in sentinel1 sentinel2 poster2; do
        echo "  Flagging post $MOD_POST_ID from $FLAGGER..."

        TX_RES=$($BINARY tx forum flag-post \
            "$MOD_POST_ID" \
            "1" \
            "Testing flag functionality from $FLAGGER" \
            --from $FLAGGER \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)

        TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

        if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
            echo "    Failed to submit flag from $FLAGGER"
        else
            sleep 6
            TX_RESULT=$(wait_for_tx $TXHASH)

            if check_tx_success "$TX_RESULT"; then
                echo "    Post flagged by $FLAGGER"
                FLAG_SUCCESS_COUNT=$((FLAG_SUCCESS_COUNT + 1))
            else
                echo "    Failed to flag from $FLAGGER"
            fi
        fi
    done

    if [ "$FLAG_SUCCESS_COUNT" -ge 3 ]; then
        FLAG_POST_RESULT="PASS"
    fi
else
    echo "  No post available to flag"
fi

echo ""

# ========================================================================
# PART 7: QUERY POST FLAGS
# ========================================================================
echo "--- PART 7: QUERY POST FLAGS ---"

FLAGS=$($BINARY query forum list-post-flag --output json 2>&1)

if echo "$FLAGS" | grep -q "error"; then
    echo "  Failed to query post flags"
else
    FLAG_COUNT=$(echo "$FLAGS" | jq -r '.post_flag | length' 2>/dev/null || echo "0")
    echo "  Total flags: $FLAG_COUNT"

    if [ "$FLAG_COUNT" -gt 0 ]; then
        echo "$FLAGS" | jq -r '.post_flag[0:3] | .[] | "    - Post \(.post_id): Weight \(.total_weight)"' 2>/dev/null
        QUERY_FLAGS_RESULT="PASS"
    fi
fi

echo ""

# ========================================================================
# PART 8: HIDE POST (Sentinel Action)
# ========================================================================
echo "--- PART 8: HIDE POST (Sentinel Action) ---"

# Create another post to hide
TX_RES=$($BINARY tx forum create-post \
    "${TEST_CATEGORY_ID:-1}" \
    "0" \
    "This post will be hidden by a sentinel" \
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
        HIDE_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")

        if [ -n "$HIDE_POST_ID" ]; then
            echo "  Created post $HIDE_POST_ID, now hiding..."

            TX_RES=$($BINARY tx forum hide-post \
                "$HIDE_POST_ID" \
                "1" \
                "Violates community guidelines" \
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
                    echo "  Post hidden successfully"

                    # Verify hide record
                    HIDE_RECORD=$($BINARY query forum get-hide-record $HIDE_POST_ID --output json 2>&1)
                    if ! echo "$HIDE_RECORD" | grep -q "error\|not found"; then
                        echo "  Hide record created"
                        echo "    Sentinel: $(echo "$HIDE_RECORD" | jq -r '.hide_record.sentinel' | head -c 20)..."
                        HIDE_POST_RESULT="PASS"
                    fi
                else
                    echo "  Failed to hide post"
                fi
            fi
        fi
    fi
fi

echo ""

# ========================================================================
# PART 9: LOCK THREAD (Sentinel Action)
# ========================================================================
echo "--- PART 9: LOCK THREAD (Sentinel Action) ---"

# Create a thread to lock
TX_RES=$($BINARY tx forum create-post \
    "${TEST_CATEGORY_ID:-1}" \
    "0" \
    "This thread will be locked by a sentinel" \
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
        LOCK_THREAD_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")

        if [ -n "$LOCK_THREAD_ID" ]; then
            echo "  Created thread $LOCK_THREAD_ID, now locking..."

            TX_RES=$($BINARY tx forum lock-thread \
                "$LOCK_THREAD_ID" \
                "Discussion has become unproductive" \
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
                    echo "  Thread locked successfully"
                    LOCK_THREAD_RESULT="PASS"

                    # Export for later tests
                    echo "export LOCKED_THREAD_ID=$LOCK_THREAD_ID" >> "$SCRIPT_DIR/.test_env"
                else
                    echo "  Failed to lock thread"
                fi
            fi
        fi
    fi
fi

echo ""

# ========================================================================
# PART 10: QUERY LOCKED THREADS
# ========================================================================
echo "--- PART 10: QUERY LOCKED THREADS ---"

LOCKED=$($BINARY query forum locked-threads --output json 2>&1)

if echo "$LOCKED" | grep -q "error"; then
    echo "  Failed to query locked threads"
else
    # locked-threads returns a flat response with root_id, locked_by, locked_at
    LOCKED_ROOT_ID=$(echo "$LOCKED" | jq -r '.root_id // "0"' 2>/dev/null)
    if [ "$LOCKED_ROOT_ID" != "0" ] && [ "$LOCKED_ROOT_ID" != "null" ]; then
        echo "  Locked thread found: $LOCKED_ROOT_ID"
        echo "    Locked by: $(echo "$LOCKED" | jq -r '.locked_by // "unknown"')"
        QUERY_LOCKED_RESULT="PASS"
    else
        echo "  No locked threads"
    fi
fi

echo ""

# ========================================================================
# PART 11: UNLOCK THREAD
# ========================================================================
echo "--- PART 11: UNLOCK THREAD ---"

if [ -n "$LOCK_THREAD_ID" ]; then
    echo "Unlocking thread $LOCK_THREAD_ID..."

    TX_RES=$($BINARY tx forum unlock-thread \
        "$LOCK_THREAD_ID" \
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
            echo "  Thread unlocked successfully"
            UNLOCK_RESULT="PASS"
        else
            echo "  Failed to unlock thread"
        fi
    fi
else
    echo "  No locked thread available"
fi

echo ""

# ========================================================================
# PART 12: DISMISS FLAGS
# ========================================================================
echo "--- PART 12: DISMISS FLAGS ---"

if [ -n "$MOD_POST_ID" ]; then
    echo "Dismissing flags on post $MOD_POST_ID..."

    TX_RES=$($BINARY tx forum dismiss-flags \
        "$MOD_POST_ID" \
        "Flags reviewed and deemed invalid" \
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
            echo "  Flags dismissed successfully"
            DISMISS_FLAGS_RESULT="PASS"
        else
            echo "  Failed to dismiss flags"
        fi
    fi
else
    echo "  No flagged post available"
fi

echo ""

# ========================================================================
# PART 13: UNBOND SENTINEL
# ========================================================================
echo "--- PART 13: UNBOND SENTINEL ---"

# Bond sentinel2 first, then unbond
echo "Bonding sentinel2 to test unbonding..."

TX_RES=$($BINARY tx forum bond-sentinel \
    "50000000" \
    --from sentinel2 \
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
        echo "  Sentinel2 bonded, now unbonding..."

        TX_RES=$($BINARY tx forum unbond-sentinel \
            "50000000" \
            --from sentinel2 \
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
                echo "  Unbonding initiated"
                UNBOND_RESULT="PASS"
            else
                echo "  Failed to unbond"
            fi
        fi
    fi
fi

echo ""

# ========================================================================
# PART 14: GET SENTINEL ACTIVITY (Single Query)
# ========================================================================
echo "--- PART 14: GET SENTINEL ACTIVITY (Single Query) ---"

echo "Querying sentinel activity for sentinel1..."

SINGLE_ACTIVITY=$($BINARY query forum get-sentinel-activity $SENTINEL1_ADDR --output json 2>&1)

if echo "$SINGLE_ACTIVITY" | grep -q "error"; then
    echo "  Failed to query sentinel activity"
else
    SA_ADDRESS=$(echo "$SINGLE_ACTIVITY" | jq -r '.sentinel_activity.address // "unknown"')
    SA_BOND=$(echo "$SINGLE_ACTIVITY" | jq -r '.sentinel_activity.current_bond // "0"')
    SA_STATUS=$(echo "$SINGLE_ACTIVITY" | jq -r '.sentinel_activity.bond_status // "unknown"')
    SA_TOTAL_HIDES=$(echo "$SINGLE_ACTIVITY" | jq -r '.sentinel_activity.total_hides // "0"')
    SA_TOTAL_LOCKS=$(echo "$SINGLE_ACTIVITY" | jq -r '.sentinel_activity.total_locks // "0"')
    SA_PENDING=$(echo "$SINGLE_ACTIVITY" | jq -r '.sentinel_activity.pending_hide_count // "0"')

    echo "  Sentinel Activity:"
    echo "    Address: $(echo "$SA_ADDRESS" | head -c 20)..."
    echo "    Bond: $SA_BOND"
    echo "    Bond Status: $SA_STATUS"
    echo "    Total Hides: $SA_TOTAL_HIDES"
    echo "    Total Locks: $SA_TOTAL_LOCKS"
    echo "    Pending Hides: $SA_PENDING"

    if [ "$SA_BOND" != "0" ] && [ "$SA_BOND" != "unknown" ]; then
        GET_ACTIVITY_RESULT="PASS"
    fi
fi

echo ""

# ========================================================================
# PART 15: BOND WITHOUT REPUTATION (Negative Test)
# ========================================================================
echo "--- PART 15: BOND WITHOUT REPUTATION (Negative Test) ---"

echo "Attempting to bond as poster1 (no reputation)..."

TX_RES=$($BINARY tx forum bond-sentinel \
    "100000000" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Transaction rejected at submission (expected)"
    BOND_NO_REP_RESULT="PASS"
else
    echo "  Transaction submitted: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        echo "  Transaction failed as expected (code: $CODE)"
        echo "  Error: $RAW_LOG"
        BOND_NO_REP_RESULT="PASS"
    else
        echo "  ERROR: Transaction succeeded - poster1 with no rep was able to bond!"
        BOND_NO_REP_RESULT="FAIL"
    fi
fi

echo ""

# ========================================================================
# PART 16: BOND BELOW MINIMUM (Negative Test)
# ========================================================================
echo "--- PART 16: BOND BELOW MINIMUM (Negative Test) ---"

echo "Attempting to bond 500 udream (below minimum 1000)..."

TX_RES=$($BINARY tx forum bond-sentinel \
    "500" \
    --from sentinel1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Transaction rejected at submission (expected)"
    BOND_BELOW_MIN_RESULT="PASS"
else
    echo "  Transaction submitted: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        echo "  Transaction failed as expected (code: $CODE)"
        echo "  Error: $RAW_LOG"
        BOND_BELOW_MIN_RESULT="PASS"
    else
        echo "  ERROR: Transaction succeeded - bond below minimum was accepted!"
        BOND_BELOW_MIN_RESULT="FAIL"
    fi
fi

echo ""

# ========================================================================
# PART 17: HIDE POST WITHOUT BEING SENTINEL (Negative Test)
# ========================================================================
echo "--- PART 17: HIDE POST WITHOUT BEING SENTINEL (Negative Test) ---"

# Create a fresh post for this test
TX_RES=$($BINARY tx forum create-post \
    "${TEST_CATEGORY_ID:-1}" \
    "0" \
    "Post for negative hide test" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
NEG_POST_ID=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        NEG_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        echo "  Created post $NEG_POST_ID for negative test"
    fi
fi

if [ -n "$NEG_POST_ID" ]; then
    echo "  Attempting to hide post $NEG_POST_ID as poster1 (not a sentinel)..."

    TX_RES=$($BINARY tx forum hide-post \
        "$NEG_POST_ID" \
        "1" \
        "Unauthorized hide attempt" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Transaction rejected at submission (expected)"
        HIDE_NOT_SENTINEL_RESULT="PASS"
    else
        echo "  Transaction submitted: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')

        if [ "$CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            echo "  Transaction failed as expected (code: $CODE)"
            echo "  Error: $RAW_LOG"
            HIDE_NOT_SENTINEL_RESULT="PASS"
        else
            echo "  ERROR: Transaction succeeded - non-sentinel was able to hide post!"
            HIDE_NOT_SENTINEL_RESULT="FAIL"
        fi
    fi
else
    echo "  Could not create test post, skipping"
fi

echo ""

# ========================================================================
# PART 18: LOCK THREAD WITHOUT BEING SENTINEL (Negative Test)
# ========================================================================
echo "--- PART 18: LOCK THREAD WITHOUT BEING SENTINEL (Negative Test) ---"

if [ -n "$NEG_POST_ID" ]; then
    echo "  Attempting to lock thread $NEG_POST_ID as poster1 (not a sentinel)..."

    TX_RES=$($BINARY tx forum lock-thread \
        "$NEG_POST_ID" \
        "Unauthorized lock attempt" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Transaction rejected at submission (expected)"
        LOCK_NOT_SENTINEL_RESULT="PASS"
    else
        echo "  Transaction submitted: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')

        if [ "$CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            echo "  Transaction failed as expected (code: $CODE)"
            echo "  Error: $RAW_LOG"
            LOCK_NOT_SENTINEL_RESULT="PASS"
        else
            echo "  ERROR: Transaction succeeded - non-sentinel was able to lock thread!"
            LOCK_NOT_SENTINEL_RESULT="FAIL"
        fi
    fi
else
    echo "  No test post available, skipping"
fi

echo ""

# ========================================================================
# PART 19: HIDE ALREADY-HIDDEN POST (Negative Test)
# ========================================================================
echo "--- PART 19: HIDE ALREADY-HIDDEN POST (Negative Test) ---"

if [ -n "$HIDE_POST_ID" ]; then
    echo "  Attempting to hide post $HIDE_POST_ID again (already hidden in Part 8)..."

    TX_RES=$($BINARY tx forum hide-post \
        "$HIDE_POST_ID" \
        "1" \
        "Double hide attempt" \
        --from sentinel1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Transaction rejected at submission (expected)"
        HIDE_ALREADY_HIDDEN_RESULT="PASS"
    else
        echo "  Transaction submitted: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')

        if [ "$CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            echo "  Transaction failed as expected (code: $CODE)"
            echo "  Error: $RAW_LOG"
            HIDE_ALREADY_HIDDEN_RESULT="PASS"
        else
            echo "  ERROR: Transaction succeeded - already-hidden post was hidden again!"
            HIDE_ALREADY_HIDDEN_RESULT="FAIL"
        fi
    fi
else
    echo "  No hidden post from Part 8 available, skipping"
fi

echo ""

# ========================================================================
# PART 20: LOCK ALREADY-LOCKED THREAD (Negative Test)
# ========================================================================
echo "--- PART 20: LOCK ALREADY-LOCKED THREAD (Negative Test) ---"

# Create a new thread and lock it, then try locking again
TX_RES=$($BINARY tx forum create-post \
    "${TEST_CATEGORY_ID:-1}" \
    "0" \
    "Thread for double-lock test" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
DOUBLE_LOCK_ID=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        DOUBLE_LOCK_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        echo "  Created thread $DOUBLE_LOCK_ID"
    fi
fi

if [ -n "$DOUBLE_LOCK_ID" ]; then
    # Lock it first
    echo "  Locking thread $DOUBLE_LOCK_ID..."
    TX_RES=$($BINARY tx forum lock-thread \
        "$DOUBLE_LOCK_ID" \
        "First lock" \
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
            echo "  Thread locked, now attempting second lock..."

            TX_RES=$($BINARY tx forum lock-thread \
                "$DOUBLE_LOCK_ID" \
                "Double lock attempt" \
                --from sentinel1 \
                --chain-id $CHAIN_ID \
                --keyring-backend test \
                --fees 5000uspark \
                -y \
                --output json 2>&1)

            TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

            if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
                echo "  Transaction rejected at submission (expected)"
                LOCK_ALREADY_LOCKED_RESULT="PASS"
            else
                echo "  Transaction submitted: $TXHASH"
                sleep 6
                TX_RESULT=$(wait_for_tx $TXHASH)
                CODE=$(echo "$TX_RESULT" | jq -r '.code')

                if [ "$CODE" != "0" ]; then
                    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
                    echo "  Transaction failed as expected (code: $CODE)"
                    echo "  Error: $RAW_LOG"
                    LOCK_ALREADY_LOCKED_RESULT="PASS"
                else
                    echo "  ERROR: Transaction succeeded - already-locked thread was locked again!"
                    LOCK_ALREADY_LOCKED_RESULT="FAIL"
                fi
            fi
        else
            echo "  Failed to lock thread for double-lock test"
        fi
    fi
else
    echo "  Could not create test thread, skipping"
fi

echo ""

# ========================================================================
# PART 21: UNLOCK THREAD NOT LOCKED (Negative Test)
# ========================================================================
echo "--- PART 21: UNLOCK THREAD NOT LOCKED (Negative Test) ---"

# Use the thread from Part 9 which was unlocked in Part 11
if [ -n "$LOCK_THREAD_ID" ]; then
    echo "  Attempting to unlock thread $LOCK_THREAD_ID (already unlocked in Part 11)..."

    TX_RES=$($BINARY tx forum unlock-thread \
        "$LOCK_THREAD_ID" \
        --from sentinel1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Transaction rejected at submission (expected)"
        UNLOCK_NOT_LOCKED_RESULT="PASS"
    else
        echo "  Transaction submitted: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')

        if [ "$CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            echo "  Transaction failed as expected (code: $CODE)"
            echo "  Error: $RAW_LOG"
            UNLOCK_NOT_LOCKED_RESULT="PASS"
        else
            echo "  ERROR: Transaction succeeded - unlocked thread was unlocked again!"
            UNLOCK_NOT_LOCKED_RESULT="FAIL"
        fi
    fi
else
    echo "  No thread available for unlock test, skipping"
fi

echo ""

# ========================================================================
# PART 22: UNBOND WHEN NOT SENTINEL (Negative Test)
# ========================================================================
echo "--- PART 22: UNBOND WHEN NOT SENTINEL (Negative Test) ---"

echo "Attempting to unbond as poster1 (not a sentinel)..."

TX_RES=$($BINARY tx forum unbond-sentinel \
    "50000000" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Transaction rejected at submission (expected)"
    UNBOND_NOT_SENTINEL_RESULT="PASS"
else
    echo "  Transaction submitted: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        echo "  Transaction failed as expected (code: $CODE)"
        echo "  Error: $RAW_LOG"
        UNBOND_NOT_SENTINEL_RESULT="PASS"
    else
        echo "  ERROR: Transaction succeeded - non-sentinel was able to unbond!"
        UNBOND_NOT_SENTINEL_RESULT="FAIL"
    fi
fi

echo ""

# ========================================================================
# PART 23: DISMISS FLAGS ON UNFLAGGED POST (Negative Test)
# ========================================================================
echo "--- PART 23: DISMISS FLAGS ON UNFLAGGED POST (Negative Test) ---"

if [ -n "$NEG_POST_ID" ]; then
    echo "  Attempting to dismiss flags on post $NEG_POST_ID (no flags)..."

    TX_RES=$($BINARY tx forum dismiss-flags \
        "$NEG_POST_ID" \
        "No flags to dismiss" \
        --from sentinel1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Transaction rejected at submission (expected)"
        DISMISS_NO_FLAGS_RESULT="PASS"
    else
        echo "  Transaction submitted: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        CODE=$(echo "$TX_RESULT" | jq -r '.code')

        if [ "$CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            echo "  Transaction failed as expected (code: $CODE)"
            echo "  Error: $RAW_LOG"
            DISMISS_NO_FLAGS_RESULT="PASS"
        else
            echo "  ERROR: Transaction succeeded - dismissed flags on unflagged post!"
            DISMISS_NO_FLAGS_RESULT="FAIL"
        fi
    fi
else
    echo "  No test post available, skipping"
fi

echo ""

# ========================================================================
# SUMMARY
# ========================================================================
echo "--- SENTINEL TEST SUMMARY ---"
echo ""
echo "  Happy Path:"
echo "    Bootstrap reputation:       $BOOTSTRAP_RESULT"
echo "    Check sentinel status:      $STATUS_CHECK_RESULT"
echo "    Bond sentinel:              $BOND_RESULT"
echo "    Query bond commitment:      $BOND_COMMITMENT_RESULT"
echo "    List sentinel activities:   $LIST_ACTIVITIES_RESULT"
echo "    Get sentinel activity:      $GET_ACTIVITY_RESULT"
echo "    Create post for mod:        $CREATE_POST_RESULT"
echo "    Flag post:                  $FLAG_POST_RESULT"
echo "    Query post flags:           $QUERY_FLAGS_RESULT"
echo "    Hide post:                  $HIDE_POST_RESULT"
echo "    Lock thread:                $LOCK_THREAD_RESULT"
echo "    Query locked threads:       $QUERY_LOCKED_RESULT"
echo "    Unlock thread:              $UNLOCK_RESULT"
echo "    Dismiss flags:              $DISMISS_FLAGS_RESULT"
echo "    Unbond sentinel:            $UNBOND_RESULT"
echo ""
echo "  Negative Tests:"
echo "    Bond without reputation:    $BOND_NO_REP_RESULT"
echo "    Bond below minimum:         $BOND_BELOW_MIN_RESULT"
echo "    Hide without sentinel:      $HIDE_NOT_SENTINEL_RESULT"
echo "    Lock without sentinel:      $LOCK_NOT_SENTINEL_RESULT"
echo "    Hide already-hidden:        $HIDE_ALREADY_HIDDEN_RESULT"
echo "    Lock already-locked:        $LOCK_ALREADY_LOCKED_RESULT"
echo "    Unlock not locked:          $UNLOCK_NOT_LOCKED_RESULT"
echo "    Unbond not sentinel:        $UNBOND_NOT_SENTINEL_RESULT"
echo "    Dismiss no flags:           $DISMISS_NO_FLAGS_RESULT"
echo ""

# Count failures
FAIL_COUNT=0
for RESULT in "$BOOTSTRAP_RESULT" "$STATUS_CHECK_RESULT" "$BOND_RESULT" \
              "$BOND_COMMITMENT_RESULT" "$LIST_ACTIVITIES_RESULT" "$GET_ACTIVITY_RESULT" \
              "$CREATE_POST_RESULT" "$FLAG_POST_RESULT" "$QUERY_FLAGS_RESULT" \
              "$HIDE_POST_RESULT" "$LOCK_THREAD_RESULT" "$QUERY_LOCKED_RESULT" \
              "$UNLOCK_RESULT" "$DISMISS_FLAGS_RESULT" "$UNBOND_RESULT" \
              "$BOND_NO_REP_RESULT" "$BOND_BELOW_MIN_RESULT" \
              "$HIDE_NOT_SENTINEL_RESULT" "$LOCK_NOT_SENTINEL_RESULT" \
              "$HIDE_ALREADY_HIDDEN_RESULT" "$LOCK_ALREADY_LOCKED_RESULT" \
              "$UNLOCK_NOT_LOCKED_RESULT" "$UNBOND_NOT_SENTINEL_RESULT" \
              "$DISMISS_NO_FLAGS_RESULT"; do
    if [ "$RESULT" == "FAIL" ]; then
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
done

if [ "$FAIL_COUNT" -gt 0 ]; then
    echo "  FAILURES: $FAIL_COUNT test(s) failed"
else
    echo "  ALL TESTS PASSED (24 tests)"
fi

echo ""
echo "SENTINEL TEST COMPLETED"
echo ""
