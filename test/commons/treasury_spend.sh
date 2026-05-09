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
    echo "[FAIL] SETUP ERROR: '$GROUP_NAME' not found. Run genesis/bootstrap first."
    exit 1
fi

echo "$GROUP_NAME Policy Address: $POLICY_ADDR"

# FUND THE COMMITTEE (Since x/split funds the Council, the Committee starts empty).
# 50 SPARK = enough for the 1-SPARK spend + per-proposal fee + safety margin.
echo "Funding Committee Treasury (Seeding from Alice)..."
$BINARY tx bank send $ALICE_ADDR $POLICY_ADDR 50000000uspark --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark > /dev/null 2>&1
sleep 3

# Check Bob's Initial Balance
INITIAL_BAL=$($BINARY query bank balances $BOB_ADDR --output json | jq -r '.balances[] | select(.denom=="uspark") | .amount')
if [ -z "$INITIAL_BAL" ]; then INITIAL_BAL=0; fi
echo "Bob's Initial Balance: $INITIAL_BAL"

# --- 1. Create the Proposal JSON (x/commons format) ---
# Amount: 1 SPARK (1,000,000 uspark)
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
  "metadata": "Send 1 SPARK to Bob from operational budget"
}' > "$PROPOSAL_DIR/msg_spend_test.json"

# --- 2. Submit Proposal (x/commons) ---
echo "Submitting proposal..."

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/msg_spend_test.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

echo "Tx Hash: $TX_HASH"
echo "Waiting for block inclusion..."
sleep 3

# Query Tx to find the Proposal ID — x/commons emits "submit_proposal" events
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ]; then
    echo "[FAIL] ERROR: Failed to create Proposal."
    echo "Tx Response: $TX_RES"
    exit 1
fi

echo "[ OK ] Proposal ID: $PROPOSAL_ID"

# --- 3. Vote ---
# Alice and Bob are both members of the Committee (from bootstrap logic)
echo "Alice Voting YES..."
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark
sleep 3
echo "Bob Voting YES..."
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark
sleep 3

echo "Votes cast. Early acceptance triggers when threshold is met..."

# --- 4. Execute ---
# Bootstrap committee MinExecutionPeriod is 1s under testparams.
echo "Executing Proposal $PROPOSAL_ID..."
EXEC_RES=$($BINARY tx commons execute-proposal $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --fees 5000000uspark --output json)
EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 3

# Verify Execution by checking proposal status
PROP_STATUS=$($BINARY query commons get-proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')
if [ "$PROP_STATUS" == "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "[ OK ] Execution Successful (status=$PROP_STATUS)."
else
    echo "[FAIL] Execution did not complete (status=$PROP_STATUS)."
    EXEC_TX_JSON=$($BINARY query tx $EXEC_TX_HASH --output json 2>/dev/null)
    echo "Exec Tx Raw Log: $(echo $EXEC_TX_JSON | jq -r '.raw_log // empty' 2>/dev/null)"
    exit 1
fi

# --- 5. Verify Balance ---
FINAL_BAL=$($BINARY query bank balances $BOB_ADDR --output json | jq -r '.balances[] | select(.denom=="uspark") | .amount')
echo "Bob's Final Balance:   $FINAL_BAL"

# Calculate Difference
DIFF=$((FINAL_BAL - INITIAL_BAL))

# Bob votes (paying ~5_000 uspark gas) and receives 1_000_000 uspark from the
# spend, so the net delta is 1_000_000 minus Bob's vote-tx fee. Allow a small
# margin to absorb fee variance.
if [ "$DIFF" -ge 990000 ] && [ "$DIFF" -le 1000000 ]; then
    echo "[ OK ] SUCCESS: Bob received ~1,000,000 uspark (delta=$DIFF, fee already deducted)."
else
    echo "[FAIL] FAILURE: Balance difference is $DIFF (Expected ~1000000)."
    exit 1
fi
