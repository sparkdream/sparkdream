#!/bin/bash

echo "--- TESTING: VETO VOTE (PROPOSAL REJECTION) ---"

# --- 0. SETUP & ADDRESS DISCOVERY ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

# Create output dir if not exists
mkdir -p proposals

# Get the Commons Council Address
COMMONS_COUNCIL_ADDR=$($BINARY query split params --output json | jq -r '.params.commons_council_address')

echo "Commons Council Address: $COMMONS_COUNCIL_ADDR"
echo "--- SNAPSHOT: BOB'S BALANCE (BEFORE) ---"
$BINARY query bank balances $BOB_ADDR

# --- 1. Create Proposal JSON ---
echo '{
  "group_policy_address": "'$COMMONS_COUNCIL_ADDR'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Controversial Spend",
  "summary": "This proposal should be vetoed",
  "messages": [
    {
      "@type": "/sparkdream.split.v1.MsgSpendFromCommons",
      "authority": "'$COMMONS_COUNCIL_ADDR'",
      "recipient": "'$BOB_ADDR'",
      "amount": [
        {
          "denom": "uspark",
          "amount": "500000000"
        }
      ] 
    }
  ]
}' > proposals/msg_veto_test.json

# --- 2. Submit Proposal ---
echo "Submitting proposal..."

# Submit and capture output
# Added --fees to ensure it passes FeeDecorator
SUBMIT_RES=$($BINARY tx group submit-proposal proposals/msg_veto_test.json --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

echo "Tx Hash: $TX_HASH"
echo "Waiting for block inclusion (3s)..."
sleep 3

# Query Tx to find Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

# Fallback lookup
if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
    PROPOSAL_ID=$(echo $TX_RES | jq -r '.logs[0].events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
    echo "❌ ERROR: Could not find Proposal ID. Submission might have failed."
    echo "Tx Response: $TX_RES"
    exit 1
fi

echo "✅ Found Proposal ID: $PROPOSAL_ID"

# --- 3. Cast Veto Votes ---
echo "Alice voting NO_WITH_VETO..."
$BINARY tx group vote $PROPOSAL_ID $ALICE_ADDR VOTE_OPTION_NO_WITH_VETO "Block this" --from alice -y --chain-id $CHAIN_ID --keyring-backend test

sleep 3

echo "Bob voting NO_WITH_VETO..."
$BINARY tx group vote $PROPOSAL_ID $BOB_ADDR VOTE_OPTION_NO_WITH_VETO "I do not want this" --from bob -y --chain-id $CHAIN_ID --keyring-backend test

# OPTIMIZATION: Wait 35s. 
# The Standard Policy (used for spending) has a 30s voting period in group_setup.sh.
echo "Votes cast. Waiting for voting period to end (35s)..."
sleep 35

# --- 4. Attempt Execution (Trigger Tally) ---
# Even though it's rejected, we run 'exec' to force the state update to PROPOSAL_STATUS_REJECTED.
echo "Attempting to execute (This triggers the Tally)..."
EXEC_RES=$($BINARY tx group exec $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash')

sleep 3

# --- 5. Verify Rejection ---
echo "--- CHECKING PROPOSAL STATUS ---"
STATUS=$($BINARY query group proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')
echo "Status: $STATUS"

if [ "$STATUS" == "PROPOSAL_STATUS_REJECTED" ]; then
  echo "✅ SUCCESS: Proposal was correctly REJECTED."
else
  echo "❌ FAILURE: Proposal status is $STATUS (Expected PROPOSAL_STATUS_REJECTED)."
  echo "   Tip: If status is SUBMITTED, voting period hasn't ended. If ACCEPTED, veto math is wrong."
fi

# Check that money did NOT move
echo "--- VERIFYING BOB'S BALANCE (SHOULD BE UNCHANGED) ---"
$BINARY query bank balances $BOB_ADDR