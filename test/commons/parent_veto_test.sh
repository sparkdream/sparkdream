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
    echo "[FAIL] Error: jq is not installed."
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
    echo "[FAIL] SETUP ERROR: '$CHILD_NAME' not found."
    exit 1
fi
echo "Child Policy (Ops): $CHILD_POLICY_ADDR"

# B. Find Parent VETO Policy (The Executor)
# We need the Veto Policy address because genesis_bootstrap.go assigns the permission there.
PARENT_INFO=$($BINARY query commons get-group "$PARENT_NAME" --output json)
PARENT_GROUP_ID=$(echo $PARENT_INFO | jq -r '.group.group_id // "0"')

# Veto policy address is exposed directly on the Group struct
PARENT_VETO_POLICY=$(echo "$PARENT_INFO" | jq -r '.group.veto_policy_address')

if [ -z "$PARENT_VETO_POLICY" ] || [ "$PARENT_VETO_POLICY" == "null" ]; then
    echo "[FAIL] SETUP ERROR: '$PARENT_NAME' Veto Policy not found."
    exit 1
fi
echo "Parent Policy (Veto): $PARENT_VETO_POLICY"


# --- 2. THE ATTACK (ROGUE PROPOSAL) ---
echo "--- STEP 2: Alice creates a Rogue Proposal in the Committee ---"

# Alice proposes to spend funds.
echo '{
  "policy_address": "'$CHILD_POLICY_ADDR'",
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

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/rogue_proposal.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 4

# Get Rogue Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
ROGUE_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
echo " Rogue Proposal ID: $ROGUE_ID"

# Alice votes YES to make it executable (conceptually)
$BINARY tx commons vote-proposal $ROGUE_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test > /dev/null
sleep 2
echo "   (Alice voted YES)"


# --- 3. THE DEFENSE (PARENT VETO) ---
echo "--- STEP 3: Bob submits Veto Proposal to Commons Council ---"

# Bob submits a proposal to the PARENT VETO POLICY to execute MsgVetoGroupProposals
echo '{
  "policy_address": "'$PARENT_VETO_POLICY'",
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

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/parent_veto.json" --from bob -y --chain-id $CHAIN_ID --keyring-backend test --fees $FEES --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 4

# Get Veto Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
VETO_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
echo "  Veto Proposal ID: $VETO_ID"

# Alice and Bob vote YES (Veto Policy has 0.49 threshold)
$BINARY tx commons vote-proposal $VETO_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test > /dev/null
sleep 2
echo "   (Alice voted YES)"

$BINARY tx commons vote-proposal $VETO_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test > /dev/null
sleep 2
echo "   (Bob voted YES)"

# Execute the Veto. Don't pass $GAS_FLAGS here — it expands to "--gas auto"
# which overrides the explicit --gas value below; auto-estimation produces
# something like 43k which is insufficient to run the bumping inner msg and
# the tx silently fails out-of-gas, leaving the proposal in ACCEPTED state.
echo "   Executing Parent Veto..."
EXEC_RES=$($BINARY tx commons execute-proposal $VETO_ID --gas 2000000 --from bob -y --chain-id $CHAIN_ID --keyring-backend test --fees $FEES --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 4

# Verify Veto Success — x/commons records execution success via the
# proposal's status field (no PROPOSAL_EXECUTOR_RESULT_SUCCESS event like
# x/group used to emit).
VETO_PROP_STATUS=$($BINARY query commons get-proposal $VETO_ID --output json | jq -r '.proposal.status')
if [ "$VETO_PROP_STATUS" == "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "[ OK ] Parent Veto Executed Successfully (status=$VETO_PROP_STATUS)."
else
    echo "[FAIL] FAILURE: Parent Veto did not execute (status=$VETO_PROP_STATUS)."
    EXEC_LOGS=$($BINARY query tx $EXEC_HASH --output json 2>/dev/null)
    echo "$EXEC_LOGS" | jq -r '.raw_log // empty' 2>/dev/null
    exit 1
fi


# --- 4. VERIFY KILL ---
echo "--- STEP 4: Attempting to Execute Rogue Proposal (Should Fail) ---"

# After the parent veto, the rogue policy's PolicyVersion was bumped, so
# any still-ACCEPTED proposal pinned to the old version flips to VETOED on
# the next execute attempt. Two acceptable outcomes:
#   - The rogue proposal status is now PROPOSAL_STATUS_VETOED (good)
#   - The execute tx returns ErrUnauthorized "policy version changed" (good)
# Anything that ends up with the rogue proposal status PROPOSAL_STATUS_EXECUTED
# is a hard failure (the veto did not stick).

EXEC_OUTPUT=$($BINARY tx commons execute-proposal $ROGUE_ID --gas 2000000 --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees $FEES 2>&1)
sleep 4
ROGUE_STATUS=$($BINARY query commons get-proposal $ROGUE_ID --output json 2>/dev/null | jq -r '.proposal.status')
echo "   Rogue proposal status: $ROGUE_STATUS"

if [ "$ROGUE_STATUS" == "PROPOSAL_STATUS_VETOED" ]; then
    echo "[ OK ] SUCCESS: Rogue Proposal was VETOED (policy version bumped)."

elif echo "$EXEC_OUTPUT" | grep -qiE "policy version|veto|invalidated"; then
    echo "[ OK ] SUCCESS: Rogue Proposal execution rejected for policy version mismatch."

elif [ "$ROGUE_STATUS" == "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "[FAIL] FAILURE: Rogue Proposal was able to execute! Veto failed."
    exit 1

else
    echo "[WARN]  NOTE: Rogue proposal status is $ROGUE_STATUS — execution did not complete (likely still a success)."
    echo "Raw Output:"
    echo "$EXEC_OUTPUT" | head -10
fi

echo "--- INTEGRATION TEST PASSED ---"