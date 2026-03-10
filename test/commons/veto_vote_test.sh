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
GROUP_INFO=$($BINARY query commons get-group "$GROUP_NAME" --output json)
POLICY_ADDR=$(echo $GROUP_INFO | jq -r '.group.policy_address')

if [ -z "$POLICY_ADDR" ] || [ "$POLICY_ADDR" == "null" ]; then
    echo "SETUP ERROR: '$GROUP_NAME' not found. Run genesis/bootstrap first."
    exit 1
fi

echo "$GROUP_NAME Policy Address: $POLICY_ADDR"

# --- CHECK BOB'S BALANCE ---
echo "--- SNAPSHOT: BOB'S BALANCE (BEFORE) ---"
INITIAL_BAL=$($BINARY query bank balances $BOB_ADDR --output json | jq -r '.balances[] | select(.denom=="uspark") | .amount')
if [ -z "$INITIAL_BAL" ]; then INITIAL_BAL=0; fi
echo "Bob's Initial Balance: $INITIAL_BAL"

# --- 1. Create Proposal JSON ---
# We propose sending a massive amount (500 SPARK) to Bob.
echo '{
  "policy_address": "'$POLICY_ADDR'",
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
  ],
  "metadata": "Controversial spend - should be vetoed by committee"
}' > "$PROPOSAL_DIR/msg_veto_test.json"

# --- 2. Submit Proposal ---
echo "Submitting proposal..."

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/msg_veto_test.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

echo "Tx Hash: $TX_HASH"
echo "Waiting for block inclusion..."
sleep 5

# Query Tx to find Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
    echo "ERROR: Could not find Proposal ID."
    echo "Tx Response: $TX_RES"
    exit 1
fi

echo "Found Proposal ID: $PROPOSAL_ID"

# --- 3. Cast Veto Votes ---
# Alice and Bob are members of the committee.
# NO_WITH_VETO votes don't count as YES votes, so the threshold won't be met.

echo "Alice voting NO_WITH_VETO..."
$BINARY tx commons vote-proposal $PROPOSAL_ID no-with-veto --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5

echo "Bob voting NO_WITH_VETO..."
$BINARY tx commons vote-proposal $PROPOSAL_ID no-with-veto --from bob -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5

# --- 4. Check Proposal Status ---
# With NO_WITH_VETO votes, the YES threshold is not met.
# The proposal should NOT be ACCEPTED. It stays SUBMITTED until EndBlock expires it.
echo "--- CHECKING PROPOSAL STATUS ---"
STATUS=$($BINARY query commons get-proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')
echo "Status: $STATUS"

if [ "$STATUS" == "PROPOSAL_STATUS_ACCEPTED" ] || [ "$STATUS" == "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "FAILURE: Proposal was incorrectly accepted/executed despite NO_WITH_VETO votes."
    exit 1
else
    echo "SUCCESS: Proposal was NOT accepted (Status: $STATUS)."
fi

# --- 5. Check that money did NOT move ---
echo "--- VERIFYING BOB'S BALANCE (SHOULD BE UNCHANGED) ---"
FINAL_BAL=$($BINARY query bank balances $BOB_ADDR --output json | jq -r '.balances[] | select(.denom=="uspark") | .amount')
if [ -z "$FINAL_BAL" ]; then FINAL_BAL=0; fi

echo "Bob's Initial Balance: $INITIAL_BAL"
echo "Bob's Final Balance:   $FINAL_BAL"

if [ "$FINAL_BAL" == "$INITIAL_BAL" ]; then
    echo "SUCCESS: Bob's balance is unchanged. Funds were NOT transferred."
else
    echo "FAILURE: Bob's balance changed (Expected $INITIAL_BAL, Got $FINAL_BAL)."
fi
