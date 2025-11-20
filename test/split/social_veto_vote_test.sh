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

# Check Balance (Fund if needed)
BALANCE=$($BINARY query bank balances $VETO_POLICY_ADDR --output json | jq -r '.balances[] | select(.denom=="uspark") | .amount')
if [ -z "$BALANCE" ] || [ "$BALANCE" == "0" ]; then
    echo "Funding Veto Policy..."
    $BINARY tx bank send $ALICE_ADDR $VETO_POLICY_ADDR 1000000uspark --from alice -y --chain-id $CHAIN_ID --keyring-backend test
    sleep 3
fi

# --- 1. Create Veto Signal JSON ---
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
}' > proposals/msg_social_veto.json

# --- 2. Submit Proposal ---
echo "Submitting Veto Signal..."
SUBMIT_RES=$($BINARY tx group submit-proposal proposals/msg_social_veto.json --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
echo "Proposal Tx Hash: $TX_HASH" # <-- This Hash contains the Title/Summary Forever

echo "Waiting for block..."
sleep 3

# Query Tx to get Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

# Fallback lookup
if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
    PROPOSAL_ID=$(echo $TX_RES | jq -r '.logs[0].events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi
echo "✅ Veto Proposal Submitted: ID $PROPOSAL_ID"

# --- 3. Vote ---
echo "Alice voting YES..."
$BINARY tx group vote $PROPOSAL_ID $ALICE_ADDR VOTE_OPTION_YES "Veto confirmed" --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3
echo "Bob voting YES..."
$BINARY tx group vote $PROPOSAL_ID $BOB_ADDR VOTE_OPTION_YES "I agree to veto" --from bob -y --chain-id $CHAIN_ID --keyring-backend test

echo "Votes cast. Waiting for Veto voting period (12s)..."
sleep 12

# --- 4. Execute ---
echo "Executing Proposal..."
EXEC_RES=$($BINARY tx group exec $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash')

echo "Waiting for execution block..."
sleep 3

# --- 5. Verify THE PERMANENT SIGNAL ---
echo "--- VERIFYING PERMANENT SIGNAL ---"

# Instead of querying the pruned proposal, we query the EXECUTION TRANSACTION.
# We look for the "Loopback" event: Sender=VetoPolicy AND Recipient=VetoPolicy.

EXEC_TX_JSON=$($BINARY query tx $EXEC_TX_HASH --output json)

# 1. Check for Successful Execution
if echo "$EXEC_TX_JSON" | grep -q "PROPOSAL_EXECUTOR_RESULT_SUCCESS"; then
    echo "✅ Execution Status: SUCCESS"
else
    echo "❌ Execution Status: FAILED"
    exit 1
fi

# 2. Check for the Loopback Event (The "Signal")
# We check if the Veto Address appears as both Sender and Recipient in the events
TRANSFERS=$(echo $EXEC_TX_JSON | grep "$VETO_POLICY_ADDR")

if [ ! -z "$TRANSFERS" ]; then
    echo "✅ PERMANENT SIGNAL FOUND: The Veto Policy successfully messaged itself."
    echo "   Tx Hash: $EXEC_TX_HASH"
    echo "   This hash is the permanent proof of the Council's Veto."
else
    echo "❌ FAILURE: Loopback signal not found in transaction events."
fi