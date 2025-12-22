#!/bin/bash

echo "--- TESTING: ECOSYSTEM SPEND (GOVERNANCE VS DIRECT ATTACK) ---"

# --- 0. SETUP & ADDRESS DISCOVERY ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

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

# Create an Attacker
if ! $BINARY keys show eve --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add eve --keyring-backend test --output json > /dev/null
fi
EVE_ADDR=$($BINARY keys show eve -a --keyring-backend test)

echo "Alice (Proposer): $ALICE_ADDR"
echo "Bob (Recipient):  $BOB_ADDR"
echo "Eve (Attacker):   $EVE_ADDR"

# Discover the Governance Module Address (The Authority)
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')

# Discover Ecosystem Module Address
ECO_MODULE_ADDR=$($BINARY query auth module-account ecosystem --output json | jq -r '.account.base_account.address // .account.value.address')

if [ -z "$GOV_ADDR" ] || [ -z "$ECO_MODULE_ADDR" ]; then
    echo "❌ Error: Could not fetch module addresses."
    exit 1
fi

echo "Governance Authority: $GOV_ADDR"
echo "Ecosystem Treasury:   $ECO_MODULE_ADDR"

# --- 1. BOOTSTRAP FUNDING ---
echo "--- STEP 1: FUNDING ECOSYSTEM MODULE ---"
# We send funds to the ecosystem module to ensure the spend doesn't fail due to insufficient funds
$BINARY tx bank send alice $ECO_MODULE_ADDR 5000000${DENOM} --chain-id $CHAIN_ID -y --fees 5000${DENOM} --keyring-backend test > /dev/null
sleep 5
echo "✅ Ecosystem Module funded."

# --- 2. SECURITY TEST (DIRECT SPEND ATTACK) ---
echo "--- STEP 2: SECURITY CHECK (EVE ATTEMPTS DIRECT SPEND) ---"

# Fund Eve first so she exists on-chain and can pay gas
echo "Funding Eve's account so she exists on-chain..."
$BINARY tx bank send alice $EVE_ADDR 10000000${DENOM} --chain-id $CHAIN_ID -y --fees 5000${DENOM} --keyring-backend test > /dev/null
sleep 6

# Eve attempts to sign a MsgSpend message directly.
echo "Eve broadcasting unauthorized transaction..."

# 1. Capture the broadcast result (JSON)
ATTACK_RES=$($BINARY tx ecosystem spend $EVE_ADDR 1000000${DENOM} --from eve --chain-id $CHAIN_ID -y --keyring-backend test --fees 5000${DENOM} --output json)
ATTACK_HASH=$(echo "$ATTACK_RES" | jq -r '.txhash')

echo "Attack TX Hash: $ATTACK_HASH"
echo "Waiting for block..."
sleep 6

# 2. Query the transaction hash to see the execution result
TX_QUERY=$($BINARY query tx $ATTACK_HASH --output json)

# 3. Analyze the on-chain result
# We expect a non-zero code (failure) and a specific error message in raw_log
TX_CODE=$(echo "$TX_QUERY" | jq -r '.code')
TX_LOG=$(echo "$TX_QUERY" | jq -r '.raw_log')

echo "TX Code: $TX_CODE"
echo "TX Log:  $TX_LOG"

# Check for failure code (anything other than 0 is a failure)
if [ "$TX_CODE" != "0" ]; then
    # Verify it failed for the RIGHT reason (authority check)
    if echo "$TX_LOG" | grep -q "invalid authority" || echo "$TX_LOG" | grep -q "unauthorized" || echo "$TX_LOG" | grep -q "does not match"; then
        echo "✅ SUCCESS: Eve was blocked. The chain rejected the invalid authority."
    else
        echo "⚠️  WARNING: Transaction failed, but possibly for the wrong reason."
        echo "    Log: $TX_LOG"
    fi
else
    echo "❌ CRITICAL FAILURE: Eve successfully spent funds! (Code 0)"
    exit 1
fi

# --- 3. GOVERNANCE SPEND (HAPPY PATH) ---
echo "--- STEP 3: GOVERNANCE SPEND PROPOSAL ---"

echo "Checking Bob's Initial Balance..."
INITIAL_BAL=$($BINARY query bank balances $BOB_ADDR --output json | jq -r --arg DENOM "$DENOM" '.balances[] | select(.denom==$DENOM) | .amount')
if [ -z "$INITIAL_BAL" ]; then INITIAL_BAL=0; fi
echo "Bob's Initial: $INITIAL_BAL $DENOM"

# Create Proposal
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
}' > "$PROPOSAL_DIR/msg_ecosystem_spend.json"

SUBMIT_RES=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/msg_ecosystem_spend.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${DENOM} --output json)
TX_HASH=$(echo "$SUBMIT_RES" | jq -r '.txhash')
sleep 4

# Get Proposal ID
PROPOSAL_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
   PROPOSAL_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

echo "Prop ID: $PROPOSAL_ID"

# Define Vote Fee variable
VOTE_FEE=2000

# Vote
$BINARY tx gov vote $PROPOSAL_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${DENOM} > /dev/null

# Bob pays 'VOTE_FEE' here
$BINARY tx gov vote $PROPOSAL_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test --fees ${VOTE_FEE}${DENOM} > /dev/null

echo "Votes cast. Polling for passing status (Max 70s)..."

# --- 4. POLLING LOOP ---
PASSED=false
for i in {1..14}; do
    STATUS=$($BINARY query gov proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')
    if [ "$STATUS" == "PROPOSAL_STATUS_PASSED" ]; then
        PASSED=true
        echo "✅ Proposal PASSED."
        break
    fi
    echo "Current Status: $STATUS... (Attempt $i/14)"
    sleep 5
done

if [ "$PASSED" = false ]; then
    echo "❌ ERROR: Proposal did not pass in time."
    exit 1
fi

# --- 5. VERIFY EXECUTION ---
echo "--- STEP 4: VERIFYING BALANCE ---"
# Check if funds moved
FINAL_BAL=$($BINARY query bank balances $BOB_ADDR --output json | jq -r --arg DENOM "$DENOM" '.balances[] | select(.denom==$DENOM) | .amount')
if [ -z "$FINAL_BAL" ]; then FINAL_BAL=0; fi

echo "Initial: $INITIAL_BAL"
echo "Final:   $FINAL_BAL"

# Calculate Real Difference (Final - Initial)
DIFFERENCE=$((FINAL_BAL - INITIAL_BAL))

# Calculate Expected Net Gain (Grant - Fee)
EXPECTED_NET=$((1000000 - VOTE_FEE))

if [ "$DIFFERENCE" -eq "$EXPECTED_NET" ]; then
    echo "✅ SUCCESS: Bob received 1M less fees (Net: +$DIFFERENCE)"
else
    echo "❌ FAILURE: Balance mismatch." 
    echo "   Expected Net: +$EXPECTED_NET (1M grant - $VOTE_FEE fee)"
    echo "   Actual Net:   +$DIFFERENCE"
    exit 1
fi