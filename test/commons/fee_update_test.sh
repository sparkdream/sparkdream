#!/bin/bash

echo "--- TESTING: DYNAMIC FEE UPDATE & SPAM PROTECTION ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

# Robust Address Lookup
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')
echo "Gov Address: $GOV_ADDR"

# --- 1. SNAPSHOT CURRENT STATE ---
PARAMS_JSON=$($BINARY query commons params --output json)
CURRENT_FEE=$(echo $PARAMS_JSON | jq -r '.params.proposal_fee')

# DISCOVERY: Find a valid Council Policy Address from the Registry
# We query the group info for "Commons Council"
COUNCIL_ADDR=$($BINARY query commons get-extended-group "Commons Council" --output json | jq -r '.extended_group.policy_address')

echo "Current Fee:     $CURRENT_FEE"
echo "Council Address: $COUNCIL_ADDR"

if [ -z "$COUNCIL_ADDR" ] || [ "$COUNCIL_ADDR" == "null" ]; then
    echo "❌ SETUP ERROR: Could not find 'Commons Council'. Run bootstrap first."
    exit 1
fi

# Define Test Fee (Double the default)
NEW_FEE_AMOUNT="10000000"
NEW_FEE_STR="${NEW_FEE_AMOUNT}uspark"

# Helper Function: Get Proposal ID
get_proposal_id() {
    local tx_hash=$1
    local retries=0
    local max_retries=10
    local prop_id=""

    while [ $retries -lt $max_retries ]; do
        sleep 1
        TX_RES=$($BINARY query tx $tx_hash --output json 2>/dev/null)
        if [ $? -eq 0 ]; then
            prop_id=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
            if [ -z "$prop_id" ] || [ "$prop_id" == "null" ]; then
                prop_id=$(echo $TX_RES | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
            fi
            
            if [ ! -z "$prop_id" ] && [ "$prop_id" != "null" ]; then
                echo "$prop_id"
                return 0
            fi
        fi
        ((retries++))
    done
    return 1
}

# --- 2. STEP 1: UPDATE FEE VIA GOVERNANCE ---
echo "--- STEP 1: PROPOSING FEE INCREASE TO $NEW_FEE_STR ---"

# Note: We only update the FEE param. The Address param is deprecated/removed from this message.
echo '{
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdateParams",
      "authority": "'$GOV_ADDR'",
      "params": {
        "proposal_fee": "'$NEW_FEE_STR'"
      }
    }
  ],
  "deposit": "100000000uspark",
  "title": "Increase Council Spam Fee",
  "summary": "Raising the ante handler fee to 10 SPARK."
}' > "$PROPOSAL_DIR/gov_fee_update.json"

# Submit Gov Proposal
SUBMIT_RES=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/gov_fee_update.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
echo "Gov Prop Tx: $TX_HASH"

echo "Waiting for block inclusion..."
GOV_PROP_ID=$(get_proposal_id $TX_HASH)

if [ -z "$GOV_PROP_ID" ]; then
    echo "❌ ERROR: Failed to find Proposal ID after retries."
    exit 1
fi
echo "✅ Proposal ID: $GOV_PROP_ID"

# Vote YES (Alice 75% stake)
$BINARY tx gov vote $GOV_PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test

echo "Waiting for Gov Voting Period (60s)..."
sleep 70

# Verify Update
UPDATED_FEE=$($BINARY query commons params --output json | jq -r '.params.proposal_fee')
if [ "$UPDATED_FEE" == "$NEW_FEE_STR" ]; then
    echo "✅ SUCCESS: Fee updated to $UPDATED_FEE"
else
    echo "❌ FAILURE: Fee did not update. Current: $UPDATED_FEE"
    exit 1
fi

# --- 3. STEP 2: VERIFY ENFORCEMENT ---
echo "--- STEP 2: VERIFYING ANTEHANDLER ENFORCEMENT ---"

# Create Dummy Group Proposal
echo '{
  "group_policy_address": "'$COUNCIL_ADDR'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Spam Check",
  "summary": "Testing fee enforcement",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgSpendFromCommons",
      "authority": "'$COUNCIL_ADDR'",
      "recipient": "'$ALICE_ADDR'",
      "amount": [
        {
          "denom": "uspark",
          "amount": "1"
        }
      ] 
    }
  ]
}' > "$PROPOSAL_DIR/msg_spam_check.json"

# TEST A: FAIL (Pay Old Fee)
echo "Attempting submission with OLD FEE (5000000uspark)... (Expect Failure)"
FAIL_OUTPUT=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/msg_spam_check.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark 2>&1)

echo "Waiting for block inclusion (3s)..."
sleep 3

if echo "$FAIL_OUTPUT" | grep -q "insufficient fee"; then
    echo "✅ SUCCESS: Transaction rejected correctly (Insufficient Fee)."
else
    echo "❌ FAILURE: Transaction was accepted or wrong error!"
    echo "Output: $FAIL_OUTPUT"
    exit 1
fi

# TEST B: SUCCESS (Pay New Fee)
echo "Attempting submission with NEW FEE ($NEW_FEE_STR)... (Expect Success)"
SUCCESS_RES=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/msg_spam_check.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees $NEW_FEE_STR --output json)
SUCCESS_CODE=$(echo $SUCCESS_RES | jq -r '.code')

echo "Waiting for block inclusion (3s)..."
sleep 3

if [ "$SUCCESS_CODE" == "0" ]; then
    echo "✅ SUCCESS: Transaction accepted with correct fee."
else
    echo "❌ FAILURE: Transaction rejected even with correct fee."
    echo "Raw Log: $(echo $SUCCESS_RES | jq -r '.raw_log')"
    exit 1
fi

# --- 4. STEP 3: RESET FEE ---
echo "--- STEP 3: RESETTING FEE TO ORIGINAL ---"

echo '{
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdateParams",
      "authority": "'$GOV_ADDR'",
      "params": {
        "proposal_fee": "'$CURRENT_FEE'"
      }
    }
  ],
  "deposit": "50000000uspark",
  "title": "Reset Council Fee",
  "summary": "Restoring default values."
}' > "$PROPOSAL_DIR/gov_fee_reset.json"

SUBMIT_RES=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/gov_fee_reset.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

echo "Waiting for block inclusion..."
GOV_PROP_ID=$(get_proposal_id $TX_HASH)

if [ -z "$GOV_PROP_ID" ]; then
    echo "❌ ERROR: Failed to find Reset Proposal ID."
    exit 1
fi
echo "Reset Proposal ID: $GOV_PROP_ID"

# Vote YES
$BINARY tx gov vote $GOV_PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test

echo "Waiting for Gov Voting Period (60s)..."
sleep 70

# Verify Reset
FINAL_FEE=$($BINARY query commons params --output json | jq -r '.params.proposal_fee')
if [ "$FINAL_FEE" == "$CURRENT_FEE" ]; then
    echo "✅ SUCCESS: Fee reset to original value ($FINAL_FEE)."
else
    echo "❌ FAILURE: Fee did not reset. Current: $FINAL_FEE"
fi