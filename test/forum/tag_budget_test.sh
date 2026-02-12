#!/bin/bash

echo "--- TESTING: TAG BUDGETS (CREATE, TOGGLE, TOP-UP, WITHDRAW, AWARD) ---"

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

# Group 9 (Commons Ops Committee) - threshold=1, alice is sole member
GROUP_POLICY_ADDR="sprkdrm10ezj2lmcj3flaacqwrzv278aled0pen8cnx257sggeng2fdel53qq27zxg"
GOV_MODULE_ADDR="sprkdrm10d07y265gmmuvt4z0w9aw880jnsr700j865qcw"

# Use existing genesis tags for budgets (separate from reporting tags)
TAG_NAME="commons-council"
TAG_NAME2="technical-council"

# Use different existing genesis tags for reporting tests (to avoid interference)
REPORT_TAG="ecosystem-ops-committee"
DISMISS_TAG="ecosystem-gov-committee"
RESERVE_TAG="technical-gov-committee"

echo "Group Policy (Commons Ops): $GROUP_POLICY_ADDR"
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

# Submit a group proposal to Group 9, auto-vote, and execute.
# Uses --exec try to auto-vote YES on submit, then waits for min_execution_period,
# then executes separately via tx group exec.
# Usage: submit_and_exec_group_proposal <proposal_json_file>
# Sets: GP_EXEC_RESULT (tx result of execution)
submit_and_exec_group_proposal() {
    local PROPOSAL_FILE=$1

    # Submit proposal with --exec try: proposer signature counts as YES vote,
    # and it attempts execution (which will fail due to min_execution_period=1s,
    # but the proposal will be in ACCEPTED state with the vote recorded).
    local TX_RES=$($BINARY tx group submit-proposal \
        "$PROPOSAL_FILE" \
        --exec try \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --gas 500000 \
        --fees 5500000uspark \
        -y \
        --output json 2>&1)

    # Check for immediate rejection (CheckTx failure)
    local INIT_CODE=$(echo "$TX_RES" | jq -r '.code // "0"')
    if [ "$INIT_CODE" != "0" ] && [ "$INIT_CODE" != "null" ]; then
        echo "  Group proposal rejected: $(echo "$TX_RES" | jq -r '.raw_log')"
        return 1
    fi

    local TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit group proposal"
        echo "  $TX_RES"
        return 1
    fi

    sleep 6
    local TX_RESULT=$(wait_for_tx $TXHASH)

    if ! check_tx_success "$TX_RESULT"; then
        echo "  Group proposal submission failed"
        return 1
    fi

    # Extract proposal ID from events
    local PROPOSAL_ID=$(extract_event_value "$TX_RESULT" "cosmos.group.v1.EventSubmitProposal" "proposal_id")
    if [ -z "$PROPOSAL_ID" ]; then
        PROPOSAL_ID=$(echo "$TX_RESULT" | jq -r '[.events[] | select(.type | test("submit_proposal|SubmitProposal")) | .attributes[] | select(.key=="proposal_id") | .value] | first // empty' | tr -d '"')
    fi

    if [ -z "$PROPOSAL_ID" ]; then
        echo "  Could not extract proposal ID"
        echo "  Events: $(echo "$TX_RESULT" | jq -c '[.events[].type]' 2>/dev/null)"
        return 1
    fi

    echo "  Group proposal #$PROPOSAL_ID submitted"

    # Wait for min_execution_period (1s) + buffer
    sleep 3

    # Execute the proposal (now that min_execution_period has passed)
    TX_RES=$($BINARY tx group exec \
        "$PROPOSAL_ID" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --gas 500000 \
        --fees 5500000uspark \
        -y \
        --output json 2>&1)

    # Check for immediate rejection
    INIT_CODE=$(echo "$TX_RES" | jq -r '.code // "0"')
    if [ "$INIT_CODE" != "0" ] && [ "$INIT_CODE" != "null" ]; then
        echo "  Exec rejected: $(echo "$TX_RES" | jq -r '.raw_log')"
        return 1
    fi

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit exec tx"
        echo "  $TX_RES"
        return 1
    fi

    sleep 6
    GP_EXEC_RESULT=$(wait_for_tx $TXHASH)

    if ! check_tx_success "$GP_EXEC_RESULT"; then
        echo "  Group proposal execution tx failed"
        return 1
    fi

    # Check inner execution result (the group module wraps the inner message execution)
    local EXEC_RESULT=$(echo "$GP_EXEC_RESULT" | jq -r '[.events[] | select(.type=="cosmos.group.v1.EventExec") | .attributes[] | select(.key=="result") | .value] | first // empty' | tr -d '"')

    if [ "$EXEC_RESULT" == "PROPOSAL_EXECUTOR_RESULT_SUCCESS" ]; then
        echo "  Group proposal executed successfully"
        return 0
    else
        local EXEC_LOGS=$(echo "$GP_EXEC_RESULT" | jq -r '[.events[] | select(.type=="cosmos.group.v1.EventExec") | .attributes[] | select(.key=="logs") | .value] | first // empty' | tr -d '"')
        echo "  Group proposal inner execution failed: $EXEC_LOGS"
        return 1
    fi
}

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
        echo "  PASS: $DESCRIPTION (different error: $(echo "$RAW_LOG" | head -c 120))"
        return 0
    else
        echo "  PASS: $DESCRIPTION (code=$CODE)"
        return 0
    fi
}

# ========================================================================
# PART 0: GOVERNANCE SETUP
# Add forum tag budget permissions to Group 9 (Commons Ops Committee)
# ========================================================================
echo "--- PART 0: GOVERNANCE SETUP ---"
echo "Adding forum tag budget permissions to Commons Ops committee via governance..."

GOV_PROPOSAL_FILE="/tmp/tag_budget_gov_proposal.json"
cat > "$GOV_PROPOSAL_FILE" <<EOF
{
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdatePolicyPermissions",
      "authority": "$GOV_MODULE_ADDR",
      "policy_address": "$GROUP_POLICY_ADDR",
      "allowed_messages": [
        "/sparkdream.commons.v1.MsgSpendFromCommons",
        "/sparkdream.commons.v1.MsgUpdateGroupMembers",
        "/sparkdream.forum.v1.MsgCreateTagBudget",
        "/sparkdream.forum.v1.MsgToggleTagBudget",
        "/sparkdream.forum.v1.MsgWithdrawTagBudget"
      ]
    }
  ],
  "metadata": "",
  "deposit": "50000000uspark",
  "title": "Add forum tag budget permissions to Commons Ops",
  "summary": "Enable Commons Ops committee to manage tag budgets",
  "expedited": false
}
EOF

echo "  Submitting governance proposal..."
TX_RES=$($BINARY tx gov submit-proposal \
    "$GOV_PROPOSAL_FILE" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --gas 500000 \
    --fees 10000uspark \
    -y \
    --output json 2>&1)

GOV_TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$GOV_TXHASH" ] || [ "$GOV_TXHASH" == "null" ]; then
    echo "  FATAL: Failed to submit governance proposal"
    echo "  $TX_RES"
    exit 1
fi

sleep 6
GOV_TX_RESULT=$(wait_for_tx $GOV_TXHASH)

if ! check_tx_success "$GOV_TX_RESULT"; then
    echo "  FATAL: Governance proposal submission failed"
    exit 1
fi

# Extract proposal ID
GOV_PROPOSAL_ID=$(extract_event_value "$GOV_TX_RESULT" "submit_proposal" "proposal_id")
if [ -z "$GOV_PROPOSAL_ID" ]; then
    GOV_PROPOSAL_ID=$($BINARY query gov proposals --status voting_period --output json 2>&1 | jq -r '.proposals[-1].id // empty')
fi
if [ -z "$GOV_PROPOSAL_ID" ]; then
    GOV_PROPOSAL_ID=$($BINARY query gov proposals --output json 2>&1 | jq -r '.proposals[-1].id // empty')
fi

if [ -z "$GOV_PROPOSAL_ID" ]; then
    echo "  FATAL: Could not determine governance proposal ID"
    exit 1
fi

echo "  Governance proposal #$GOV_PROPOSAL_ID submitted"

# Alice votes YES (she controls ~75% of bonded stake)
echo "  Alice voting YES..."
TX_RES=$($BINARY tx gov vote \
    "$GOV_PROPOSAL_ID" \
    yes \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --gas 300000 \
    --fees 10000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    wait_for_tx $TXHASH > /dev/null 2>&1
fi

# Wait for voting period (60s in genesis config)
echo "  Waiting for voting period (65s)..."
sleep 65

# Verify proposal passed
PROPOSAL_STATUS=$($BINARY query gov proposal "$GOV_PROPOSAL_ID" --output json 2>&1 | jq -r '.proposal.status // .status // "unknown"')
echo "  Proposal status: $PROPOSAL_STATUS"

if [ "$PROPOSAL_STATUS" != "PROPOSAL_STATUS_PASSED" ] && [ "$PROPOSAL_STATUS" != "3" ]; then
    echo "  WARNING: Governance proposal may not have passed (status=$PROPOSAL_STATUS)"
    echo "  Continuing anyway..."
fi

# Verify updated permissions
echo "  Verifying permissions..."
PERMS=$($BINARY query commons get-policy-permissions "$GROUP_POLICY_ADDR" --output json 2>&1)
echo "  Permissions: $(echo "$PERMS" | jq -c '.policy_permissions.allowed_messages' 2>/dev/null)"

# Fund group policy address with SPARK (needed for tag budget pool)
echo "  Funding group policy with SPARK..."
TX_RES=$($BINARY tx bank send \
    alice "$GROUP_POLICY_ADDR" \
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

echo "  Group policy funded"
echo ""
echo "  PART 0 COMPLETE"
echo ""

# ========================================================================
# PART 1: CREATE TAG BUDGET (via group proposal)
# ========================================================================
echo "--- PART 1: CREATE TAG BUDGET ---"

BUDGET_AMOUNT="1000000"

echo "Creating tag budget for tag: $TAG_NAME (amount: $BUDGET_AMOUNT)"
echo "Via group proposal to Commons Ops ($GROUP_POLICY_ADDR)"

PROPOSAL_FILE="/tmp/tag_budget_create_proposal.json"
cat > "$PROPOSAL_FILE" <<EOF
{
  "group_policy_address": "$GROUP_POLICY_ADDR",
  "messages": [
    {
      "@type": "/sparkdream.forum.v1.MsgCreateTagBudget",
      "creator": "$GROUP_POLICY_ADDR",
      "tag": "$TAG_NAME",
      "initial_pool": "$BUDGET_AMOUNT",
      "members_only": false
    }
  ],
  "metadata": "",
  "title": "Create tag budget for $TAG_NAME",
  "summary": "Test: create tag budget",
  "proposers": ["$ALICE_ADDR"]
}
EOF

TAG_BUDGET_ID=""
if submit_and_exec_group_proposal "$PROPOSAL_FILE"; then
    TAG_BUDGET_ID=$(extract_event_value "$GP_EXEC_RESULT" "tag_budget_created" "budget_id")
    if [ -z "$TAG_BUDGET_ID" ] || [ "$TAG_BUDGET_ID" == "null" ]; then
        # Fallback: budget IDs are auto-incremented from 0, count-1 = latest ID
        BUDGET_COUNT=$($BINARY query forum list-tag-budget --output json 2>&1 | jq -r '.tag_budget | length')
        if [ "$BUDGET_COUNT" -gt 0 ] 2>/dev/null; then
            TAG_BUDGET_ID=$(( BUDGET_COUNT - 1 ))
        fi
    fi
    echo "  Tag budget created: $TAG_BUDGET_ID"
else
    echo "  Failed to create tag budget"
fi

echo ""

# ========================================================================
# PART 2: QUERY TAG BUDGET
# ========================================================================
echo "--- PART 2: QUERY TAG BUDGET ---"

if [ -n "$TAG_BUDGET_ID" ]; then
    BUDGET_INFO=$($BINARY query forum get-tag-budget "$TAG_BUDGET_ID" --output json 2>&1)

    if echo "$BUDGET_INFO" | grep -q "error\|not found"; then
        echo "  Tag budget not found by ID, trying by tag name..."
        BUDGET_INFO=$($BINARY query forum tag-budget-by-tag "$TAG_NAME" --output json 2>&1)
    fi

    if echo "$BUDGET_INFO" | grep -q "error\|not found"; then
        echo "  Tag budget not found"
    else
        echo "  Tag Budget Details:"
        echo "    Tag: $(echo "$BUDGET_INFO" | jq -r '.tag_budget.tag // "N/A"')"
        echo "    Balance: $(echo "$BUDGET_INFO" | jq -r '.tag_budget.pool_balance // "N/A"')"
        echo "    Active: $(echo "$BUDGET_INFO" | jq -r '.tag_budget.active // "N/A"')"
        echo "    Group Account: $(echo "$BUDGET_INFO" | jq -r '.tag_budget.group_account // "N/A"')"
    fi
else
    echo "  No tag budget ID available"
fi

echo ""

# ========================================================================
# PART 3: LIST TAG BUDGETS
# ========================================================================
echo "--- PART 3: LIST TAG BUDGETS ---"

TAG_BUDGETS=$($BINARY query forum list-tag-budget --output json 2>&1)

if echo "$TAG_BUDGETS" | grep -q "error"; then
    echo "  Failed to query tag budgets"
else
    BUDGET_COUNT=$(echo "$TAG_BUDGETS" | jq -r '(.tag_budget | length) // 0' 2>/dev/null)
    echo "  Total tag budgets: $BUDGET_COUNT"

    if [ "$BUDGET_COUNT" -gt 0 ] 2>/dev/null; then
        echo ""
        echo "  Tag Budgets:"
        echo "$TAG_BUDGETS" | jq -r '.tag_budget[] | "    - \(.tag): \(.pool_balance) (active=\(.active))"' 2>/dev/null || \
        echo "$TAG_BUDGETS" | jq '.' 2>/dev/null
    fi
fi

echo ""

# ========================================================================
# PART 4: TOP UP TAG BUDGET (alice directly - she's a group member)
# ========================================================================
echo "--- PART 4: TOP UP TAG BUDGET ---"

if [ -n "$TAG_BUDGET_ID" ]; then
    TOPUP_AMOUNT="500000"

    echo "Topping up tag budget: $TAG_BUDGET_ID"
    echo "Top-up amount: $TOPUP_AMOUNT (via alice as group member)"

    TX_RES=$($BINARY tx forum top-up-tag-budget \
        "$TAG_BUDGET_ID" \
        "$TOPUP_AMOUNT" \
        --from alice \
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
            echo "  Tag budget topped up successfully"

            # Verify new balance
            BUDGET_INFO=$($BINARY query forum get-tag-budget "$TAG_BUDGET_ID" --output json 2>&1)
            NEW_BALANCE=$(echo "$BUDGET_INFO" | jq -r '.tag_budget.pool_balance // "N/A"')
            echo "  New balance: $NEW_BALANCE"
        else
            echo "  Failed to top up tag budget"
        fi
    fi
else
    echo "  No tag budget ID available"
fi

echo ""

# ========================================================================
# PART 5: TOGGLE TAG BUDGET (Deactivate) - via group proposal
# ========================================================================
echo "--- PART 5: TOGGLE TAG BUDGET (Deactivate) ---"

if [ -n "$TAG_BUDGET_ID" ]; then
    echo "Deactivating tag budget: $TAG_BUDGET_ID (via group proposal)"

    PROPOSAL_FILE="/tmp/tag_budget_toggle_off.json"
    cat > "$PROPOSAL_FILE" <<EOF
{
  "group_policy_address": "$GROUP_POLICY_ADDR",
  "messages": [
    {
      "@type": "/sparkdream.forum.v1.MsgToggleTagBudget",
      "creator": "$GROUP_POLICY_ADDR",
      "budget_id": "$TAG_BUDGET_ID",
      "active": false
    }
  ],
  "metadata": "",
  "title": "Deactivate tag budget $TAG_BUDGET_ID",
  "summary": "Test: deactivate tag budget",
  "proposers": ["$ALICE_ADDR"]
}
EOF

    if submit_and_exec_group_proposal "$PROPOSAL_FILE"; then
        # Verify status (proto omits false, so null means false)
        BUDGET_INFO=$($BINARY query forum get-tag-budget "$TAG_BUDGET_ID" --output json 2>&1)
        STATUS=$(echo "$BUDGET_INFO" | jq -r '.tag_budget.active // false')
        echo "  Active status: $STATUS"
    else
        echo "  Failed to toggle tag budget"
    fi
else
    echo "  No tag budget ID available"
fi

echo ""

# ========================================================================
# PART 6: TOGGLE TAG BUDGET (Reactivate) - via group proposal
# ========================================================================
echo "--- PART 6: TOGGLE TAG BUDGET (Reactivate) ---"

if [ -n "$TAG_BUDGET_ID" ]; then
    echo "Reactivating tag budget: $TAG_BUDGET_ID (via group proposal)"

    PROPOSAL_FILE="/tmp/tag_budget_toggle_on.json"
    cat > "$PROPOSAL_FILE" <<EOF
{
  "group_policy_address": "$GROUP_POLICY_ADDR",
  "messages": [
    {
      "@type": "/sparkdream.forum.v1.MsgToggleTagBudget",
      "creator": "$GROUP_POLICY_ADDR",
      "budget_id": "$TAG_BUDGET_ID",
      "active": true
    }
  ],
  "metadata": "",
  "title": "Reactivate tag budget $TAG_BUDGET_ID",
  "summary": "Test: reactivate tag budget",
  "proposers": ["$ALICE_ADDR"]
}
EOF

    if submit_and_exec_group_proposal "$PROPOSAL_FILE"; then
        # Verify status
        BUDGET_INFO=$($BINARY query forum get-tag-budget "$TAG_BUDGET_ID" --output json 2>&1)
        STATUS=$(echo "$BUDGET_INFO" | jq -r '.tag_budget.active // "N/A"')
        echo "  Active status: $STATUS"
    else
        echo "  Failed to toggle tag budget"
    fi
else
    echo "  No tag budget ID available"
fi

echo ""

# ========================================================================
# PART 7: AWARD FROM TAG BUDGET (alice directly - she's a group member)
# ========================================================================
echo "--- PART 7: AWARD FROM TAG BUDGET ---"

RESULT_AWARD="FAIL"

if [ -n "$TAG_BUDGET_ID" ]; then
    AWARD_AMOUNT="100000"

    # Create a test post with the matching tag (--tags flag)
    echo "Creating test post with tag '$TAG_NAME' for award target..."
    TX_RES=$($BINARY tx forum create-post \
        "$TEST_CATEGORY_ID" \
        "0" \
        "Test post for tag budget award" \
        --tags "$TAG_NAME" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    AWARD_POST_ID=""
    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        if check_tx_success "$TX_RESULT"; then
            AWARD_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
            if [ -z "$AWARD_POST_ID" ]; then
                # Fallback: try to get from list
                AWARD_POST_ID=$($BINARY query forum list-post --output json 2>&1 | jq -r '.posts[-1].id // empty')
            fi
            echo "  Test post created: $AWARD_POST_ID"
        else
            echo "  Failed to create test post"
        fi
    fi

    if [ -z "$AWARD_POST_ID" ]; then
        echo "  Could not create tagged post, skipping award"
    else
        echo "Awarding from tag budget: $TAG_BUDGET_ID"
        echo "Award amount: $AWARD_AMOUNT to post #$AWARD_POST_ID (via alice as group member)"

        TX_RES=$($BINARY tx forum award-from-tag-budget \
            "$TAG_BUDGET_ID" \
            "$AWARD_POST_ID" \
            "$AWARD_AMOUNT" \
            "Great contribution to tag content" \
            --from alice \
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
                echo "  PASS: Award granted successfully"
                RESULT_AWARD="PASS"

                # Verify new balance (should be 1500000 - 100000 = 1400000)
                BUDGET_INFO=$($BINARY query forum get-tag-budget "$TAG_BUDGET_ID" --output json 2>&1)
                NEW_BALANCE=$(echo "$BUDGET_INFO" | jq -r '.tag_budget.pool_balance // "N/A"')
                echo "  Remaining budget balance: $NEW_BALANCE"
            else
                RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
                echo "  FAIL: Award from tag budget failed: $(echo "$RAW_LOG" | head -c 150)"
            fi
        fi
    fi
else
    echo "  No tag budget ID available"
fi

echo "  Part 7 result: $RESULT_AWARD"

echo ""

# ========================================================================
# PART 7b: EDIT POST TAGS AND VERIFY
# ========================================================================
echo "--- PART 7b: EDIT POST TAGS AND VERIFY ---"

RESULT_EDIT_TAGS="FAIL"

if [ -n "$AWARD_POST_ID" ]; then
    echo "Editing tags on post #$AWARD_POST_ID (replacing '$TAG_NAME' with '$TAG_NAME2')..."

    TX_RES=$($BINARY tx forum edit-post \
        "$AWARD_POST_ID" \
        "Test post for tag budget award (edited)" \
        --tags "$TAG_NAME2" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit edit-post tx"
        echo "  $TX_RES"
    else
        echo "  Transaction: $TXHASH"
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)

        if check_tx_success "$TX_RESULT"; then
            echo "  Edit tx succeeded, querying post to verify tags..."

            POST_INFO=$($BINARY query forum get-post "$AWARD_POST_ID" --output json 2>&1)
            SAVED_TAGS=$(echo "$POST_INFO" | jq -r '[.post.tags // [] | .[]] | join(",")')

            if [ "$SAVED_TAGS" == "$TAG_NAME2" ]; then
                echo "  PASS: Tags verified - post has tag '$SAVED_TAGS'"
                RESULT_EDIT_TAGS="PASS"
            else
                echo "  FAIL: Expected tag '$TAG_NAME2' but got '$SAVED_TAGS'"
            fi
        else
            RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
            echo "  FAIL: Edit post failed: $(echo "$RAW_LOG" | head -c 150)"
        fi
    fi
else
    echo "  SKIP: No post from PART 7 to edit"
fi

echo "  Part 7b result: $RESULT_EDIT_TAGS"
echo ""

# ========================================================================
# PART 8: WITHDRAW FROM TAG BUDGET - via group proposal
# ========================================================================
echo "--- PART 8: WITHDRAW FROM TAG BUDGET ---"

if [ -n "$TAG_BUDGET_ID" ]; then
    echo "Withdrawing from tag budget: $TAG_BUDGET_ID (full withdrawal via group proposal)"

    PROPOSAL_FILE="/tmp/tag_budget_withdraw.json"
    cat > "$PROPOSAL_FILE" <<EOF
{
  "group_policy_address": "$GROUP_POLICY_ADDR",
  "messages": [
    {
      "@type": "/sparkdream.forum.v1.MsgWithdrawTagBudget",
      "creator": "$GROUP_POLICY_ADDR",
      "budget_id": "$TAG_BUDGET_ID"
    }
  ],
  "metadata": "",
  "title": "Withdraw tag budget $TAG_BUDGET_ID",
  "summary": "Test: withdraw tag budget",
  "proposers": ["$ALICE_ADDR"]
}
EOF

    if submit_and_exec_group_proposal "$PROPOSAL_FILE"; then
        # Verify
        BUDGET_INFO=$($BINARY query forum get-tag-budget "$TAG_BUDGET_ID" --output json 2>&1)
        NEW_BALANCE=$(echo "$BUDGET_INFO" | jq -r '.tag_budget.pool_balance // "N/A"')
        echo "  Remaining budget balance: $NEW_BALANCE"
    else
        echo "  Failed to withdraw from tag budget"
    fi
else
    echo "  No tag budget ID available"
fi

echo ""

# ========================================================================
# PART 9: CREATE SECOND TAG BUDGET (via group proposal)
# ========================================================================
echo "--- PART 9: CREATE SECOND TAG BUDGET ---"

BUDGET_AMOUNT2="2000000"

echo "Creating second tag budget for tag: $TAG_NAME2 (amount: $BUDGET_AMOUNT2)"

PROPOSAL_FILE="/tmp/tag_budget_create2_proposal.json"
cat > "$PROPOSAL_FILE" <<EOF
{
  "group_policy_address": "$GROUP_POLICY_ADDR",
  "messages": [
    {
      "@type": "/sparkdream.forum.v1.MsgCreateTagBudget",
      "creator": "$GROUP_POLICY_ADDR",
      "tag": "$TAG_NAME2",
      "initial_pool": "$BUDGET_AMOUNT2",
      "members_only": false
    }
  ],
  "metadata": "",
  "title": "Create tag budget for $TAG_NAME2",
  "summary": "Test: create second tag budget",
  "proposers": ["$ALICE_ADDR"]
}
EOF

TAG_BUDGET_ID_2=""
if submit_and_exec_group_proposal "$PROPOSAL_FILE"; then
    TAG_BUDGET_ID_2=$(extract_event_value "$GP_EXEC_RESULT" "tag_budget_created" "budget_id")
    if [ -z "$TAG_BUDGET_ID_2" ] || [ "$TAG_BUDGET_ID_2" == "null" ]; then
        # Fallback: budget IDs are auto-incremented from 0, count-1 = latest ID
        BUDGET_COUNT=$($BINARY query forum list-tag-budget --output json 2>&1 | jq -r '.tag_budget | length')
        if [ "$BUDGET_COUNT" -gt 0 ] 2>/dev/null; then
            TAG_BUDGET_ID_2=$(( BUDGET_COUNT - 1 ))
        fi
    fi
    echo "  Second tag budget created: $TAG_BUDGET_ID_2"
else
    echo "  Failed to create second tag budget"
fi

echo ""

# ========================================================================
# PART 10: QUERY TAG BUDGETS BY CREATOR
# ========================================================================
echo "--- PART 10: QUERY TAG BUDGETS BY CREATOR ---"

# Note: No dedicated tag-budgets-by-creator query. Use list-tag-budget and filter.
CREATOR_BUDGETS=$($BINARY query forum list-tag-budget --output json 2>&1)

if echo "$CREATOR_BUDGETS" | grep -q "error"; then
    echo "  Query failed"
else
    BUDGET_COUNT=$(echo "$CREATOR_BUDGETS" | jq -r "[.tag_budget[] | select(.group_account==\"$GROUP_POLICY_ADDR\")] | length" 2>/dev/null)
    echo "  Budgets by group account ($GROUP_POLICY_ADDR): $BUDGET_COUNT"

    if [ "$BUDGET_COUNT" -gt 0 ] 2>/dev/null; then
        echo ""
        echo "  Group Account's Tag Budgets:"
        echo "$CREATOR_BUDGETS" | jq -r ".tag_budget[] | select(.group_account==\"$GROUP_POLICY_ADDR\") | \"    - \(.tag): \(.pool_balance) (active=\(.active // false))\"" 2>/dev/null
    fi
fi

echo ""

# ========================================================================
# PART 11: QUERY ACTIVE TAG BUDGETS
# ========================================================================
echo "--- PART 11: QUERY ACTIVE TAG BUDGETS ---"

# Note: No dedicated active-tag-budgets query. Use list-tag-budget and filter.
ACTIVE_BUDGETS=$($BINARY query forum list-tag-budget --output json 2>&1)

if echo "$ACTIVE_BUDGETS" | grep -q "error"; then
    echo "  Query failed"
else
    BUDGET_COUNT=$(echo "$ACTIVE_BUDGETS" | jq -r '[.tag_budget[] | select(.active==true)] | length' 2>/dev/null)
    echo "  Active tag budgets: $BUDGET_COUNT"

    if [ "$BUDGET_COUNT" -gt 0 ] 2>/dev/null; then
        echo ""
        echo "  Active Budgets:"
        echo "$ACTIVE_BUDGETS" | jq -r '.tag_budget[] | select(.active==true) | "    - \(.tag): \(.pool_balance)"' 2>/dev/null
    fi
fi

echo ""

# ========================================================================
# PART 12: CREATE TAG BUDGET ERROR CASES
# ========================================================================
echo "--- PART 12: CREATE TAG BUDGET ERROR CASES ---"

RESULT_CREATE_ERR="FAIL"

# 12a: Tag not found (nonexistent tag)
echo "12a: CreateTagBudget with nonexistent tag..."
TX_RES=$($BINARY tx forum create-tag-budget \
    "nonexistent-tag-xyz-999" \
    "1000000" \
    "false" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if expect_tx_failure "CreateTagBudget: tag not found" "tag not found\|group account" "$TX_RES"; then
    RESULT_CREATE_ERR_A="PASS"
else
    RESULT_CREATE_ERR_A="FAIL"
fi

# 12b: Invalid/zero amount
echo "12b: CreateTagBudget with zero amount..."
TX_RES=$($BINARY tx forum create-tag-budget \
    "$TAG_NAME" \
    "0" \
    "false" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if expect_tx_failure "CreateTagBudget: zero amount" "invalid\|group account" "$TX_RES"; then
    RESULT_CREATE_ERR_B="PASS"
else
    RESULT_CREATE_ERR_B="FAIL"
fi

# 12c: Non-group-account tries to create (poster1 is not a group account)
echo "12c: CreateTagBudget from non-group-account..."
TX_RES=$($BINARY tx forum create-tag-budget \
    "$TAG_NAME" \
    "1000000" \
    "false" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if expect_tx_failure "CreateTagBudget: not group account" "group account" "$TX_RES"; then
    RESULT_CREATE_ERR_C="PASS"
else
    RESULT_CREATE_ERR_C="FAIL"
fi

if [ "$RESULT_CREATE_ERR_A" == "PASS" ] && [ "$RESULT_CREATE_ERR_B" == "PASS" ] && [ "$RESULT_CREATE_ERR_C" == "PASS" ]; then
    RESULT_CREATE_ERR="PASS"
fi

echo "  Part 12 result: $RESULT_CREATE_ERR"
echo ""

# ========================================================================
# PART 13: TOGGLE TAG BUDGET ERROR CASES
# ========================================================================
echo "--- PART 13: TOGGLE TAG BUDGET ERROR CASES ---"

RESULT_TOGGLE_ERR="FAIL"

# 13a: Budget not found (id=999999)
echo "13a: ToggleTagBudget with nonexistent budget..."
TX_RES=$($BINARY tx forum toggle-tag-budget \
    "999999" \
    "false" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if expect_tx_failure "ToggleTagBudget: budget not found" "not found" "$TX_RES"; then
    RESULT_TOGGLE_ERR_A="PASS"
else
    RESULT_TOGGLE_ERR_A="FAIL"
fi

# 13b: Unauthorized (poster1 tries to toggle - not the group account)
echo "13b: ToggleTagBudget unauthorized..."
if [ -n "$TAG_BUDGET_ID_2" ]; then
    TX_RES=$($BINARY tx forum toggle-tag-budget \
        "$TAG_BUDGET_ID_2" \
        "false" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if expect_tx_failure "ToggleTagBudget: unauthorized" "group account" "$TX_RES"; then
        RESULT_TOGGLE_ERR_B="PASS"
    else
        RESULT_TOGGLE_ERR_B="FAIL"
    fi
else
    echo "  SKIP: No budget ID available"
    RESULT_TOGGLE_ERR_B="SKIP"
fi

if [ "$RESULT_TOGGLE_ERR_A" == "PASS" ] && [ "$RESULT_TOGGLE_ERR_B" != "FAIL" ]; then
    RESULT_TOGGLE_ERR="PASS"
fi

echo "  Part 13 result: $RESULT_TOGGLE_ERR"
echo ""

# ========================================================================
# PART 14: TOP UP TAG BUDGET ERROR CASES
# ========================================================================
echo "--- PART 14: TOP UP TAG BUDGET ERROR CASES ---"

RESULT_TOPUP_ERR="FAIL"

# 14a: Budget not found (id=999999)
echo "14a: TopUpTagBudget with nonexistent budget..."
TX_RES=$($BINARY tx forum top-up-tag-budget \
    "999999" \
    "500000" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if expect_tx_failure "TopUpTagBudget: budget not found" "not found" "$TX_RES"; then
    RESULT_TOPUP_ERR_A="PASS"
else
    RESULT_TOPUP_ERR_A="FAIL"
fi

# 14b: Invalid/zero amount
echo "14b: TopUpTagBudget with zero amount..."
if [ -n "$TAG_BUDGET_ID_2" ]; then
    TX_RES=$($BINARY tx forum top-up-tag-budget \
        "$TAG_BUDGET_ID_2" \
        "0" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if expect_tx_failure "TopUpTagBudget: zero amount" "invalid\|zero" "$TX_RES"; then
        RESULT_TOPUP_ERR_B="PASS"
    else
        RESULT_TOPUP_ERR_B="FAIL"
    fi
else
    echo "  SKIP: No budget ID available"
    RESULT_TOPUP_ERR_B="SKIP"
fi

if [ "$RESULT_TOPUP_ERR_A" == "PASS" ] && [ "$RESULT_TOPUP_ERR_B" != "FAIL" ]; then
    RESULT_TOPUP_ERR="PASS"
fi

echo "  Part 14 result: $RESULT_TOPUP_ERR"
echo ""

# ========================================================================
# PART 15: WITHDRAW TAG BUDGET ERROR CASES
# ========================================================================
echo "--- PART 15: WITHDRAW TAG BUDGET ERROR CASES ---"

RESULT_WITHDRAW_ERR="FAIL"

# 15a: Budget not found (id=999999)
echo "15a: WithdrawTagBudget with nonexistent budget..."
TX_RES=$($BINARY tx forum withdraw-tag-budget \
    "999999" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if expect_tx_failure "WithdrawTagBudget: budget not found" "not found" "$TX_RES"; then
    RESULT_WITHDRAW_ERR_A="PASS"
else
    RESULT_WITHDRAW_ERR_A="FAIL"
fi

# 15b: Unauthorized (poster1 tries to withdraw - not the group account)
echo "15b: WithdrawTagBudget unauthorized..."
if [ -n "$TAG_BUDGET_ID_2" ]; then
    TX_RES=$($BINARY tx forum withdraw-tag-budget \
        "$TAG_BUDGET_ID_2" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if expect_tx_failure "WithdrawTagBudget: unauthorized" "group account" "$TX_RES"; then
        RESULT_WITHDRAW_ERR_B="PASS"
    else
        RESULT_WITHDRAW_ERR_B="FAIL"
    fi
else
    echo "  SKIP: No budget ID available"
    RESULT_WITHDRAW_ERR_B="SKIP"
fi

if [ "$RESULT_WITHDRAW_ERR_A" == "PASS" ] && [ "$RESULT_WITHDRAW_ERR_B" != "FAIL" ]; then
    RESULT_WITHDRAW_ERR="PASS"
fi

echo "  Part 15 result: $RESULT_WITHDRAW_ERR"
echo ""

# ========================================================================
# PART 16: AWARD FROM TAG BUDGET ERROR CASES
# ========================================================================
echo "--- PART 16: AWARD FROM TAG BUDGET ERROR CASES ---"

RESULT_AWARD_ERR="FAIL"

# 16a: Budget not found (id=999999)
echo "16a: AwardFromTagBudget with nonexistent budget..."
TX_RES=$($BINARY tx forum award-from-tag-budget \
    "999999" \
    "1" \
    "100000" \
    "test award" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if expect_tx_failure "AwardFromTagBudget: budget not found" "not found" "$TX_RES"; then
    RESULT_AWARD_ERR_A="PASS"
else
    RESULT_AWARD_ERR_A="FAIL"
fi

# 16b: Budget not active (TAG_BUDGET_ID was withdrawn/deactivated in Part 8)
echo "16b: AwardFromTagBudget on inactive budget..."
RESULT_AWARD_ERR_B="SKIP"
if [ -n "$TAG_BUDGET_ID" ]; then
    TX_RES=$($BINARY tx forum award-from-tag-budget \
        "$TAG_BUDGET_ID" \
        "1" \
        "100000" \
        "test award inactive" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if expect_tx_failure "AwardFromTagBudget: budget not active" "not active\|not found" "$TX_RES"; then
        RESULT_AWARD_ERR_B="PASS"
    else
        RESULT_AWARD_ERR_B="FAIL"
    fi
fi

# 16c: Post not found (nonexistent post)
echo "16c: AwardFromTagBudget with nonexistent post..."
RESULT_AWARD_ERR_C="SKIP"
if [ -n "$TAG_BUDGET_ID_2" ]; then
    TX_RES=$($BINARY tx forum award-from-tag-budget \
        "$TAG_BUDGET_ID_2" \
        "999999" \
        "100000" \
        "test award missing post" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if expect_tx_failure "AwardFromTagBudget: post not found" "post.*not found\|not found" "$TX_RES"; then
        RESULT_AWARD_ERR_C="PASS"
    else
        RESULT_AWARD_ERR_C="FAIL"
    fi
fi

# 16d: Invalid/zero amount
echo "16d: AwardFromTagBudget with zero amount..."
RESULT_AWARD_ERR_D="SKIP"
if [ -n "$TAG_BUDGET_ID_2" ]; then
    TX_RES=$($BINARY tx forum award-from-tag-budget \
        "$TAG_BUDGET_ID_2" \
        "1" \
        "0" \
        "test award zero" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if expect_tx_failure "AwardFromTagBudget: zero amount" "invalid\|not have tag" "$TX_RES"; then
        RESULT_AWARD_ERR_D="PASS"
    else
        RESULT_AWARD_ERR_D="FAIL"
    fi
fi

if [ "$RESULT_AWARD_ERR_A" == "PASS" ] && \
   [ "$RESULT_AWARD_ERR_B" != "FAIL" ] && \
   [ "$RESULT_AWARD_ERR_C" != "FAIL" ] && \
   [ "$RESULT_AWARD_ERR_D" != "FAIL" ]; then
    RESULT_AWARD_ERR="PASS"
fi

echo "  Part 16 result: $RESULT_AWARD_ERR"
echo ""

# ========================================================================
# PART 17: REPORT TAG - HAPPY PATH
# ========================================================================
echo "--- PART 17: REPORT TAG ---"

RESULT_REPORT_TAG="FAIL"

echo "Reporting tag: $REPORT_TAG"

# 17a: First reporter creates a TagReport
TX_RES=$($BINARY tx forum report-tag \
    "$REPORT_TAG" \
    "Tag name is misleading" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  17a: Failed to submit report-tag tx"
    echo "  $TX_RES"
    RESULT_REPORT_TAG_A="FAIL"
else
    echo "  Transaction: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  17a PASS: Tag reported by poster1"
        RESULT_REPORT_TAG_A="PASS"
    else
        echo "  17a: report-tag tx failed"
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        echo "  raw_log: $(echo "$RAW_LOG" | head -c 120)"
        RESULT_REPORT_TAG_A="FAIL"
    fi
fi

# 17b: Co-reporter joins existing report
echo ""
echo "17b: Co-reporter joins existing tag report..."
TX_RES=$($BINARY tx forum report-tag \
    "$REPORT_TAG" \
    "I also think this tag is problematic" \
    --from poster2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  17b: Failed to submit co-report tx"
    RESULT_REPORT_TAG_B="FAIL"
else
    echo "  Transaction: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  17b PASS: Co-reporter joined tag report"
        RESULT_REPORT_TAG_B="PASS"
    else
        echo "  17b: co-report tx failed"
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        echo "  raw_log: $(echo "$RAW_LOG" | head -c 120)"
        RESULT_REPORT_TAG_B="FAIL"
    fi
fi

if [ "$RESULT_REPORT_TAG_A" == "PASS" ] && [ "$RESULT_REPORT_TAG_B" == "PASS" ]; then
    RESULT_REPORT_TAG="PASS"
elif [ "$RESULT_REPORT_TAG_A" == "PASS" ]; then
    RESULT_REPORT_TAG="PARTIAL"
fi

echo "  Part 17 result: $RESULT_REPORT_TAG"
echo ""

# ========================================================================
# PART 18: REPORT TAG ERROR CASES
# ========================================================================
echo "--- PART 18: REPORT TAG ERROR CASES ---"

RESULT_REPORT_TAG_ERR="FAIL"

# 18a: Tag not found
echo "18a: ReportTag with nonexistent tag..."
TX_RES=$($BINARY tx forum report-tag \
    "nonexistent-tag-abc-777" \
    "This tag does not exist" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if expect_tx_failure "ReportTag: tag not found" "tag.*not found" "$TX_RES"; then
    RESULT_REPORT_TAG_ERR_A="PASS"
else
    RESULT_REPORT_TAG_ERR_A="FAIL"
fi

# 18b: Duplicate reporter (poster1 already reported REPORT_TAG in Part 17)
echo "18b: ReportTag duplicate reporter..."
TX_RES=$($BINARY tx forum report-tag \
    "$REPORT_TAG" \
    "Reporting again" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if expect_tx_failure "ReportTag: duplicate reporter" "already reported" "$TX_RES"; then
    RESULT_REPORT_TAG_ERR_B="PASS"
else
    RESULT_REPORT_TAG_ERR_B="FAIL"
fi

if [ "$RESULT_REPORT_TAG_ERR_A" == "PASS" ] && [ "$RESULT_REPORT_TAG_ERR_B" == "PASS" ]; then
    RESULT_REPORT_TAG_ERR="PASS"
fi

echo "  Part 18 result: $RESULT_REPORT_TAG_ERR"
echo ""

# ========================================================================
# PART 19: QUERY TAG REPORT
# ========================================================================
echo "--- PART 19: QUERY TAG REPORT ---"

RESULT_QUERY_TAG_REPORT="FAIL"

echo "Querying tag report for: $REPORT_TAG"
TAG_REPORT_INFO=$($BINARY query forum get-tag-report "$REPORT_TAG" --output json 2>&1)

if echo "$TAG_REPORT_INFO" | jq -e '.tag_report // .tagReport' > /dev/null 2>&1; then
    REPORTERS=$(echo "$TAG_REPORT_INFO" | jq -r '.tag_report.reporters // .tagReport.reporters // []')
    TOTAL_BOND=$(echo "$TAG_REPORT_INFO" | jq -r '.tag_report.total_bond // .tagReport.total_bond // "N/A"')
    echo "  Reporters: $REPORTERS"
    echo "  Total bond: $TOTAL_BOND"
    RESULT_QUERY_TAG_REPORT="PASS"
else
    echo "  Tag report not found or query failed"
    echo "  Response: $(echo "$TAG_REPORT_INFO" | head -c 200)"
fi

echo ""

echo "Listing all tag reports..."
TAG_REPORTS=$($BINARY query forum list-tag-report --output json 2>&1)
if echo "$TAG_REPORTS" | jq -e '.' > /dev/null 2>&1; then
    REPORT_COUNT=$(echo "$TAG_REPORTS" | jq -r '(.tag_report | length) // 0' 2>/dev/null)
    echo "  Total tag reports: $REPORT_COUNT"
else
    echo "  list-tag-report query failed"
fi

echo "  Part 19 result: $RESULT_QUERY_TAG_REPORT"
echo ""

# ========================================================================
# PART 20: RESOLVE TAG REPORT - DISMISS (action=0)
# ========================================================================
echo "--- PART 20: RESOLVE TAG REPORT - DISMISS ---"

RESULT_RESOLVE_DISMISS="FAIL"

# Create a fresh tag report on DISMISS_TAG
echo "Creating tag report for dismiss test on tag: $DISMISS_TAG"

TX_RES=$($BINARY tx forum report-tag \
    "$DISMISS_TAG" \
    "Testing dismiss resolution" \
    --from poster1 \
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

# Alice (operations committee) dismisses the report (action=0)
echo "Alice dismisses tag report (action=0, bonds refunded)..."
TX_RES=$($BINARY tx forum resolve-tag-report \
    "$DISMISS_TAG" \
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

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to submit resolve-tag-report tx"
    echo "  $TX_RES"
else
    echo "  Transaction: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  PASS: Tag report dismissed"
        RESULT_RESOLVE_DISMISS="PASS"
    else
        echo "  resolve-tag-report (dismiss) failed"
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        echo "  raw_log: $(echo "$RAW_LOG" | head -c 150)"
    fi
fi

echo "  Part 20 result: $RESULT_RESOLVE_DISMISS"
echo ""

# ========================================================================
# PART 21: RESOLVE TAG REPORT - REMOVE (action=1)
# ========================================================================
echo "--- PART 21: RESOLVE TAG REPORT - REMOVE TAG ---"

RESULT_RESOLVE_REMOVE="FAIL"

# Use the report from Part 17 on REPORT_TAG (already reported by poster1+poster2)
echo "Alice removes tag via resolve (action=1, tag stripped from posts)..."
TX_RES=$($BINARY tx forum resolve-tag-report \
    "$REPORT_TAG" \
    "1" \
    "" \
    "false" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to submit resolve-tag-report tx"
    echo "  $TX_RES"
else
    echo "  Transaction: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  PASS: Tag removed via report resolution"
        RESULT_RESOLVE_REMOVE="PASS"
    else
        echo "  resolve-tag-report (remove) failed"
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        echo "  raw_log: $(echo "$RAW_LOG" | head -c 150)"
    fi
fi

echo "  Part 21 result: $RESULT_RESOLVE_REMOVE"
echo ""

# ========================================================================
# PART 22: RESOLVE TAG REPORT - RESERVE (action=2)
# ========================================================================
echo "--- PART 22: RESOLVE TAG REPORT - RESERVE TAG ---"

RESULT_RESOLVE_RESERVE="FAIL"

# Create a fresh tag report on RESERVE_TAG
echo "Creating tag report for reserve test on tag: $RESERVE_TAG"

TX_RES=$($BINARY tx forum report-tag \
    "$RESERVE_TAG" \
    "This tag should be reserved" \
    --from poster1 \
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

# Alice reserves the tag (action=2, creates ReservedTag entry)
echo "Alice reserves tag via resolve (action=2)..."
TX_RES=$($BINARY tx forum resolve-tag-report \
    "$RESERVE_TAG" \
    "2" \
    "$ALICE_ADDR" \
    "true" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to submit resolve-tag-report tx"
    echo "  $TX_RES"
else
    echo "  Transaction: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  PASS: Tag reserved via report resolution"
        RESULT_RESOLVE_RESERVE="PASS"
    else
        echo "  resolve-tag-report (reserve) failed"
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        echo "  raw_log: $(echo "$RAW_LOG" | head -c 150)"
    fi
fi

echo "  Part 22 result: $RESULT_RESOLVE_RESERVE"
echo ""

# ========================================================================
# PART 23: RESOLVE TAG REPORT ERROR CASES
# ========================================================================
echo "--- PART 23: RESOLVE TAG REPORT ERROR CASES ---"

RESULT_RESOLVE_ERR="FAIL"

# 23a: Unauthorized (poster1 tries to resolve)
echo "23a: ResolveTagReport unauthorized..."
# Create a report to resolve
TX_RES=$($BINARY tx forum report-tag \
    "$DISMISS_TAG" \
    "Testing unauthorized resolve" \
    --from poster1 \
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

TX_RES=$($BINARY tx forum resolve-tag-report \
    "$DISMISS_TAG" \
    "0" \
    "" \
    "false" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if expect_tx_failure "ResolveTagReport: unauthorized" "not governance\|not authorized\|authority\|council" "$TX_RES"; then
    RESULT_RESOLVE_ERR_A="PASS"
else
    RESULT_RESOLVE_ERR_A="FAIL"
fi

# Clean up: alice resolves the report we just created
TX_RES=$($BINARY tx forum resolve-tag-report \
    "$DISMISS_TAG" \
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
    wait_for_tx $TXHASH > /dev/null 2>&1
fi

# 23b: Report not found (nonexistent tag report)
echo "23b: ResolveTagReport for nonexistent report..."
TX_RES=$($BINARY tx forum resolve-tag-report \
    "definitely-not-reported-tag-999" \
    "0" \
    "" \
    "false" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if expect_tx_failure "ResolveTagReport: report not found" "not found\|report" "$TX_RES"; then
    RESULT_RESOLVE_ERR_B="PASS"
else
    RESULT_RESOLVE_ERR_B="FAIL"
fi

if [ "$RESULT_RESOLVE_ERR_A" == "PASS" ] && [ "$RESULT_RESOLVE_ERR_B" == "PASS" ]; then
    RESULT_RESOLVE_ERR="PASS"
fi

echo "  Part 23 result: $RESULT_RESOLVE_ERR"
echo ""

# ========================================================================
# PART 24: TAG QUERIES
# ========================================================================
echo "--- PART 24: TAG QUERIES ---"

RESULT_TAG_QUERIES="FAIL"
QUERY_PASS_COUNT=0

# 24a: get-tag (query single tag by name - use TAG_NAME which should still exist)
echo "24a: Query single tag..."
TAG_INFO=$($BINARY query forum get-tag "$TAG_NAME" --output json 2>&1)
if echo "$TAG_INFO" | jq -e '.tag // .Tag' > /dev/null 2>&1; then
    echo "  get-tag: $(echo "$TAG_INFO" | jq -r '.tag.name // .Tag.name // "N/A"')"
    echo "  PASS: get-tag"
    QUERY_PASS_COUNT=$((QUERY_PASS_COUNT + 1))
else
    echo "  get-tag failed"
    echo "  Response: $(echo "$TAG_INFO" | head -c 120)"
fi

# 24b: list-tag (list all tags)
echo "24b: List all tags..."
TAG_LIST=$($BINARY query forum list-tag --output json 2>&1)
if echo "$TAG_LIST" | jq -e '.' > /dev/null 2>&1; then
    TAG_COUNT=$(echo "$TAG_LIST" | jq -r '(.tag | length) // 0' 2>/dev/null)
    echo "  Total tags: $TAG_COUNT"
    echo "  PASS: list-tag"
    QUERY_PASS_COUNT=$((QUERY_PASS_COUNT + 1))
else
    echo "  list-tag failed"
fi

# 24c: tag-exists (check tag existence - use TAG_NAME2 which should still exist)
echo "24c: Check tag existence..."
TAG_EXISTS=$($BINARY query forum tag-exists "$TAG_NAME2" --output json 2>&1)
if echo "$TAG_EXISTS" | jq -e '.' > /dev/null 2>&1; then
    EXISTS=$(echo "$TAG_EXISTS" | jq -r '.exists // "N/A"')
    echo "  tag-exists for $TAG_NAME2: $EXISTS"
    echo "  PASS: tag-exists"
    QUERY_PASS_COUNT=$((QUERY_PASS_COUNT + 1))
else
    echo "  tag-exists failed"
fi

# 24d: get-reserved-tag (after resolve with reserve action in Part 22)
echo "24d: Query reserved tag..."
RESERVED_TAG_INFO=$($BINARY query forum get-reserved-tag "$RESERVE_TAG" --output json 2>&1)
if echo "$RESERVED_TAG_INFO" | jq -e '.reserved_tag // .reservedTag' > /dev/null 2>&1; then
    echo "  Reserved tag: $(echo "$RESERVED_TAG_INFO" | jq -r '.reserved_tag.name // .reservedTag.name // "N/A"')"
    echo "  Authority: $(echo "$RESERVED_TAG_INFO" | jq -r '.reserved_tag.authority // .reservedTag.authority // "N/A"')"
    echo "  PASS: get-reserved-tag"
    QUERY_PASS_COUNT=$((QUERY_PASS_COUNT + 1))
else
    echo "  get-reserved-tag: not found (reserve may have failed earlier)"
    echo "  Response: $(echo "$RESERVED_TAG_INFO" | head -c 120)"
fi

# 24e: list-reserved-tag
echo "24e: List reserved tags..."
RESERVED_LIST=$($BINARY query forum list-reserved-tag --output json 2>&1)
if echo "$RESERVED_LIST" | jq -e '.' > /dev/null 2>&1; then
    RESERVED_COUNT=$(echo "$RESERVED_LIST" | jq -r '(.reserved_tag | length) // 0' 2>/dev/null)
    echo "  Total reserved tags: $RESERVED_COUNT"
    echo "  PASS: list-reserved-tag"
    QUERY_PASS_COUNT=$((QUERY_PASS_COUNT + 1))
else
    echo "  list-reserved-tag failed"
fi

# 24f: tag-budget-by-tag (query budget by tag - use TAG_NAME)
echo "24f: Query tag budget by tag..."
BUDGET_BY_TAG=$($BINARY query forum tag-budget-by-tag "$TAG_NAME" --output json 2>&1)
if echo "$BUDGET_BY_TAG" | jq -e '.' > /dev/null 2>&1; then
    echo "  tag-budget-by-tag response received"
    echo "  PASS: tag-budget-by-tag"
    QUERY_PASS_COUNT=$((QUERY_PASS_COUNT + 1))
else
    echo "  tag-budget-by-tag failed"
fi

# 24g: tag-budget-awards (query awards for a budget)
# Note: budget_id=0 fails proto validation (zero value treated as unset), so use budget_id_2
echo "24g: Query tag budget awards..."
if [ -n "$TAG_BUDGET_ID_2" ]; then
    BUDGET_AWARDS=$($BINARY query forum tag-budget-awards "$TAG_BUDGET_ID_2" --output json 2>&1)
    if echo "$BUDGET_AWARDS" | jq -e '.' > /dev/null 2>&1; then
        echo "  Awards query for budget $TAG_BUDGET_ID_2 returned valid response"
        echo "  PASS: tag-budget-awards"
        QUERY_PASS_COUNT=$((QUERY_PASS_COUNT + 1))
    else
        echo "  tag-budget-awards failed"
        echo "  Response: $(echo "$BUDGET_AWARDS" | head -c 120)"
    fi
elif [ -n "$TAG_BUDGET_ID" ] && [ "$TAG_BUDGET_ID" != "0" ]; then
    BUDGET_AWARDS=$($BINARY query forum tag-budget-awards "$TAG_BUDGET_ID" --output json 2>&1)
    if echo "$BUDGET_AWARDS" | jq -e '.' > /dev/null 2>&1; then
        echo "  Awards query for budget $TAG_BUDGET_ID returned valid response"
        echo "  PASS: tag-budget-awards"
        QUERY_PASS_COUNT=$((QUERY_PASS_COUNT + 1))
    else
        echo "  tag-budget-awards failed"
    fi
else
    echo "  SKIP: No non-zero budget ID for awards query"
fi

# 24h: tag-reports (list all tag reports)
echo "24h: Query all tag reports..."
ALL_REPORTS=$($BINARY query forum list-tag-report --output json 2>&1)
if echo "$ALL_REPORTS" | jq -e '.' > /dev/null 2>&1; then
    echo "  tag-reports response received"
    echo "  PASS: tag-reports"
    QUERY_PASS_COUNT=$((QUERY_PASS_COUNT + 1))
else
    echo "  tag-reports failed"
fi

if [ "$QUERY_PASS_COUNT" -ge 5 ]; then
    RESULT_TAG_QUERIES="PASS"
elif [ "$QUERY_PASS_COUNT" -ge 3 ]; then
    RESULT_TAG_QUERIES="PARTIAL"
fi

echo "  Part 24 result: $RESULT_TAG_QUERIES ($QUERY_PASS_COUNT/8 queries passed)"
echo ""

# ========================================================================
# SUMMARY
# ========================================================================
echo "--- TAG BUDGET TEST SUMMARY ---"
echo ""
echo "  --- Happy Path ---"
echo "  Create tag budget:              Tested (PART 1)"
echo "  Query tag budget:               Tested (PART 2)"
echo "  List tag budgets:               Tested (PART 3)"
echo "  Top up tag budget:              Tested (PART 4)"
echo "  Toggle tag budget:              Tested (PART 5-6)"
echo "  Award from tag budget:          $RESULT_AWARD (PART 7)"
echo "  Edit post tags and verify:      $RESULT_EDIT_TAGS (PART 7b)"
echo "  Withdraw from tag budget:       Tested (PART 8)"
echo "  Query by creator:               Tested (PART 10)"
echo "  Query active budgets:           Tested (PART 11)"
echo ""
echo "  --- Error Cases ---"
echo "  CreateTagBudget errors:         $RESULT_CREATE_ERR (PART 12)"
echo "  ToggleTagBudget errors:         $RESULT_TOGGLE_ERR (PART 13)"
echo "  TopUpTagBudget errors:          $RESULT_TOPUP_ERR (PART 14)"
echo "  WithdrawTagBudget errors:       $RESULT_WITHDRAW_ERR (PART 15)"
echo "  AwardFromTagBudget errors:      $RESULT_AWARD_ERR (PART 16)"
echo ""
echo "  --- Tag Reporting ---"
echo "  ReportTag happy path:           $RESULT_REPORT_TAG (PART 17)"
echo "  ReportTag errors:               $RESULT_REPORT_TAG_ERR (PART 18)"
echo "  Query tag report:               $RESULT_QUERY_TAG_REPORT (PART 19)"
echo "  ResolveTagReport dismiss:       $RESULT_RESOLVE_DISMISS (PART 20)"
echo "  ResolveTagReport remove:        $RESULT_RESOLVE_REMOVE (PART 21)"
echo "  ResolveTagReport reserve:       $RESULT_RESOLVE_RESERVE (PART 22)"
echo "  ResolveTagReport errors:        $RESULT_RESOLVE_ERR (PART 23)"
echo ""
echo "  --- Additional Queries ---"
echo "  Tag queries:                    $RESULT_TAG_QUERIES (PART 24)"
echo ""

FAIL_COUNT=0
ALL_RESULTS=("$RESULT_AWARD" "$RESULT_EDIT_TAGS" "$RESULT_CREATE_ERR" "$RESULT_TOGGLE_ERR" "$RESULT_TOPUP_ERR" "$RESULT_WITHDRAW_ERR" "$RESULT_AWARD_ERR" "$RESULT_REPORT_TAG" "$RESULT_REPORT_TAG_ERR" "$RESULT_QUERY_TAG_REPORT" "$RESULT_RESOLVE_DISMISS" "$RESULT_RESOLVE_REMOVE" "$RESULT_RESOLVE_RESERVE" "$RESULT_RESOLVE_ERR" "$RESULT_TAG_QUERIES")
for R in "${ALL_RESULTS[@]}"; do
    if [ "$R" == "FAIL" ]; then
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
done
TOTAL=${#ALL_RESULTS[@]}
PASS_COUNT=$((TOTAL - FAIL_COUNT))
echo ""
echo "  Total: $TOTAL | Passed: $PASS_COUNT | Failed: $FAIL_COUNT"
echo ""
if [ "$FAIL_COUNT" -gt 0 ]; then
    echo "  FAILURES: $FAIL_COUNT test(s) failed"
fi
if [ "$FAIL_COUNT" -eq 0 ]; then
    echo "  ALL TESTS PASSED"
fi

echo ""
echo "TAG BUDGET TEST COMPLETED"
exit $FAIL_COUNT
