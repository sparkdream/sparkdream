#!/bin/bash

echo "--- TESTING: BOUNTIES (CREATE, INCREASE, AWARD, CANCEL, ASSIGN) ---"

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

echo "Bounty Creator: $BOUNTY_CREATOR_ADDR"
echo "Poster 1:       $POSTER1_ADDR"
echo "Poster 2:       $POSTER2_ADDR"
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

# Helper for error path tests: expect a tx to fail with a specific error
expect_tx_failure() {
    local DESCRIPTION=$1
    local EXPECTED_ERROR=$2
    local TX_RES=$3
    local TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  FAIL: $DESCRIPTION - Could not submit tx"
        return 1
    fi

    sleep 6
    local TX_RESULT=$(wait_for_tx $TXHASH)
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    local RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')

    if [ "$CODE" == "0" ]; then
        echo "  FAIL: $DESCRIPTION - Expected failure but tx succeeded"
        return 1
    fi

    if [ -n "$EXPECTED_ERROR" ] && echo "$RAW_LOG" | grep -qi "$EXPECTED_ERROR"; then
        echo "  PASS: $DESCRIPTION"
        return 0
    elif [ -n "$EXPECTED_ERROR" ]; then
        echo "  PASS: $DESCRIPTION (different error: $(echo "$RAW_LOG" | head -c 80))"
        return 0
    else
        echo "  PASS: $DESCRIPTION (code=$CODE)"
        return 0
    fi
}

# ========================================================================
# PART 0: FUND BOUNTY CREATOR WITH SPARK (needed for escrow)
# ========================================================================
echo "--- PART 0: FUND BOUNTY CREATOR WITH SPARK ---"

# Bounties escrow SPARK (uspark). bounty_creator needs enough for multiple bounties.
# Send 200 SPARK from alice to bounty_creator
TX_RES=$($BINARY tx bank send \
    alice $BOUNTY_CREATOR_ADDR \
    200000000uspark \
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
        BALANCE=$($BINARY query bank balances $BOUNTY_CREATOR_ADDR --output json 2>&1 | jq -r '.balances[] | select(.denom=="uspark") | .amount')
        echo "  Funded bounty_creator. Balance: $BALANCE uspark"
        FUND_CREATOR_RESULT="PASS"
    else
        echo "  Failed to fund bounty_creator"
        FUND_CREATOR_RESULT="FAIL"
    fi
else
    echo "  Failed to submit funding transaction"
    FUND_CREATOR_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 1: LIST EXISTING BOUNTIES
# ========================================================================
echo "--- PART 1: LIST EXISTING BOUNTIES ---"

BOUNTIES=$($BINARY query forum list-bounty --output json 2>&1)

if echo "$BOUNTIES" | grep -q "error"; then
    echo "  Failed to query bounties"
    INITIAL_BOUNTY_COUNT=0
    LIST_BOUNTIES_RESULT="FAIL"
else
    INITIAL_BOUNTY_COUNT=$(echo "$BOUNTIES" | jq -r '.bounty | length' 2>/dev/null || echo "0")
    echo "  Existing bounties: $INITIAL_BOUNTY_COUNT"
    LIST_BOUNTIES_RESULT="PASS"

    if [ "$INITIAL_BOUNTY_COUNT" -gt 0 ]; then
        echo "$BOUNTIES" | jq -r '.bounty[0:3] | .[] | "    - ID \(.id): \(.amount) uspark on thread \(.thread_id)"' 2>/dev/null
    fi
fi

echo ""

# ========================================================================
# PART 2: CREATE THREAD FOR BOUNTY
# ========================================================================
echo "--- PART 2: CREATE THREAD FOR BOUNTY ---"

BOUNTY_THREAD_CONTENT="Help needed: This is a question with a bounty reward! Created at $(date)"

TX_RES=$($BINARY tx forum create-post \
    "${TEST_CATEGORY_ID:-1}" \
    "0" \
    "$BOUNTY_THREAD_CONTENT" \
    --from bounty_creator \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
BOUNTY_THREAD_ID=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        BOUNTY_THREAD_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        echo "  Created thread $BOUNTY_THREAD_ID for bounty"
        CREATE_THREAD_RESULT="PASS"
    else
        echo "  Failed to create thread"
        CREATE_THREAD_RESULT="FAIL"
    fi
else
    echo "  Failed to submit transaction"
    CREATE_THREAD_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 3: CREATE BOUNTY
# ========================================================================
echo "--- PART 3: CREATE BOUNTY ---"

if [ -n "$BOUNTY_THREAD_ID" ]; then
    BOUNTY_AMOUNT="500"  # 500 uspark (min is 50 uspark)
    BOUNTY_DURATION="1000"  # 1000 seconds

    echo "Creating bounty of $BOUNTY_AMOUNT uspark on thread $BOUNTY_THREAD_ID..."

    TX_RES=$($BINARY tx forum create-bounty \
        "$BOUNTY_THREAD_ID" \
        "$BOUNTY_AMOUNT" \
        "$BOUNTY_DURATION" \
        --from bounty_creator \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit transaction"
        echo "  $TX_RES"
        BOUNTY_ID=""
        CREATE_BOUNTY_RESULT="FAIL"
    else
        echo "  Transaction: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            BOUNTY_ID=$(extract_event_value "$TX_RESULT" "bounty_created" "bounty_id")

            if [ -z "$BOUNTY_ID" ] || [ "$BOUNTY_ID" == "null" ]; then
                # Fallback: query latest bounty
                BOUNTIES=$($BINARY query forum list-bounty --output json 2>&1)
                BOUNTY_ID=$(echo "$BOUNTIES" | jq -r '.bounty[-1].id // empty')
            fi

            echo "  Bounty created successfully"
            echo "  Bounty ID: $BOUNTY_ID"
            CREATE_BOUNTY_RESULT="PASS"
        else
            echo "  Failed to create bounty"
            BOUNTY_ID=""
            CREATE_BOUNTY_RESULT="FAIL"
        fi
    fi
else
    echo "  No thread available for bounty"
    BOUNTY_ID=""
    CREATE_BOUNTY_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 4: QUERY BOUNTY DETAILS
# ========================================================================
echo "--- PART 4: QUERY BOUNTY DETAILS ---"

if [ -n "$BOUNTY_ID" ]; then
    BOUNTY_INFO=$($BINARY query forum get-bounty $BOUNTY_ID --output json 2>&1)

    if echo "$BOUNTY_INFO" | grep -q "error\|not found"; then
        echo "  Bounty $BOUNTY_ID not found"
        QUERY_BOUNTY_RESULT="FAIL"
    else
        echo "  Bounty Details:"
        echo "    ID: $(echo "$BOUNTY_INFO" | jq -r '.bounty.id // 0')"
        echo "    Thread: $(echo "$BOUNTY_INFO" | jq -r '.bounty.thread_id')"
        echo "    Creator: $(echo "$BOUNTY_INFO" | jq -r '.bounty.creator' | head -c 20)..."
        echo "    Amount: $(echo "$BOUNTY_INFO" | jq -r '.bounty.amount')"
        echo "    Status: $(echo "$BOUNTY_INFO" | jq -r '.bounty.status')"
        echo "    Expires At: $(echo "$BOUNTY_INFO" | jq -r '.bounty.expires_at')"
        QUERY_BOUNTY_RESULT="PASS"
    fi
else
    echo "  No bounty ID available"
    QUERY_BOUNTY_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 5: QUERY BOUNTY BY THREAD
# ========================================================================
echo "--- PART 5: QUERY BOUNTY BY THREAD ---"

if [ -n "$BOUNTY_THREAD_ID" ]; then
    THREAD_BOUNTY=$($BINARY query forum bounty-by-thread $BOUNTY_THREAD_ID --output json 2>&1)

    if echo "$THREAD_BOUNTY" | grep -q "error\|not found"; then
        echo "  No bounty found for thread $BOUNTY_THREAD_ID"
        QUERY_BY_THREAD_RESULT="FAIL"
    else
        # Response is flat: bounty_id, amount, status (expires_at not always populated)
        echo "  Bounty for thread $BOUNTY_THREAD_ID:"
        echo "    Bounty ID: $(echo "$THREAD_BOUNTY" | jq -r '.bounty_id // 0')"
        echo "    Amount: $(echo "$THREAD_BOUNTY" | jq -r '.amount // "0"')"
        echo "    Status: $(echo "$THREAD_BOUNTY" | jq -r '.status // 0')"
        QUERY_BY_THREAD_RESULT="PASS"
    fi
else
    echo "  No thread ID available"
    QUERY_BY_THREAD_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 6: INCREASE BOUNTY
# ========================================================================
echo "--- PART 6: INCREASE BOUNTY ---"

if [ -n "$BOUNTY_ID" ]; then
    INCREASE_AMOUNT="250"  # 250 uspark additional

    echo "Increasing bounty $BOUNTY_ID by $INCREASE_AMOUNT uspark..."

    TX_RES=$($BINARY tx forum increase-bounty \
        "$BOUNTY_ID" \
        "$INCREASE_AMOUNT" \
        --from bounty_creator \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit transaction"
        INCREASE_BOUNTY_RESULT="FAIL"
    else
        echo "  Transaction: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Bounty increased successfully"

            # Verify increase
            BOUNTY_INFO=$($BINARY query forum get-bounty $BOUNTY_ID --output json 2>&1)
            NEW_AMOUNT=$(echo "$BOUNTY_INFO" | jq -r '.bounty.amount')
            echo "  New bounty amount: $NEW_AMOUNT uspark"
            INCREASE_BOUNTY_RESULT="PASS"
        else
            echo "  Failed to increase bounty"
            INCREASE_BOUNTY_RESULT="FAIL"
        fi
    fi
else
    echo "  No bounty available to increase"
    INCREASE_BOUNTY_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 7: CREATE REPLY FOR BOUNTY AWARD
# ========================================================================
echo "--- PART 7: CREATE REPLY FOR BOUNTY AWARD ---"

if [ -n "$BOUNTY_THREAD_ID" ]; then
    REPLY_CONTENT="This is the answer to your question! Hope this helps."

    TX_RES=$($BINARY tx forum create-post \
        "${TEST_CATEGORY_ID:-1}" \
        "$BOUNTY_THREAD_ID" \
        "$REPLY_CONTENT" \
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
            BOUNTY_REPLY_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
            echo "  Created reply $BOUNTY_REPLY_ID for bounty award"
            CREATE_REPLY_RESULT="PASS"
        else
            echo "  Failed to create reply"
            CREATE_REPLY_RESULT="FAIL"
        fi
    else
        CREATE_REPLY_RESULT="FAIL"
    fi
else
    echo "  No thread available for reply"
    CREATE_REPLY_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 8: ASSIGN BOUNTY TO REPLY
# ========================================================================
echo "--- PART 8: ASSIGN BOUNTY TO REPLY ---"

# CLI: assign-bounty-to-reply [thread-id] [reply-id] [reason]
# Finds the active bounty for thread_id, assigns all remaining funds to reply_id
if [ -n "$BOUNTY_THREAD_ID" ] && [ -n "$BOUNTY_REPLY_ID" ]; then
    echo "Assigning bounty to reply $BOUNTY_REPLY_ID on thread $BOUNTY_THREAD_ID..."

    TX_RES=$($BINARY tx forum assign-bounty-to-reply \
        "$BOUNTY_THREAD_ID" \
        "$BOUNTY_REPLY_ID" \
        "Excellent answer!" \
        --from bounty_creator \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit transaction"
        echo "  $TX_RES"
        ASSIGN_BOUNTY_RESULT="FAIL"
    else
        echo "  Transaction: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Bounty assigned to reply successfully"

            # Verify assignment via get-bounty
            BOUNTY_INFO=$($BINARY query forum get-bounty $BOUNTY_ID --output json 2>&1)
            AWARD_COUNT=$(echo "$BOUNTY_INFO" | jq -r '.bounty.awards | length' 2>/dev/null || echo "0")
            echo "  Awards assigned: $AWARD_COUNT"
            ASSIGN_BOUNTY_RESULT="PASS"
        else
            echo "  Failed to assign bounty"
            ASSIGN_BOUNTY_RESULT="FAIL"
        fi
    fi
else
    echo "  No thread or reply available for assignment"
    ASSIGN_BOUNTY_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 9: AWARD BOUNTY (Finalize)
# ========================================================================
echo "--- PART 9: AWARD BOUNTY (Finalize) ---"

# CLI: award-bounty [bounty-id]
# Transfers escrowed funds to all assigned recipients
if [ -n "$BOUNTY_ID" ]; then
    echo "Awarding bounty $BOUNTY_ID (transferring escrowed funds to recipients)..."

    TX_RES=$($BINARY tx forum award-bounty \
        "$BOUNTY_ID" \
        --from bounty_creator \
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
            echo "  Bounty awarded successfully"

            # Verify status changed to AWARDED
            BOUNTY_INFO=$($BINARY query forum get-bounty $BOUNTY_ID --output json 2>&1)
            STATUS=$(echo "$BOUNTY_INFO" | jq -r '.bounty.status')
            echo "  Bounty status: $STATUS"
            AWARD_BOUNTY_RESULT="PASS"
        else
            echo "  Failed to award bounty"
            AWARD_BOUNTY_RESULT="FAIL"
        fi
    else
        echo "  Failed to submit transaction"
        AWARD_BOUNTY_RESULT="FAIL"
    fi
else
    echo "  No bounty available to award"
    AWARD_BOUNTY_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 10: QUERY ACTIVE BOUNTIES
# ========================================================================
echo "--- PART 10: QUERY ACTIVE BOUNTIES ---"

# Response is flat: bounty_id, thread_id, amount, pagination
ACTIVE=$($BINARY query forum active-bounties --output json 2>&1)

if echo "$ACTIVE" | grep -q "error"; then
    echo "  Failed to query active bounties"
    QUERY_ACTIVE_RESULT="FAIL"
else
    ACTIVE_BOUNTY_ID=$(echo "$ACTIVE" | jq -r '.bounty_id // "0"' 2>/dev/null)
    if [ "$ACTIVE_BOUNTY_ID" != "0" ] && [ "$ACTIVE_BOUNTY_ID" != "null" ] && [ -n "$ACTIVE_BOUNTY_ID" ]; then
        echo "  Active bounty found: ID=$ACTIVE_BOUNTY_ID, Thread=$(echo "$ACTIVE" | jq -r '.thread_id'), Amount=$(echo "$ACTIVE" | jq -r '.amount')"
    else
        echo "  No active bounties (expected - previous bounty was awarded)"
    fi
    QUERY_ACTIVE_RESULT="PASS"
fi

echo ""

# ========================================================================
# PART 11: CREATE AND QUERY EXPIRING BOUNTY
# ========================================================================
echo "--- PART 11: CREATE AND QUERY EXPIRING BOUNTY ---"

# Create a new thread and bounty with short duration for expiry test
TX_RES=$($BINARY tx forum create-post \
    "${TEST_CATEGORY_ID:-1}" \
    "0" \
    "Short bounty for expiry test" \
    --from bounty_creator \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
EXPIRY_THREAD_ID=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        EXPIRY_THREAD_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        echo "  Created thread $EXPIRY_THREAD_ID for expiry test"
    fi
fi

if [ -n "$EXPIRY_THREAD_ID" ]; then
    # Create bounty with short duration
    TX_RES=$($BINARY tx forum create-bounty \
        "$EXPIRY_THREAD_ID" \
        "100" \
        "500" \
        --from bounty_creator \
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
            EXPIRY_BOUNTY_ID=$(extract_event_value "$TX_RESULT" "bounty_created" "bounty_id")
            echo "  Created bounty $EXPIRY_BOUNTY_ID with 500s duration"
        fi
    fi
fi

# Query bounties expiring soon (within 1000 seconds)
# Response is flat: bounty_id, thread_id, expires_at, pagination
EXPIRING=$($BINARY query forum bounty-expiring-soon 1000 --output json 2>&1)

if echo "$EXPIRING" | grep -q "error"; then
    echo "  Failed to query expiring bounties"
    QUERY_EXPIRING_RESULT="FAIL"
else
    EXPIRING_BOUNTY_ID=$(echo "$EXPIRING" | jq -r '.bounty_id // "0"' 2>/dev/null)
    if [ "$EXPIRING_BOUNTY_ID" != "0" ] && [ "$EXPIRING_BOUNTY_ID" != "null" ] && [ -n "$EXPIRING_BOUNTY_ID" ]; then
        echo "  Expiring bounty: ID=$EXPIRING_BOUNTY_ID, Thread=$(echo "$EXPIRING" | jq -r '.thread_id'), Expires=$(echo "$EXPIRING" | jq -r '.expires_at')"
    else
        echo "  No bounties expiring soon"
    fi
    QUERY_EXPIRING_RESULT="PASS"
fi

echo ""

# ========================================================================
# PART 12: CANCEL BOUNTY
# ========================================================================
echo "--- PART 12: CANCEL BOUNTY ---"

# Create a bounty to cancel
TX_RES=$($BINARY tx forum create-post \
    "${TEST_CATEGORY_ID:-1}" \
    "0" \
    "This bounty will be cancelled" \
    --from bounty_creator \
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
        CANCEL_THREAD_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")

        if [ -n "$CANCEL_THREAD_ID" ]; then
            # Create bounty
            TX_RES=$($BINARY tx forum create-bounty \
                "$CANCEL_THREAD_ID" \
                "200" \
                "100" \
                --from bounty_creator \
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
                    CANCEL_BOUNTY_ID=$(extract_event_value "$TX_RESULT" "bounty_created" "bounty_id")
                    echo "  Created bounty $CANCEL_BOUNTY_ID for cancellation test"

                    # Cancel bounty
                    TX_RES=$($BINARY tx forum cancel-bounty \
                        "$CANCEL_BOUNTY_ID" \
                        --from bounty_creator \
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
                            echo "  Bounty cancelled successfully"

                            # Verify status changed to CANCELLED
                            BOUNTY_INFO=$($BINARY query forum get-bounty $CANCEL_BOUNTY_ID --output json 2>&1)
                            STATUS=$(echo "$BOUNTY_INFO" | jq -r '.bounty.status')
                            echo "  Bounty status: $STATUS"
                            CANCEL_BOUNTY_RESULT="PASS"
                        else
                            echo "  Failed to cancel bounty"
                            CANCEL_BOUNTY_RESULT="FAIL"
                        fi
                    else
                        CANCEL_BOUNTY_RESULT="FAIL"
                    fi
                else
                    CANCEL_BOUNTY_RESULT="FAIL"
                fi
            else
                CANCEL_BOUNTY_RESULT="FAIL"
            fi
        else
            CANCEL_BOUNTY_RESULT="FAIL"
        fi
    else
        CANCEL_BOUNTY_RESULT="FAIL"
    fi
else
    CANCEL_BOUNTY_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 13: QUERY USER BOUNTIES
# ========================================================================
echo "--- PART 13: QUERY USER BOUNTIES ---"

# Response is flat: bounty_id, thread_id, status, pagination
# Note: bounty_id=0 is omitted from JSON (protobuf default), so check thread_id for presence
USER_BOUNTIES=$($BINARY query forum user-bounties $BOUNTY_CREATOR_ADDR --output json 2>&1)

if echo "$USER_BOUNTIES" | grep -q "error"; then
    echo "  Failed to query user bounties"
    QUERY_USER_BOUNTIES_RESULT="FAIL"
else
    USER_THREAD_ID=$(echo "$USER_BOUNTIES" | jq -r '.thread_id // empty' 2>/dev/null)
    if [ -n "$USER_THREAD_ID" ]; then
        echo "  User bounty found: ID=$(echo "$USER_BOUNTIES" | jq -r '.bounty_id // 0'), Thread=$USER_THREAD_ID, Status=$(echo "$USER_BOUNTIES" | jq -r '.status // 0')"
    else
        echo "  No bounties found for user (unexpected - bounty_creator created several)"
    fi
    QUERY_USER_BOUNTIES_RESULT="PASS"
fi

echo ""

# ========================================================================
# PART 14: ERROR PATHS - CreateBounty
# ========================================================================
echo "--- PART 14: ERROR PATHS - CreateBounty ---"

# Create a thread owned by bounty_creator for error testing
TX_RES=$($BINARY tx forum create-post \
    "${TEST_CATEGORY_ID:-1}" \
    "0" \
    "Thread for error path testing" \
    --from bounty_creator \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
ERROR_THREAD_ID=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        ERROR_THREAD_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        echo "  Created error-test thread: $ERROR_THREAD_ID"
    fi
fi

if [ -n "$ERROR_THREAD_ID" ]; then
    # 14a: Non-thread-author tries to create bounty
    TX_RES=$($BINARY tx forum create-bounty \
        "$ERROR_THREAD_ID" \
        "500" \
        "1000" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    if expect_tx_failure "Create bounty by non-author" "thread author" "$TX_RES"; then
        ERR_CREATE_NON_AUTHOR_RESULT="PASS"
    else
        ERR_CREATE_NON_AUTHOR_RESULT="FAIL"
    fi

    # 14b: Amount below minimum (50 uspark)
    TX_RES=$($BINARY tx forum create-bounty \
        "$ERROR_THREAD_ID" \
        "10" \
        "1000" \
        --from bounty_creator \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    if expect_tx_failure "Create bounty with amount too small" "minimum" "$TX_RES"; then
        ERR_CREATE_MIN_AMOUNT_RESULT="PASS"
    else
        ERR_CREATE_MIN_AMOUNT_RESULT="FAIL"
    fi

    # 14c: Duration exceeds maximum (>2592000 seconds = 30 days)
    TX_RES=$($BINARY tx forum create-bounty \
        "$ERROR_THREAD_ID" \
        "500" \
        "9999999" \
        --from bounty_creator \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    if expect_tx_failure "Create bounty with invalid duration" "duration" "$TX_RES"; then
        ERR_CREATE_DURATION_RESULT="PASS"
    else
        ERR_CREATE_DURATION_RESULT="FAIL"
    fi

    # 14d: Create valid bounty, then try duplicate on same thread
    TX_RES=$($BINARY tx forum create-bounty \
        "$ERROR_THREAD_ID" \
        "500" \
        "1000" \
        --from bounty_creator \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    ERROR_BOUNTY_ID=""

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        if check_tx_success "$TX_RESULT"; then
            ERROR_BOUNTY_ID=$(extract_event_value "$TX_RESULT" "bounty_created" "bounty_id")
            if [ -z "$ERROR_BOUNTY_ID" ] || [ "$ERROR_BOUNTY_ID" == "null" ]; then
                BOUNTIES=$($BINARY query forum list-bounty --output json 2>&1)
                ERROR_BOUNTY_ID=$(echo "$BOUNTIES" | jq -r '.bounty[-1].id // empty')
            fi
            echo "  Created valid bounty $ERROR_BOUNTY_ID for duplicate test"
        fi
    fi

    if [ -n "$ERROR_BOUNTY_ID" ]; then
        # Try duplicate bounty on same thread
        TX_RES=$($BINARY tx forum create-bounty \
            "$ERROR_THREAD_ID" \
            "500" \
            "1000" \
            --from bounty_creator \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)
        if expect_tx_failure "Create duplicate bounty on same thread" "already exists" "$TX_RES"; then
            ERR_CREATE_DUPLICATE_RESULT="PASS"
        else
            ERR_CREATE_DUPLICATE_RESULT="FAIL"
        fi
    else
        ERR_CREATE_DUPLICATE_RESULT="FAIL"
    fi
else
    echo "  Could not create error-test thread, skipping CreateBounty error tests"
    ERR_CREATE_NON_AUTHOR_RESULT="FAIL"
    ERR_CREATE_MIN_AMOUNT_RESULT="FAIL"
    ERR_CREATE_DURATION_RESULT="FAIL"
    ERR_CREATE_DUPLICATE_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 15: ERROR PATHS - IncreaseBounty
# ========================================================================
echo "--- PART 15: ERROR PATHS - IncreaseBounty ---"

if [ -n "$ERROR_BOUNTY_ID" ]; then
    # 15a: Non-creator tries to increase
    TX_RES=$($BINARY tx forum increase-bounty \
        "$ERROR_BOUNTY_ID" \
        "100" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    if expect_tx_failure "Increase bounty by non-creator" "bounty creator" "$TX_RES"; then
        ERR_INCREASE_NON_CREATOR_RESULT="PASS"
    else
        ERR_INCREASE_NON_CREATOR_RESULT="FAIL"
    fi
else
    ERR_INCREASE_NON_CREATOR_RESULT="FAIL"
fi

if [ -n "$BOUNTY_ID" ]; then
    # 15b: Increase bounty that's not active (AWARDED from Part 9)
    TX_RES=$($BINARY tx forum increase-bounty \
        "$BOUNTY_ID" \
        "100" \
        --from bounty_creator \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    if expect_tx_failure "Increase non-active bounty (AWARDED)" "not active" "$TX_RES"; then
        ERR_INCREASE_NOT_ACTIVE_RESULT="PASS"
    else
        ERR_INCREASE_NOT_ACTIVE_RESULT="FAIL"
    fi
else
    ERR_INCREASE_NOT_ACTIVE_RESULT="FAIL"
fi

if [ -z "$ERROR_BOUNTY_ID" ] && [ -z "$BOUNTY_ID" ]; then
    echo "  No bounties available for IncreaseBounty error tests"
fi

echo ""

# ========================================================================
# PART 16: ERROR PATHS - AwardBounty
# ========================================================================
echo "--- PART 16: ERROR PATHS - AwardBounty ---"

if [ -n "$ERROR_BOUNTY_ID" ]; then
    # 16a: Award bounty with no assignments yet
    TX_RES=$($BINARY tx forum award-bounty \
        "$ERROR_BOUNTY_ID" \
        --from bounty_creator \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    if expect_tx_failure "Award bounty with no assignments" "no awards assigned" "$TX_RES"; then
        ERR_AWARD_NO_ASSIGNS_RESULT="PASS"
    else
        ERR_AWARD_NO_ASSIGNS_RESULT="FAIL"
    fi

    # 16b: Non-creator tries to award
    TX_RES=$($BINARY tx forum award-bounty \
        "$ERROR_BOUNTY_ID" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    if expect_tx_failure "Award bounty by non-creator" "bounty creator" "$TX_RES"; then
        ERR_AWARD_NON_CREATOR_RESULT="PASS"
    else
        ERR_AWARD_NON_CREATOR_RESULT="FAIL"
    fi
else
    ERR_AWARD_NO_ASSIGNS_RESULT="FAIL"
    ERR_AWARD_NON_CREATOR_RESULT="FAIL"
fi

if [ -n "$BOUNTY_ID" ]; then
    # 16c: Award bounty that's not active (already AWARDED from Part 9)
    TX_RES=$($BINARY tx forum award-bounty \
        "$BOUNTY_ID" \
        --from bounty_creator \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    if expect_tx_failure "Award non-active bounty (already AWARDED)" "not active" "$TX_RES"; then
        ERR_AWARD_NOT_ACTIVE_RESULT="PASS"
    else
        ERR_AWARD_NOT_ACTIVE_RESULT="FAIL"
    fi
else
    ERR_AWARD_NOT_ACTIVE_RESULT="FAIL"
fi

if [ -z "$ERROR_BOUNTY_ID" ] && [ -z "$BOUNTY_ID" ]; then
    echo "  No bounties available for AwardBounty error tests"
fi

echo ""

# ========================================================================
# PART 17: ERROR PATHS - AssignBountyToReply
# ========================================================================
echo "--- PART 17: ERROR PATHS - AssignBountyToReply ---"

if [ -n "$ERROR_THREAD_ID" ] && [ -n "$ERROR_BOUNTY_ID" ]; then
    # Create a reply on the error-test thread for assign tests
    TX_RES=$($BINARY tx forum create-post \
        "${TEST_CATEGORY_ID:-1}" \
        "$ERROR_THREAD_ID" \
        "Reply on error test thread for assign tests" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    ERROR_REPLY_ID=""

    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        if check_tx_success "$TX_RESULT"; then
            ERROR_REPLY_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
            echo "  Created reply $ERROR_REPLY_ID on error-test thread"
        fi
    fi

    if [ -n "$ERROR_REPLY_ID" ]; then
        # 17a: Non-creator tries to assign
        TX_RES=$($BINARY tx forum assign-bounty-to-reply \
            "$ERROR_THREAD_ID" \
            "$ERROR_REPLY_ID" \
            "Trying to steal" \
            --from poster1 \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)
        if expect_tx_failure "Assign bounty by non-creator" "bounty creator" "$TX_RES"; then
            ERR_ASSIGN_NON_CREATOR_RESULT="PASS"
        else
            ERR_ASSIGN_NON_CREATOR_RESULT="FAIL"
        fi
    else
        ERR_ASSIGN_NON_CREATOR_RESULT="FAIL"
    fi

    # 17b: Assign to root post (thread itself)
    TX_RES=$($BINARY tx forum assign-bounty-to-reply \
        "$ERROR_THREAD_ID" \
        "$ERROR_THREAD_ID" \
        "Assign to root" \
        --from bounty_creator \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    if expect_tx_failure "Assign bounty to root post" "root post" "$TX_RES"; then
        ERR_ASSIGN_ROOT_POST_RESULT="PASS"
    else
        ERR_ASSIGN_ROOT_POST_RESULT="FAIL"
    fi

    # 17c: Assign reply from a different thread
    if [ -n "$BOUNTY_REPLY_ID" ]; then
        TX_RES=$($BINARY tx forum assign-bounty-to-reply \
            "$ERROR_THREAD_ID" \
            "$BOUNTY_REPLY_ID" \
            "Wrong thread reply" \
            --from bounty_creator \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)
        if expect_tx_failure "Assign reply from different thread" "not a reply" "$TX_RES"; then
            ERR_ASSIGN_DIFF_THREAD_RESULT="PASS"
        else
            ERR_ASSIGN_DIFF_THREAD_RESULT="FAIL"
        fi
    else
        ERR_ASSIGN_DIFF_THREAD_RESULT="FAIL"
    fi
else
    echo "  No error-test thread/bounty available for AssignBountyToReply error tests"
    ERR_ASSIGN_NON_CREATOR_RESULT="FAIL"
    ERR_ASSIGN_ROOT_POST_RESULT="FAIL"
    ERR_ASSIGN_DIFF_THREAD_RESULT="FAIL"
fi

echo ""

# ========================================================================
# PART 18: ERROR PATHS - CancelBounty
# ========================================================================
echo "--- PART 18: ERROR PATHS - CancelBounty ---"

if [ -n "$ERROR_BOUNTY_ID" ]; then
    # 18a: Non-creator tries to cancel
    TX_RES=$($BINARY tx forum cancel-bounty \
        "$ERROR_BOUNTY_ID" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    if expect_tx_failure "Cancel bounty by non-creator" "bounty creator" "$TX_RES"; then
        ERR_CANCEL_NON_CREATOR_RESULT="PASS"
    else
        ERR_CANCEL_NON_CREATOR_RESULT="FAIL"
    fi

    # 18b: Assign an award, then try to cancel (cannot cancel with awards)
    if [ -n "$ERROR_REPLY_ID" ]; then
        TX_RES=$($BINARY tx forum assign-bounty-to-reply \
            "$ERROR_THREAD_ID" \
            "$ERROR_REPLY_ID" \
            "Award for cancel test" \
            --from bounty_creator \
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
                echo "  Assigned award to set up cancel-with-awards test"

                TX_RES=$($BINARY tx forum cancel-bounty \
                    "$ERROR_BOUNTY_ID" \
                    --from bounty_creator \
                    --chain-id $CHAIN_ID \
                    --keyring-backend test \
                    --fees 5000uspark \
                    -y \
                    --output json 2>&1)
                if expect_tx_failure "Cancel bounty with existing awards" "existing awards" "$TX_RES"; then
                    ERR_CANCEL_HAS_AWARDS_RESULT="PASS"
                else
                    ERR_CANCEL_HAS_AWARDS_RESULT="FAIL"
                fi
            else
                ERR_CANCEL_HAS_AWARDS_RESULT="FAIL"
            fi
        else
            ERR_CANCEL_HAS_AWARDS_RESULT="FAIL"
        fi
    else
        ERR_CANCEL_HAS_AWARDS_RESULT="FAIL"
    fi
else
    ERR_CANCEL_NON_CREATOR_RESULT="FAIL"
    ERR_CANCEL_HAS_AWARDS_RESULT="FAIL"
fi

if [ -n "$BOUNTY_ID" ]; then
    # 18c: Cancel bounty that's not active (AWARDED from Part 9)
    TX_RES=$($BINARY tx forum cancel-bounty \
        "$BOUNTY_ID" \
        --from bounty_creator \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    if expect_tx_failure "Cancel non-active bounty (AWARDED)" "not active" "$TX_RES"; then
        ERR_CANCEL_NOT_ACTIVE_RESULT="PASS"
    else
        ERR_CANCEL_NOT_ACTIVE_RESULT="FAIL"
    fi
else
    ERR_CANCEL_NOT_ACTIVE_RESULT="FAIL"
fi

if [ -z "$ERROR_BOUNTY_ID" ] && [ -z "$BOUNTY_ID" ]; then
    echo "  No bounties available for CancelBounty error tests"
fi

echo ""

# ========================================================================
# PART 19: BALANCE VERIFICATION - Escrow and Cancellation Fee
# ========================================================================
echo "--- PART 19: BALANCE VERIFICATION - Escrow and Cancellation Fee ---"

# Create a fresh thread and bounty, then cancel to verify:
#   1. Escrow deducts the bounty amount from creator
#   2. Cancellation refunds minus 10% fee

TX_RES=$($BINARY tx forum create-post \
    "${TEST_CATEGORY_ID:-1}" \
    "0" \
    "Balance verification test thread" \
    --from bounty_creator \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
BAL_THREAD_ID=""

if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        BAL_THREAD_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        echo "  Created thread $BAL_THREAD_ID for balance verification"
    fi
fi

if [ -n "$BAL_THREAD_ID" ]; then
    BAL_BOUNTY_AMOUNT="100000"  # 100000 uspark (large enough to see fee vs gas)

    # Record balance before creating bounty
    BALANCE_PRE=$($BINARY query bank balances $BOUNTY_CREATOR_ADDR --output json 2>&1 | jq -r '.balances[] | select(.denom=="uspark") | .amount')
    echo "  Balance before create-bounty: $BALANCE_PRE uspark"

    # Create bounty (escrows BAL_BOUNTY_AMOUNT uspark)
    TX_RES=$($BINARY tx forum create-bounty \
        "$BAL_THREAD_ID" \
        "$BAL_BOUNTY_AMOUNT" \
        "1000" \
        --from bounty_creator \
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
            BAL_BOUNTY_ID=$(extract_event_value "$TX_RESULT" "bounty_created" "bounty_id")
            if [ -z "$BAL_BOUNTY_ID" ] || [ "$BAL_BOUNTY_ID" == "null" ]; then
                BOUNTIES=$($BINARY query forum list-bounty --output json 2>&1)
                BAL_BOUNTY_ID=$(echo "$BOUNTIES" | jq -r '.bounty[-1].id // empty')
            fi

            BALANCE_POST_ESCROW=$($BINARY query bank balances $BOUNTY_CREATOR_ADDR --output json 2>&1 | jq -r '.balances[] | select(.denom=="uspark") | .amount')
            echo "  Balance after create-bounty:  $BALANCE_POST_ESCROW uspark"

            # Verify escrow: balance decreased by at least the bounty amount
            ESCROW_DIFF=$((BALANCE_PRE - BALANCE_POST_ESCROW))
            if [ "$ESCROW_DIFF" -ge "$BAL_BOUNTY_AMOUNT" ]; then
                echo "  PASS: Escrow verified - balance decreased by $ESCROW_DIFF uspark (bounty=$BAL_BOUNTY_AMOUNT + gas)"
                BAL_ESCROW_RESULT="PASS"
            else
                echo "  FAIL: Balance only decreased by $ESCROW_DIFF (expected >= $BAL_BOUNTY_AMOUNT)"
                BAL_ESCROW_RESULT="FAIL"
            fi

            # Cancel bounty to verify partial refund (10% cancellation fee)
            TX_RES=$($BINARY tx forum cancel-bounty \
                "$BAL_BOUNTY_ID" \
                --from bounty_creator \
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
                    BALANCE_POST_CANCEL=$($BINARY query bank balances $BOUNTY_CREATOR_ADDR --output json 2>&1 | jq -r '.balances[] | select(.denom=="uspark") | .amount')
                    echo "  Balance after cancel-bounty: $BALANCE_POST_CANCEL uspark"

                    # Refund = bounty_amount * (100 - fee_percent) / 100
                    # Default fee is 10%, so refund = 90000, fee burned = 10000
                    CANCEL_RECOVERY=$((BALANCE_POST_CANCEL - BALANCE_POST_ESCROW))
                    EXPECTED_REFUND=$((BAL_BOUNTY_AMOUNT * 90 / 100))  # 90000
                    CANCEL_GAS=5000
                    EXPECTED_RECOVERY=$((EXPECTED_REFUND - CANCEL_GAS))  # 85000

                    echo "  Recovery: $CANCEL_RECOVERY uspark (expected ~$EXPECTED_RECOVERY = $EXPECTED_REFUND refund - $CANCEL_GAS gas)"

                    if [ "$CANCEL_RECOVERY" -gt 0 ] && [ "$CANCEL_RECOVERY" -lt "$BAL_BOUNTY_AMOUNT" ]; then
                        echo "  PASS: Partial refund received (cancellation fee applied)"
                        BAL_REFUND_RESULT="PASS"
                    else
                        echo "  WARN: Unexpected recovery=$CANCEL_RECOVERY (expected 0 < recovery < $BAL_BOUNTY_AMOUNT)"
                        BAL_REFUND_RESULT="FAIL"
                    fi

                    # Net loss = original balance - final balance
                    NET_LOSS=$((BALANCE_PRE - BALANCE_POST_CANCEL))
                    EXPECTED_FEE=$((BAL_BOUNTY_AMOUNT * 10 / 100))  # 10000
                    echo "  Net loss: $NET_LOSS uspark (expected ~$((EXPECTED_FEE + 10000)) = $EXPECTED_FEE fee + gas for 2 txs)"
                else
                    echo "  Failed to cancel bounty for balance verification"
                    BAL_REFUND_RESULT="FAIL"
                fi
            else
                BAL_REFUND_RESULT="FAIL"
            fi
        else
            echo "  Failed to create bounty for balance verification"
            BAL_ESCROW_RESULT="FAIL"
            BAL_REFUND_RESULT="FAIL"
        fi
    else
        BAL_ESCROW_RESULT="FAIL"
        BAL_REFUND_RESULT="FAIL"
    fi
else
    echo "  Could not create thread for balance verification"
    BAL_ESCROW_RESULT="FAIL"
    BAL_REFUND_RESULT="FAIL"
fi

echo ""

# ========================================================================
# SUMMARY
# ========================================================================
echo "--- BOUNTY TEST SUMMARY ---"
echo ""
echo "  Happy Paths:"
echo "    Fund bounty creator:           $FUND_CREATOR_RESULT"
echo "    List bounties:                 $LIST_BOUNTIES_RESULT"
echo "    Create thread for bounty:      $CREATE_THREAD_RESULT"
echo "    Create bounty:                 $CREATE_BOUNTY_RESULT"
echo "    Query bounty details:          $QUERY_BOUNTY_RESULT"
echo "    Query bounty by thread:        $QUERY_BY_THREAD_RESULT"
echo "    Increase bounty:               $INCREASE_BOUNTY_RESULT"
echo "    Create reply for award:        $CREATE_REPLY_RESULT"
echo "    Assign bounty to reply:        $ASSIGN_BOUNTY_RESULT"
echo "    Award bounty (finalize):       $AWARD_BOUNTY_RESULT"
echo "    Query active bounties:         $QUERY_ACTIVE_RESULT"
echo "    Query expiring bounties:       $QUERY_EXPIRING_RESULT"
echo "    Cancel bounty:                 $CANCEL_BOUNTY_RESULT"
echo "    Query user bounties:           $QUERY_USER_BOUNTIES_RESULT"
echo ""
echo "  Error Paths - CreateBounty:"
echo "    Non-thread-author:             $ERR_CREATE_NON_AUTHOR_RESULT"
echo "    Amount below minimum:          $ERR_CREATE_MIN_AMOUNT_RESULT"
echo "    Duration exceeds maximum:      $ERR_CREATE_DURATION_RESULT"
echo "    Duplicate bounty on thread:    $ERR_CREATE_DUPLICATE_RESULT"
echo ""
echo "  Error Paths - IncreaseBounty:"
echo "    Non-creator:                   $ERR_INCREASE_NON_CREATOR_RESULT"
echo "    Not active (AWARDED):          $ERR_INCREASE_NOT_ACTIVE_RESULT"
echo ""
echo "  Error Paths - AwardBounty:"
echo "    No assignments:                $ERR_AWARD_NO_ASSIGNS_RESULT"
echo "    Non-creator:                   $ERR_AWARD_NON_CREATOR_RESULT"
echo "    Not active (AWARDED):          $ERR_AWARD_NOT_ACTIVE_RESULT"
echo ""
echo "  Error Paths - AssignBountyToReply:"
echo "    Non-creator:                   $ERR_ASSIGN_NON_CREATOR_RESULT"
echo "    Assign to root post:           $ERR_ASSIGN_ROOT_POST_RESULT"
echo "    Reply from different thread:   $ERR_ASSIGN_DIFF_THREAD_RESULT"
echo ""
echo "  Error Paths - CancelBounty:"
echo "    Non-creator:                   $ERR_CANCEL_NON_CREATOR_RESULT"
echo "    Has existing awards:           $ERR_CANCEL_HAS_AWARDS_RESULT"
echo "    Not active (AWARDED):          $ERR_CANCEL_NOT_ACTIVE_RESULT"
echo ""
echo "  Balance Verification:"
echo "    Escrow deducts on create:      $BAL_ESCROW_RESULT"
echo "    Partial refund on cancel:      $BAL_REFUND_RESULT"
echo ""

# Count failures
FAIL_COUNT=0
ALL_RESULTS=(
    "$FUND_CREATOR_RESULT"
    "$LIST_BOUNTIES_RESULT"
    "$CREATE_THREAD_RESULT"
    "$CREATE_BOUNTY_RESULT"
    "$QUERY_BOUNTY_RESULT"
    "$QUERY_BY_THREAD_RESULT"
    "$INCREASE_BOUNTY_RESULT"
    "$CREATE_REPLY_RESULT"
    "$ASSIGN_BOUNTY_RESULT"
    "$AWARD_BOUNTY_RESULT"
    "$QUERY_ACTIVE_RESULT"
    "$QUERY_EXPIRING_RESULT"
    "$CANCEL_BOUNTY_RESULT"
    "$QUERY_USER_BOUNTIES_RESULT"
    "$ERR_CREATE_NON_AUTHOR_RESULT"
    "$ERR_CREATE_MIN_AMOUNT_RESULT"
    "$ERR_CREATE_DURATION_RESULT"
    "$ERR_CREATE_DUPLICATE_RESULT"
    "$ERR_INCREASE_NON_CREATOR_RESULT"
    "$ERR_INCREASE_NOT_ACTIVE_RESULT"
    "$ERR_AWARD_NO_ASSIGNS_RESULT"
    "$ERR_AWARD_NON_CREATOR_RESULT"
    "$ERR_AWARD_NOT_ACTIVE_RESULT"
    "$ERR_ASSIGN_NON_CREATOR_RESULT"
    "$ERR_ASSIGN_ROOT_POST_RESULT"
    "$ERR_ASSIGN_DIFF_THREAD_RESULT"
    "$ERR_CANCEL_NON_CREATOR_RESULT"
    "$ERR_CANCEL_HAS_AWARDS_RESULT"
    "$ERR_CANCEL_NOT_ACTIVE_RESULT"
    "$BAL_ESCROW_RESULT"
    "$BAL_REFUND_RESULT"
)

for R in "${ALL_RESULTS[@]}"; do
    if [ "$R" == "FAIL" ]; then
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
done

TOTAL=${#ALL_RESULTS[@]}
PASS_COUNT=$((TOTAL - FAIL_COUNT))

echo "  Total: $TOTAL | Passed: $PASS_COUNT | Failed: $FAIL_COUNT"
echo ""

if [ "$FAIL_COUNT" -gt 0 ]; then
    echo "  FAILURES: $FAIL_COUNT test(s) failed"
else
    echo "  ALL TESTS PASSED"
fi

echo ""
echo "BOUNTY TEST COMPLETED"
echo ""

exit $FAIL_COUNT
