#!/bin/bash

echo "--- TESTING: POLICY PERMISSIONS (RATCHET DOWN & GOV OVERRIDE) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

mkdir -p proposals

# robust Address Lookup
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')

# DISCOVER COUNCIL (Commons Council Standard Policy)
COUNCIL_INFO=$($BINARY query commons get-extended-group "Commons Council" --output json)
COUNCIL_ADDR=$(echo $COUNCIL_INFO | jq -r '.extended_group.policy_address')

echo "Gov Address:     $GOV_ADDR"
echo "Council Address: $COUNCIL_ADDR"

if [ -z "$COUNCIL_ADDR" ] || [ "$COUNCIL_ADDR" == "null" ]; then
    echo "❌ SETUP ERROR: Council Address not found. Run group_setup.sh first."
    exit 1
fi

# --- 1. BASELINE CHECK ---
echo "--- STEP 1: VERIFY INITIAL PERMISSIONS ---"
PERMS_JSON=$($BINARY query commons show-policy-permissions $COUNCIL_ADDR --output json)
echo "Current Permissions:"
echo "$PERMS_JSON" | jq -r '.policy_permissions.allowed_messages[]'

# Check if MsgSpendFromCommons is currently allowed
if echo "$PERMS_JSON" | grep -q "MsgSpendFromCommons"; then
    echo "✅ MsgSpendFromCommons is currently ALLOWED."
else
    echo "❌ SETUP ERROR: MsgSpendFromCommons should be allowed at start."
    exit 1
fi

# --- 2. SELF-REGULATION (RATCHET DOWN) ---
echo "--- STEP 2: COUNCIL VOLUNTARILY REMOVES SPEND PERMISSION ---"

# We create a new list that EXCLUDES Spend but KEEPS UpdatePolicyPermissions
echo '{
  "group_policy_address": "'$COUNCIL_ADDR'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Self Restriction",
  "summary": "We are voluntarily giving up the power to spend.",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdatePolicyPermissions",
      "authority": "'$COUNCIL_ADDR'",
      "policy_address": "'$COUNCIL_ADDR'",
      "allowed_messages": [
        "/sparkdream.commons.v1.MsgDeleteGroup",
			  "/sparkdream.commons.v1.MsgRegisterGroup",
			  "/sparkdream.commons.v1.MsgRenewGroup",
			  "/sparkdream.commons.v1.MsgUpdateGroupConfig",
			  "/sparkdream.commons.v1.MsgUpdateGroupMembers",
			  "/sparkdream.commons.v1.MsgUpdatePolicyPermissions",
			  "/sparkdream.name.v1.MsgResolveDispute",
			  "/cosmos.group.v1.MsgVote"
      ]
    }
  ]
}' > "$PROPOSAL_DIR/msg_ratchet_down.json"

# Submit, Vote, Exec
echo "Submitting Ratchet Down Proposal..."
SUBMIT_RES=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/msg_ratchet_down.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 3
PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

echo "Proposal ID: $PROP_ID"
$BINARY tx group vote $PROP_ID $ALICE_ADDR VOTE_OPTION_YES "Limit power" --from alice -y --chain-id $CHAIN_ID --keyring-backend test
$BINARY tx group vote $PROP_ID $BOB_ADDR VOTE_OPTION_YES "Limit power" --from bob -y --chain-id $CHAIN_ID --keyring-backend test

echo "Waiting for voting period (35s)..."
sleep 35

echo "Executing Ratchet Down..."
$BINARY tx group exec $PROP_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --output json > /dev/null
sleep 3

# Verify Removal
NEW_PERMS=$($BINARY query commons show-policy-permissions $COUNCIL_ADDR --output json)
if echo "$NEW_PERMS" | grep -q "MsgSpendFromCommons"; then
    echo "❌ FAILURE: MsgSpendFromCommons is STILL in the list."
    exit 1
else
    echo "✅ SUCCESS: MsgSpendFromCommons successfully removed."
fi

# --- 3. ENFORCEMENT CHECK ---
echo "--- STEP 3: VERIFY COUNCIL CANNOT SPEND ---"

echo '{
  "group_policy_address": "'$COUNCIL_ADDR'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Illegal Spend",
  "summary": "Trying to spend after removing permission",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgSpendFromCommons",
      "authority": "'$COUNCIL_ADDR'",
      "recipient": "'$ALICE_ADDR'",
      "amount": [{"denom": "uspark", "amount": "1"}] 
    }
  ]
}' > "$PROPOSAL_DIR/msg_illegal_spend.json"

# Attempt Submission (Should fail at AnteHandler level)
OUTPUT=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/msg_illegal_spend.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark 2>&1)

if echo "$OUTPUT" | grep -q "msg /sparkdream.commons.v1.MsgSpendFromCommons not allowed for policy"; then
    echo "✅ SUCCESS: AnteHandler blocked the Spend attempt."
else
    echo "❌ FAILURE: Spend attempt was NOT blocked."
    echo "$OUTPUT"
    exit 1
fi

# --- 4. UNAUTHORIZED EXPANSION (RATCHET CHECK) ---
echo "--- STEP 4: COUNCIL TRIES TO ADD PERMISSION BACK (SHOULD FAIL) ---"

# Council tries to add MsgSpendFromCommons back
echo '{
  "group_policy_address": "'$COUNCIL_ADDR'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Sneaky Expansion",
  "summary": "Trying to add spend permission back",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdatePolicyPermissions",
      "authority": "'$COUNCIL_ADDR'",
      "policy_address": "'$COUNCIL_ADDR'",
      "allowed_messages": [
        "/sparkdream.commons.v1.MsgDeleteGroup",
			  "/sparkdream.commons.v1.MsgRegisterGroup",
			  "/sparkdream.commons.v1.MsgRenewGroup",
			  "/sparkdream.commons.v1.MsgSpendFromCommons",
			  "/sparkdream.commons.v1.MsgUpdateGroupConfig",
			  "/sparkdream.commons.v1.MsgUpdateGroupMembers",
			  "/sparkdream.commons.v1.MsgUpdatePolicyPermissions",
			  "/sparkdream.name.v1.MsgResolveDispute",
			  "/cosmos.group.v1.MsgVote"
      ]
    }
  ]
}' > "$PROPOSAL_DIR/msg_sneaky_expansion.json"

# 1. Submission: WILL SUCCEED (because UpdatePolicyPermissions is allowed)
SUBMIT_RES=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/msg_sneaky_expansion.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 3
PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

echo "Sneaky Proposal ID: $PROP_ID"
$BINARY tx group vote $PROP_ID $ALICE_ADDR VOTE_OPTION_YES "Try it" --from alice -y --chain-id $CHAIN_ID --keyring-backend test
$BINARY tx group vote $PROP_ID $BOB_ADDR VOTE_OPTION_YES "Try it" --from bob -y --chain-id $CHAIN_ID --keyring-backend test

echo "Waiting for voting period (35s)..."
sleep 35

# 2. Execution: MUST FAIL
echo "Executing Sneaky Expansion..."
EXEC_RES=$($BINARY tx group exec $PROP_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 3

EXEC_LOGS=$($BINARY query tx $EXEC_HASH --output json)

if echo "$EXEC_LOGS" | grep -q "ratchet down violation"; then
    echo "✅ SUCCESS: Execution failed with 'ratchet down violation'."
elif echo "$EXEC_LOGS" | grep -q "PROPOSAL_EXECUTOR_RESULT_FAILURE"; then
    echo "✅ SUCCESS: Proposal Execution Result = FAILURE."
else
    echo "❌ CRITICAL FAILURE: The Council was able to expand its own permissions!"
    echo "Raw Log: $(echo $EXEC_LOGS)"
    exit 1
fi

# --- 5. SUPREME AUTHORITY RESTORATION ---
echo "--- STEP 5: GOVERNANCE RESTORES THE PERMISSION ---"

# x/gov (Community) proposes to fix the permissions.
echo '{
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdatePolicyPermissions",
      "authority": "'$GOV_ADDR'", 
      "policy_address": "'$COUNCIL_ADDR'",
      "allowed_messages": [
        "/sparkdream.commons.v1.MsgDeleteGroup",
			  "/sparkdream.commons.v1.MsgRegisterGroup",
			  "/sparkdream.commons.v1.MsgRenewGroup",
			  "/sparkdream.commons.v1.MsgSpendFromCommons",
			  "/sparkdream.commons.v1.MsgUpdateGroupConfig",
			  "/sparkdream.commons.v1.MsgUpdateGroupMembers",
			  "/sparkdream.commons.v1.MsgUpdatePolicyPermissions",
			  "/sparkdream.name.v1.MsgResolveDispute",
			  "/cosmos.group.v1.MsgVote"
      ]
    }
  ],
  "deposit": "100000000uspark",
  "title": "Restore Spend Powers",
  "summary": "Community restores spending power to the council.",
  "expedited": true
}' > "$PROPOSAL_DIR/gov_restore_perms.json"

SUBMIT_RES=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/gov_restore_perms.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 400000 --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 3
GOV_PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

if [ -z "$GOV_PROP_ID" ] || [ "$GOV_PROP_ID" == "null" ]; then
    # Fallback
    GOV_PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

echo "Gov Proposal ID: $GOV_PROP_ID"

# Vote YES
$BINARY tx gov vote $GOV_PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test

echo "Waiting for Expedited Voting (40s)..."
sleep 45

# --- 6. FINAL VERIFICATION ---
echo "--- STEP 6: VERIFY RESTORATION ---"

FINAL_PERMS=$($BINARY query commons show-policy-permissions $COUNCIL_ADDR --output json)

if echo "$FINAL_PERMS" | grep -q "MsgSpendFromCommons"; then
    echo "🎉 GRAND SUCCESS: Governance successfully restored the spending permission."
else
    echo "❌ FAILURE: Permission was not restored."
    exit 1
fi