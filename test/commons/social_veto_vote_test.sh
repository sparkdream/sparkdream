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
GROUP_INFO=$($BINARY query commons get-extended-group "$GROUP_NAME" --output json)
POLICY_ADDR=$(echo $GROUP_INFO | jq -r '.extended_group.policy_address')

if [ -z "$POLICY_ADDR" ] || [ "$POLICY_ADDR" == "null" ]; then
    echo "❌ SETUP ERROR: '$GROUP_NAME' not found. Run group_setup.sh first."
    exit 1
fi

echo "Signaling Policy Address: $POLICY_ADDR"

# Check Balance (Fund if needed for gas/spend)
BALANCE=$($BINARY query bank balances $POLICY_ADDR --output json | jq -r '.balances[] | select(.denom=="uspark") | .amount')
if [ -z "$BALANCE" ] || [ "$BALANCE" == "0" ]; then
    echo "Funding Policy Account..."
    $BINARY tx bank send $ALICE_ADDR $POLICY_ADDR 1000000uspark --from alice -y --chain-id $CHAIN_ID --keyring-backend test
    sleep 3
fi

# --- 2. CREATE SIGNAL PROPOSAL ---
# We use MsgSpendFromCommons for the loopback because MsgSend is likely blocked by PolicyPermissions.
echo '{
  "group_policy_address": "'$POLICY_ADDR'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "OFFICIAL STATEMENT: WE DISAPPROVE",
  "summary": "Signal: The Commons Council formally registers disapproval of recent events via on-chain signal.",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgSpendFromCommons",
      "authority": "'$POLICY_ADDR'",
      "recipient": "'$POLICY_ADDR'",
      "amount": [{"denom": "uspark", "amount": "1"}]
    }
  ]
}' > "$PROPOSAL_DIR/msg_social_signal.json"

# --- 3. SUBMIT ---
echo "Submitting Signal Proposal..."
SUBMIT_RES=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/msg_social_signal.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
echo "Proposal Tx Hash: $TX_HASH" 

echo "Waiting for block..."
sleep 3

# Get ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
if [ -z "$PROPOSAL_ID" ]; then
    PROPOSAL_ID=$(echo $TX_RES | jq -r '.logs[0].events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi
echo "✅ Signal Proposal ID: $PROPOSAL_ID"

# --- 4. VOTE ---
# Commons Council members (Alice & Bob from bootstrap) vote
echo "Alice voting YES..."
$BINARY tx group vote $PROPOSAL_ID $ALICE_ADDR VOTE_OPTION_YES "Confirmed" --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3
echo "Bob voting YES..."
$BINARY tx group vote $PROPOSAL_ID $BOB_ADDR VOTE_OPTION_YES "Confirmed" --from bob -y --chain-id $CHAIN_ID --keyring-backend test

echo "Votes cast. Attempting Execution..."
# No wait needed for Commons Council in testing environment

# --- 5. EXECUTE ---
EXEC_RES=$($BINARY tx group exec $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash')

echo "Waiting for execution block..."
sleep 3

# --- 6. VERIFY SIGNAL ---
echo "--- VERIFYING PERMANENT SIGNAL ---"

EXEC_TX_JSON=$($BINARY query tx $EXEC_TX_HASH --output json)

# 1. Check Execution Success
if echo "$EXEC_TX_JSON" | grep -q "PROPOSAL_EXECUTOR_RESULT_SUCCESS"; then
    echo "✅ Execution Status: SUCCESS"
else
    echo "❌ Execution Status: FAILED"
    echo "Raw: $(echo $EXEC_TX_JSON)"
    exit 1
fi

# 2. STRICT LOOPBACK CHECK
# Logic: 
#   1. Find events of type 'transfer'
#   2. Convert attributes array [{"key":"sender","value":"X"}, ...] into object {"sender":"X", ...}
#   3. Select only if sender == POLICY_ADDR AND recipient == POLICY_ADDR
LOOPBACK_AMOUNT=$(echo "$EXEC_TX_JSON" | jq -r --arg ADDR "$POLICY_ADDR" '
  .events[] 
  | select(.type=="transfer") 
  | .attributes 
  | map({(.key): .value}) 
  | add 
  | select(.sender == $ADDR and .recipient == $ADDR) 
  | .amount
')

if [ -n "$LOOPBACK_AMOUNT" ] && [ "$LOOPBACK_AMOUNT" != "null" ]; then
    echo "✅ PERMANENT SIGNAL FOUND: Loopback confirmed."
    echo "   Sender:    $POLICY_ADDR"
    echo "   Recipient: $POLICY_ADDR"
    echo "   Amount:    $LOOPBACK_AMOUNT"
    echo "   Tx Hash:   $EXEC_TX_HASH"
else
    echo "❌ FAILURE: No valid loopback transfer found (Sender != Recipient)."
    echo "Raw Events: $(echo $EXEC_TX_JSON | jq '.events[] | select(.type=="transfer")')"
    exit 1
fi