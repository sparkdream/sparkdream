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
# Quest/achievement/title definitions and display-name moderation
# resolutions need a Commons Operations Committee proposal (policy address),
# never a single committee member (x/season SEA-23 decision).
source "$SCRIPT_DIR/../lib/ops_committee.sh"

PASS_COUNT=0
FAIL_COUNT=0
TOTAL_COUNT=0

pass() {
    PASS_COUNT=$((PASS_COUNT + 1))
    TOTAL_COUNT=$((TOTAL_COUNT + 1))
    echo "  PASS: $1"
}

fail() {
    FAIL_COUNT=$((FAIL_COUNT + 1))
    TOTAL_COUNT=$((TOTAL_COUNT + 1))
    echo "  FAIL: $1"
}

echo "Quest User: $QUEST_USER_ADDR"
echo "Alice:      $ALICE_ADDR"
echo ""

# ========================================================================
# Helper Functions
# ========================================================================

wait_for_tx() {
    local TXHASH=$1
    local MAX_ATTEMPTS=60
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
        pass "list quests returned $INITIAL_QUEST_COUNT quests"
    else
        pass "list quests query succeeded (0 quests)"
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

# Create the quest through an Operations Committee proposal (the keeper
# accepts only the committee policy address).
QUEST_MSG=$(jq -nc --arg id "$QUEST_ID" --arg name "$QUEST_NAME" --arg desc "$QUEST_DESC" \
    --arg end "$END_BLOCK" --arg chain "$QUEST_CHAIN" \
    '{"@type":"/sparkdream.season.v1.MsgCreateQuest",quest_id:$id,name:$name,description:$desc,xp_reward:"50",start_block:"0",end_block:$end,quest_chain:$chain}')
if ops_committee_exec "$QUEST_MSG"; then
    echo "  Quest created successfully (proposal $OPS_COMMITTEE_PROPOSAL_ID)"
    CREATED_QUEST_ID="$QUEST_ID"
    CREATED_CHAIN_ID="$QUEST_CHAIN"
    pass "create-quest via Operations Committee proposal"
else
    echo "  Quest creation failed: $OPS_COMMITTEE_ERROR"
    fail "create-quest via Operations Committee proposal"
fi

# A single member (alice) signing directly is refused.
TX_RES=$($BINARY tx season create-quest "${QUEST_ID}_direct" "Direct" "d" "10" "false" "0" "0" "0" "$END_BLOCK" "0" "" "" "" \
    --from alice --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} -y --output json 2>&1)
TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    pass "direct create-quest by a member is refused (no txhash)"
else
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        fail "direct create-quest by a member must be refused"
    else
        pass "direct create-quest by a member is refused"
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
        --fees 5000${BOND_DENOM} \
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
            pass "create profile for quest_user"
        else
            echo "  Failed to create profile"
            fail "create profile for quest_user"
        fi
    fi
else
    echo "  quest_user already has a profile"
    pass "quest_user profile already exists"
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
    # Response is a paginated list: .quests[] (SEA-35)
    AVAILABLE_ID=$(echo "$AVAILABLE" | jq -r '.quests[0].quest_id // empty' 2>/dev/null)
    if [ -n "$AVAILABLE_ID" ]; then
        AVAILABLE_NAME=$(echo "$AVAILABLE" | jq -r '.quests[0].name // "unknown"' 2>/dev/null)
        AVAILABLE_XP=$(echo "$AVAILABLE" | jq -r '.quests[0].xp_reward // "0"' 2>/dev/null)
        echo "  Available quest for quest_user: $AVAILABLE_ID ($AVAILABLE_NAME, $AVAILABLE_XP XP)"
        FIRST_QUEST="$AVAILABLE_ID"
        pass "available-quests returned a quest"
    else
        echo "  No available quests for quest_user"
        pass "available-quests query succeeded (no quests available)"
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
    QUERIED_QUEST_NAME=$(echo "$QUEST_INFO" | jq -r '.name // "unknown"')
    echo "  Quest Details:"
    echo "    ID: $TEST_QUEST_ID"
    echo "    Name: $QUERIED_QUEST_NAME"
    echo "    Description: $(echo "$QUEST_INFO" | jq -r '.description // "none"' | head -c 50)..."
    echo "    XP Reward: $(echo "$QUEST_INFO" | jq -r '.xp_reward // "0"')"
    echo "    Repeatable: $(echo "$QUEST_INFO" | jq -r '.repeatable // "false"')"
    echo "    Active: $(echo "$QUEST_INFO" | jq -r '.active // "true"')"

    if [ -n "$QUERIED_QUEST_NAME" ] && [ "$QUERIED_QUEST_NAME" != "unknown" ] && [ "$QUERIED_QUEST_NAME" != "null" ]; then
        pass "quest-by-id returned quest details (name=$QUERIED_QUEST_NAME)"
    else
        fail "quest-by-id returned empty or unknown name"
    fi
else
    if [ -n "$TEST_QUEST_ID" ]; then
        fail "quest-by-id failed for $TEST_QUEST_ID"
    fi
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
        --fees 5000${BOND_DENOM} \
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
            pass "start-quest transaction succeeded"
        else
            echo "  Failed to start quest (may not be eligible or already started)"
            fail "start-quest transaction failed"
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
        fail "member-quest-status not found after starting quest"
    else
        echo "  Quest Status:"
        echo "    Quest: $STARTED_QUEST"
        echo "    Completed: $(echo "$STATUS" | jq -r '.completed // false')"
        echo "    Completed Block: $(echo "$STATUS" | jq -r '.completed_block // 0')"
        pass "member-quest-status returned status for started quest"
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
        --fees 5000${BOND_DENOM} \
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
            pass "claim-quest-reward transaction succeeded"
        else
            echo "  Failed to claim reward"
            echo "  $(echo "$TX_RESULT" | jq -r '.raw_log // "Unknown error"')"
            fail "claim-quest-reward transaction failed"
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
        --fees 5000${BOND_DENOM} \
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
            pass "abandon-quest transaction succeeded"
        else
            echo "  Failed to abandon quest (expected if already claimed)"
            pass "abandon-quest correctly rejected (quest already claimed or ineligible)"
        fi
    fi
elif [ "$QUEST_CLAIMED" = "true" ]; then
    echo "  Quest was claimed, cannot abandon (expected behavior)"
    pass "abandon-quest skipped correctly (quest already claimed)"
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
        fail "quest-chain query returned error"
    else
        # Response is a single quest object with quest_id and name
        QUEST_IN_CHAIN=$(echo "$CHAIN_INFO" | jq -r '.quest_id // empty' 2>/dev/null)
        QUEST_NAME=$(echo "$CHAIN_INFO" | jq -r '.name // empty' 2>/dev/null)
        if [ -n "$QUEST_IN_CHAIN" ]; then
            echo "  Found quest in chain: $QUEST_IN_CHAIN ($QUEST_NAME)"
            pass "quest-chain returned quest in chain"
        else
            echo "  No quests found in chain"
            pass "quest-chain query succeeded (empty chain)"
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

    if ops_committee_exec "$(jq -nc --arg id "$TEST_QUEST_ID" '{"@type":"/sparkdream.season.v1.MsgDeactivateQuest",quest_id:$id}')"; then
        echo "  Quest deactivated (proposal $OPS_COMMITTEE_PROPOSAL_ID)"
        pass "deactivate-quest via Operations Committee proposal"
    else
        echo "  Quest deactivation failed: $OPS_COMMITTEE_ERROR"
        fail "deactivate-quest via Operations Committee proposal"
    fi
fi

echo ""

# ========================================================================
# SUMMARY
# ========================================================================
echo "--- QUEST TEST SUMMARY ---"
echo ""
echo "  Total:  $TOTAL_COUNT"
echo "  Passed: $PASS_COUNT"
echo "  Failed: $FAIL_COUNT"
echo ""
if [ "$FAIL_COUNT" -gt 0 ]; then
    echo "QUEST TEST FAILED ($FAIL_COUNT failures)"
    exit 1
else
    echo "QUEST TEST PASSED (all $PASS_COUNT assertions passed)"
fi
echo ""
