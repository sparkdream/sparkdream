#!/bin/bash

echo "--- TESTING: EXECUTIVE VETO (EMERGENCY CANCEL) ---"

# --- 0. SETUP ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

# Robust Gov Address Lookup
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')
echo "Gov Module Address: $GOV_ADDR"

# Discover Veto Policy Address
VETO_POLICY_ADDR=$($BINARY query group group-policies-by-group 1 -o json | jq -r '.group_policies[] | select(.metadata == "veto") | .address' | head -n 1)
echo "Veto Policy Address: $VETO_POLICY_ADDR"

# --- 1. ATTACK: Create & Vote on "Bad" Governance Proposal ---
echo "--- PHASE 1: THE ATTACK ---"
echo "Creating a malicious Governance Proposal..."

# Message: Treasury pays Alice (Valid because Gov signs it if passed)
echo '{
  "messages": [
    {
      "@type": "/cosmos.bank.v1beta1.MsgSend",
      "from_address": "'$GOV_ADDR'",
      "to_address": "'$ALICE_ADDR'",
      "amount": [{"denom": "uspark", "amount": "1"}]
    }
  ],
  "deposit": "50000000uspark",
  "title": "Malicious Treasury Drain",
  "summary": "This proposal attempts to steal funds."
}' > proposals/bad_proposal.json

SUBMIT_RES=$($BINARY tx gov submit-proposal proposals/bad_proposal.json --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

echo "Submitted Gov Prop. Hash: $TX_HASH"
echo "Waiting for block inclusion (3s)..."
sleep 3

# Get Gov Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
GOV_PROP_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

# Fallback lookup
if [ -z "$GOV_PROP_ID" ] || [ "$GOV_PROP_ID" == "null" ]; then
   GOV_PROP_ID=$(echo $TX_RES | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

if [ -z "$GOV_PROP_ID" ] || [ "$GOV_PROP_ID" == "null" ]; then
  echo "❌ ERROR: Failed to create Gov Proposal."
  echo "Tx Response: $TX_RES"
  exit 1
fi

echo "⚠️  Target Gov Proposal ID: $GOV_PROP_ID"

# --- ALICE VOTES YES ---
# This puts the proposal in a "PASSING" state.
echo "⚠️  Alice votes YES (Threatening to pass the proposal)..."
$BINARY tx gov vote $GOV_PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test

echo "Waiting for vote inclusion (3s)..."
sleep 3

# --- 2. DEFENSE: Create the "Kill Switch" Group Proposal ---
echo "--- PHASE 2: THE DEFENSE ---"
echo "Creating Executive Veto Proposal..."

echo '{
  "group_policy_address": "'$VETO_POLICY_ADDR'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "EXECUTIVE ORDER: CANCEL PROP '$GOV_PROP_ID'",
  "summary": "Immediate cancellation of malicious proposal.",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgEmergencyCancelProposal",
      "authority": "'$VETO_POLICY_ADDR'",
      "proposal_id": '$GOV_PROP_ID'
    }
  ]
}' > proposals/msg_exec_veto.json

# --- 3. Submit Group Proposal ---
echo "Submitting Executive Veto Proposal..."
SUBMIT_GROUP_RES=$($BINARY tx group submit-proposal proposals/msg_exec_veto.json --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
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

if [ -z "$GROUP_PROP_ID" ] || [ "$GROUP_PROP_ID" == "null" ]; then
  echo "❌ ERROR: Failed to create Group Proposal."
  echo "Tx Response: $GROUP_TX_RES"
  exit 1
fi

echo "✅ Group Proposal ID: $GROUP_PROP_ID"

# --- 4. Vote & Execute (Threshold 50%) ---
# Assuming Alice realizes her mistake or Bob+Carol override her.
# For test stability, we have Alice vote YES on the kill switch too.
echo "Alice voting YES..."
$BINARY tx group vote $GROUP_PROP_ID $ALICE_ADDR VOTE_OPTION_YES "Kill it" --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3
echo "Bob voting YES..."
$BINARY tx group vote $GROUP_PROP_ID $BOB_ADDR VOTE_OPTION_YES "Agreed" --from bob -y --chain-id $CHAIN_ID --keyring-backend test

# Wait for Veto Policy voting period (10s configured, wait 12s)
echo "Votes cast. Waiting for veto voting period (12s)..."
sleep 12

echo "Executing Veto..."
EXEC_RES=$($BINARY tx group exec $GROUP_PROP_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash')

# Wait for exec block to be processed
echo "Waiting for execution block (5s)..."
sleep 5

# Fetch the TX result
EXEC_TX_JSON=$($BINARY query tx $EXEC_TX_HASH --output json)

# Check for Success Event
if echo "$EXEC_TX_JSON" | grep -q "PROPOSAL_EXECUTOR_RESULT_SUCCESS"; then
    echo "✅ Group Execution Successful (Confirmed via Event)."
else
    echo "❌ CRITICAL FAILURE: Group Execution Failed."
    echo "Raw Log: $(echo $EXEC_TX_JSON | jq -r '.raw_log')"
    exit 1
fi

# --- 5. Verify Gov Proposal Status ---
echo "--- VERIFYING KILL ---"
GOV_STATUS_JSON=$($BINARY query gov proposal $GOV_PROP_ID --output json 2>&1)

if echo "$GOV_STATUS_JSON" | grep -q "not found"; then
  echo "✅ SUCCESS: Proposal $GOV_PROP_ID was DELETED from state."
else
  STATUS=$(echo $GOV_STATUS_JSON | jq -r '.proposal.status')
  echo "Current Status: $STATUS"
  
  if [ "$STATUS" == "PROPOSAL_STATUS_FAILED" ] || [ "$STATUS" == "PROPOSAL_STATUS_REJECTED" ]; then
     echo "✅ SUCCESS: Proposal status forced to $STATUS."
  elif [ "$STATUS" == "PROPOSAL_STATUS_VOTING_PERIOD" ]; then
     echo "❌ FAILURE: Proposal is still active! The Veto failed."
  elif [ "$STATUS" == "PROPOSAL_STATUS_PASSED" ]; then
     echo "❌ CRITICAL FAILURE: The malicious proposal PASSED!"
  else
     echo "⚠️  NOTE: Proposal exists with status: $STATUS"
  fi
fi