#!/bin/bash

echo "--- TESTING: POLICY LIFECYCLE (ATTACKS & SUNSETTING) ---"

# --- 0. SETUP ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)

mkdir -p proposals

# Address Lookups
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')
COUNCIL_ADDR=$($BINARY query commons params --output json | jq -r '.params.commons_council_address')

echo "Gov Address:     $GOV_ADDR"
echo "Council Address: $COUNCIL_ADDR"
echo "Attacker:        $CAROL_ADDR"

if [ -z "$COUNCIL_ADDR" ]; then
    echo "❌ SETUP ERROR: Council Address not found."
    exit 1
fi

# --- 1. ATTACK SIMULATION (SECURITY) ---
echo "--- STEP 1: ATTACKER (CAROL) TRIES TO MODIFY COUNCIL PERMS ---"

# Attempt 1: Carol tries to overwrite Council permissions
echo "Carol attempting MsgUpdatePolicyPermissions..."
SUBMIT_RES=$($BINARY tx commons update-policy-permissions $COUNCIL_ADDR "/cosmos.bank.v1beta1.MsgSend" \
  --from carol -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

echo "Tx Hash: $TX_HASH"
echo "Waiting for block inclusion..."
sleep 3

# Query the On-Chain Result
TX_RES=$($BINARY query tx $TX_HASH --output json 2>/dev/null)
TX_CODE=$(echo $TX_RES | jq -r '.code')
RAW_LOG=$(echo $TX_RES | jq -r '.raw_log')

# Code 4 is the standard SDK error for Unauthorized / signature verification failure in some contexts, 
# or the specific error code bubbled up from your module. 
# Your logs showed "code: 4", so we check for that.
if [ "$TX_CODE" == "4" ]; then
    echo "✅ SECURITY SUCCESS: Update blocked on-chain with Code 4 (Unauthorized)."
else
    echo "❌ SECURITY FAILURE: Carol's update transaction did not fail as expected."
    echo "Code: $TX_CODE"
    echo "Log: $RAW_LOG"
    exit 1
fi

# Attempt 2: Carol tries to delete Council permissions
echo "Carol attempting MsgDeletePolicyPermissions..."
SUBMIT_RES=$($BINARY tx commons delete-policy-permissions $COUNCIL_ADDR \
  --from carol -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

echo "Tx Hash: $TX_HASH"
echo "Waiting for block inclusion..."
sleep 3

# Query the On-Chain Result
TX_RES=$($BINARY query tx $TX_HASH --output json 2>/dev/null)
TX_CODE=$(echo $TX_RES | jq -r '.code')
RAW_LOG=$(echo $TX_RES | jq -r '.raw_log')

if [ "$TX_CODE" == "4" ]; then
    echo "✅ SECURITY SUCCESS: Delete blocked on-chain with Code 4 (Unauthorized)."
else
    echo "❌ SECURITY FAILURE: Carol's delete transaction did not fail as expected."
    echo "Code: $TX_CODE"
    echo "Log: $RAW_LOG"
    exit 1
fi

# --- 2. SUNSET PROTOCOL (GOVERNANCE) ---
echo "--- STEP 2: GOVERNANCE VOTES TO SUNSET (DELETE) COUNCIL ---"

# The Community decides the Council is no longer needed.
# We use MsgDeletePolicyPermissions signed by x/gov authority.
echo '{
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgDeletePolicyPermissions",
      "authority": "'$GOV_ADDR'",
      "policy_address": "'$COUNCIL_ADDR'"
    }
  ],
  "deposit": "100000000uspark",
  "title": "Sunset Commons Council",
  "summary": "Dissolving the Council by revoking all policy permissions.",
  "expedited": true
}' > proposals/gov_sunset.json

# Submit
SUBMIT_RES=$($BINARY tx gov submit-proposal proposals/gov_sunset.json --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
echo "Sunset Proposal Tx: $TX_HASH"
sleep 3

# Get Prop ID
GOV_PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

# Fallback
if [ -z "$GOV_PROP_ID" ]; then
    GOV_PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

echo "Gov Proposal ID: $GOV_PROP_ID"

# Vote YES
$BINARY tx gov vote $GOV_PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test

echo "Waiting for Expedited Voting (40s)..."
sleep 45

# Verify Deletion
PERMS_CHECK=$($BINARY query commons show-policy-permissions $COUNCIL_ADDR --output json 2>&1)

if echo "$PERMS_CHECK" | grep -q "key not found" || echo "$PERMS_CHECK" | grep -q "policy permissions not found"; then
    echo "✅ SUCCESS: Policy permissions verified deleted from state."
else
    echo "❌ FAILURE: Policy permissions still exist!"
    echo "$PERMS_CHECK"
    exit 1
fi

# --- 3. POST-MORTEM CHECK (DEAD COUNCIL) ---
echo "--- STEP 3: VERIFY COUNCIL IS FUNCTIONALLY DEAD ---"

# The Council tries to do a standard operation (e.g., Update Members)
# This used to be allowed. Now it should be blocked by AnteHandler.
echo '{
  "group_policy_address": "'$COUNCIL_ADDR'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Zombie Action",
  "summary": "Trying to act after sunset",
  "messages": [
    {
      "@type": "/cosmos.group.v1.MsgUpdateGroupMembers",
      "admin": "'$COUNCIL_ADDR'",
      "group_id": "1",
      "member_updates": []
    }
  ]
}' > proposals/msg_zombie.json

# Attempt Submission
# We force fees to ensure we pass the Fee AnteHandler and reach the Group Logic
OUTPUT=$($BINARY tx group submit-proposal proposals/msg_zombie.json --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark 2>&1)

# We expect the AnteHandler error: "no policy permissions found"
if echo "$OUTPUT" | grep -q "no policy permissions found"; then
    echo "✅ GRAND SUCCESS: The Council is effectively dead. AnteHandler rejected the tx."
else
    echo "❌ CRITICAL FAILURE: The Council was able to act (or got wrong error)!"
    echo "$OUTPUT"
    exit 1
fi