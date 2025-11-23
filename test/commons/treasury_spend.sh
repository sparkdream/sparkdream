#!/bin/bash

echo "--- TESTING SPEND: SUCCESSFUL TREASURY SPEND (25% VOTES) ---"

# --- 0. SETUP & ADDRESS DISCOVERY ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

# Discover the Commons Council Address
COMMONS_COUNCIL_ADDR=$($BINARY query commons params --output json | jq -r '.params.commons_council_address')

echo "Commons Council Address: $COMMONS_COUNCIL_ADDR"
echo "--- CHECKING BOB'S INITIAL BALANCE ---"
$BINARY query bank balances $BOB_ADDR

# --- 1. Create the Proposal JSON ---
echo '{
  "group_policy_address": "'$COMMONS_COUNCIL_ADDR'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Test Spend",
  "summary": "Send 1 SPARK to Bob",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgSpendFromCommons",
      "authority": "'$COMMONS_COUNCIL_ADDR'",
      "recipient": "'$BOB_ADDR'",
      "amount": [
        {
          "denom": "uspark",
          "amount": "1000000"
        }
      ] 
    }
  ]
}' > proposals/msg_spend_test.json

# --- 2. Submit Proposal (FIX: Capture ID dynamically) ---
echo "Submitting proposal..."

# Capture the Output
SUBMIT_RES=$($BINARY tx group submit-proposal proposals/msg_spend_test.json --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

echo "Tx Hash: $TX_HASH"
echo "Waiting for block inclusion (3s)..."
sleep 3

# Query Tx to find the REAL Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

# Fallback lookup (sometimes events are inside logs)
if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
    PROPOSAL_ID=$(echo $TX_RES | jq -r '.logs[0].events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
    echo "❌ ERROR: Failed to create Proposal. Tx failed or ID not found."
    echo "Tx Response: $TX_RES"
    exit 1
fi

echo "✅ Target Proposal ID: $PROPOSAL_ID"

# --- 3. Vote from Bob ---
echo "Bob Voting on Proposal $PROPOSAL_ID..."
$BINARY tx group vote $PROPOSAL_ID $BOB_ADDR VOTE_OPTION_YES "Agreed" --from bob -y --chain-id $CHAIN_ID --keyring-backend test

echo "Votes cast. Waiting for voting period to end (30s)..."
sleep 32 

# --- 4. Execute the Passed Proposal ---
echo "Executing Proposal $PROPOSAL_ID..."
EXEC_RES=$($BINARY tx group exec $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash')

sleep 3

# Verify Execution
EXEC_TX_JSON=$($BINARY query tx $EXEC_TX_HASH --output json)
if echo "$EXEC_TX_JSON" | grep -q "PROPOSAL_EXECUTOR_RESULT_SUCCESS"; then
    echo "✅ Execution Successful."
else
    echo "❌ Execution Failed."
    echo "Raw Log: $(echo $EXEC_TX_JSON | jq -r '.raw_log')"
fi

echo "--- VERIFYING BOB'S BALANCE (Should be +1 SPARK) ---"
$BINARY query bank balances $BOB_ADDR