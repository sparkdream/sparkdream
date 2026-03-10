#!/bin/bash

echo "--- TESTING: INTERIM COUNCIL (DICTATOR MODE & RESTORATION) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)

GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')
echo "Gov Address: $GOV_ADDR"

# --- 1. PHASE 1: INSTALL BOB AS DICTATOR ---
echo "--- PHASE 1: INSTALLING BOB AS INTERIM DICTATOR ---"
echo "Alice (Validator) votes to wipe Commons Council and install Bob..."

# Using MsgRenewGroup to wipe the slate
echo '{
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgRenewGroup",
      "authority": "'$GOV_ADDR'",
      "group_name": "Commons Council",
      "new_members": ["'$BOB_ADDR'"],
      "new_member_weights": ["1"]
    }
  ],
  "deposit": "100000000uspark",
  "title": "Emergency Dictator Act",
  "summary": "Setting Bob as sole member of Commons Council.",
  "expedited": true
}' > "$PROPOSAL_DIR/set_bob_dictator.json"

# Submit & Vote
SUBMIT_RES=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/set_bob_dictator.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 5

# Get Prop ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROP_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
if [ -z "$PROP_ID" ] || [ "$PROP_ID" == "null" ]; then
   PROP_ID=$(echo $TX_RES | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

echo "Dictator Prop ID: $PROP_ID"
$BINARY tx gov vote $PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test

echo "Waiting for Expedited Voting (45s)..."
sleep 45

# Verify Bob is Sole Member
GROUP_INFO=$($BINARY query commons get-group "Commons Council" --output json)
POLICY_ADDR=$(echo $GROUP_INFO | jq -r '.group.policy_address')

# Check Members via commons query
MEMBERS_JSON=$($BINARY query commons get-council-members "Commons Council" --output json)
COUNT=$(echo $MEMBERS_JSON | jq '.members | length')
MEMBER_ADDR=$(echo $MEMBERS_JSON | jq -r '.members[0].address')

if [ "$COUNT" == "1" ] && [ "$MEMBER_ADDR" == "$BOB_ADDR" ]; then
    echo "SUCCESS: Bob is now the sole member (Dictator)."
else
    echo "FAILURE: Membership update failed. Count: $COUNT"
    exit 1
fi

# --- 2. ATTACK & VETO (Dictator Veto) ---
echo "--- PHASE 2: BOB VETOES MALICIOUS PROPOSAL ---"

# Carol creates malicious proposal
echo '{
  "messages": [
    {
      "@type": "/cosmos.bank.v1beta1.MsgSend",
      "from_address": "'$GOV_ADDR'",
      "to_address": "'$CAROL_ADDR'",
      "amount": [{"denom": "uspark", "amount": "1"}]
    }
  ],
  "deposit": "50000000uspark",
  "title": "Steal Funds",
  "summary": "Trying to steal while Bob is in charge."
}' > "$PROPOSAL_DIR/bad_prop_bob.json"

SUBMIT_RES=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/bad_prop_bob.json" --from carol -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 5
BAD_PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
echo "Bad Prop ID: $BAD_PROP_ID"

echo "Discovering Veto Policy..."

# Discover Veto Policy from get-group
VETO_POLICY_ADDR=$($BINARY query commons get-group "Commons Council" --output json | jq -r '.group.veto_policy_address')

if [ -z "$VETO_POLICY_ADDR" ] || [ "$VETO_POLICY_ADDR" == "null" ]; then
    echo "ERROR: Could not find Veto Policy for Commons Council"
    exit 1
fi

echo "Using Veto Policy: $VETO_POLICY_ADDR"

# Bob submits Veto via VETO Policy
echo "Bob submits Veto via Commons Proposal..."
echo '{
  "policy_address": "'$VETO_POLICY_ADDR'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgEmergencyCancelGovProposal",
      "authority": "'$VETO_POLICY_ADDR'",
      "proposal_id": '$BAD_PROP_ID'
    }
  ],
  "metadata": "FAST VETO - Immediate execution."
}' > "$PROPOSAL_DIR/fast_veto.json"

# Submit Commons Proposal
SUBMIT_GROUP=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/fast_veto.json" --from bob -y --chain-id $CHAIN_ID --keyring-backend test --output json)
GROUP_TX=$(echo $SUBMIT_GROUP | jq -r '.txhash')
sleep 5

GROUP_PROP_ID=$(echo $($BINARY query tx $GROUP_TX --output json) | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
if [ -z "$GROUP_PROP_ID" ] || [ "$GROUP_PROP_ID" == "null" ]; then
   GROUP_PROP_ID=$(echo $($BINARY query tx $GROUP_TX --output json) | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi
echo "Commons Prop ID: $GROUP_PROP_ID"

# Bob Votes YES (100% of weight - sole member)
$BINARY tx commons vote-proposal $GROUP_PROP_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5
$BINARY tx commons execute-proposal $GROUP_PROP_ID --from bob -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000
sleep 5

# Verify Kill
STATUS=$($BINARY query gov proposal $BAD_PROP_ID --output json | jq -r '.proposal.status')
if [ "$STATUS" == "PROPOSAL_STATUS_FAILED" ] || [ "$STATUS" == "PROPOSAL_STATUS_REJECTED" ]; then
    echo "SUCCESS: Bob successfully vetoed using Veto Policy."
else
    echo "FAILURE: Proposal is still $STATUS"
    exit 1
fi

# --- 3. RESTORATION: EXPAND GROUP ---
echo "--- PHASE 3: RESTORING THE REPUBLIC ---"
echo "Bob (or Gov) proposes to add Alice and Carol back..."

# Bob cannot do this himself because MsgRenewGroup checks "isGov".
# So we must use a Gov Proposal again.

echo '{
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgRenewGroup",
      "authority": "'$GOV_ADDR'",
      "group_name": "Commons Council",
      "new_members": ["'$ALICE_ADDR'", "'$BOB_ADDR'", "'$CAROL_ADDR'"],
      "new_member_weights": ["1", "1", "1"]
    }
  ],
  "deposit": "100000000uspark",
  "title": "Restore Democracy",
  "summary": "Adding members back to the council.",
  "expedited": true
}' > "$PROPOSAL_DIR/restore_council.json"

# Submit Proposal
SUBMIT_RES=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/restore_council.json" --from bob -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 5
RESTORE_PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
if [ -z "$RESTORE_PROP_ID" ] || [ "$RESTORE_PROP_ID" == "null" ]; then
   RESTORE_PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

echo "Restoration Prop ID: $RESTORE_PROP_ID"

# Alice Votes Yes to ratify
$BINARY tx gov vote $RESTORE_PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test

echo "Waiting for Expedited Voting (45s)..."
sleep 45

# Verify Final State via commons query
FINAL_MEMBERS=$($BINARY query commons get-council-members "Commons Council" --output json)
FINAL_COUNT=$(echo $FINAL_MEMBERS | jq '.members | length')

if [ "$FINAL_COUNT" == "3" ]; then
    echo "SUCCESS: The Republic is Restored. 3 Members found."
else
    echo "FAILURE: Restoration failed. Count: $FINAL_COUNT"
fi
