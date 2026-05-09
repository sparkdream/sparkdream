#!/bin/bash

echo "--- TESTING: HOSTILE HANDOVER ATTEMPT VS EXECUTIVE VETO ---"

# --- 0. SETUP & ADDRESS DISCOVERY ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')

# DISCOVER COMMONS COUNCIL
# Get Group ID
GROUP_INFO=$($BINARY query commons get-group "Commons Council" --output json)
GROUP_ID=$(echo $GROUP_INFO | jq -r '.group.group_id // "0"')
COUNCIL_POLICY_ADDR=$(echo $GROUP_INFO | jq -r '.group.policy_address // empty')

if [ -z "$COUNCIL_POLICY_ADDR" ]; then
    echo "[FAIL] SETUP ERROR: Commons Council not found."
    exit 1
fi

# Veto policy address is exposed directly on the Group struct
VETO_POLICY_ADDR=$(echo "$GROUP_INFO" | jq -r '.group.veto_policy_address')

if [ -z "$VETO_POLICY_ADDR" ] || [ "$VETO_POLICY_ADDR" == "null" ]; then
    echo "[FAIL] SETUP ERROR: Veto Policy not found for Commons Council."
    exit 1
fi

echo "Gov Address:         $GOV_ADDR"
echo "Council Veto Policy: $VETO_POLICY_ADDR"

# --- 1. ATTACK: SUBMIT HOSTILE GOV PROPOSAL ---
echo "--- ATTACK STARTING: Alice attempts to wipe the Council via Governance ---"

# Alice (Validator) proposes to wipe the Commons Council and install herself as the only member.
echo '{
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgRenewGroup",
      "authority": "'$GOV_ADDR'",
      "group_name": "Commons Council",
      "new_members": ["'$ALICE_ADDR'"],
      "new_member_weights": ["1"]
    }
  ],
  "deposit": "50000000uspark",
  "title": "Hostile Handover",
  "summary": "Malicious proposal to hijack the protocol."
}' > "$PROPOSAL_DIR/gov_handover_hostile.json"

# Submit
SUBMIT_RES=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/gov_handover_hostile.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

echo "Waiting for block inclusion (3s)..."
sleep 3

# Get Gov Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
GOV_PROP_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

if [ -z "$GOV_PROP_ID" ] || [ "$GOV_PROP_ID" == "null" ]; then
   GOV_PROP_ID=$(echo $TX_RES | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

if [ -z "$GOV_PROP_ID" ]; then
  echo "[FAIL] ERROR: Failed to create Gov Proposal."
  exit 1
fi

echo "[WARN]  Hostile Gov Proposal ID: $GOV_PROP_ID (Status: VOTING_PERIOD)"

# --- 3. ATTACK: ALICE VOTES YES ---
# Assuming Alice has enough stake to pass it if uncontested.
echo "Alice voting YES (Threatening to pass)..."
$BINARY tx gov vote $GOV_PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3

# --- 4. DEFENSE: COUNCIL EXECUTES VETO ---
echo "--- DEFENSE ACTIVATED: Council executing Emergency Cancel ---"

# The existing council (Alice, Bob, Carol) must vote to veto.
# Assuming Alice is the attacker, Bob and Carol must outvote her?
# But wait, Alice is in the council. If she votes NO on Veto, does it fail?
# Veto Threshold = 0.67 (2/3). 
# If members are [Alice, Bob, Carol] (1,1,1), we need 2/3 = 66%. So 2 votes needed.
# Bob + Carol = 2 votes. They can override Alice.

echo '{
  "policy_address": "'$VETO_POLICY_ADDR'",
  "proposers": ["'$BOB_ADDR'"],
  "title": "EMERGENCY: KILL PROP '$GOV_PROP_ID'",
  "summary": "Stopping hostile takeover.",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgEmergencyCancelGovProposal",
      "authority": "'$VETO_POLICY_ADDR'",
      "proposal_id": '$GOV_PROP_ID'
    }
  ]
}' > "$PROPOSAL_DIR/exec_veto.json"

# Submit Veto Group Proposal
SUBMIT_GROUP=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/exec_veto.json" --from bob -y --chain-id $CHAIN_ID --keyring-backend test --output json)
GROUP_TX=$(echo $SUBMIT_GROUP | jq -r '.txhash')
echo $SUBMIT_GROUP
sleep 3

# Get Group Proposal ID
GROUP_TX_RES=$($BINARY query tx $GROUP_TX --output json)
GROUP_PROP_ID=$(echo $GROUP_TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
if [ -z "$GROUP_PROP_ID" ]; then
    GROUP_PROP_ID=$(echo $GROUP_TX_RES | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi
echo "  Defense Group Proposal ID: $GROUP_PROP_ID"

# Vote & Execute
echo "Bob voting YES to Veto..."
$BINARY tx commons vote-proposal $GROUP_PROP_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3
echo "Carol voting YES to Veto..."
$BINARY tx commons vote-proposal $GROUP_PROP_ID yes --from carol -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3

# Wait for Veto Voting Period (4 Hours in bootstrap? We need to wait or rely on TryExec)
# Note: For testing, we assume 'TryExec' works if threshold met immediately.
# But 4h is a long time if threshold is not met.
# Since we have 2/3 votes (Bob+Carol), we meet the threshold.

echo "Attempting Execution (Threshold Met)..."
EXEC_RES=$($BINARY tx commons execute-proposal $GROUP_PROP_ID --gas 2000000 --from bob -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash')

echo "Waiting for execution block (3s)..."
sleep 3

# --- 5. VERIFICATION ---
echo "--- VERIFICATION RESULTS ---"

# 1. Verify Execution Success via proposal status
GROUP_PROP_STATUS=$($BINARY query commons get-proposal $GROUP_PROP_ID --output json | jq -r '.proposal.status')
if [ "$GROUP_PROP_STATUS" == "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "[ OK ] Veto Execution Logic: SUCCESS (status=$GROUP_PROP_STATUS)"
else
    echo "[FAIL] CRITICAL FAILURE: The Veto Group Proposal did not execute (status=$GROUP_PROP_STATUS)."
    EXEC_TX_JSON=$($BINARY query tx $EXEC_TX_HASH --output json 2>/dev/null)
    echo "Raw Log: $(echo $EXEC_TX_JSON | jq -r '.raw_log // empty' 2>/dev/null)"
    exit 1
fi

# 2. Check Gov Proposal Status
GOV_STATUS_JSON=$($BINARY query gov proposal $GOV_PROP_ID --output json 2>&1)

if echo "$GOV_STATUS_JSON" | grep -q "not found"; then
  echo "[ OK ] SUCCESS: Hostile Proposal was DELETED from state."
else
  STATUS=$(echo $GOV_STATUS_JSON | jq -r '.proposal.status')
  if [ "$STATUS" == "PROPOSAL_STATUS_FAILED" ] || [ "$STATUS" == "PROPOSAL_STATUS_REJECTED" ]; then
     echo "[ OK ] SUCCESS: Hostile Proposal status is $STATUS."
  elif [ "$STATUS" == "PROPOSAL_STATUS_PASSED" ]; then
     echo "[FAIL] CRITICAL FAILURE: The Hostile Proposal PASSED! Defense failed."
     exit 1
  else
     echo "[FAIL] FAILURE: Hostile Proposal is still $STATUS!"
  fi
fi