#!/bin/bash

echo "--- TESTING: HIERARCHICAL MEMBER MANAGEMENT (Commons Council) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
FEES="5000000uspark"

# Keys
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
DAVE_ADDR=$($BINARY keys show dave -a --keyring-backend test)

# Ensure Dave exists
if [ -z "$DAVE_ADDR" ]; then
    $BINARY keys add dave --keyring-backend test --output json > /dev/null
    DAVE_ADDR=$($BINARY keys show dave -a --keyring-backend test)
fi

echo "Alice: $ALICE_ADDR"
echo "Dave:  $DAVE_ADDR"

# --- DISCOVERY ---
PARENT_NAME="Commons Council"
CHILD_NAME="Commons Governance Committee"

echo "Discovering Groups..."

# Get Parent Info
PARENT_INFO=$($BINARY query commons get-group "$PARENT_NAME" --output json)
PARENT_POLICY_ADDR=$(echo $PARENT_INFO | jq -r '.group.policy_address')

# Get Child Info
CHILD_INFO=$($BINARY query commons get-group "$CHILD_NAME" --output json)
CHILD_POLICY_ADDR=$(echo $CHILD_INFO | jq -r '.group.policy_address')

echo "Parent ($PARENT_NAME): Policy=$PARENT_POLICY_ADDR"
echo "Child  ($CHILD_NAME):  Policy=$CHILD_POLICY_ADDR"

if [ -z "$PARENT_POLICY_ADDR" ] || [ -z "$CHILD_POLICY_ADDR" ]; then
    echo "FAIL SETUP ERROR: Could not find groups. Run genesis bootstrap first."
    exit 1
fi

# --- 1. FAILURE TEST: UNAUTHORIZED SUBMISSION ---
echo "--- TEST 1: Non-member tries to submit proposal (Should Fail) ---"
# Create an eve account if needed, who is NOT a committee member
if ! $BINARY keys show eve --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add eve --keyring-backend test --output json > /dev/null
fi
EVE_ADDR=$($BINARY keys show eve -a --keyring-backend test)
# Fund Eve so she can pay fees
$BINARY tx bank send alice $EVE_ADDR 5000000uspark --chain-id $CHAIN_ID -y --fees 5000uspark > /dev/null 2>&1
sleep 5

echo '{
  "policy_address": "'$CHILD_POLICY_ADDR'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdateGroupMembers",
      "authority": "'$CHILD_POLICY_ADDR'",
      "group_policy_address": "'$PARENT_POLICY_ADDR'",
      "members_to_add": ["'$DAVE_ADDR'"],
      "weights_to_add": ["1"],
      "members_to_remove": []
    }
  ],
  "metadata": "Eve tries unauthorized submission."
}' > "$PROPOSAL_DIR/fail_unauthorized.json"

FAIL_OUTPUT=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/fail_unauthorized.json" --from eve -y --chain-id $CHAIN_ID --keyring-backend test --fees $FEES --output json 2>&1)

if echo "$FAIL_OUTPUT" | grep -qi "unauthorized\|not a member"; then
    echo "OK SUCCESS: Non-member cannot submit proposals (Unauthorized)."
else
    TX_HASH=$(echo $FAIL_OUTPUT | jq -r '.txhash' 2>/dev/null)
    if [ -n "$TX_HASH" ] && [ "$TX_HASH" != "null" ]; then
        sleep 5
        TX_RES=$($BINARY query tx $TX_HASH --output json 2>/dev/null)
        TX_CODE=$(echo $TX_RES | jq -r '.code')
        if [ "$TX_CODE" != "0" ]; then
            echo "OK SUCCESS: Non-member submission rejected on-chain."
        else
            echo "FAIL: Non-member was able to submit proposal!"
            echo "Raw: $(echo $TX_RES)"
            exit 1
        fi
    else
        echo "OK SUCCESS: Transaction failed as expected."
    fi
fi

# --- 2. SUCCESS TEST: CHILD COMMITTEE PROPOSAL (ADD) ---
echo "--- TEST 2: $CHILD_NAME votes to Add Dave to $PARENT_NAME ---"

# We use the custom wrapper message: /sparkdream.commons.v1.MsgUpdateGroupMembers
echo '{
  "policy_address": "'$CHILD_POLICY_ADDR'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdateGroupMembers",
      "authority": "'$CHILD_POLICY_ADDR'",
      "group_policy_address": "'$PARENT_POLICY_ADDR'",
      "members_to_add": ["'$DAVE_ADDR'"],
      "weights_to_add": ["1"],
      "members_to_remove": []
    }
  ],
  "metadata": "Governance Committee executing bottom-up management."
}' > "$PROPOSAL_DIR/msg_safe_update.json"

echo "Submitting Proposal..."
SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/msg_safe_update.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees $FEES --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 5

# Get Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ]; then
    echo "FAIL: Failed to submit proposal. Raw Log:"
    echo "$TX_RES" | jq -r '.raw_log'
    exit 1
fi
echo "OK Proposal ID: $PROPOSAL_ID"

# Vote (Alice is the only member of the Committee in 'founderMembers' bootstrap)
echo "Alice voting YES..."
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5

echo "Votes cast. Attempting Execution..."

EXEC_RES=$($BINARY tx commons execute-proposal $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 5

# Verify Execution
PROP_STATUS=$($BINARY query commons get-proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')

if [ "$PROP_STATUS" == "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "OK Proposal Executed Successfully."
else
    echo "FAIL EXECUTION FAILURE. Status: $PROP_STATUS"
    FAIL_REASON=$($BINARY query commons get-proposal $PROPOSAL_ID --output json | jq -r '.proposal.failed_reason')
    echo "Reason: $FAIL_REASON"
    exit 1
fi

# --- 3. VERIFY ADDITION ---
echo "--- VERIFYING MEMBER ADDITION ---"

MEMBERS_JSON=$($BINARY query commons get-council-members "$PARENT_NAME" --output json)

if echo "$MEMBERS_JSON" | jq -r '.members[].address' | grep -q "$DAVE_ADDR"; then
    echo "OK SUCCESS: Dave was added to the Commons Council!"
else
    echo "FAIL: Dave was NOT found in the parent group."
    echo "Members: $MEMBERS_JSON"
    exit 1
fi

# --- 4. CLEANUP (REMOVE DAVE) ---
echo ""
echo "--- TEST 3: CLEANUP (Remove Dave from $PARENT_NAME) ---"
echo "Restoring state for future tests..."

echo '{
  "policy_address": "'$CHILD_POLICY_ADDR'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdateGroupMembers",
      "authority": "'$CHILD_POLICY_ADDR'",
      "group_policy_address": "'$PARENT_POLICY_ADDR'",
      "members_to_add": [],
      "weights_to_add": [],
      "members_to_remove": ["'$DAVE_ADDR'"]
    }
  ],
  "metadata": "Restoring council state."
}' > "$PROPOSAL_DIR/msg_cleanup_remove.json"

echo "Submitting Removal Proposal..."
SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/msg_cleanup_remove.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees $FEES --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 5

# Get Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
CLEANUP_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

if [ -z "$CLEANUP_ID" ]; then
    echo "FAIL: Failed to submit cleanup proposal."
    exit 1
fi
echo "OK Cleanup Proposal ID: $CLEANUP_ID"

# Vote
echo "Alice voting YES..."
$BINARY tx commons vote-proposal $CLEANUP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test > /dev/null
sleep 5

# Execute
echo "Executing Cleanup..."
EXEC_RES=$($BINARY tx commons execute-proposal $CLEANUP_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 5

# Verify Removal
MEMBERS_JSON=$($BINARY query commons get-council-members "$PARENT_NAME" --output json)

if echo "$MEMBERS_JSON" | jq -r '.members[].address' | grep -q "$DAVE_ADDR"; then
    echo "FAIL: Dave is STILL in the group. Cleanup failed."
    exit 1
else
    echo "OK SUCCESS: Dave removed. State restored."
fi
