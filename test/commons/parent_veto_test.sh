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
    echo "Error: jq is not installed."
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
CHILD_INFO=$($BINARY query commons get-group "$CHILD_NAME" --output json)
CHILD_POLICY_ADDR=$(echo $CHILD_INFO | jq -r '.group.policy_address')

if [ -z "$CHILD_POLICY_ADDR" ] || [ "$CHILD_POLICY_ADDR" == "null" ]; then
    echo "SETUP ERROR: '$CHILD_NAME' not found."
    exit 1
fi
echo "Child Policy (Ops): $CHILD_POLICY_ADDR"

# B. Find Parent VETO Policy (The Executor)
PARENT_INFO=$($BINARY query commons get-group "$PARENT_NAME" --output json)
PARENT_VETO_POLICY=$(echo $PARENT_INFO | jq -r '.group.veto_policy_address')

if [ -z "$PARENT_VETO_POLICY" ] || [ "$PARENT_VETO_POLICY" == "null" ]; then
    echo "SETUP ERROR: '$PARENT_NAME' Veto Policy not found."
    exit 1
fi
echo "Parent Policy (Veto): $PARENT_VETO_POLICY"


# --- 2. THE ATTACK (ROGUE PROPOSAL) ---
echo "--- STEP 2: Alice creates a Rogue Proposal in the Committee ---"

# Alice proposes to spend funds via the child committee's policy.
echo '{
  "policy_address": "'$CHILD_POLICY_ADDR'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgSpendFromCommons",
      "authority": "'$CHILD_POLICY_ADDR'",
      "recipient": "'$ALICE_ADDR'",
      "amount": [{"denom": "uspark", "amount": "999999"}]
    }
  ],
  "metadata": "Unauthorized spend that should be vetoed by the parent."
}' > "$PROPOSAL_DIR/rogue_proposal.json"

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/rogue_proposal.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 5

# Get Rogue Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
ROGUE_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
echo "Rogue Proposal ID: $ROGUE_ID"

# Alice votes YES to make it accepted (early acceptance)
$BINARY tx commons vote-proposal $ROGUE_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test > /dev/null
sleep 5
echo "   (Alice voted YES)"


# --- 3. THE DEFENSE (PARENT VETO) ---
echo "--- STEP 3: Bob submits Veto Proposal to Commons Council ---"

# Bob submits a proposal to the PARENT VETO POLICY to execute MsgVetoGroupProposals
# This increments the child committee's policy version, invalidating all pending proposals.
echo '{
  "policy_address": "'$PARENT_VETO_POLICY'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgVetoGroupProposals",
      "authority": "'$PARENT_VETO_POLICY'",
      "group_name": "'$CHILD_NAME'"
    }
  ],
  "metadata": "Emergency intervention to stop rogue spending."
}' > "$PROPOSAL_DIR/parent_veto.json"

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/parent_veto.json" --from bob -y --chain-id $CHAIN_ID --keyring-backend test --fees $FEES --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 5

# Get Veto Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
VETO_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
echo "Veto Proposal ID: $VETO_ID"

# Alice and Bob vote YES (Veto Policy has 0.49 threshold)
$BINARY tx commons vote-proposal $VETO_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test > /dev/null
sleep 5
echo "   (Alice voted YES)"

$BINARY tx commons vote-proposal $VETO_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test > /dev/null
sleep 5
echo "   (Bob voted YES)"

# Execute the Veto
echo "   Executing Parent Veto..."
EXEC_RES=$($BINARY tx commons execute-proposal $VETO_ID --from bob -y --chain-id $CHAIN_ID --keyring-backend test $GAS_FLAGS --fees $FEES --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 5

# Verify Veto Success - check proposal status is EXECUTED
EXEC_TX=$($BINARY query tx $EXEC_HASH --output json)
EXEC_CODE=$(echo $EXEC_TX | jq -r '.code')

if [ "$EXEC_CODE" == "0" ]; then
    # Double-check proposal status
    PROP_STATUS=$($BINARY query commons get-proposal $VETO_ID --output json | jq -r '.proposal.status')
    if [ "$PROP_STATUS" == "PROPOSAL_STATUS_EXECUTED" ]; then
        echo "SUCCESS: Parent Veto Executed Successfully."
    else
        echo "WARNING: Tx succeeded but proposal status is $PROP_STATUS"
    fi
else
    echo "FAILURE: Parent Veto failed to execute."
    echo "$EXEC_TX" | jq -r '.raw_log'
    exit 1
fi


# --- 4. VERIFY KILL ---
echo "--- STEP 4: Attempting to Execute Rogue Proposal (Should Fail) ---"

# The veto incremented the child's policy version. When we try to execute the rogue proposal,
# the policy_version check will fail -> VETOED status.
EXEC_OUTPUT=$($BINARY tx commons execute-proposal $ROGUE_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test $GAS_FLAGS --fees $FEES --output json 2>&1)
EXEC_CODE=$(echo $EXEC_OUTPUT | jq -r '.code // "error"' 2>/dev/null)

# Logic:
# 1. If execute tx returns non-zero code, the rogue proposal could not execute (SUCCESS for the test)
# 2. If it returns code 0, the Veto FAILED (FAILURE for the test)

if [ "$EXEC_CODE" != "0" ]; then
    if echo "$EXEC_OUTPUT" | grep -q "policy version changed"; then
        echo "SUCCESS: Rogue Proposal was VETOED (policy version mismatch)."
    elif echo "$EXEC_OUTPUT" | grep -q "not accepted"; then
        echo "SUCCESS: Rogue Proposal could not execute (not in accepted status)."
    else
        echo "SUCCESS: Rogue Proposal Execution Failed (as expected)."
        echo "   Detail: $(echo $EXEC_OUTPUT | jq -r '.raw_log // empty' 2>/dev/null)"
    fi
else
    echo "FAILURE: Rogue Proposal was able to execute! Veto failed."
    exit 1
fi

echo "--- INTEGRATION TEST PASSED ---"
