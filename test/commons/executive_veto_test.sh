#!/bin/bash

echo "--- TESTING: EXECUTIVE VETO (EMERGENCY CANCEL) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

# Gov Address
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')
echo "Gov Module Address: $GOV_ADDR"

echo "Scanning for Veto Policy..."
# Check Group 1 (Commons Council) first. Alice and Bob are members here.
VETO_POLICY_ADDR=$($BINARY query group group-policies-by-group 1 --output json | jq -r '.group_policies[] | select(.metadata == "veto") | .address' | head -n 1)

if [ -z "$VETO_POLICY_ADDR" ] || [ "$VETO_POLICY_ADDR" == "null" ]; then
    echo "❌ ERROR: Could not find Veto Policy for Group 1"
    exit 1
fi

echo "✅ Target Veto Policy Address: $VETO_POLICY_ADDR"

# --- 1. ATTACK: Create & Vote on "Bad" Governance Proposal ---
echo "--- PHASE 1: THE ATTACK ---"
echo "Creating a malicious Governance Proposal..."

# Message: Treasury pays Alice
echo '{
  "messages": [
    {
      "@type": "/cosmos.bank.v1beta1.MsgSend",
      "from_address": "'$GOV_ADDR'",
      "to_address": "'$ALICE_ADDR'",
      "amount": [{"denom": "uspark", "amount": "1"}]
    }
  ],
  "deposit": "50000000uspark",
  "title": "Malicious Treasury Drain",
  "summary": "This proposal attempts to steal funds."
}' > "$PROPOSAL_DIR/bad_proposal.json"

SUBMIT_RES=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/bad_proposal.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

echo "Submitted Gov Prop. Hash: $TX_HASH"
echo "Waiting for block inclusion (3s)..."
sleep 3

# Get Gov Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
GOV_PROP_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
if [ -z "$GOV_PROP_ID" ] || [ "$GOV_PROP_ID" == "null" ]; then
   GOV_PROP_ID=$(echo $TX_RES | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

echo "⚠️  Target Gov Proposal ID: $GOV_PROP_ID"

# --- ALICE VOTES YES ---
echo "⚠️  Alice votes YES..."
$BINARY tx gov vote $GOV_PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3

# --- 2. DEFENSE: Create the "Kill Switch" Group Proposal ---
echo "--- PHASE 2: THE DEFENSE ---"
echo "Creating Executive Veto Proposal..."

echo '{
  "group_policy_address": "'$VETO_POLICY_ADDR'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "EXECUTIVE ORDER: CANCEL PROP '$GOV_PROP_ID'",
  "summary": "Immediate cancellation of malicious proposal.",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgEmergencyCancelGovProposal",
      "authority": "'$VETO_POLICY_ADDR'",
      "proposal_id": '$GOV_PROP_ID'
    }
  ]
}' > "$PROPOSAL_DIR/msg_exec_veto.json"

# --- 3. Submit Group Proposal ---
SUBMIT_RES=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/msg_exec_veto.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 3

# Get Group Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ]; then
    echo "❌ ERROR: Failed to submit proposal."
    echo "Raw: $(echo $SUBMIT_RES)"
    exit 1
fi
echo "✅ Group Proposal ID: $PROPOSAL_ID"

# --- 4. Vote & Execute ---
echo "Alice voting YES..."
$BINARY tx group vote $PROPOSAL_ID $ALICE_ADDR VOTE_OPTION_YES "Kill it" --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3
echo "Bob voting YES..."
$BINARY tx group vote $PROPOSAL_ID $BOB_ADDR VOTE_OPTION_YES "Agreed" --from bob -y --chain-id $CHAIN_ID --keyring-backend test

echo "Votes cast. Waiting for Veto voting period (5s buffer for 4h window? No, bootstrap sets 4 hours)..."
# NOTE: The bootstrap sets Veto Window to 4 hours.
# To make this test run fast, you must either:
# 1. Update bootstrap to use 10s for testnet/local
# 2. Or rely on "TryExec" immediately if threshold is met (Instant Execution)

echo "Attempting Immediate Execution (Threshold Met)..."
EXEC_RES=$($BINARY tx group exec $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --output json)
EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash')

echo "Waiting for execution block (5s)..."
sleep 5

# Fetch the TX result
EXEC_TX_JSON=$($BINARY query tx $EXEC_TX_HASH --output json)

if echo "$EXEC_TX_JSON" | grep -q "PROPOSAL_EXECUTOR_RESULT_SUCCESS"; then
    echo "✅ Group Execution Successful."
else
    echo "❌ CRITICAL FAILURE: Group Execution Failed."
    echo "Raw Log: $(echo $EXEC_TX_JSON | jq -r '.raw_log')"
    exit 1
fi

# --- 5. Verify Gov Proposal Status ---
echo "--- VERIFYING KILL ---"
GOV_STATUS_JSON=$($BINARY query gov proposal $GOV_PROP_ID --output json 2>&1)

STATUS=$(echo $GOV_STATUS_JSON | jq -r '.proposal.status')
echo "Current Status: $STATUS"

if [ "$STATUS" == "PROPOSAL_STATUS_FAILED" ] || [ "$STATUS" == "PROPOSAL_STATUS_REJECTED" ]; then
    echo "✅ SUCCESS: Proposal status forced to $STATUS."
else
    echo "❌ FAILURE: Proposal is still active (Status: $STATUS)."
fi