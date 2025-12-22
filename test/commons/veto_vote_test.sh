#!/bin/bash

echo "--- TESTING: VETO VOTE (PROPOSAL REJECTION) ---"

# --- 0. SETUP & ADDRESS DISCOVERY ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

GROUP_NAME="Commons Operations Committee"

echo "Looking up '$GROUP_NAME'..."
GROUP_INFO=$($BINARY query commons get-extended-group "$GROUP_NAME" --output json)
POLICY_ADDR=$(echo $GROUP_INFO | jq -r '.extended_group.policy_address')

if [ -z "$POLICY_ADDR" ] || [ "$POLICY_ADDR" == "null" ]; then
    echo "❌ SETUP ERROR: '$GROUP_NAME' not found. Run genesis/bootstrap first."
    exit 1
fi

echo "$GROUP_NAME Policy Address: $POLICY_ADDR"

# --- CHECK BOB'S BALANCE ---
echo "--- SNAPSHOT: BOB'S BALANCE (BEFORE) ---"
$BINARY query bank balances $BOB_ADDR

# --- 1. Create Proposal JSON ---
# We propose sending a massive amount (500 SPARK) to Bob.
echo '{
  "group_policy_address": "'$POLICY_ADDR'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Controversial Spend",
  "summary": "This proposal should be vetoed by the committee.",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgSpendFromCommons",
      "authority": "'$POLICY_ADDR'",
      "recipient": "'$BOB_ADDR'",
      "amount": [
        {
          "denom": "uspark",
          "amount": "500000000"
        }
      ] 
    }
  ]
}' > "$PROPOSAL_DIR/msg_veto_test.json"

# --- 2. Submit Proposal ---
echo "Submitting proposal..."

# Added --fees to ensure it passes FeeDecorator
SUBMIT_RES=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/msg_veto_test.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

echo "Tx Hash: $TX_HASH"
echo "Waiting for block inclusion..."
sleep 3

# Query Tx to find Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
    # Fallback
    PROPOSAL_ID=$(echo $TX_RES | jq -r '.logs[0].events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
    echo "❌ ERROR: Could not find Proposal ID."
    echo "Tx Response: $TX_RES"
    exit 1
fi

echo "✅ Found Proposal ID: $PROPOSAL_ID"

# --- 3. Cast Veto Votes ---
# Alice and Bob are members of the committee.
# Voting NO_WITH_VETO usually counts strongly against passing.

echo "Alice voting NO_WITH_VETO..."
$BINARY tx group vote $PROPOSAL_ID $ALICE_ADDR VOTE_OPTION_NO_WITH_VETO "Block this" --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3

echo "Bob voting NO_WITH_VETO..."
$BINARY tx group vote $PROPOSAL_ID $BOB_ADDR VOTE_OPTION_NO_WITH_VETO "I do not want this" --from bob -y --chain-id $CHAIN_ID --keyring-backend test

echo "Votes cast. Attempting Execution (Early Tally)..."
# We do not wait for the 24h voting period. 
# Since Alice and Bob constitute the majority/all of the committee weight,
# x/group allows 'TryExec' to tally immediately.

# --- 4. Attempt Execution (Trigger Tally) ---
# Even though it's rejected, we run 'exec' to force the state update to PROPOSAL_STATUS_REJECTED.
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
  echo "   Note: If status is PROPOSAL_STATUS_SUBMITTED, the total voting weight wasn't enough to trigger early tally."
fi

# Check that money did NOT move
echo "--- VERIFYING BOB'S BALANCE (SHOULD BE UNCHANGED) ---"
FINAL_BAL=$($BINARY query bank balances $BOB_ADDR --output json | jq -r '.balances[] | select(.denom=="uspark") | .amount')
if [ -z "$FINAL_BAL" ]; then FINAL_BAL=0; fi

# Note: We didn't capture initial balance variable, but we printed it. 
# Visually verify it matches.
echo "Bob's Final Balance: $FINAL_BAL"