#!/bin/bash

echo "--- TESTING: CONSTITUTIONAL REMOVAL (FIRING THE COUNCIL) ---"

# --- 0. SETUP ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

# Gov Address
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')
echo "Gov Module Address: $GOV_ADDR"

# Discover Veto Policy
VETO_POLICY_ADDR=$($BINARY query group group-policies-by-group 1 -o json | jq -r '.group_policies[] | select(.metadata == "veto") | .address' | head -n 1)
echo "Veto Policy Address: $VETO_POLICY_ADDR"

# Check who is currently the council
OLD_COUNCIL=$($BINARY query split params --output json | jq -r '.params.commons_council_address')
echo "Current Council:     $OLD_COUNCIL"

if [ "$OLD_COUNCIL" == "$BOB_ADDR" ]; then
    echo "❌ SETUP ERROR: Bob is already the council. Reset the chain."
    exit 1
fi

# --- 1. ATTACK: Validators Vote to Fire the Council ---
echo "--- PHASE 1: THE CONSTITUTIONAL COUP ---"
echo "Alice submits EXPEDITED proposal to change Council Address to Bob..."

echo '{
  "messages": [
    {
      "@type": "/sparkdream.split.v1.MsgUpdateParams",
      "authority": "'$GOV_ADDR'",
      "params": {
        "commons_council_address": "'$BOB_ADDR'"
      }
    }
  ],
  "deposit": "100000000uspark", 
  "title": "FIRE THE COUNCIL",
  "summary": "The Council has gone rogue. We are removing them immediately via Expedited Proposal.",
  "expedited": true
}' > proposals/fire_council.json

# Submit EXPEDITED Proposal
SUBMIT_RES=$($BINARY tx gov submit-proposal proposals/fire_council.json --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

echo "Submitted Expedited Prop. Hash: $TX_HASH"
echo "Waiting for block inclusion (3s)..."
sleep 3

# Get Gov Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
GOV_PROP_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

# Fallback
if [ -z "$GOV_PROP_ID" ] || [ "$GOV_PROP_ID" == "null" ]; then
   GOV_PROP_ID=$(echo $TX_RES | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

if [ -z "$GOV_PROP_ID" ] || [ "$GOV_PROP_ID" == "null" ]; then
  echo "❌ ERROR: Failed to create Gov Proposal."
  echo "Tx Response: $TX_RES"
  exit 1
fi

echo "⚠️  Expedited Gov Proposal ID: $GOV_PROP_ID"

# ALICE VOTES YES (Super-Majority)
echo "Alice votes YES (75% Stake)..."
$BINARY tx gov vote $GOV_PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test

echo "Waiting for vote inclusion (3s)..."
sleep 3

# --- 2. DEFENSE: Council Tries to Veto ---
echo "--- PHASE 2: THE FAILED DEFENSE ---"
echo "Council panics and tries to Veto the proposal..."

echo '{
  "group_policy_address": "'$VETO_POLICY_ADDR'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "STOP THE COUP",
  "summary": "Trying to kill the proposal that fires us.",
  "messages": [
    {
      "@type": "/sparkdream.split.v1.MsgEmergencyCancelProposal",
      "authority": "'$VETO_POLICY_ADDR'",
      "proposal_id": '$GOV_PROP_ID'
    }
  ]
}' > proposals/msg_fail_veto.json

# Submit Group Proposal
SUBMIT_GROUP_RES=$($BINARY tx group submit-proposal proposals/msg_fail_veto.json --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
GROUP_TX_HASH=$(echo $SUBMIT_GROUP_RES | jq -r '.txhash')

echo "Submitted Group Prop. Hash: $GROUP_TX_HASH"
echo "Waiting for block inclusion (3s)..."
sleep 3

# Get Group Proposal ID
GROUP_TX_RES=$($BINARY query tx $GROUP_TX_HASH --output json)
GROUP_PROP_ID=$(echo $GROUP_TX_RES | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
# Fallback
if [ -z "$GROUP_PROP_ID" ] || [ "$GROUP_PROP_ID" == "null" ]; then
   GROUP_PROP_ID=$(echo $GROUP_TX_RES | jq -r '.logs[0].events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi
echo "✅ Group Proposal ID: $GROUP_PROP_ID"

# Vote to Veto (Consensus)
echo "Council votes YES to Veto..."
$BINARY tx group vote $GROUP_PROP_ID $ALICE_ADDR VOTE_OPTION_YES "Stop it" --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3
$BINARY tx group vote $GROUP_PROP_ID $BOB_ADDR VOTE_OPTION_YES "Stop it" --from bob -y --chain-id $CHAIN_ID --keyring-backend test

echo "Votes cast. Waiting for Veto voting period (12s)..."
sleep 12

# EXECUTE VETO -> THIS MUST FAIL
echo "Attempting to Execute Veto (Expect Failure)..."
EXEC_RES=$($BINARY tx group exec $GROUP_PROP_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash')

echo "Waiting for execution block (5s)..."
sleep 5

# --- 3. VERIFY VETO FAILURE ---
echo "--- VERIFYING VETO FAILURE ---"

# FIX: Check the Transaction Log, NOT the Proposal Object.
# This works even if the proposal is pruned.
EXEC_TX_JSON=$($BINARY query tx $EXEC_TX_HASH --output json)

# We check for the specific error message we wrote in the Go code
if echo "$EXEC_TX_JSON" | grep -q "Constitutional Protection"; then
    echo "✅ SUCCESS: The Code Exception worked!"
    echo "   Found Error: 'Constitutional Protection' in transaction logs."
elif echo "$EXEC_TX_JSON" | grep -q "PROPOSAL_EXECUTOR_RESULT_FAILURE"; then
     echo "✅ SUCCESS: Group Proposal logic executed but returned FAILURE."
else
    echo "❌ FAILURE: The Veto Execution did NOT fail as expected."
    echo "   Full Logs: $(echo $EXEC_TX_JSON | jq -r '.raw_log')"
    exit 1
fi

# --- 4. VERIFY GOV SUCCESS ---
echo "--- WAITING FOR GOV PROPOSAL TO PASS ---"
# Expedited Period is 40s. We have spent about 25s so far. Wait 20s more.
echo "Waiting 20s for Expedited Voting Period to end..."
sleep 20

echo "--- VERIFYING NEW REGIME ---"

# Check Gov Prop Status
GOV_STATUS=$($BINARY query gov proposal $GOV_PROP_ID --output json | jq -r '.proposal.status')
echo "Gov Prop Status: $GOV_STATUS"

# Note: For Expedited proposals, if they pass, the status might move to PASSED immediately
if [ "$GOV_STATUS" == "PROPOSAL_STATUS_PASSED" ]; then
    echo "✅ SUCCESS: The Constitutional Coup PASSED."
else
    echo "❌ FAILURE: Gov Proposal did not pass (Status: $GOV_STATUS)."
fi

# Check Actual Params
NEW_COUNCIL=$($BINARY query split params --output json | jq -r '.params.commons_council_address')
echo "New Council:     $NEW_COUNCIL"

if [ "$NEW_COUNCIL" == "$BOB_ADDR" ]; then
    echo "🎉 GRAND SUCCESS: The Council has been fired. Bob is the new Council."
else
    echo "❌ FAILURE: Params were not updated."
fi