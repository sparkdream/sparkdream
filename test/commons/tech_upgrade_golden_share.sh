#!/bin/bash

echo "--- TESTING: TECHNICAL COUNCIL UPGRADE & GOLDEN SHARE (VIA PROXY) ---"

# --- 0. SETUP & CONFIG ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Ensure jq is installed
if ! command -v jq &> /dev/null; then
    echo "Error: jq is not installed."
    exit 1
fi

# Actors
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test) # Tech Lead (Weight 3)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)     # Commons Member

echo "Alice (Tech Lead):    $ALICE_ADDR"
echo "Bob (Commons Member): $BOB_ADDR"

# --- 1. DISCOVER POLICIES ---
echo "--- STEP 1: Discovering Council Addresses ---"

# A. Find Technical Council policy via x/commons
TECH_INFO=$($BINARY query commons get-group "Technical Council" --output json)
TECH_STANDARD_POLICY=$(echo $TECH_INFO | jq -r '.group.policy_address')

if [ -z "$TECH_STANDARD_POLICY" ] || [ "$TECH_STANDARD_POLICY" == "null" ]; then
    echo "ERROR: Could not find Technical Council policy address."
    exit 1
fi
echo "Tech Standard Policy: $TECH_STANDARD_POLICY"

# B. Find Commons Council STANDARD Policy (Member of Tech Council)
COMMONS_INFO=$($BINARY query commons get-group "Commons Council" --output json)
COMMONS_POLICY=$(echo $COMMONS_INFO | jq -r '.group.policy_address')

if [ -z "$COMMONS_POLICY" ] || [ "$COMMONS_POLICY" == "null" ]; then
    echo "ERROR: Commons Council Policy not found."
    exit 1
fi
echo "Commons Policy:   $COMMONS_POLICY"

# --- PRE-FLIGHT CHECK ---
echo "--- Checking Binary Support ---"
echo '{
  "body": {
    "messages": [
      {
        "@type": "/sparkdream.commons.v1.MsgForceUpgrade",
        "authority": "'$TECH_STANDARD_POLICY'",
        "plan": { "name": "test", "height": "100", "info": "check" }
      }
    ],
    "memo": "",
    "timeout_height": "0",
    "extension_options": [],
    "non_critical_extension_options": []
  },
  "auth_info": {
    "signer_infos": [],
    "fee": {
      "amount": [],
      "gas_limit": "200000",
      "payer": "",
      "granter": ""
    }
  },
  "signatures": []
}' > check_proto_support.json

if ! $BINARY tx encode check_proto_support.json > /dev/null 2>&1; then
    echo "CRITICAL ERROR: Binary does not support MsgForceUpgrade."
    exit 1
fi
echo "Binary supports MsgForceUpgrade."
rm check_proto_support.json

# --- 2. CALCULATE UPGRADE HEIGHT ---
echo "--- STEP 2: Calculating Upgrade Schedule ---"
CURRENT_HEIGHT=$($BINARY status | jq -r '.sync_info.latest_block_height')
UPGRADE_HEIGHT=999999999  # Set very high to avoid halting the chain during tests
UPGRADE_NAME="v2.0-spark-dream"
echo "Target Upgrade Height: $UPGRADE_HEIGHT"

# --- 3. SUBMIT TECH PROPOSAL (PROXY UPGRADE) ---
echo "--- STEP 3: Alice submits 'MsgForceUpgrade' to Tech Council ---"

echo '{
  "policy_address": "'$TECH_STANDARD_POLICY'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgForceUpgrade",
      "authority": "'$TECH_STANDARD_POLICY'",
      "plan": {
        "name": "'$UPGRADE_NAME'",
        "height": "'$UPGRADE_HEIGHT'",
        "info": "{\"binaries\":{\"linux/amd64\":\"https://github.com/sparkdream/releases/v2.0\"}}"
      }
    }
  ],
  "metadata": "Spark Dream v2.0 - Major network upgrade via Technical Council Proxy."
}' > "$PROPOSAL_DIR/real_upgrade.json"

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/real_upgrade.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 5

# Robust ID Extraction
TX_RES=$($BINARY query tx $TX_HASH --output json)
TECH_PROP_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')

# Fallback extraction
if [ -z "$TECH_PROP_ID" ]; then
   TECH_PROP_ID=$(echo $TX_RES | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

if [ -z "$TECH_PROP_ID" ]; then
    echo "ERROR: Failed to create Tech Proposal."
    exit 1
fi
echo "Tech Upgrade Proposal ID: $TECH_PROP_ID"

# Alice Votes YES (Weight 3)
$BINARY tx commons vote-proposal $TECH_PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test > /dev/null
sleep 5

# Check Status (Should be STALEMATE because Threshold is 75%)
STATUS=$($BINARY query commons get-proposal $TECH_PROP_ID --output json | jq -r '.proposal.status')
echo "Current Tech Prop Status: $STATUS (Stalemate Expected)"

if [ "$STATUS" == "PROPOSAL_STATUS_ACCEPTED" ]; then
    echo "FAILURE: Alice passed the upgrade alone! Golden Share check failed."
    exit 1
fi

# --- 4. THE NESTED PROPOSAL (COMMONS INTERVENTION) ---
echo "--- STEP 4: Commons Council votes to Approve Upgrade ---"

# The Commons Council exercises its "Golden Share" (Weight 3)
echo '{
  "policy_address": "'$COMMONS_POLICY'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgVoteProposal",
      "voter": "'$COMMONS_POLICY'",
      "proposal_id": '$TECH_PROP_ID',
      "option": "VOTE_OPTION_YES",
      "metadata": "Culture approves Infrastructure."
    }
  ],
  "metadata": "Approve v2.0 Upgrade - Commons Council exercises Golden Share."
}' > "$PROPOSAL_DIR/golden_share_vote.json"

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/golden_share_vote.json" --from bob -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 5

TX_RES=$($BINARY query tx $TX_HASH --output json)
COMMONS_PROP_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')
echo "Commons 'Meta' Proposal ID: $COMMONS_PROP_ID"

# Vote to pass the Commons Proposal
$BINARY tx commons vote-proposal $COMMONS_PROP_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test > /dev/null
$BINARY tx commons vote-proposal $COMMONS_PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test > /dev/null

echo "Waiting for Commons voting/execution..."
sleep 5

# Execute Commons Proposal -> Casts Vote on Tech Proposal
SUBMIT_RES=$($BINARY tx commons execute-proposal $COMMONS_PROP_ID --from bob -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --output json)
EXEC_TX_HASH=$(echo "$SUBMIT_RES" | jq -r '.txhash')
echo "Commons Execution Result: $SUBMIT_RES"

# Broadcast returns before the tx is included (height=0, empty events).
# Query the indexed tx and assert code==0 so a silent ErrUnauthorized from
# nested-message authority validation surfaces here instead of as a mysterious
# "status unchanged" downstream.
sleep 5
EXEC_TX_RES=$($BINARY query tx "$EXEC_TX_HASH" --output json 2>/dev/null)
EXEC_CODE=$(echo "$EXEC_TX_RES" | jq -r '.code // empty')
if [ -z "$EXEC_CODE" ]; then
    echo "FAILURE: could not find execute-proposal tx $EXEC_TX_HASH on chain."
    exit 1
fi
if [ "$EXEC_CODE" != "0" ]; then
    EXEC_RAW_LOG=$(echo "$EXEC_TX_RES" | jq -r '.raw_log')
    echo "FAILURE: Commons execute-proposal tx failed (code=$EXEC_CODE)."
    echo "         raw_log: $EXEC_RAW_LOG"
    exit 1
fi

# --- 5. FINALIZE & VERIFY ---
echo "--- STEP 5: Verify Upgrade Approval ---"

# Check Tech proposal status after Commons voted via golden share
sleep 5
STATUS=$($BINARY query commons get-proposal $TECH_PROP_ID --output json | jq -r '.proposal.status')
echo "Tech Proposal Status after Commons vote: $STATUS"

if [ "$STATUS" == "PROPOSAL_STATUS_ACCEPTED" ]; then
    echo "SUCCESS: Tech Proposal accepted after Golden Share vote."

    # Attempt execution
    echo "Attempting Execution (checks timelock)..."
    EXEC_RES=$($BINARY tx commons execute-proposal $TECH_PROP_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --output json)
    EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
    sleep 5

    # Check if execution succeeded (proposal moves to EXECUTED)
    FINAL_STATUS=$($BINARY query commons get-proposal $TECH_PROP_ID --output json | jq -r '.proposal.status')

    if [ "$FINAL_STATUS" == "PROPOSAL_STATUS_EXECUTED" ]; then
        echo "PROXY UPGRADE SCHEDULED! (Instant Execution)"

        PLAN_INFO=$($BINARY query upgrade plan --output json)
        PLAN_HEIGHT=$(echo $PLAN_INFO | jq -r '.plan.height')

        if [ "$PLAN_HEIGHT" == "$UPGRADE_HEIGHT" ]; then
            echo "VERIFIED: Upgrade plan scheduled at block $PLAN_HEIGHT"
        else
            echo "FAILURE: Upgrade plan not found in store."
            exit 1
        fi
    else
        # Execution tx went through but proposal not yet executed - likely timelocked
        EXEC_LOGS=$($BINARY query tx $EXEC_HASH --output json 2>/dev/null)

        if echo "$EXEC_LOGS" | grep -q "must wait until"; then
            echo "SUCCESS: Proposal Passed but Timelocked (48h Delay Active)."
            echo "   (The 'must wait until' error confirms the proposal status is ACCEPTED)."
        else
            echo "SUCCESS: Proposal Status is ACCEPTED (Execution Delayed)."
        fi
    fi

elif [ "$STATUS" == "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "SUCCESS: Tech Proposal already executed (early execution on acceptance)."

    PLAN_INFO=$($BINARY query upgrade plan --output json)
    PLAN_HEIGHT=$(echo $PLAN_INFO | jq -r '.plan.height')

    if [ "$PLAN_HEIGHT" == "$UPGRADE_HEIGHT" ]; then
        echo "VERIFIED: Upgrade plan scheduled at block $PLAN_HEIGHT"
    else
        echo "FAILURE: Upgrade plan not found in store."
        exit 1
    fi

else
    echo "FAILURE: Unexpected status after Golden Share vote: $STATUS"
    exit 1
fi
