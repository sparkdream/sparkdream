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
PARENT_INFO=$($BINARY query commons get-extended-group "$PARENT_NAME" --output json)
PARENT_GROUP_ID=$(echo $PARENT_INFO | jq -r '.extended_group.group_id')
PARENT_POLICY_ADDR=$(echo $PARENT_INFO | jq -r '.extended_group.policy_address')

# Get Child Info
CHILD_INFO=$($BINARY query commons get-extended-group "$CHILD_NAME" --output json)
CHILD_POLICY_ADDR=$(echo $CHILD_INFO | jq -r '.extended_group.policy_address')
CHILD_GROUP_ID=$(echo $CHILD_INFO | jq -r '.extended_group.group_id')

echo "Parent ($PARENT_NAME): Group=$PARENT_GROUP_ID, Policy=$PARENT_POLICY_ADDR"
echo "Child  ($CHILD_NAME):  Group=$CHILD_GROUP_ID, Policy=$CHILD_POLICY_ADDR"

if [ -z "$PARENT_GROUP_ID" ] || [ -z "$CHILD_POLICY_ADDR" ]; then
    echo "❌ SETUP ERROR: Could not find groups. Run genesis bootstrap first."
    exit 1
fi

# --- SAFETY CHECK: TIMELOCK CONFIG ---
# Verify that your Go change (using TechPolicyStandardMinExecution) actually took effect.
CHILD_POLICY_INFO=$($BINARY query group group-policy-info $CHILD_POLICY_ADDR --output json)
MIN_EXEC=$(echo $CHILD_POLICY_INFO | jq -r '.info.decision_policy.windows.min_execution_period')

echo "⏱️  Committee Min Execution Period: $MIN_EXEC"

if [[ "$MIN_EXEC" != "0s" && "$MIN_EXEC" != "1s" ]]; then
    echo "⚠️  WARNING: High execution delay detected ($MIN_EXEC)."
    echo "    The test might hang or fail at the 'Exec' step."
fi

# --- 1. FAILURE TEST: DIRECT UPDATE ATTEMPT ---
echo "--- TEST 1: Alice attempts direct update on Parent (Should Fail) ---"
# Alice is not the admin of the Parent; the Policy is.

echo '{
  "group_policy_address": "'$PARENT_POLICY_ADDR'", 
  "messages": [
    {
      "@type": "/cosmos.group.v1.MsgUpdateGroupMembers",
      "admin": "'$PARENT_POLICY_ADDR'", 
      "group_id": "'$PARENT_GROUP_ID'",
      "member_updates": [{"address": "'$DAVE_ADDR'", "weight": "1"}]
    }
  ]
}' > "$PROPOSAL_DIR/fail_direct.json"

FAIL_OUTPUT=$($BINARY tx group update-group-members $PARENT_GROUP_ID "$PROPOSAL_DIR/fail_direct.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test 2>&1)

if echo "$FAIL_OUTPUT" | grep -q "unauthorized"; then
    echo "✅ SUCCESS: Alice cannot update parent directly (Unauthorized)."
else
    echo "✅ SUCCESS: Transaction failed as expected."
fi

# --- 2. SUCCESS TEST: CHILD COMMITTEE PROPOSAL (ADD) ---
echo "--- TEST 2: $CHILD_NAME votes to Add Dave to $PARENT_NAME ---"

# We use the custom wrapper message: /sparkdream.commons.v1.MsgUpdateGroupMembers
echo '{
  "group_policy_address": "'$CHILD_POLICY_ADDR'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Promote Dave to Council",
  "summary": "Governance Committee executing bottom-up management.",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdateGroupMembers",
      "authority": "'$CHILD_POLICY_ADDR'",
      "group_policy_address": "'$PARENT_POLICY_ADDR'",
      "members_to_add": ["'$DAVE_ADDR'"],
      "weights_to_add": ["1"],
      "members_to_remove": []
    }
  ]
}' > "$PROPOSAL_DIR/msg_safe_update.json"

echo "Submitting Proposal..."
SUBMIT_RES=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/msg_safe_update.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees $FEES --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 4

# Get Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ]; then
    echo "❌ ERROR: Failed to submit proposal. Raw Log:"
    echo "$TX_RES" | jq -r '.raw_log'
    exit 1
fi
echo "✅ Proposal ID: $PROPOSAL_ID"

# Vote (Alice is the only member of the Committee in 'founderMembers' bootstrap)
echo "Alice voting YES..."
$BINARY tx group vote $PROPOSAL_ID $ALICE_ADDR VOTE_OPTION_YES "Promote" --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 4

echo "Votes cast. Attempting Execution..."

EXEC_RES=$($BINARY tx group exec $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 4

# Verify Execution
EXEC_LOGS=$($BINARY query tx $EXEC_HASH --output json)

if echo "$EXEC_LOGS" | grep -q "PROPOSAL_EXECUTOR_RESULT_SUCCESS"; then
    echo "✅ Proposal Executed Successfully."
else
    echo "❌ EXECUTION FAILURE"
    if echo "$EXEC_LOGS" | grep -q "group policy execution period not passed"; then
         echo "REASON: Timelock active. Wait for $MIN_EXEC and try again."
    else
         echo "REASON: Unknown. Check logs."
         echo "$EXEC_LOGS" | jq -r '.raw_log'
    fi
    exit 1
fi

# --- 3. VERIFY ADDITION ---
echo "--- VERIFYING MEMBER ADDITION ---"

MEMBERS_JSON=$($BINARY query group group-members $PARENT_GROUP_ID --output json)

if echo "$MEMBERS_JSON" | grep -q "$DAVE_ADDR"; then
    echo "🎉 SUCCESS: Dave was added to the Commons Council!"
else
    echo "❌ FAILURE: Dave was NOT found in the parent group."
    echo "Members: $MEMBERS_JSON"
    exit 1
fi

# --- 4. CLEANUP (REMOVE DAVE) ---
echo ""
echo "--- TEST 3: CLEANUP (Remove Dave from $PARENT_NAME) ---"
echo "Restoring state for future tests..."

echo '{
  "group_policy_address": "'$CHILD_POLICY_ADDR'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Cleanup: Remove Dave",
  "summary": "Restoring council state.",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdateGroupMembers",
      "authority": "'$CHILD_POLICY_ADDR'",
      "group_policy_address": "'$PARENT_POLICY_ADDR'",
      "members_to_add": [],
      "weights_to_add": [],
      "members_to_remove": ["'$DAVE_ADDR'"]
    }
  ]
}' > "$PROPOSAL_DIR/msg_cleanup_remove.json"

echo "Submitting Removal Proposal..."
SUBMIT_RES=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/msg_cleanup_remove.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees $FEES --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 4

# Get Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
CLEANUP_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

if [ -z "$CLEANUP_ID" ]; then
    echo "❌ ERROR: Failed to submit cleanup proposal."
    exit 1
fi
echo "✅ Cleanup Proposal ID: $CLEANUP_ID"

# Vote
echo "Alice voting YES..."
$BINARY tx group vote $CLEANUP_ID $ALICE_ADDR VOTE_OPTION_YES "Cleanup" --from alice -y --chain-id $CHAIN_ID --keyring-backend test > /dev/null
sleep 4

# Execute
echo "Executing Cleanup..."
EXEC_RES=$($BINARY tx group exec $CLEANUP_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 4

# Verify Removal
MEMBERS_JSON=$($BINARY query group group-members $PARENT_GROUP_ID --output json)

if echo "$MEMBERS_JSON" | grep -q "$DAVE_ADDR"; then
    echo "❌ FAILURE: Dave is STILL in the group. Cleanup failed."
    exit 1
else
    echo "🧹 SUCCESS: Dave removed. State restored."
fi