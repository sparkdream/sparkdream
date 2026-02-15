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
sleep 3

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
# 1. Get Group ID
GROUP_INFO=$($BINARY query commons get-extended-group "Commons Council" --output json)
GROUP_ID=$(echo $GROUP_INFO | jq -r '.extended_group.group_id')
POLICY_ADDR=$(echo $GROUP_INFO | jq -r '.extended_group.policy_address')

# 2. Check Members
MEMBERS_JSON=$($BINARY query group group-members $GROUP_ID --output json)
COUNT=$(echo $MEMBERS_JSON | jq '.members | length')
MEMBER_ADDR=$(echo $MEMBERS_JSON | jq -r '.members[0].member.address')

if [ "$COUNT" == "1" ] && [ "$MEMBER_ADDR" == "$BOB_ADDR" ]; then
    echo "✅ SUCCESS: Bob is now the sole member (Dictator)."
else
    echo "❌ FAILURE: Membership update failed. Count: $COUNT"
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
sleep 3
BAD_PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
echo "Bad Prop ID: $BAD_PROP_ID"

echo "Discovering Veto Policy..."

# 1. DISCOVER VETO POLICY for Commons Council
# We look for the policy with metadata "veto" (or however you tagged it in genesis/setup)
VETO_POLICY_ADDR=$($BINARY query group group-policies-by-group $GROUP_ID --output json | jq -r '.group_policies[] | select(.metadata == "veto") | .address' | head -n 1)

if [ -z "$VETO_POLICY_ADDR" ] || [ "$VETO_POLICY_ADDR" == "null" ]; then
    echo "❌ ERROR: Could not find Veto Policy for Group $GROUP_ID"
    exit 1
fi

echo "Using Veto Policy: $VETO_POLICY_ADDR"

# 2. Bob submits Veto via VETO Policy
echo "Bob submits Veto via Group Proposal..."
echo '{
  "group_policy_address": "'$VETO_POLICY_ADDR'",
  "proposers": ["'$BOB_ADDR'"],
  "title": "FAST VETO",
  "summary": "Immediate execution.",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgEmergencyCancelGovProposal",
      "authority": "'$VETO_POLICY_ADDR'",
      "proposal_id": '$BAD_PROP_ID'
    }
  ]
}' > "$PROPOSAL_DIR/fast_veto.json"

# Submit Group Proposal
SUBMIT_GROUP=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/fast_veto.json" --from bob -y --chain-id $CHAIN_ID --keyring-backend test --output json)
GROUP_TX=$(echo $SUBMIT_GROUP | jq -r '.txhash')
sleep 3

GROUP_PROP_ID=$(echo $($BINARY query tx $GROUP_TX --output json) | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
if [ -z "$GROUP_PROP_ID" ] || [ "$GROUP_PROP_ID" == "null" ]; then
   GROUP_PROP_ID=$(echo $($BINARY query tx $GROUP_TX --output json) | jq -r '.logs[0].events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi
echo "Group Prop ID: $GROUP_PROP_ID"

# Bob Votes YES (100% of weight)
$BINARY tx group vote $GROUP_PROP_ID $BOB_ADDR VOTE_OPTION_YES "Veto" --from bob -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3
$BINARY tx group exec $GROUP_PROP_ID --from bob -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000
sleep 3

# Verify Kill
STATUS=$($BINARY query gov proposal $BAD_PROP_ID --output json | jq -r '.proposal.status')
if [ "$STATUS" == "PROPOSAL_STATUS_FAILED" ] || [ "$STATUS" == "PROPOSAL_STATUS_REJECTED" ]; then
    echo "✅ SUCCESS: Bob successfully vetoed using Veto Policy."
else
    echo "❌ FAILURE: Proposal is still $STATUS"
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
sleep 3
RESTORE_PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
if [ -z "$RESTORE_PROP_ID" ] || [ "$RESTORE_PROP_ID" == "null" ]; then
   RESTORE_PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

echo "Restoration Prop ID: $RESTORE_PROP_ID"

# Alice Votes Yes to ratify
$BINARY tx gov vote $RESTORE_PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test

echo "Waiting for Expedited Voting (45s)..."
sleep 45

# Verify Final State
FINAL_MEMBERS=$($BINARY query group group-members $GROUP_ID --output json)
FINAL_COUNT=$(echo $FINAL_MEMBERS | jq '.members | length')

if [ "$FINAL_COUNT" == "3" ]; then
    echo "🎉 SUCCESS: The Republic is Restored. 3 Members found."
else
    echo "❌ FAILURE: Restoration failed. Count: $FINAL_COUNT"
fi