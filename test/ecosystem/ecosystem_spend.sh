#!/bin/bash

echo "--- TESTING SPEND: SUCCESSFUL ECOSYSTEM SPEND (GOVERNANCE) ---"

# --- 0. SETUP & ADDRESS DISCOVERY ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
DENOM="uspark"

# Ensure jq is installed
if ! command -v jq &> /dev/null; then
    echo "❌ Error: jq is not installed."
    exit 1
fi

ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

echo "Alice: $ALICE_ADDR"
echo "Bob:   $BOB_ADDR"

# Discover the Governance Module Address (The Authority)
# Adopted robust pattern from interim_council_test.sh
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')

if [ -z "$GOV_ADDR" ] || [ "$GOV_ADDR" == "null" ]; then
    echo "❌ Error: Could not fetch Governance Module address."
    exit 1
fi

echo "Governance Authority Address: $GOV_ADDR"

# Check Ecosystem Module Balance (Optional, just to see it exists)
# We assume the ecosystem module name is "ecosystem"
ECO_MODULE_ADDR=$($BINARY query auth module-account ecosystem --output json | jq -r '.account.base_account.address // .account.value.address')
echo "Ecosystem Module Address: $ECO_MODULE_ADDR"
$BINARY query bank balances $ECO_MODULE_ADDR

echo "--- CHECKING BOB'S INITIAL BALANCE ---"
INITIAL_BAL=$($BINARY query bank balances $BOB_ADDR --output json | jq -r --arg DENOM "$DENOM" '.balances[] | select(.denom==$DENOM) | .amount')
if [ -z "$INITIAL_BAL" ]; then INITIAL_BAL=0; fi
echo "Bob's Initial Balance: $INITIAL_BAL $DENOM"

# --- 1. Create the Proposal JSON ---
# Note: 'amount' must be an array [ ... ]
echo '{
  "messages": [
    {
      "@type": "/sparkdream.ecosystem.v1.MsgSpend",
      "authority": "'$GOV_ADDR'",
      "recipient": "'$BOB_ADDR'",
      "amount": [
        {
          "denom": "'$DENOM'",
          "amount": "1000000"
        }
      ]
    }
  ],
  "metadata": "ipfs://CID", 
  "deposit": "50000000'$DENOM'", 
  "title": "Ecosystem Spend Test",
  "summary": "Proposal to spend 1 SPARK from Ecosystem to Bob"
}' > proposals/msg_ecosystem_spend.json

# --- 2. Submit Proposal ---
echo "Submitting Governance Proposal..."

# We submit from Alice and include a deposit to enter voting period immediately
SUBMIT_RES=$($BINARY tx gov submit-proposal proposals/msg_ecosystem_spend.json --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas auto --gas-adjustment 1.5 --fees 5000${DENOM} --output json)

# Error handling: Check if txhash exists in the response
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash // empty')

if [ -z "$TX_HASH" ]; then
    echo "❌ ERROR: Transaction failed. Response:"
    echo "$SUBMIT_RES"
    exit 1
fi

echo "Tx Hash: $TX_HASH"
echo "Waiting for block inclusion (5s)..."
sleep 5

# Query Tx to find the Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
    # Fallback for different SDK versions (sometimes it's in 'gov_submit_proposal')
    PROPOSAL_ID=$(echo $TX_RES | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
    echo "❌ ERROR: Failed to create Proposal. Tx failed or ID not found."
    echo "Raw Log: $(echo $TX_RES | jq -r '.raw_log')"
    exit 1
fi

echo "✅ Target Proposal ID: $PROPOSAL_ID"

# --- 3. Vote on Proposal ---
echo "Alice Voting YES on Proposal $PROPOSAL_ID..."
$BINARY tx gov vote $PROPOSAL_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 2000${DENOM}

echo "Votes cast. Waiting for voting period to end (60s config + buffer)..."
# Config voting_period is 60s. We sleep 65s to be safe.
sleep 65 

# --- 4. Verify Execution ---
echo "Checking Proposal Status..."

# FIX: Using the exact jq path from interim_council_test.sh
PROP_STATUS=$($BINARY query gov proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')
echo "Final Status: $PROP_STATUS"

if [ "$PROP_STATUS" != "PROPOSAL_STATUS_PASSED" ]; then
    echo "❌ Warning: Proposal did not pass. Status: $PROP_STATUS"
fi

echo "--- VERIFYING BOB'S BALANCE (Should be +1000000 $DENOM) ---"
FINAL_BAL=$($BINARY query bank balances $BOB_ADDR --output json | jq -r --arg DENOM "$DENOM" '.balances[] | select(.denom==$DENOM) | .amount')
if [ -z "$FINAL_BAL" ]; then FINAL_BAL=0; fi

echo "Initial: $INITIAL_BAL"
echo "Final:   $FINAL_BAL"

DIFFERENCE=$((FINAL_BAL - INITIAL_BAL))

if [ "$DIFFERENCE" -eq "1000000" ]; then
    echo "✅ SUCCESS: Bob received exactly 1000000 $DENOM"
else
    echo "❌ FAILURE: Balance change was $DIFFERENCE"
fi