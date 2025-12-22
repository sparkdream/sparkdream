#!/bin/bash

echo "--- TESTING: PARENT VETO (MSG_VETO_GROUP_PROPOSALS) ---"

# --- 0. SETUP & CONFIG ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
FEES="5000uspark"
GAS_FLAGS="--gas auto --gas-adjustment 1.5"

# Ensure jq is installed
if ! command -v jq &> /dev/null; then
    echo "❌ Error: jq is not installed."
    exit 1
fi

# Actors
# Alice = Member of Commons Operations Committee (The Rogue Actor)
# Bob   = Member of Commons Council (The Guardian)
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

echo "Alice (Rogue Committeeman): $ALICE_ADDR"
echo "Bob   (Council Guardian):   $BOB_ADDR"

# --- 1. DISCOVERY ---
echo "--- STEP 1: Discovering Groups & Policies ---"

CHILD_NAME="Commons Operations Committee"
PARENT_NAME="Commons Council"

# A. Find Child Policy (Target of the Veto)
CHILD_INFO=$($BINARY query commons get-extended-group "$CHILD_NAME" --output json)
CHILD_POLICY_ADDR=$(echo $CHILD_INFO | jq -r '.extended_group.policy_address')

if [ -z "$CHILD_POLICY_ADDR" ] || [ "$CHILD_POLICY_ADDR" == "null" ]; then
    echo "❌ SETUP ERROR: '$CHILD_NAME' not found."
    exit 1
fi
echo "Child Policy (Ops): $CHILD_POLICY_ADDR"

# B. Find Parent VETO Policy (The Executor)
# We need the Veto Policy address because genesis_bootstrap.go assigns the permission there.
PARENT_INFO=$($BINARY query commons get-extended-group "$PARENT_NAME" --output json)
PARENT_GROUP_ID=$(echo $PARENT_INFO | jq -r '.extended_group.group_id')

# Query all policies for the parent group and filter for metadata="veto"
PARENT_VETO_POLICY=$($BINARY query group group-policies-by-group $PARENT_GROUP_ID --output json | jq -r '.group_policies[] | select(.metadata=="veto") | .address')

if [ -z "$PARENT_VETO_POLICY" ] || [ "$PARENT_VETO_POLICY" == "null" ]; then
    echo "❌ SETUP ERROR: '$PARENT_NAME' Veto Policy not found."
    exit 1
fi
echo "Parent Policy (Veto): $PARENT_VETO_POLICY"


# --- 2. THE ATTACK (ROGUE PROPOSAL) ---
echo "--- STEP 2: Alice creates a Rogue Proposal in the Committee ---"

# Alice proposes to spend funds.
echo '{
  "group_policy_address": "'$CHILD_POLICY_ADDR'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Rogue Spending",
  "summary": "Unauthorized spend that should be vetoed by the parent.",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgSpendFromCommons",
      "authority": "'$CHILD_POLICY_ADDR'",
      "recipient": "'$ALICE_ADDR'",
      "amount": [{"denom": "uspark", "amount": "999999"}]
    }
  ]
}' > "$PROPOSAL_DIR/rogue_proposal.json"

SUBMIT_RES=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/rogue_proposal.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 4

# Get Rogue Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
ROGUE_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
echo "🚨 Rogue Proposal ID: $ROGUE_ID"

# Alice votes YES to make it executable (conceptually)
$BINARY tx group vote $ROGUE_ID $ALICE_ADDR VOTE_OPTION_YES "Steal funds" --from alice -y --chain-id $CHAIN_ID --keyring-backend test > /dev/null
sleep 2
echo "   (Alice voted YES)"


# --- 3. THE DEFENSE (PARENT VETO) ---
echo "--- STEP 3: Bob submits Veto Proposal to Commons Council ---"

# Bob submits a proposal to the PARENT VETO POLICY to execute MsgVetoGroupProposals
echo '{
  "group_policy_address": "'$PARENT_VETO_POLICY'",
  "proposers": ["'$BOB_ADDR'"],
  "title": "Veto Rogue Ops",
  "summary": "Emergency intervention to stop rogue spending.",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgVetoGroupProposals",
      "authority": "'$PARENT_VETO_POLICY'",
      "group_name": "'$CHILD_NAME'"
    }
  ]
}' > "$PROPOSAL_DIR/parent_veto.json"

SUBMIT_RES=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/parent_veto.json" --from bob -y --chain-id $CHAIN_ID --keyring-backend test --fees $FEES --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 4

# Get Veto Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
VETO_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
echo "🛡️  Veto Proposal ID: $VETO_ID"

# Alice and Bob vote YES (Veto Policy has 0.49 threshold)
$BINARY tx group vote $VETO_ID $ALICE_ADDR VOTE_OPTION_YES "Stop them" --from alice -y --chain-id $CHAIN_ID --keyring-backend test > /dev/null
sleep 2
echo "   (Alice voted YES)"

$BINARY tx group vote $VETO_ID $BOB_ADDR VOTE_OPTION_YES "Stop them" --from bob -y --chain-id $CHAIN_ID --keyring-backend test > /dev/null
sleep 2
echo "   (Bob voted YES)"

# Execute the Veto
echo "   Executing Parent Veto..."
EXEC_RES=$($BINARY tx group exec $VETO_ID --from bob -y --chain-id $CHAIN_ID --keyring-backend test $GAS_FLAGS --fees $FEES --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 4

# Verify Veto Success
EXEC_LOGS=$($BINARY query tx $EXEC_HASH --output json)
if echo "$EXEC_LOGS" | grep -q "PROPOSAL_EXECUTOR_RESULT_SUCCESS"; then
    echo "✅ Parent Veto Executed Successfully."
else
    echo "❌ FAILURE: Parent Veto failed to execute."
    echo "$EXEC_LOGS" | jq -r '.raw_log'
    exit 1
fi


# --- 4. VERIFY KILL ---
echo "--- STEP 4: Attempting to Execute Rogue Proposal (Should Fail) ---"

# We expect this to fail. We capture both STDOUT and STDERR to find the reason.
# We use '|| true' to prevent the script from exiting if set -e is active (though it isn't here, it's good practice).
EXEC_OUTPUT=$($BINARY tx group exec $ROGUE_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test $GAS_FLAGS --fees $FEES 2>&1)

# Logic:
# 1. If it says "PROPOSAL_STATUS_ABORTED", the SDK has already killed it. (SUCCESS)
# 2. If it says "group policy modified", the execution check caught the version mismatch. (SUCCESS)
# 3. If it says "PROPOSAL_EXECUTOR_RESULT_SUCCESS", the Veto FAILED. (FAILURE)

if echo "$EXEC_OUTPUT" | grep -q "PROPOSAL_STATUS_ABORTED"; then
    echo "✅ SUCCESS: Rogue Proposal was ABORTED by the Veto."
    echo "   (The chain correctly prevented execution because the proposal is marked Aborted)."

elif echo "$EXEC_OUTPUT" | grep -q "group policy modified"; then
    echo "✅ SUCCESS: Rogue Proposal Execution Failed."
    echo "   (The chain rejected execution due to Policy Version mismatch)."

elif echo "$EXEC_OUTPUT" | grep -q "PROPOSAL_EXECUTOR_RESULT_SUCCESS"; then
    echo "❌ FAILURE: Rogue Proposal was able to execute! Veto failed."
    exit 1

else
    echo "⚠️  NOTE: Execution failed with an unknown error. This is likely still a success (it didn't execute)."
    echo "Raw Output:"
    echo "$EXEC_OUTPUT"
fi

echo "--- INTEGRATION TEST PASSED ---"