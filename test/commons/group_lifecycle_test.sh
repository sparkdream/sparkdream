#!/bin/bash

echo "--- TESTING: GROUP LIFECYCLE (BOOTSTRAP, CREATE, UPDATE, RENEW) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Helper: wait for tx to be indexed, extract proposal ID
get_proposal_id() {
    local tx_hash=$1
    local retries=0
    while [ $retries -lt 10 ]; do
        sleep 2
        TX_RES=$($BINARY query tx $tx_hash --output json 2>/dev/null)
        if [ $? -eq 0 ]; then
            local pid=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
            if [ -n "$pid" ] && [ "$pid" != "null" ]; then
                echo "$pid"
                return 0
            fi
        fi
        retries=$((retries + 1))
    done
    return 1
}
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)

if ! $BINARY keys show dave --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add dave --keyring-backend test --output json > /dev/null
fi
DAVE_ADDR=$($BINARY keys show dave -a --keyring-backend test)

# --- 1. VERIFY GENESIS BOOTSTRAP ---
echo "--- STEP 1: VERIFYING THREE PILLARS BOOTSTRAP ---"

TECH_INFO=$($BINARY query commons get-group "Technical Council" --output json)
TECH_POLICY=$(echo $TECH_INFO | jq -r '.group.policy_address')

if [ -z "$TECH_POLICY" ] || [ "$TECH_POLICY" == "null" ]; then
    echo "FAILURE: Technical Council not found."
    exit 1
fi
echo "Technical Council OK."

# --- 2. CREATE NEW COMMITTEE (SHORT TERM) ---
echo "--- STEP 2: COMMONS COUNCIL CREATES 'DIGITAL ART DAO' ---"

COMMONS_INFO=$($BINARY query commons get-group "Commons Council" --output json)
COMMONS_POLICY=$(echo $COMMONS_INFO | jq -r '.group.policy_address')

# We set term_duration to 60s so we can test renewal quickly!
# NOTE: futarchy_enabled=false because the x/commons module account needs funding
# for futarchy markets (1000 SPARK subsidy). Futarchy is tested separately.
echo '{
  "policy_address": "'$COMMONS_POLICY'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgRegisterGroup",
      "authority": "'$COMMONS_POLICY'",
      "name": "Digital Art DAO",
      "description": "Sub-committee for NFT grants",
      "members": ["'$DAVE_ADDR'"],
      "member_weights": ["4"],
      "min_members": 1,
      "max_members": 5,
      "term_duration": 60,
      "vote_threshold": "1",
      "policy_type": "threshold",
      "voting_period": 3600,
      "min_execution_period": 0,
      "max_spend_per_epoch": "1000",
      "update_cooldown": 3600,
      "funding_weight": 0,
      "futarchy_enabled": false
    }
  ],
  "metadata": "Create Art DAO sub-committee with short term"
}' > "$PROPOSAL_DIR/create_art_dao.json"

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/create_art_dao.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
PROPOSAL_ID=$(get_proposal_id $TX_HASH)

if [ -z "$PROPOSAL_ID" ]; then
    echo "ERROR: Failed to submit proposal."
    echo "Logs: $TX_HASH"
    exit 1
fi

echo "Create Prop ID: $PROPOSAL_ID"

# Vote
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5

# Execute
EXEC_RES=$($BINARY tx commons execute-proposal $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 5

# Verify execution status
PROP_STATUS=$($BINARY query commons get-proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')
if [ "$PROP_STATUS" != "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "FAILURE: Proposal not executed. Status: $PROP_STATUS"
    exit 1
fi

# Verify
NEW_GROUP_INFO=$($BINARY query commons get-group "Digital Art DAO" --output json 2>&1)
GROUP_ID=$(echo $NEW_GROUP_INFO | jq -r '.group.group_id')

if [ -n "$GROUP_ID" ] && [ "$GROUP_ID" != "null" ]; then
    echo "SUCCESS: 'Digital Art DAO' created with 60s term (Group ID: $GROUP_ID)."
else
    echo "FAILURE: New group not found."
    echo "Query Output: $NEW_GROUP_INFO"
    exit 1
fi

# --- 3. UPDATE CONFIG ---
echo "--- STEP 3: PARENT UPDATES BUDGET ---"

echo '{
  "policy_address": "'$COMMONS_POLICY'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdateGroupConfig",
      "authority": "'$COMMONS_POLICY'",
      "group_name": "Digital Art DAO",
      "max_spend_per_epoch": "50000"
    }
  ],
  "metadata": "Raising budget limit for Digital Art DAO"
}' > "$PROPOSAL_DIR/update_config.json"

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/update_config.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
PROPOSAL_ID=$(get_proposal_id $TX_HASH)

if [ -z "$PROPOSAL_ID" ]; then
    echo "ERROR: Failed to submit proposal."
    echo "Logs: $TX_HASH"
    exit 1
fi

echo "Update Prop ID: $PROPOSAL_ID"

$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5

$BINARY tx commons execute-proposal $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000
sleep 5

# Verify execution status
PROP_STATUS=$($BINARY query commons get-proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')
if [ "$PROP_STATUS" != "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "FAILURE: Update proposal not executed. Status: $PROP_STATUS"
    exit 1
fi

# Verify Update
UPDATED_INFO=$($BINARY query commons get-group "Digital Art DAO" --output json)
NEW_LIMIT=$(echo $UPDATED_INFO | jq -r '.group.max_spend_per_epoch')
if [ "$NEW_LIMIT" == "50000" ]; then
    echo "SUCCESS: Spend limit updated."
else
    echo "FAILURE: Spend limit is $NEW_LIMIT."
    exit 1
fi


# --- 4. RENEW MEMBERS (WAIT FOR EXPIRATION) ---
echo "--- STEP 4: WAITING FOR TERM EXPIRATION (30s remaining)... ---"
sleep 30

echo "--- EXECUTING RENEWAL (SWAP DAVE -> CAROL) ---"

echo '{
  "policy_address": "'$COMMONS_POLICY'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgRenewGroup",
      "authority": "'$COMMONS_POLICY'",
      "group_name": "Digital Art DAO",
      "new_members": ["'$CAROL_ADDR'"],
      "new_member_weights": ["4"]
    }
  ],
  "metadata": "Rotate members: Dave out, Carol in"
}' > "$PROPOSAL_DIR/renew_members.json"

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/renew_members.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
PROPOSAL_ID=$(get_proposal_id $TX_HASH)

if [ -z "$PROPOSAL_ID" ]; then
    echo "ERROR: Failed to submit proposal."
    echo "Logs: $TX_HASH"
    exit 1
fi

echo "Renew Prop ID: $PROPOSAL_ID"

$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5

EXEC_RES=$($BINARY tx commons execute-proposal $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 5

# Verify Execution Success
PROP_STATUS=$($BINARY query commons get-proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')
if [ "$PROP_STATUS" != "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "RENEWAL FAILED. Status: $PROP_STATUS (Did term expire?)"
    exit 1
fi

# --- 5. VERIFY MEMBERSHIP ---
MEMBERS=$($BINARY query commons get-council-members "Digital Art DAO" --output json)

echo "Final Members: $MEMBERS"

# 1. Check Carol (Human)
if echo "$MEMBERS" | jq -r '.members[].address' | grep -q "$CAROL_ADDR"; then
    echo "SUCCESS: Carol is now a member."
else
    echo "FAILURE: Carol not found."
fi

# 2. Check Dave (Removed)
if echo "$MEMBERS" | jq -r '.members[].address' | grep -q "$DAVE_ADDR"; then
    echo "FAILURE: Dave is STILL a member."
fi

# --- 6. DELETE GROUP ---
echo "--- STEP 6: PARENT DELETES CHILD GROUP ---"
echo "Commons Council voting to delete 'Digital Art DAO'..."

echo '{
  "policy_address": "'$COMMONS_POLICY'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgDeleteGroup",
      "authority": "'$COMMONS_POLICY'",
      "group_name": "Digital Art DAO"
    }
  ],
  "metadata": "Sunsetting the Digital Art DAO sub-committee"
}' > "$PROPOSAL_DIR/delete_group.json"

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/delete_group.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
PROPOSAL_ID=$(get_proposal_id $TX_HASH)

if [ -z "$PROPOSAL_ID" ]; then
    echo "ERROR: Failed to submit delete proposal."
    echo "Logs: $TX_HASH"
    exit 1
fi

echo "Delete Proposal ID: $PROPOSAL_ID"

# Vote (Alice & Bob are members of Commons Council)
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5

EXEC_RES=$($BINARY tx commons execute-proposal $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 5

# Verify Execution
PROP_STATUS=$($BINARY query commons get-proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')
if [ "$PROP_STATUS" != "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "DELETION EXECUTION FAILED. Status: $PROP_STATUS"
    exit 1
fi

# Verify Deletion from Registry
# The query should fail or return key not found
CHECK_INFO=$($BINARY query commons get-group "Digital Art DAO" --output json 2>&1)
if echo "$CHECK_INFO" | grep -q "not found"; then
    echo "SUCCESS: 'Digital Art DAO' successfully deleted from registry."
else
    echo "FAILURE: Group still exists in registry."
    echo "$CHECK_INFO"
    exit 1
fi

echo "--- LIFECYCLE TESTS COMPLETE ---"
