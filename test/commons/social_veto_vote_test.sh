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
    echo "[FAIL] SETUP ERROR: '$GROUP_NAME' not found. Run group_setup.sh first."
    exit 1
fi

echo "Signaling Policy Address: $POLICY_ADDR"

# Pre-fund the policy: needs 5 SPARK proposal fee + 1 uspark loopback +
# whatever the policy already holds. 50 SPARK is plenty.
$BINARY tx bank send $ALICE_ADDR $POLICY_ADDR 50000000uspark --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark > /dev/null 2>&1
sleep 3

# --- 2. CREATE SIGNAL PROPOSAL (x/commons format) ---
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
  "metadata": "OFFICIAL STATEMENT: We disapprove. Loopback signal."
}' > "$PROPOSAL_DIR/msg_social_signal.json"

# --- 3. SUBMIT (x/commons) ---
echo "Submitting Signal Proposal..."
SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/msg_social_signal.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
echo "Proposal Tx Hash: $TX_HASH"

echo "Waiting for block..."
sleep 3

# Get ID — x/commons emits "submit_proposal" events
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ]; then
    echo "[FAIL] ERROR: Could not extract proposal ID."
    echo "Tx Response: $TX_RES"
    exit 1
fi
echo "[ OK ] Signal Proposal ID: $PROPOSAL_ID"

# --- 4. VOTE ---
# Commons Council members (Alice & Bob from bootstrap) vote
echo "Alice voting YES..."
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark
sleep 3
echo "Bob voting YES..."
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark
sleep 3

echo "Votes cast. Attempting Execution..."

# --- 5. EXECUTE ---
EXEC_RES=$($BINARY tx commons execute-proposal $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --fees 5000000uspark --output json)
EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash')

echo "Waiting for execution block..."
sleep 3

# --- 6. VERIFY SIGNAL ---
echo "--- VERIFYING PERMANENT SIGNAL ---"

# Check execution success via proposal status
PROP_STATUS=$($BINARY query commons get-proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')
if [ "$PROP_STATUS" != "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "[FAIL] Execution Status: $PROP_STATUS (expected EXECUTED)"
    EXEC_TX_JSON=$($BINARY query tx $EXEC_TX_HASH --output json 2>/dev/null)
    echo "Raw Log: $(echo $EXEC_TX_JSON | jq -r '.raw_log // empty' 2>/dev/null)"
    exit 1
fi
echo "[ OK ] Execution Status: SUCCESS"

# 2. PERMANENT SIGNAL: the executed proposal is itself the on-chain
# evidence that the Commons Council formally registered the signal. Bank's
# SendCoins skips emitting a transfer event when sender == recipient
# (loopback optimization in the SDK), so we verify execution via the
# proposal's metadata + status rather than scanning bank events. The
# proposal record persists in the chain state forever.
PROP_META=$($BINARY query commons get-proposal $PROPOSAL_ID --output json | jq -r '.proposal.metadata // empty')
if [ -n "$PROP_META" ]; then
    echo "[ OK ] PERMANENT SIGNAL RECORDED:"
    echo "   Proposal ID: $PROPOSAL_ID"
    echo "   Status:      $PROP_STATUS"
    echo "   Metadata:    $PROP_META"
    echo "   Loopback:    $POLICY_ADDR (self-spend, 1 uspark)"
    echo "   Tx Hash:     $EXEC_TX_HASH"
else
    echo "[FAIL] FAILURE: Proposal metadata not preserved on-chain."
    EXEC_TX_JSON=$($BINARY query tx $EXEC_TX_HASH --output json 2>/dev/null)
    echo "Raw events: $(echo $EXEC_TX_JSON | jq '.events[] | select(.type=="transfer")' 2>/dev/null)"
    exit 1
fi
