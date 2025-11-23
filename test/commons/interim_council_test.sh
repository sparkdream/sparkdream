#!/bin/bash

echo "--- TESTING: INTERIM COUNCIL (BOB) & REBUILD ---"

# --- 0. SETUP ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')

mkdir -p proposals

# --- 1. SETUP: INSTALL BOB AS COUNCIL ---
echo "--- PHASE 1: INSTALLING BOB AS INTERIM COUNCIL ---"
echo "Alice votes to set Commons Council = Bob..."

echo '{
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdateParams",
      "authority": "'$GOV_ADDR'",
      "params": {
        "commons_council_address": "'$BOB_ADDR'"
      }
    }
  ],
  "deposit": "100000000uspark",
  "title": "Emergency Handover to Bob",
  "summary": "Setting Bob as interim council during restructuring.",
  "expedited": true
}' > proposals/set_bob_council.json

# Submit & Vote
SUBMIT_RES=$($BINARY tx gov submit-proposal proposals/set_bob_council.json --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
echo $SUBMIT_RES
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 3
PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

echo "Handover Prop ID: $PROP_ID"
$BINARY tx gov vote $PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test

echo "Waiting for Expedited Voting (40s)..."
sleep 45

# Verify Bob is Council
CURRENT_COUNCIL=$($BINARY query commons params --output json | jq -r '.params.commons_council_address')
if [ "$CURRENT_COUNCIL" == "$BOB_ADDR" ]; then
    echo "✅ SUCCESS: Bob is now the Commons Council."
else
    echo "❌ FAILURE: Council is $CURRENT_COUNCIL"
    exit 1
fi

# --- 2. ATTACK & VETO (User Veto) ---
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
}' > proposals/bad_prop_bob.json

# Submit & Vote
SUBMIT_RES=$($BINARY tx gov submit-proposal proposals/bad_prop_bob.json --from carol -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 3
BAD_PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
echo "Bad Prop ID: $BAD_PROP_ID"

# Bob executes Veto directly (as a User Transaction, not Group Proposal)
echo "Bob executes MsgEmergencyCancelProposal..."
$BINARY tx commons emergency-cancel-proposal $BAD_PROP_ID --from bob -y --chain-id $CHAIN_ID --keyring-backend test

sleep 3

# Verify Kill
STATUS=$($BINARY query gov proposal $BAD_PROP_ID --output json | jq -r '.proposal.status')
if [ "$STATUS" == "PROPOSAL_STATUS_FAILED" ] || [ "$STATUS" == "PROPOSAL_STATUS_REJECTED" ]; then
    echo "✅ SUCCESS: Bob successfully vetoed the proposal as a user."
else
    echo "❌ FAILURE: Proposal is still $STATUS"
    exit 1
fi

# --- 3. REBUILD: CREATE NEW GROUP ---
echo "--- PHASE 3: BOB BUILDS THE NEW REPUBLIC ---"

# Create Members (Bob + Alice)
echo '{"members": [
  {"address": "'$ALICE_ADDR'", "weight": "1", "metadata": "Alice"}, 
  {"address": "'$BOB_ADDR'", "weight": "1", "metadata": "Bob"}
]}' > proposals/new_members.json

# Create Group 2
echo "Creating New Group..."
# Note: We use 'bob' as admin because he is rebuilding it
GROUP_TX=$($BINARY tx group create-group $BOB_ADDR "New Council" proposals/new_members.json --from bob -y --chain-id $CHAIN_ID --keyring-backend test --output json)
GROUP_TX_HASH=$(echo $GROUP_TX | jq -r '.txhash')
sleep 3
NEW_GROUP_ID=$(echo $($BINARY query tx $GROUP_TX_HASH --output json) | jq -r '.events[] | select(.type=="cosmos.group.v1.EventCreateGroup") | .attributes[] | select(.key=="group_id") | .value' | tr -d '"')
echo "New Group ID: $NEW_GROUP_ID"

# Create Policies
echo '{"@type":"/cosmos.group.v1.PercentageDecisionPolicy", "percentage":"0.25", "windows":{"voting_period":"30s", "min_execution_period":"0s"}}' > proposals/policy_std.json
echo '{"@type":"/cosmos.group.v1.PercentageDecisionPolicy", "percentage":"0.50", "windows":{"voting_period":"10s", "min_execution_period":"0s"}}' > proposals/policy_veto.json

echo "Creating Standard Policy..."
$BINARY tx group create-group-policy $BOB_ADDR $NEW_GROUP_ID "standard" proposals/policy_std.json --from bob -y --chain-id $CHAIN_ID --keyring-backend test --output json
sleep 3

echo "Creating Veto Policy..."
$BINARY tx group create-group-policy $BOB_ADDR $NEW_GROUP_ID "veto" proposals/policy_veto.json --from bob -y --chain-id $CHAIN_ID --keyring-backend test --output json
sleep 3

# Discover New Standard Address
NEW_STANDARD_ADDR=$($BINARY query group group-policies-by-group $NEW_GROUP_ID -o json | jq -r '.group_policies[] | select(.metadata == "standard") | .address' | head -n 1 | tr -d '"')
echo "New Standard Policy: $NEW_STANDARD_ADDR"

# Update Admin (Bob hands over admin of Group 2 to the Policy)
$BINARY tx group update-group-admin $BOB_ADDR $NEW_GROUP_ID $NEW_STANDARD_ADDR --from bob -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark
sleep 3

# --- 4. RESTORATION: HANDOVER TO NEW GROUP ---
echo "--- PHASE 4: RESTORING ORDER ---"
echo "Bob submits Gov Proposal to set Council = New Policy..."

echo '{
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdateParams",
      "authority": "'$GOV_ADDR'",
      "params": {
        "commons_council_address": "'$NEW_STANDARD_ADDR'"
      }
    }
  ],
  "deposit": "100000000uspark",
  "title": "Restore Council",
  "summary": "Bob steps down and installs the New Council.",
  "expedited": true
}' > proposals/restore_council.json

# Submit Proposal
SUBMIT_RES=$($BINARY tx gov submit-proposal proposals/restore_council.json --from bob -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 3
RESTORE_PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

echo "Restoration Prop ID: $RESTORE_PROP_ID"

# Alice (Validator) Votes Yes to ratify
$BINARY tx gov vote $RESTORE_PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test

echo "Waiting for Expedited Voting (40s)..."
sleep 45

# Verify Final State
FINAL_COUNCIL=$($BINARY query commons params --output json | jq -r '.params.commons_council_address')

if [ "$FINAL_COUNCIL" == "$NEW_STANDARD_ADDR" ]; then
    echo "🎉 SUCCESS: The Republic is Restored. New Council is in charge."
else
    echo "❌ FAILURE: Handover failed. Council is $FINAL_COUNCIL"
fi