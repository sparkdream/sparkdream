#!/bin/bash

echo "--- TESTING: POLICY LIFECYCLE (ATTACKS & SUNSETTING) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)

mkdir -p proposals

# Gov Address
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')

echo "Gov Address: $GOV_ADDR"
echo "Attacker:    $CAROL_ADDR"

# --- 1. SETUP DISPOSABLE TARGET GROUP ---
echo "--- STEP 1: Creating Disposable 'Sunset DAO' ---"
# We create a specific group for this test so we don't destroy the
# 'Commons Council' singleton needed by other tests (like fire_council_test.sh).

echo '{
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgRegisterGroup",
      "authority": "'$GOV_ADDR'",
      "name": "Sunset DAO",
      "description": "Destructible Test Group",
      "members": ["'$ALICE_ADDR'"],
      "member_weights": ["1"],
      "min_members": 1,
      "max_members": 3,
      "term_duration": 3600,
      "voting_period": 10,
      "min_execution_period": 0,
      "max_spend_per_epoch": "1000000",
      "update_cooldown": 0,
      "funding_weight": 0,
      "futarchy_enabled": false,
      "vote_threshold": "1",
      "policy_type": "threshold",
      "allowed_messages": [
        "/sparkdream.commons.v1.MsgSpendFromCommons"
      ]
    }
  ],
  "deposit": "100000000uspark",
  "title": "Create Sunset DAO",
  "summary": "A temporary group to test permission revocation.",
  "expedited": true
}' > "$PROPOSAL_DIR/create_sunset_dao.json"

# Submit via Gov
SUBMIT_RES=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/create_sunset_dao.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 5

# Get Gov Prop ID & Vote
GOV_PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
if [ -z "$GOV_PROP_ID" ]; then
    GOV_PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi
echo "Setup Proposal ID: $GOV_PROP_ID"

$BINARY tx gov vote $GOV_PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
echo "Waiting for setup vote (45s for Expedited)..."
sleep 45

# DISCOVER NEW GROUP ADDRESS
SUNSET_INFO=$($BINARY query commons get-group "Sunset DAO" --output json)
COUNCIL_ADDR=$(echo $SUNSET_INFO | jq -r '.group.policy_address')

if [ -z "$COUNCIL_ADDR" ] || [ "$COUNCIL_ADDR" == "null" ]; then
    echo "FAIL SETUP ERROR: Failed to create 'Sunset DAO'."
    # Debugging info
    $BINARY query gov proposal $GOV_PROP_ID --output json
    exit 1
fi
echo "OK Target Policy Address: $COUNCIL_ADDR"

# Fund it (so we can test spending later)
$BINARY tx bank send $ALICE_ADDR $COUNCIL_ADDR 1000uspark --chain-id $CHAIN_ID -y > /dev/null
sleep 5

# --- 2. ATTACK SIMULATION (SECURITY) ---
echo "--- STEP 2: ATTACKER (CAROL) TRIES TO MODIFY PERMS ---"

# Attempt 1: Carol tries to overwrite permissions
echo "Carol attempting MsgUpdatePolicyPermissions..."
SUBMIT_RES=$($BINARY tx commons update-policy-permissions $COUNCIL_ADDR "/cosmos.bank.v1beta1.MsgSend" \
  --from carol -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 5

TX_RES=$($BINARY query tx $TX_HASH --output json 2>/dev/null)
TX_CODE=$(echo $TX_RES | jq -r '.code')
RAW_LOG=$(echo $TX_RES | jq -r '.raw_log')

if [ "$TX_CODE" != "0" ]; then
    echo "OK SECURITY SUCCESS: Update blocked on-chain (Code $TX_CODE)."
else
    echo "FAIL SECURITY FAILURE: Carol's update transaction SUCCEEDED!"
    echo "Log: $RAW_LOG"
    exit 1
fi

# Attempt 2: Carol tries to delete permissions
echo "Carol attempting MsgDeletePolicyPermissions..."
SUBMIT_RES=$($BINARY tx commons delete-policy-permissions $COUNCIL_ADDR \
  --from carol -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 5

TX_RES=$($BINARY query tx $TX_HASH --output json 2>/dev/null)
TX_CODE=$(echo $TX_RES | jq -r '.code')

if [ "$TX_CODE" != "0" ]; then
    echo "OK SECURITY SUCCESS: Delete blocked on-chain (Code $TX_CODE)."
else
    echo "FAIL SECURITY FAILURE: Carol's delete transaction SUCCEEDED!"
    exit 1
fi

# --- 3. SUNSET PROTOCOL (GOVERNANCE) ---
echo "--- STEP 3: GOVERNANCE VOTES TO SUNSET (DELETE) DAO ---"

echo '{
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgDeletePolicyPermissions",
      "authority": "'$GOV_ADDR'",
      "policy_address": "'$COUNCIL_ADDR'"
    }
  ],
  "deposit": "100000000uspark",
  "title": "Sunset DAO",
  "summary": "Dissolving the DAO by revoking all policy permissions.",
  "expedited": true
}' > "$PROPOSAL_DIR/gov_sunset.json"

# Submit
SUBMIT_RES=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/gov_sunset.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 5

# Get Prop ID
GOV_PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
if [ -z "$GOV_PROP_ID" ]; then
    GOV_PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

echo "Gov Proposal ID: $GOV_PROP_ID"

# Vote YES
$BINARY tx gov vote $GOV_PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test

echo "Waiting for Expedited Voting (45s)..."
sleep 45

# Verify Deletion
PERMS_CHECK=$($BINARY query commons show-policy-permissions $COUNCIL_ADDR --output json 2>&1)

if echo "$PERMS_CHECK" | grep -q "key not found" || echo "$PERMS_CHECK" | grep -q "policy permissions not found"; then
    echo "OK SUCCESS: Policy permissions verified deleted from state."
else
    echo "FAIL: Policy permissions still exist!"
    echo "$PERMS_CHECK"
    exit 1
fi

# --- 4. POST-MORTEM CHECK (DEAD DAO) ---
echo "--- STEP 4: VERIFY DAO IS FUNCTIONALLY DEAD ---"

echo '{
  "policy_address": "'$COUNCIL_ADDR'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgSpendFromCommons",
      "authority": "'$COUNCIL_ADDR'",
      "recipient": "'$ALICE_ADDR'",
      "amount": [{"denom": "uspark", "amount": "1"}]
    }
  ],
  "metadata": "Trying to act after sunset"
}' > "$PROPOSAL_DIR/msg_zombie.json"

# Attempt Submission - should fail because permissions were deleted
OUTPUT=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/msg_zombie.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json 2>&1)

if echo "$OUTPUT" | grep -qi "no policy permissions found\|no permissions found"; then
    echo "OK GRAND SUCCESS: The DAO is effectively dead. Submission rejected."
else
    # Check if tx went through but failed on-chain
    TX_HASH=$(echo $OUTPUT | jq -r '.txhash' 2>/dev/null)
    if [ -n "$TX_HASH" ] && [ "$TX_HASH" != "null" ]; then
        sleep 5
        TX_RES=$($BINARY query tx $TX_HASH --output json 2>/dev/null)
        TX_CODE=$(echo $TX_RES | jq -r '.code')
        if [ "$TX_CODE" != "0" ] && echo "$TX_RES" | grep -qi "no.*permissions\|not found"; then
            echo "OK GRAND SUCCESS: The DAO is effectively dead. Rejected on-chain."
        else
            echo "FAIL CRITICAL FAILURE: The DAO was able to act (or got wrong error)!"
            echo "$TX_RES"
            exit 1
        fi
    else
        echo "FAIL CRITICAL FAILURE: The DAO was able to act (or got wrong error)!"
        echo "$OUTPUT"
        exit 1
    fi
fi
