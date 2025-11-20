#!/bin/bash

echo "--- TESTING: SOCIAL VETO (COMMONS COUNCIL SIGNAL) ---"

# --- 0. SETUP ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

# Discover Veto Policy
VETO_POLICY_ADDR=$($BINARY query group group-policies-by-group 1 -o json | jq -r '.group_policies[] | select(.metadata == "veto") | .address' | head -n 1)
echo "Commons Council Veto Policy: $VETO_POLICY_ADDR"

# --- 1. Create Veto Signal JSON (FIXED) ---
# We use a Loopback MsgSend (From VetoAddr -> To VetoAddr) as the signal.
echo '{
  "group_policy_address": "'$VETO_POLICY_ADDR'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "URGENT: VETO GOV PROPOSAL #1",
  "summary": "Signal: The Commons Council formally vetoes Gov Proposal #1",
  "messages": [
    {
      "@type": "/cosmos.bank.v1beta1.MsgSend",
      "from_address": "'$VETO_POLICY_ADDR'",
      "to_address": "'$VETO_POLICY_ADDR'",
      "amount": [{"denom": "uspark", "amount": "1"}]
    }
  ]
}' > msg_social_veto.json

# --- 2. Submit Proposal ---
echo "Submitting Veto Signal..."
SUBMIT_RES=$($BINARY tx group submit-proposal msg_social_veto.json --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

echo "Waiting for block..."
sleep 6

# Query Tx to get Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

# Fallback lookup
if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
    PROPOSAL_ID=$(echo $TX_RES | jq -r '.logs[0].events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi
echo "✅ Veto Proposal Submitted: ID $PROPOSAL_ID"

# --- 3. Vote (Threshold 50%) ---
echo "Alice voting YES..."
$BINARY tx group vote $PROPOSAL_ID $ALICE_ADDR VOTE_OPTION_YES "Veto confirmed" --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3
echo "Bob voting YES..."
$BINARY tx group vote $PROPOSAL_ID $BOB_ADDR VOTE_OPTION_YES "I agree to veto" --from bob -y --chain-id $CHAIN_ID --keyring-backend test

echo "Votes cast. Waiting for voting period to end (65s)..."
sleep 65

# --- 4. Execute (Formalizing the Decision) ---
echo "Executing Proposal..."
$BINARY tx group exec $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test

sleep 6

# --- 5. Verify Result ---
echo "--- CHECKING PROPOSAL STATUS ---"
STATUS=$($BINARY query group proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')
echo "Status: $STATUS"

if [ "$STATUS" == "PROPOSAL_STATUS_ACCEPTED" ] || [ "$STATUS" == "PROPOSAL_STATUS_PASSED" ]; then
  echo "✅ SUCCESS: The Social Veto was passed and executed (Signal Sent)."
else
  echo "❌ FAILURE: Status is $STATUS"
fi