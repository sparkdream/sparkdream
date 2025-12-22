#!/bin/bash

echo "--- TESTING: GROUP LIFECYCLE (BOOTSTRAP, CREATE, UPDATE, RENEW) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)

if ! $BINARY keys show dave --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add dave --keyring-backend test --output json > /dev/null
fi
DAVE_ADDR=$($BINARY keys show dave -a --keyring-backend test)

# --- 1. VERIFY GENESIS BOOTSTRAP ---
echo "--- STEP 1: VERIFYING THREE PILLARS BOOTSTRAP ---"

TECH_INFO=$($BINARY query commons get-extended-group "Technical Council" --output json)
TECH_POLICY=$(echo $TECH_INFO | jq -r '.extended_group.policy_address')

if [ -z "$TECH_POLICY" ] || [ "$TECH_POLICY" == "null" ]; then
    echo "❌ FAILURE: Technical Council not found."
    exit 1
fi
echo "✅ Technical Council OK."

# --- 2. CREATE NEW COMMITTEE (SHORT TERM) ---
echo "--- STEP 2: COMMONS COUNCIL CREATES 'DIGITAL ART DAO' ---"

COMMONS_INFO=$($BINARY query commons get-extended-group "Commons Council" --output json)
COMMONS_POLICY=$(echo $COMMONS_INFO | jq -r '.extended_group.policy_address')

# We set term_duration to 60s so we can test renewal quickly!
# UPDATED: Added mandatory fields 'policy_type', 'voting_period', and 'min_execution_period'
echo '{
  "group_policy_address": "'$COMMONS_POLICY'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Create Art DAO",
  "summary": "Sub-committee with short term.",
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
      "max_spend_per_epoch": "1000uspark", 
      "update_cooldown": 3600,
      "funding_weight": 0,
      "futarchy_enabled": true
    }
  ]
}' > "$PROPOSAL_DIR/create_art_dao.json"

SUBMIT_RES=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/create_art_dao.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 3

TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ]; then
    echo "❌ ERROR: Failed to submit proposal."
    echo "Logs: $TX_HASH"
    exit 1
fi

echo "Create Prop ID: $PROPOSAL_ID"

# Vote & Exec
$BINARY tx group vote $PROPOSAL_ID $ALICE_ADDR VOTE_OPTION_YES "Yes" --from alice -y --chain-id $CHAIN_ID --keyring-backend test
$BINARY tx group vote $PROPOSAL_ID $BOB_ADDR VOTE_OPTION_YES "Yes" --from bob -y --chain-id $CHAIN_ID --keyring-backend test
echo "Waiting for voting period (35s)..."
sleep 35
EXEC_RES=$($BINARY tx group exec $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 3
EXEC_LOGS=$($BINARY query tx $EXEC_HASH --output json)

# Verify
NEW_GROUP_INFO=$($BINARY query commons get-extended-group "Digital Art DAO" --output json 2>&1)
GROUP_ID=$(echo $NEW_GROUP_INFO | jq -r '.extended_group.group_id')

if [ -n "$GROUP_ID" ] && [ "$GROUP_ID" != "null" ]; then
    echo "✅ SUCCESS: 'Digital Art DAO' created with 60s term (Group ID: $GROUP_ID)."
else
    echo "❌ FAILURE: New group not found."
    echo "Query Output: $NEW_GROUP_INFO"
    exit 1
fi

# --- 3. UPDATE CONFIG ---
echo "--- STEP 3: PARENT UPDATES BUDGET ---"

# Note: We omit 'futarchy_enabled' here to verify that the pointer implementation
# correctly handles omission (it should remain true from creation).
echo '{
  "group_policy_address": "'$COMMONS_POLICY'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Budget Increase",
  "summary": "Raising limit.",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdateGroupConfig",
      "authority": "'$COMMONS_POLICY'",
      "group_name": "Digital Art DAO",
      "max_spend_per_epoch": "50000uspark"
    }
  ]
}' > "$PROPOSAL_DIR/update_config.json"

SUBMIT_RES=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/update_config.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 3

TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ]; then
    echo "❌ ERROR: Failed to submit proposal."
    echo "Logs: $TX_HASH"
    exit 1
fi

echo "Create Prop ID: $PROPOSAL_ID"

$BINARY tx group vote $PROPOSAL_ID $ALICE_ADDR VOTE_OPTION_YES "Approve" --from alice -y --chain-id $CHAIN_ID --keyring-backend test
$BINARY tx group vote $PROPOSAL_ID $BOB_ADDR VOTE_OPTION_YES "Approve" --from bob -y --chain-id $CHAIN_ID --keyring-backend test
sleep 35
$BINARY tx group exec $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3

# Verify Update
UPDATED_INFO=$($BINARY query commons get-extended-group "Digital Art DAO" --output json)
NEW_LIMIT=$(echo $UPDATED_INFO | jq -r '.extended_group.max_spend_per_epoch')
if [ "$NEW_LIMIT" == "50000uspark" ]; then
    echo "✅ SUCCESS: Spend limit updated."
else
    echo "❌ FAILURE: Spend limit is $NEW_LIMIT."
    exit 1
fi

# Verify Futarchy remains enabled (proving pointer logic works)
FUTARCHY_STATUS=$(echo $UPDATED_INFO | jq -r '.extended_group.futarchy_enabled')
if [ "$FUTARCHY_STATUS" == "true" ]; then
    echo "✅ SUCCESS: Futarchy enabled status preserved."
else
    echo "❌ FAILURE: Futarchy enabled status lost (became false)."
    exit 1
fi


# --- 4. RENEW MEMBERS (WAIT FOR EXPIRATION) ---
echo "--- STEP 4: WAITING FOR TERM EXPIRATION (30s remaining)... ---"
sleep 30

echo "--- EXECUTING RENEWAL (SWAP DAVE -> CAROL) ---"

# Dave (Weight 4) -> Carol (Weight 4)
# Human Weight = 4. 
# Futarchy Logic: 20% of Total (Human+Futarchy). Or Futarchy = Human/4.
# Futarchy Weight should be 4/4 = 1.
echo '{
  "group_policy_address": "'$COMMONS_POLICY'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Rotate Members",
  "summary": "Dave is out, Carol is in.",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgRenewGroup",
      "authority": "'$COMMONS_POLICY'",
      "group_name": "Digital Art DAO",
      "new_members": ["'$CAROL_ADDR'"],
      "new_member_weights": ["4"]
    }
  ]
}' > "$PROPOSAL_DIR/renew_members.json"

SUBMIT_RES=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/renew_members.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 3

TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ]; then
    echo "❌ ERROR: Failed to submit proposal."
    echo "Logs: $TX_HASH"
    exit 1
fi

echo "Create Prop ID: $PROPOSAL_ID"

$BINARY tx group vote $PROPOSAL_ID $ALICE_ADDR VOTE_OPTION_YES "Rotate" --from alice -y --chain-id $CHAIN_ID --keyring-backend test
$BINARY tx group vote $PROPOSAL_ID $BOB_ADDR VOTE_OPTION_YES "Rotate" --from bob -y --chain-id $CHAIN_ID --keyring-backend test
sleep 35

EXEC_RES=$($BINARY tx group exec $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 3

# Verify Execution Success
EXEC_LOGS=$($BINARY query tx $EXEC_HASH --output json)
if ! echo "$EXEC_LOGS" | grep -q "PROPOSAL_EXECUTOR_RESULT_SUCCESS"; then
    echo "❌ RENEWAL FAILED. Check logs (Did term expire?)"
    echo "Raw: $(echo $EXEC_LOGS)"
    exit 1
fi

# --- 5. VERIFY MEMBERSHIP & FUTARCHY ---
ART_DAO_ID=$(echo $UPDATED_INFO | jq -r '.extended_group.group_id')
MEMBERS=$($BINARY query group group-members $ART_DAO_ID --output json)

echo "Final Members: $MEMBERS"

# 1. Check Carol (Human)
if echo "$MEMBERS" | grep -q "$CAROL_ADDR"; then
    echo "✅ SUCCESS: Carol is now a member."
else
    echo "❌ FAILURE: Carol not found."
fi

# 2. Check Dave (Removed)
if echo "$MEMBERS" | grep -q "$DAVE_ADDR"; then
    echo "❌ FAILURE: Dave is STILL a member."
fi

# 3. Check Futarchy Bot (Auto-Added)
# We don't know the exact address, but we check if there is a member with Metadata "Futarchy Seat"
if echo "$MEMBERS" | grep -q "Futarchy Seat"; then
    echo "✅ SUCCESS: Futarchy Bot was automatically added/retained."
else
    echo "❌ FAILURE: Futarchy Bot missing."
fi

# --- 6. DELETE GROUP ---
echo "--- STEP 6: PARENT DELETES CHILD GROUP ---"
echo "Commons Council voting to delete 'Digital Art DAO'..."

echo '{
  "group_policy_address": "'$COMMONS_POLICY'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Delete Digital Art DAO",
  "summary": "Sunsetting the sub-committee.",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgDeleteGroup",
      "authority": "'$COMMONS_POLICY'",
      "group_name": "Digital Art DAO"
    }
  ]
}' > "$PROPOSAL_DIR/delete_group.json"

SUBMIT_RES=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/delete_group.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 3

TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ]; then
    echo "❌ ERROR: Failed to submit delete proposal."
    echo "Logs: $TX_HASH"
    exit 1
fi

echo "Delete Proposal ID: $PROPOSAL_ID"

# Vote (Alice & Bob are members of Commons Council)
$BINARY tx group vote $PROPOSAL_ID $ALICE_ADDR VOTE_OPTION_YES "Delete" --from alice -y --chain-id $CHAIN_ID --keyring-backend test
$BINARY tx group vote $PROPOSAL_ID $BOB_ADDR VOTE_OPTION_YES "Delete" --from bob -y --chain-id $CHAIN_ID --keyring-backend test

echo "Waiting for voting period (35s)..."
sleep 35

EXEC_RES=$($BINARY tx group exec $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 3

# Verify Execution
EXEC_LOGS=$($BINARY query tx $EXEC_HASH --output json)
if ! echo "$EXEC_LOGS" | grep -q "PROPOSAL_EXECUTOR_RESULT_SUCCESS"; then
    echo "❌ DELETION EXECUTION FAILED."
    echo "Raw: $(echo $EXEC_LOGS)"
    exit 1
fi

# Verify Deletion from Registry
# The query should fail or return key not found
CHECK_INFO=$($BINARY query commons get-extended-group "Digital Art DAO" --output json 2>&1)
if echo "$CHECK_INFO" | grep -q "not found"; then
    echo "✅ SUCCESS: 'Digital Art DAO' successfully deleted from registry."
else
    echo "❌ FAILURE: Group still exists in registry."
    echo "$CHECK_INFO"
    exit 1
fi

echo "--- LIFECYCLE TESTS COMPLETE ---"