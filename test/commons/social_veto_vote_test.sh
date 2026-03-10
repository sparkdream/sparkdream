#!/bin/bash

echo "--- TESTING: SOCIAL SIGNAL (COMMONS COUNCIL LOOPBACK) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

# --- 1. DISCOVERY ---
GROUP_NAME="Commons Council"

echo "Looking up '$GROUP_NAME'..."
GROUP_INFO=$($BINARY query commons get-group "$GROUP_NAME" --output json)
POLICY_ADDR=$(echo $GROUP_INFO | jq -r '.group.policy_address')

if [ -z "$POLICY_ADDR" ] || [ "$POLICY_ADDR" == "null" ]; then
    echo "SETUP ERROR: '$GROUP_NAME' not found. Run group_setup.sh first."
    exit 1
fi

echo "Signaling Policy Address: $POLICY_ADDR"

# Check Balance (Fund if needed for gas/spend)
BALANCE=$($BINARY query bank balances $POLICY_ADDR --output json | jq -r '.balances[] | select(.denom=="uspark") | .amount')
if [ -z "$BALANCE" ] || [ "$BALANCE" == "0" ]; then
    echo "Funding Policy Account..."
    $BINARY tx bank send $ALICE_ADDR $POLICY_ADDR 1000000uspark --from alice -y --chain-id $CHAIN_ID --keyring-backend test
    sleep 5
fi

# --- 2. CREATE SIGNAL PROPOSAL ---
# We use MsgSpendFromCommons for the loopback because MsgSend is likely blocked by PolicyPermissions.
echo '{
  "policy_address": "'$POLICY_ADDR'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgSpendFromCommons",
      "authority": "'$POLICY_ADDR'",
      "recipient": "'$POLICY_ADDR'",
      "amount": [{"denom": "uspark", "amount": "1"}]
    }
  ],
  "metadata": "OFFICIAL STATEMENT: The Commons Council formally registers disapproval of recent events via on-chain signal."
}' > "$PROPOSAL_DIR/msg_social_signal.json"

# --- 3. SUBMIT ---
echo "Submitting Signal Proposal..."
SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/msg_social_signal.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
echo "Proposal Tx Hash: $TX_HASH"

echo "Waiting for block..."
sleep 5

# Get ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
    echo "ERROR: Could not find Proposal ID."
    echo "Tx Response: $TX_RES"
    exit 1
fi
echo "Signal Proposal ID: $PROPOSAL_ID"

# --- 4. VOTE ---
# Commons Council members (Alice & Bob from bootstrap) vote
echo "Alice voting YES..."
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5
echo "Bob voting YES..."
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5

# --- 5. EXECUTE ---
EXEC_RES=$($BINARY tx commons execute-proposal $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --output json)
EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash')

echo "Waiting for execution block..."
sleep 5

# --- 6. VERIFY SIGNAL ---
echo "--- VERIFYING PERMANENT SIGNAL ---"

# 1. Check Execution Success via proposal status
PROP_STATUS=$($BINARY query commons get-proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')
if [ "$PROP_STATUS" == "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "Execution Status: SUCCESS"
else
    echo "Execution Status: FAILED (Status: $PROP_STATUS)"
    exit 1
fi

# 2. VERIFY ON-CHAIN SIGNAL RECORD
# The executed proposal serves as the permanent on-chain signal.
# Verify the metadata contains the council's statement.
PROPOSAL_JSON=$($BINARY query commons get-proposal $PROPOSAL_ID --output json)
METADATA=$(echo "$PROPOSAL_JSON" | jq -r '.proposal.metadata')

if echo "$METADATA" | grep -q "OFFICIAL STATEMENT"; then
    echo "PERMANENT SIGNAL FOUND: Council statement recorded on-chain."
    echo "   Proposal ID: $PROPOSAL_ID"
    echo "   Status:      EXECUTED"
    echo "   Statement:   $METADATA"
    echo "   Tx Hash:     $EXEC_TX_HASH"
else
    echo "FAILURE: Signal metadata not found in executed proposal."
    echo "Metadata: $METADATA"
    exit 1
fi
