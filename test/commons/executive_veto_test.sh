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
VETO_POLICY_ADDR=$($BINARY query commons get-group "Commons Council" --output json | jq -r '.group.veto_policy_address')

if [ -z "$VETO_POLICY_ADDR" ] || [ "$VETO_POLICY_ADDR" == "null" ]; then
    echo "ERROR: Could not find Veto Policy for Commons Council"
    exit 1
fi

echo "Target Veto Policy Address: $VETO_POLICY_ADDR"

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
sleep 5

# Get Gov Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
GOV_PROP_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
if [ -z "$GOV_PROP_ID" ] || [ "$GOV_PROP_ID" == "null" ]; then
   GOV_PROP_ID=$(echo $TX_RES | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

echo "Target Gov Proposal ID: $GOV_PROP_ID"

# --- ALICE VOTES YES ---
echo "Alice votes YES..."
$BINARY tx gov vote $GOV_PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5

# --- 2. DEFENSE: Create the "Kill Switch" Commons Proposal ---
echo "--- PHASE 2: THE DEFENSE ---"
echo "Creating Executive Veto Proposal..."

echo '{
  "policy_address": "'$VETO_POLICY_ADDR'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgEmergencyCancelGovProposal",
      "authority": "'$VETO_POLICY_ADDR'",
      "proposal_id": '$GOV_PROP_ID'
    }
  ],
  "metadata": "EXECUTIVE ORDER: CANCEL PROP '$GOV_PROP_ID' - Immediate cancellation of malicious proposal."
}' > "$PROPOSAL_DIR/msg_exec_veto.json"

# --- 3. Submit Commons Proposal ---
SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/msg_exec_veto.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 5

# Get Commons Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ]; then
    echo "ERROR: Failed to submit proposal."
    echo "Raw: $(echo $SUBMIT_RES)"
    exit 1
fi
echo "Commons Proposal ID: $PROPOSAL_ID"

# --- 4. Vote & Execute ---
echo "Alice voting YES..."
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5
echo "Bob voting YES..."
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5

echo "Attempting Execution (Threshold Met)..."
EXEC_RES=$($BINARY tx commons execute-proposal $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --output json)
EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash')

echo "Waiting for execution block (3s)..."
sleep 5

# Fetch the TX result
EXEC_TX_JSON=$($BINARY query tx $EXEC_TX_HASH --output json)
EXEC_CODE=$(echo $EXEC_TX_JSON | jq -r '.code')

if [ "$EXEC_CODE" == "0" ]; then
    # Verify proposal status is EXECUTED
    PROP_STATUS=$($BINARY query commons get-proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')
    if [ "$PROP_STATUS" == "PROPOSAL_STATUS_EXECUTED" ]; then
        echo "Commons Execution Successful."
    else
        echo "WARNING: Tx succeeded but proposal status is $PROP_STATUS"
    fi
else
    echo "CRITICAL FAILURE: Commons Execution Failed."
    echo "Raw Log: $(echo $EXEC_TX_JSON | jq -r '.raw_log')"
    exit 1
fi

# --- 5. Verify Gov Proposal Status ---
echo "--- VERIFYING KILL ---"
GOV_STATUS_JSON=$($BINARY query gov proposal $GOV_PROP_ID --output json 2>&1)

STATUS=$(echo $GOV_STATUS_JSON | jq -r '.proposal.status')
echo "Current Status: $STATUS"

if [ "$STATUS" == "PROPOSAL_STATUS_FAILED" ] || [ "$STATUS" == "PROPOSAL_STATUS_REJECTED" ]; then
    echo "SUCCESS: Proposal status forced to $STATUS."
else
    echo "FAILURE: Proposal is still active (Status: $STATUS)."
fi
