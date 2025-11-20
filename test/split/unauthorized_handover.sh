#!/bin/bash

echo "--- TESTING: HOSTILE HANDOVER ATTEMPT VS EXECUTIVE VETO ---"

# --- 0. SETUP & ADDRESS DISCOVERY ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

# Robust Gov Address Lookup
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')

# Robust Veto Policy Lookup
VETO_POLICY_ADDR=$($BINARY query group group-policies-by-group 1 -o json | jq -r '.group_policies[] | select(.metadata == "veto") | .address' | head -n 1)

echo "Gov Address:         $GOV_ADDR"
echo "Council Veto Policy: $VETO_POLICY_ADDR"

# --- 1. SNAPSHOT CURRENT PARAMS ---
ORIGINAL_COUNCIL=$($BINARY query split params --output json | jq -r '.params.commons_council_address')
echo "Current Council Addr: $ORIGINAL_COUNCIL"

if [ "$ORIGINAL_COUNCIL" == "$ALICE_ADDR" ]; then
  echo "❌ INVALID START STATE: Alice is already the council. Reset chain."
  exit 1
fi

# --- 2. ATTACK: SUBMIT HOSTILE GOV PROPOSAL ---
echo "--- ATTACK STARTING: Alice attempts to make herself the Council ---"

echo '{
  "messages": [
    {
      "@type": "/sparkdream.split.v1.MsgUpdateParams",
      "authority": "'$GOV_ADDR'",
      "params": {
        "commons_council_address": "'$ALICE_ADDR'"
      }
    }
  ],
  "deposit": "50000000uspark",
  "title": "Hostile Handover",
  "summary": "Malicious proposal to hijack the protocol revenue."
}' > proposals/gov_handover_hostile.json

# Submit
SUBMIT_RES=$($BINARY tx gov submit-proposal proposals/gov_handover_hostile.json --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

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

echo "⚠️  Hostile Gov Proposal ID: $GOV_PROP_ID (Status: VOTING_PERIOD)"

# --- 3. ATTACK: ALICE VOTES YES ---
echo "Alice voting YES (Threatening to pass)..."
$BINARY tx gov vote $GOV_PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3

# --- 4. DEFENSE: COUNCIL EXECUTES VETO ---
echo "--- DEFENSE ACTIVATED: Council executing Emergency Cancel ---"

echo '{
  "group_policy_address": "'$VETO_POLICY_ADDR'",
  "proposers": ["'$BOB_ADDR'"],
  "title": "EMERGENCY: KILL PROP '$GOV_PROP_ID'",
  "summary": "Stopping hostile takeover.",
  "messages": [
    {
      "@type": "/sparkdream.split.v1.MsgEmergencyCancelProposal",
      "authority": "'$VETO_POLICY_ADDR'",
      "proposal_id": '$GOV_PROP_ID'
    }
  ]
}' > proposals/exec_veto.json

# Submit Veto Group Proposal
SUBMIT_GROUP=$($BINARY tx group submit-proposal proposals/exec_veto.json --from bob -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
GROUP_TX=$(echo $SUBMIT_GROUP | jq -r '.txhash')

echo "Waiting for block inclusion (3s)..."
sleep 3

# Get Group Proposal ID
GROUP_TX_RES=$($BINARY query tx $GROUP_TX --output json)
GROUP_PROP_ID=$(echo $GROUP_TX_RES | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

# Fallback
if [ -z "$GROUP_PROP_ID" ] || [ "$GROUP_PROP_ID" == "null" ]; then
    GROUP_PROP_ID=$(echo $GROUP_TX_RES | jq -r '.logs[0].events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi
echo "🛡️  Defense Group Proposal ID: $GROUP_PROP_ID"

# Vote & Execute
echo "Bob voting YES to Veto..."
$BINARY tx group vote $GROUP_PROP_ID $BOB_ADDR VOTE_OPTION_YES "Stop attack" --from bob -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3
echo "Alice voting YES to Veto (Simulating consensus/regret)..."
$BINARY tx group vote $GROUP_PROP_ID $ALICE_ADDR VOTE_OPTION_YES "Ok fine" --from alice -y --chain-id $CHAIN_ID --keyring-backend test

# OPTIMIZATION: Wait 12s for Veto Policy (10s config), NOT 65s.
# If we wait 65s, the Gov proposal (60s) will expire naturally before we kill it.
echo "Votes cast. Waiting for Veto voting period (12s)..."
sleep 12

echo "Executing Veto..."
EXEC_RES=$($BINARY tx group exec $GROUP_PROP_ID --from bob -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash')

echo "Waiting for execution block (3s)..."
sleep 3

# --- 5. VERIFICATION ---
echo "--- VERIFICATION RESULTS ---"

# 1. Verify Execution Success via Tx Logs
EXEC_TX_JSON=$($BINARY query tx $EXEC_TX_HASH --output json)
if echo "$EXEC_TX_JSON" | grep -q "PROPOSAL_EXECUTOR_RESULT_SUCCESS"; then
    echo "✅ Veto Execution Logic: SUCCESS"
else
    echo "❌ CRITICAL FAILURE: The Veto Group Proposal failed to execute internally."
    echo "Raw Log: $(echo $EXEC_TX_JSON | jq -r '.raw_log')"
    exit 1
fi

# 2. Check Gov Proposal Status
GOV_STATUS_JSON=$($BINARY query gov proposal $GOV_PROP_ID --output json 2>&1)

if echo "$GOV_STATUS_JSON" | grep -q "not found"; then
  echo "✅ SUCCESS 1/2: Hostile Proposal was DELETED from state."
else
  STATUS=$(echo $GOV_STATUS_JSON | jq -r '.proposal.status')
  if [ "$STATUS" == "PROPOSAL_STATUS_FAILED" ] || [ "$STATUS" == "PROPOSAL_STATUS_REJECTED" ]; then
     echo "✅ SUCCESS 1/2: Hostile Proposal status is $STATUS."
  elif [ "$STATUS" == "PROPOSAL_STATUS_PASSED" ]; then
     echo "❌ CRITICAL FAILURE: The Hostile Proposal PASSED! Defense failed."
     exit 1
  else
     echo "❌ FAILURE 1/2: Hostile Proposal is still $STATUS!"
  fi
fi

# 3. Check Params Unchanged
CURRENT_COUNCIL=$($BINARY query split params --output json | jq -r '.params.commons_council_address')
if [ "$CURRENT_COUNCIL" == "$ORIGINAL_COUNCIL" ]; then
    echo "✅ SUCCESS 2/2: Council Address matches original ($CURRENT_COUNCIL)."
else
    echo "❌ FAILURE 2/2: Council Address WAS CHANGED to $CURRENT_COUNCIL!"
fi