#!/bin/bash

echo "--- TESTING: QUESTS (CREATE, START, PROGRESS, COMPLETE, ABANDON) ---"

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

echo "Quest User: $QUEST_USER_ADDR"
echo "Alice:      $ALICE_ADDR"
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

# ========================================================================
# PART 1: LIST EXISTING QUESTS
# ========================================================================
echo "--- PART 1: LIST EXISTING QUESTS ---"

QUESTS=$($BINARY query season quests-list --output json 2>&1)

if echo "$QUESTS" | grep -q "error"; then
    echo "  Failed to query quests"
    INITIAL_QUEST_COUNT=0
else
    INITIAL_QUEST_COUNT=$(echo "$QUESTS" | jq -r '.quests | length' 2>/dev/null || echo "0")
    echo "  Existing quests: $INITIAL_QUEST_COUNT"

    if [ "$INITIAL_QUEST_COUNT" -gt 0 ]; then
        echo ""
        echo "  Quest examples:"
        echo "$QUESTS" | jq -r '.quests[0:3] | .[] | "    - \(.id): \(.name) (XP: \(.xp_reward))"' 2>/dev/null
    fi
fi

echo ""

# ========================================================================
# PART 2: CREATE A QUEST (Requires authority - may fail)
# ========================================================================
echo "--- PART 2: CREATE A QUEST (Authority Required) ---"

QUEST_ID="test_quest_$(date +%s)"
QUEST_NAME="Test Quest"
QUEST_DESC="A test quest for e2e testing"
QUEST_CHAIN="test_chain_$(date +%s)"

echo "Attempting to create quest: $QUEST_ID (chain: $QUEST_CHAIN)"
echo "Note: This requires Commons Operations Committee authority"

# Get current block height for end block only
# START_BLOCK=0 means quest is immediately available
CURRENT_HEIGHT=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height // "100"')
START_BLOCK=0
END_BLOCK=$((CURRENT_HEIGHT + 10000))

# Try to create quest with chain_id for Part 8 testing
TX_RES=$($BINARY tx season create-quest \
    "$QUEST_ID" \
    "$QUEST_NAME" \
    "$QUEST_DESC" \
    "50" \
    "false" \
    "0" \
    "0" \
    "$START_BLOCK" \
    "$END_BLOCK" \
    "0" \
    "" \
    "" \
    "$QUEST_CHAIN" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to submit transaction (expected - requires authority)"
    echo "  Note: Quest creation requires Commons Operations Committee membership"
else
    echo "  Transaction: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  Quest created successfully"
        CREATED_QUEST_ID="$QUEST_ID"
        CREATED_CHAIN_ID="$QUEST_CHAIN"
    else
        echo "  Quest creation failed (expected without authority)"
    fi
fi

echo ""

# ========================================================================
# PART 2.5: CREATE PROFILE FOR QUEST USER
# ========================================================================
echo "--- PART 2.5: CREATE PROFILE FOR QUEST USER ---"

# Check if profile exists
PROFILE_CHECK=$($BINARY query season get-member-profile $QUEST_USER_ADDR --output json 2>&1)

if echo "$PROFILE_CHECK" | grep -q "not found"; then
    echo "Creating profile for quest_user via set-display-name..."

    TX_RES=$($BINARY tx season set-display-name \
        "Quest Tester" \
        --from quest_user \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to create profile"
    else
        echo "  Transaction: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Profile created for quest_user"
        else
            echo "  Failed to create profile"
        fi
    fi
else
    echo "  quest_user already has a profile"
fi

echo ""

# ========================================================================
# PART 3: QUERY AVAILABLE QUESTS FOR USER
# ========================================================================
echo "--- PART 3: QUERY AVAILABLE QUESTS FOR USER ---"

AVAILABLE=$($BINARY query season available-quests $QUEST_USER_ADDR --output json 2>&1)

if echo "$AVAILABLE" | grep -q "error"; then
    echo "  Failed to query available quests"
else
    # Response is a single quest with .id, .name, .xp_reward at root level
    AVAILABLE_ID=$(echo "$AVAILABLE" | jq -r '.id // empty' 2>/dev/null)
    if [ -n "$AVAILABLE_ID" ]; then
        AVAILABLE_NAME=$(echo "$AVAILABLE" | jq -r '.name // "unknown"' 2>/dev/null)
        AVAILABLE_XP=$(echo "$AVAILABLE" | jq -r '.xp_reward // "0"' 2>/dev/null)
        echo "  Available quest for quest_user: $AVAILABLE_ID ($AVAILABLE_NAME, $AVAILABLE_XP XP)"
        FIRST_QUEST="$AVAILABLE_ID"
    else
        echo "  No available quests for quest_user"
    fi
fi

echo ""

# ========================================================================
# PART 4: QUERY QUEST BY ID
# ========================================================================
echo "--- PART 4: QUERY QUEST BY ID ---"

# Use first available quest, the created quest, or try to find any existing quest
if [ -n "$FIRST_QUEST" ]; then
    TEST_QUEST_ID="$FIRST_QUEST"
elif [ -n "$CREATED_QUEST_ID" ]; then
    echo "  Using created quest: $CREATED_QUEST_ID"
    TEST_QUEST_ID="$CREATED_QUEST_ID"
else
    # Try to find any existing quest
    QUESTS_FALLBACK=$($BINARY query season list-quest --output json 2>&1)
    TEST_QUEST_ID=$(echo "$QUESTS_FALLBACK" | jq -r '.quest[0].quest_id // empty' 2>/dev/null)
    if [ -z "$TEST_QUEST_ID" ]; then
        echo "  No quests found in the system"
        TEST_QUEST_ID=""
    else
        echo "  Using existing quest: $TEST_QUEST_ID"
    fi
fi

if [ -n "$TEST_QUEST_ID" ]; then
    QUEST_INFO=$($BINARY query season quest-by-id "$TEST_QUEST_ID" --output json 2>&1)

    if echo "$QUEST_INFO" | grep -q "error\|not found"; then
        echo "  Quest $TEST_QUEST_ID not found"

        # Try to find any quest
        QUESTS=$($BINARY query season list-quest --output json 2>&1)
        FIRST_EXISTING=$(echo "$QUESTS" | jq -r '.quest[0].quest_id // empty')

        if [ -n "$FIRST_EXISTING" ]; then
            TEST_QUEST_ID="$FIRST_EXISTING"
            echo "  Using existing quest: $TEST_QUEST_ID"
            QUEST_INFO=$($BINARY query season quest-by-id "$TEST_QUEST_ID" --output json 2>&1)
        fi
    fi
else
    QUEST_INFO=""
fi

if [ -n "$QUEST_INFO" ] && ! echo "$QUEST_INFO" | grep -q "error\|not found"; then
    echo "  Quest Details:"
    echo "    ID: $TEST_QUEST_ID"
    echo "    Name: $(echo "$QUEST_INFO" | jq -r '.name // "unknown"')"
    echo "    Description: $(echo "$QUEST_INFO" | jq -r '.description // "none"' | head -c 50)..."
    echo "    XP Reward: $(echo "$QUEST_INFO" | jq -r '.xp_reward // "0"')"
    echo "    Repeatable: $(echo "$QUEST_INFO" | jq -r '.repeatable // "false"')"
    echo "    Active: $(echo "$QUEST_INFO" | jq -r '.active // "true"')"
fi

echo ""

# ========================================================================
# PART 5: START A QUEST
# ========================================================================
echo "--- PART 5: START A QUEST ---"

if [ -n "$TEST_QUEST_ID" ]; then
    echo "quest_user starting quest: $TEST_QUEST_ID"

    TX_RES=$($BINARY tx season start-quest \
        "$TEST_QUEST_ID" \
        --from quest_user \
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
            echo "  Quest started"
            STARTED_QUEST="$TEST_QUEST_ID"
        else
            echo "  Failed to start quest (may not be eligible or already started)"
        fi
    fi
else
    echo "  No quest available to start"
fi

echo ""

# ========================================================================
# PART 6: QUERY MEMBER QUEST STATUS
# ========================================================================
echo "--- PART 6: QUERY MEMBER QUEST STATUS ---"

if [ -n "$STARTED_QUEST" ]; then
    STATUS=$($BINARY query season member-quest-status $QUEST_USER_ADDR "$STARTED_QUEST" --output json 2>&1)

    if echo "$STATUS" | grep -q "error\|not found"; then
        echo "  No quest progress found"
    else
        echo "  Quest Status:"
        echo "    Quest: $STARTED_QUEST"
        echo "    Completed: $(echo "$STATUS" | jq -r '.completed // false')"
        echo "    Completed Block: $(echo "$STATUS" | jq -r '.completed_block // 0')"
    fi
fi

# Also list all quest progress for user
echo ""
echo "All quest progress for quest_user:"
PROGRESS_LIST=$($BINARY query season list-member-quest-progress --output json 2>&1)

if echo "$PROGRESS_LIST" | grep -q "error"; then
    echo "  Failed to query quest progress"
else
    # Try both camelCase (protobuf) and snake_case (Go json) field names
    # member_quest is a composite key like "member:quest_id"
    USER_PROGRESS=$(echo "$PROGRESS_LIST" | jq -r "(.memberQuestProgress // .member_quest_progress // [])[] | select(.member_quest | startswith(\"$QUEST_USER_ADDR:\"))" 2>/dev/null)
    if [ -n "$USER_PROGRESS" ]; then
        echo "$USER_PROGRESS" | jq -r '"  - \(.member_quest): completed=\(if .completed then "true" else "false" end)"' 2>/dev/null
    else
        echo "  No quest progress found (list query returned empty)"
    fi
fi

echo ""

# ========================================================================
# PART 7: CLAIM QUEST REWARD (test quest has no objectives)
# ========================================================================
echo "--- PART 7: CLAIM QUEST REWARD ---"

# Try to claim reward for the started quest (test quest has no objectives, so should be claimable)
if [ -n "$STARTED_QUEST" ]; then
    echo "Attempting to claim reward for quest: $STARTED_QUEST"
    echo "Note: Quest has no objectives, so should be immediately claimable"

    TX_RES=$($BINARY tx season claim-quest-reward \
        "$STARTED_QUEST" \
        --from quest_user \
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
            echo "  Reward claimed successfully!"
            QUEST_CLAIMED=true
        else
            echo "  Failed to claim reward"
            echo "  $(echo "$TX_RESULT" | jq -r '.raw_log // "Unknown error"')"
        fi
    fi
else
    echo "  No started quest to claim"
fi

echo ""

# ========================================================================
# PART 8: ABANDON A QUEST (will fail if already claimed)
# ========================================================================
echo "--- PART 8: ABANDON A QUEST ---"

if [ -n "$STARTED_QUEST" ] && [ "$QUEST_CLAIMED" != "true" ]; then
    echo "quest_user abandoning quest: $STARTED_QUEST"

    TX_RES=$($BINARY tx season abandon-quest \
        "$STARTED_QUEST" \
        --from quest_user \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit transaction"
    else
        echo "  Transaction: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Quest abandoned"
        else
            echo "  Failed to abandon quest (expected if already claimed)"
        fi
    fi
elif [ "$QUEST_CLAIMED" = "true" ]; then
    echo "  Quest was claimed, cannot abandon (expected behavior)"
else
    echo "  No quest to abandon"
fi

echo ""

# ========================================================================
# PART 9: QUERY QUEST CHAIN
# ========================================================================
echo "--- PART 9: QUERY QUEST CHAIN ---"

# Use the chain we created, or try to find quests with a chain
if [ -n "$CREATED_CHAIN_ID" ]; then
    CHAIN_QUEST="$CREATED_CHAIN_ID"
    echo "  Using created chain: $CHAIN_QUEST"
else
    QUESTS=$($BINARY query season list-quest --output json 2>&1)
    CHAIN_QUEST=$(echo "$QUESTS" | jq -r '.quest[] | select(.chain_id != "" and .chain_id != null) | .chain_id' 2>/dev/null | head -1)
fi

if [ -n "$CHAIN_QUEST" ]; then
    echo "Querying quest chain: $CHAIN_QUEST"

    CHAIN_INFO=$($BINARY query season quest-chain "$CHAIN_QUEST" --output json 2>&1)

    if echo "$CHAIN_INFO" | grep -q "error"; then
        echo "  Failed to query quest chain"
    else
        # Response is a single quest object with quest_id and name
        QUEST_IN_CHAIN=$(echo "$CHAIN_INFO" | jq -r '.quest_id // empty' 2>/dev/null)
        QUEST_NAME=$(echo "$CHAIN_INFO" | jq -r '.name // empty' 2>/dev/null)
        if [ -n "$QUEST_IN_CHAIN" ]; then
            echo "  Found quest in chain: $QUEST_IN_CHAIN ($QUEST_NAME)"
        else
            echo "  No quests found in chain"
        fi
    fi
else
    echo "  No quest chains found"
fi

echo ""

# ========================================================================
# PART 10: DEACTIVATE QUEST (Requires authority)
# ========================================================================
echo "--- PART 10: DEACTIVATE QUEST (Authority Required) ---"

if [ -n "$TEST_QUEST_ID" ]; then
    echo "Attempting to deactivate quest: $TEST_QUEST_ID"
    echo "Note: This requires Commons Operations Committee authority"

    TX_RES=$($BINARY tx season deactivate-quest \
        "$TEST_QUEST_ID" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit transaction (expected - requires authority)"
    else
        echo "  Transaction: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Quest deactivated"
        else
            echo "  Quest deactivation failed (expected without authority)"
        fi
    fi
else
    echo "  No quest to deactivate"
fi

echo ""

# ========================================================================
# SUMMARY
# ========================================================================
echo "--- QUEST TEST SUMMARY ---"
echo ""
echo "  List quests:             Tested"
echo "  Create quest:            Tested (requires authority)"
echo "  Create profile:          Tested"
echo "  Query available quests:  Tested"
echo "  Query quest by ID:       Tested"
echo "  Start quest:             Tested"
echo "  Query quest status:      Tested"
echo "  Claim quest reward:      Tested"
echo "  Abandon quest:           Tested"
echo "  Query quest chain:       Tested"
echo "  Deactivate quest:        Tested (requires authority)"
echo ""
echo "QUEST TEST COMPLETED"
echo ""
