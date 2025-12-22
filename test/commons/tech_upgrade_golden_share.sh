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
    echo "❌ Error: jq is not installed."
    exit 1
fi

# Actors
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test) # Tech Lead (Weight 3)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)     # Commons Member

echo "Alice (Tech Lead):    $ALICE_ADDR"
echo "Bob (Commons Member): $BOB_ADDR"

# --- 1. DISCOVER POLICIES ---
echo "--- STEP 1: Discovering Council Addresses ---"

# A. Find Technical Council Group ID via x/commons
TECH_INFO=$($BINARY query commons get-extended-group "Technical Council" --output json)
TECH_GROUP_ID=$(echo $TECH_INFO | jq -r '.extended_group.group_id')

if [ -z "$TECH_GROUP_ID" ] || [ "$TECH_GROUP_ID" == "null" ]; then
    echo "❌ ERROR: Technical Council not found in x/commons registry."
    exit 1
fi
echo "Technical Council Group ID: $TECH_GROUP_ID"

# B. Find Technical Council STANDARD Policy (Where Upgrades happen)
TECH_STANDARD_POLICY=$($BINARY query group group-policies-by-group $TECH_GROUP_ID --output json | jq -r '.group_policies[] | select(.metadata=="standard") | .address')

if [ -z "$TECH_STANDARD_POLICY" ] || [ "$TECH_STANDARD_POLICY" == "null" ]; then
    echo "❌ ERROR: Could not find Technical Council 'standard' policy."
    exit 1
fi
echo "Tech Standard Policy: $TECH_STANDARD_POLICY"

# C. Find Commons Council STANDARD Policy (Member of Tech Council)
COMMONS_INFO=$($BINARY query commons get-extended-group "Commons Council" --output json)
COMMONS_POLICY=$(echo $COMMONS_INFO | jq -r '.extended_group.policy_address')

if [ -z "$COMMONS_POLICY" ] || [ "$COMMONS_POLICY" == "null" ]; then
    echo "❌ ERROR: Commons Council Policy not found."
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
    echo "❌ CRITICAL ERROR: Binary does not support MsgForceUpgrade."
    exit 1
fi
echo "✅ Binary supports MsgForceUpgrade."
rm check_proto_support.json

# --- 2. CALCULATE UPGRADE HEIGHT ---
echo "--- STEP 2: Calculating Upgrade Schedule ---"
CURRENT_HEIGHT=$($BINARY status | jq -r '.sync_info.latest_block_height')
UPGRADE_HEIGHT=$((CURRENT_HEIGHT + 100))
UPGRADE_NAME="v2.0-spark-dream"
echo "Target Upgrade Height: $UPGRADE_HEIGHT"

# --- 3. SUBMIT TECH PROPOSAL (PROXY UPGRADE) ---
echo "--- STEP 3: Alice submits 'MsgForceUpgrade' to Tech Council ---"

echo '{
  "group_policy_address": "'$TECH_STANDARD_POLICY'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Spark Dream v2.0",
  "summary": "Major network upgrade via Technical Council Proxy.",
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
  ]
}' > "$PROPOSAL_DIR/real_upgrade.json"

SUBMIT_RES=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/real_upgrade.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 3

# Robust ID Extraction
TX_RES=$($BINARY query tx $TX_HASH --output json)
TECH_PROP_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')

# Fallback extraction
if [ -z "$TECH_PROP_ID" ]; then
   TECH_PROP_ID=$(echo $TX_RES | jq -r '.logs[0].events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

if [ -z "$TECH_PROP_ID" ]; then
    echo "❌ ERROR: Failed to create Tech Proposal."
    exit 1
fi
echo "Tech Upgrade Proposal ID: $TECH_PROP_ID"

# Alice Votes YES (Weight 3)
$BINARY tx group vote $TECH_PROP_ID $ALICE_ADDR VOTE_OPTION_YES "Deploy it" --from alice -y --chain-id $CHAIN_ID --keyring-backend test > /dev/null
sleep 3

# Check Status (Should be STALEMATE because Threshold is 75%)
STATUS=$($BINARY query group proposal $TECH_PROP_ID --output json | jq -r '.proposal.status')
echo "Current Tech Prop Status: $STATUS (Stalemate Expected)"

if [ "$STATUS" == "PROPOSAL_STATUS_ACCEPTED" ]; then
    echo "❌ FAILURE: Alice passed the upgrade alone! Golden Share check failed."
    exit 1
fi

# --- 4. THE NESTED PROPOSAL (COMMONS INTERVENTION) ---
echo "--- STEP 4: Commons Council votes to Approve Upgrade ---"

# The Commons Council exercises its "Golden Share" (Weight 3)
echo '{
  "group_policy_address": "'$COMMONS_POLICY'",
  "proposers": ["'$BOB_ADDR'"],
  "title": "Approve v2.0 Upgrade",
  "summary": "Commons Council exercises Golden Share to approve Tech Upgrade.",
  "messages": [
    {
      "@type": "/cosmos.group.v1.MsgVote",
      "proposal_id": "'$TECH_PROP_ID'",
      "voter": "'$COMMONS_POLICY'",
      "option": "VOTE_OPTION_YES",
      "metadata": "Culture approves Infrastructure."
    }
  ]
}' > "$PROPOSAL_DIR/golden_share_vote.json"

SUBMIT_RES=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/golden_share_vote.json" --from bob -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 3

TX_RES=$($BINARY query tx $TX_HASH --output json)
COMMONS_PROP_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')
echo "Commons 'Meta' Proposal ID: $COMMONS_PROP_ID"

# Vote to pass the Commons Proposal
$BINARY tx group vote $COMMONS_PROP_ID $BOB_ADDR VOTE_OPTION_YES "Yes" --from bob -y --chain-id $CHAIN_ID --keyring-backend test > /dev/null
$BINARY tx group vote $COMMONS_PROP_ID $ALICE_ADDR VOTE_OPTION_YES "Yes" --from alice -y --chain-id $CHAIN_ID --keyring-backend test > /dev/null

echo "Waiting for Commons voting/execution..."
sleep 5

# Execute Commons Proposal -> Casts Vote on Tech Proposal
SUBMIT_RES=$($BINARY tx group exec $COMMONS_PROP_ID --from bob -y --chain-id $CHAIN_ID --keyring-backend test --output json)
echo "Commons Execution Result: $SUBMIT_RES"

# --- 5. FINALIZE & VERIFY ---
echo "--- STEP 5: Verify Upgrade Approval ---"

# We attempt execution IMMEDIATELY.
# This serves two purposes:
# 1. Triggers the tally update (flipping status to ACCEPTED).
# 2. Checks the Timelock (verifying the Security Delay is active).
echo "Attempting Execution (triggers tally & checks timelock)..."
EXEC_RES=$($BINARY tx group exec $TECH_PROP_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 3

EXEC_LOGS=$($BINARY query tx $EXEC_HASH --output json)

# 1. CHECK FOR INSTANT SUCCESS
if echo "$EXEC_LOGS" | grep -q "PROPOSAL_EXECUTOR_RESULT_SUCCESS"; then
     echo "🚀 PROXY UPGRADE SCHEDULED! (Instant Execution)"
     
     PLAN_INFO=$($BINARY query upgrade plan --output json)
     PLAN_HEIGHT=$(echo $PLAN_INFO | jq -r '.plan.height')
     
     if [ "$PLAN_HEIGHT" == "$UPGRADE_HEIGHT" ]; then
         echo "✅ VERIFIED: Upgrade plan scheduled at block $PLAN_HEIGHT"
     else
         echo "❌ FAILURE: Upgrade plan not found in store."
         exit 1
     fi

# 2. CHECK FOR TIME LOCK (SUCCESSFUL PASS, DELAYED EXECUTION)
# This confirms the Golden Share worked (it passed) AND the Security Delay is working.
elif echo "$EXEC_LOGS" | grep -q "must wait until"; then
     echo "✅ SUCCESS: Proposal Passed but Timelocked (48h Delay Active)."
     echo "   (The 'must wait until' error confirms the proposal status is ACCEPTED)."

elif echo "$EXEC_LOGS" | grep -q "PROPOSAL_EXECUTOR_RESULT_NOT_RUN"; then
      # Double check status - MsgExec should have updated it to ACCEPTED now.
      STATUS=$($BINARY query group proposal $TECH_PROP_ID --output json | jq -r '.proposal.status')
      if [ "$STATUS" == "PROPOSAL_STATUS_ACCEPTED" ]; then
         echo "✅ SUCCESS: Proposal Status updated to ACCEPTED (Execution Delayed)."
      else
         echo "❌ FAILURE: Unexpected execution error. Status is still $STATUS."
         echo "Raw: $(echo $EXEC_LOGS)"
         exit 1
      fi

else
     echo "❌ FAILURE: Unexpected execution error."
     echo "Raw: $(echo $EXEC_LOGS)"
     exit 1
fi