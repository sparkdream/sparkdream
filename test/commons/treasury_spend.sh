#!/bin/bash

echo "--- TESTING SPEND: COMMONS OPS COMMITTEE (OPERATIONAL SPEND) ---"

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

# FUND THE COMMITTEE (Since x/split funds the Council, the Committee starts empty)
echo "Funding Committee Treasury (Seeding from Alice)..."
$BINARY tx bank send $ALICE_ADDR $POLICY_ADDR 10000000uspark --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5

# Check Bob's Initial Balance
INITIAL_BAL=$($BINARY query bank balances $BOB_ADDR --output json | jq -r '.balances[] | select(.denom=="uspark") | .amount')
if [ -z "$INITIAL_BAL" ]; then INITIAL_BAL=0; fi
echo "Bob's Initial Balance: $INITIAL_BAL"

# --- 1. Create the Proposal JSON ---
# Amount: 1 SPARK (1,000,000 uspark)
# Committee threshold=1, so Alice's single vote triggers early acceptance.
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
          "amount": "1000000"
        }
      ]
    }
  ],
  "metadata": "Send 1 SPARK to Bob from Operational Budget"
}' > "$PROPOSAL_DIR/msg_spend_test.json"

# --- 2. Submit Proposal ---
echo "Submitting proposal..."

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/msg_spend_test.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

echo "Tx Hash: $TX_HASH"
echo "Waiting for block inclusion..."
sleep 5

# Query Tx to find the Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
    echo "ERROR: Failed to create Proposal."
    echo "Tx Response: $TX_RES"
    exit 1
fi

echo "Proposal ID: $PROPOSAL_ID"

# --- 3. Vote ---
# Alice is a member of the Committee (from bootstrap logic)
# Threshold=1, so Alice's single vote triggers early acceptance.
echo "Alice Voting YES..."
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5

# --- 4. Execute ---
echo "Executing Proposal $PROPOSAL_ID..."
EXEC_RES=$($BINARY tx commons execute-proposal $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --output json)
EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash')

echo "Waiting for execution block..."
sleep 5

# Verify Execution
PROP_STATUS=$($BINARY query commons get-proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')
if [ "$PROP_STATUS" == "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "Execution Successful."
else
    echo "Execution Failed. Status: $PROP_STATUS"
    exit 1
fi

# --- 5. Verify Balance ---
FINAL_BAL=$($BINARY query bank balances $BOB_ADDR --output json | jq -r '.balances[] | select(.denom=="uspark") | .amount')
echo "Bob's Final Balance:   $FINAL_BAL"

# Calculate Difference
DIFF=$((FINAL_BAL - INITIAL_BAL))

if [ "$DIFF" == "1000000" ]; then
    echo "SUCCESS: Bob received exactly 1,000,000 uspark."
else
    echo "FAILURE: Balance difference is $DIFF (Expected 1000000)."
fi
