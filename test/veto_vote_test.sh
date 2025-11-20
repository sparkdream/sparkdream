#!/bin/bash

echo "--- TESTING: VETO VOTE (PROPOSAL REJECTION) ---"

# --- 0. SETUP & ADDRESS DISCOVERY ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

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
}' > msg_veto_test.json

# --- 2. Submit Proposal ---
echo "Submitting proposal..."

# 1. Submit and capture ONLY the TxHash
SUBMIT_RES=$($BINARY tx group submit-proposal msg_veto_test.json --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

echo "Tx Hash: $TX_HASH"
echo "Waiting for block inclusion..."
sleep 6

# 2. Query the Transaction to get the actual events
echo "Querying Tx to find Proposal ID..."
TX_RES=$($BINARY query tx $TX_HASH --output json)

# 3. Extract Proposal ID
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
    # Fallback lookup
    PROPOSAL_ID=$(echo $TX_RES | jq -r '.logs[0].events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

echo "✅ Found Proposal ID: $PROPOSAL_ID"

# --- 3. Cast Veto Votes ---
echo "Alice voting NO_WITH_VETO..."
$BINARY tx group vote $PROPOSAL_ID $ALICE_ADDR VOTE_OPTION_NO_WITH_VETO "Block this" --from alice -y --chain-id $CHAIN_ID --keyring-backend test

sleep 3

echo "Bob voting NO_WITH_VETO..."
$BINARY tx group vote $PROPOSAL_ID $BOB_ADDR VOTE_OPTION_NO_WITH_VETO "I do not want this" --from bob -y --chain-id $CHAIN_ID --keyring-backend test

# FIX: Sleep for 65s because Policy Voting Period is 60s
echo "Votes cast. Waiting for voting period to end (65s)..."
sleep 65

# --- 4. Attempt Execution (Trigger Tally) ---
# Note: In x/group, status often updates "Lazily" when someone tries to Exec or Tally.
# Even if it fails, this command forces the chain to calculate the rejection.
echo "Attempting to execute (This triggers the Tally)..."
$BINARY tx group exec $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test

sleep 6

# --- 5. Verify Rejection ---
echo "--- CHECKING PROPOSAL STATUS ---"
STATUS=$($BINARY query group proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')
echo "Status: $STATUS"

if [ "$STATUS" == "PROPOSAL_STATUS_REJECTED" ]; then
  echo "✅ SUCCESS: Proposal was correctly REJECTED."
else
  echo "❌ FAILURE: Proposal status is $STATUS (Expected PROPOSAL_STATUS_REJECTED)."
fi

echo "--- VERIFYING BOB'S BALANCE (SHOULD BE UNCHANGED) ---"
$BINARY query bank balances $BOB_ADDR