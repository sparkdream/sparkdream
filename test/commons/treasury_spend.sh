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
GROUP_INFO=$($BINARY query commons get-extended-group "$GROUP_NAME" --output json)
POLICY_ADDR=$(echo $GROUP_INFO | jq -r '.extended_group.policy_address')

if [ -z "$POLICY_ADDR" ] || [ "$POLICY_ADDR" == "null" ]; then
    echo "❌ SETUP ERROR: '$GROUP_NAME' not found. Run genesis/bootstrap first."
    exit 1
fi

echo "$GROUP_NAME Policy Address: $POLICY_ADDR"

# FUND THE COMMITTEE (Since x/split funds the Council, the Committee starts empty)
echo "Funding Committee Treasury (Seeding from Alice)..."
$BINARY tx bank send $ALICE_ADDR $POLICY_ADDR 10000000uspark --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3

# Check Bob's Initial Balance
INITIAL_BAL=$($BINARY query bank balances $BOB_ADDR --output json | jq -r '.balances[] | select(.denom=="uspark") | .amount')
if [ -z "$INITIAL_BAL" ]; then INITIAL_BAL=0; fi
echo "Bob's Initial Balance: $INITIAL_BAL"

# --- 1. Create the Proposal JSON ---
# Amount: 1 SPARK (1,000,000 uspark)
echo '{
  "group_policy_address": "'$POLICY_ADDR'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Test Spend",
  "summary": "Send 1 SPARK to Bob from Operational Budget",
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
  ]
}' > "$PROPOSAL_DIR/msg_spend_test.json"

# --- 2. Submit Proposal ---
echo "Submitting proposal..."

SUBMIT_RES=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/msg_spend_test.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

echo "Tx Hash: $TX_HASH"
echo "Waiting for block inclusion..."
sleep 3

# Query Tx to find the Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
    # Fallback
    PROPOSAL_ID=$(echo $TX_RES | jq -r '.logs[0].events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

if [ -z "$PROPOSAL_ID" ]; then
    echo "❌ ERROR: Failed to create Proposal."
    echo "Tx Response: $TX_RES"
    exit 1
fi

echo "✅ Proposal ID: $PROPOSAL_ID"

# --- 3. Vote ---
# Alice and Bob are both members of the Committee (from bootstrap logic)
echo "Alice Voting YES..."
$BINARY tx group vote $PROPOSAL_ID $ALICE_ADDR VOTE_OPTION_YES "Approve" --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3
echo "Bob Voting YES..."
$BINARY tx group vote $PROPOSAL_ID $BOB_ADDR VOTE_OPTION_YES "Approve" --from bob -y --chain-id $CHAIN_ID --keyring-backend test

echo "Votes cast. Waiting for voting period (24h? No, check config)..."
# Bootstrap config for Committee says "24h voting period", but "0s execution delay".
# However, x/group has "TryExec" feature. If we have enough votes (threshold met), we can execute immediately.
# Threshold for committee is "2". Alice + Bob = 2. We can exec now.

# --- 4. Execute ---
echo "Executing Proposal $PROPOSAL_ID..."
EXEC_RES=$($BINARY tx group exec $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash')

echo "Waiting for execution block..."
sleep 3

# Verify Execution
EXEC_TX_JSON=$($BINARY query tx $EXEC_TX_HASH --output json)
if echo "$EXEC_TX_JSON" | grep -q "PROPOSAL_EXECUTOR_RESULT_SUCCESS"; then
    echo "✅ Execution Successful."
else
    echo "❌ Execution Failed."
    echo "Raw Log: $(echo $EXEC_TX_JSON | jq -r '.raw_log')"
    exit 1
fi

# --- 5. Verify Balance ---
FINAL_BAL=$($BINARY query bank balances $BOB_ADDR --output json | jq -r '.balances[] | select(.denom=="uspark") | .amount')
echo "Bob's Final Balance:   $FINAL_BAL"

# Calculate Difference
DIFF=$((FINAL_BAL - INITIAL_BAL))

if [ "$DIFF" == "1000000" ]; then
    echo "🎉 SUCCESS: Bob received exactly 1,000,000 uspark."
else
    echo "❌ FAILURE: Balance difference is $DIFF (Expected 1000000)."
fi