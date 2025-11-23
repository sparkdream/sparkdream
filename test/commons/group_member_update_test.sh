#!/bin/bash

echo "--- TESTING: COMMONS COUNCIL MEMBER MANAGEMENT ---"

# --- 0. SETUP ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
GROUP_ID=1
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)

# Create a new key for "Dave" (The new member)
# We check if he exists first to avoid errors on re-run
if ! $BINARY keys show dave --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add dave --keyring-backend test --output json > /dev/null
fi
DAVE_ADDR=$($BINARY keys show dave -a --keyring-backend test)

# Ensure proposals dir exists
mkdir -p proposals

# Discover Addresses
STANDARD_ADDR=$($BINARY query group group-policies-by-group $GROUP_ID -o json | jq -r '.group_policies[] | select(.metadata == "standard") | .address' | head -n 1 | tr -d '"')
GROUP_ADMIN=$($BINARY query group group-info $GROUP_ID --output json | jq -r '.info.admin')

echo "Standard Policy (Council): $STANDARD_ADDR"
echo "Current Group Admin:       $GROUP_ADMIN"

# Sanity Check
if [ "$GROUP_ADMIN" != "$STANDARD_ADDR" ]; then
    echo "❌ SETUP ERROR: The Group Admin is NOT the Standard Policy."
    echo "   Did you run the updated 'group_setup.sh' with the admin handover step?"
    exit 1
fi

# --- 1. FAILURE TEST: DIRECT UPDATE ATTEMPT ---
echo "--- TEST 1: Alice attempts direct member update (Should Fail) ---"

# Create a members file for the direct attempt
echo '{"members": [{"address": "'$DAVE_ADDR'", "weight": "1", "metadata": "Illegal Entry"}]}' > proposals/members_fail.json

# Attempt update (Alice signing)
OUTPUT=$($BINARY tx group update-group-members $GROUP_ID proposals/members_fail.json --from alice -y --chain-id $CHAIN_ID --keyring-backend test 2>&1)

if echo "$OUTPUT" | grep -q "unauthorized"; then
    echo "✅ SUCCESS: Alice was blocked from updating members directly."
else
    # Check for code in json output if grep missed it
    CODE=$(echo "$OUTPUT" | jq -r '.code' 2>/dev/null)
    if [ "$CODE" != "null" ] && [ "$CODE" != "0" ]; then
         echo "✅ SUCCESS: Transaction failed with code $CODE."
    else
         echo "❌ FAILURE: Alice was able to update members! (Or unexpected output)"
         echo "Output: $OUTPUT"
         exit 1
    fi
fi

# --- 2. SUCCESS TEST: GROUP PROPOSAL ---
echo "--- TEST 2: Council votes to Add Dave / Remove Carol ---"

# Create Proposal JSON
# Action: Add Dave (Weight 1), Remove Carol (Weight 0)
# Signer: Must be STANDARD_ADDR (The Group Admin)
echo '{
  "group_policy_address": "'$STANDARD_ADDR'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Welcome Dave, Bye Carol",
  "summary": "Updating council membership.",
  "messages": [
    {
      "@type": "/cosmos.group.v1.MsgUpdateGroupMembers",
      "admin": "'$STANDARD_ADDR'",
      "group_id": "'$GROUP_ID'",
      "member_updates": [
        {"address": "'$DAVE_ADDR'", "weight": "1", "metadata": "Dave"},
        {"address": "'$CAROL_ADDR'", "weight": "0", "metadata": "Carol"}
      ]
    }
  ]
}' > proposals/msg_update_members.json

# Submit
echo "Submitting Proposal..."
SUBMIT_RES=$($BINARY tx group submit-proposal proposals/msg_update_members.json --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

echo "Waiting for block..."
sleep 3

# Get Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
    # Fallback
    PROPOSAL_ID=$(echo $TX_RES | jq -r '.logs[0].events[] | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

if [ -z "$PROPOSAL_ID" ]; then
    echo "❌ ERROR: Failed to submit proposal."
    exit 1
fi
echo "✅ Proposal ID: $PROPOSAL_ID"

# Vote (Alice + Bob)
echo "Alice voting YES..."
$BINARY tx group vote $PROPOSAL_ID $ALICE_ADDR VOTE_OPTION_YES "Yes" --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3
echo "Bob voting YES..."
$BINARY tx group vote $PROPOSAL_ID $BOB_ADDR VOTE_OPTION_YES "Yes" --from bob -y --chain-id $CHAIN_ID --keyring-backend test

# Wait for Voting Period (Standard Policy = 30s in setup script)
echo "Votes cast. Waiting for voting period (35s)..."
sleep 35

# Execute
echo "Executing..."
EXEC_RES=$($BINARY tx group exec $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')

echo "Waiting for execution block..."
sleep 3

# Verify Execution Success
EXEC_LOGS=$($BINARY query tx $EXEC_HASH --output json)
if echo "$EXEC_LOGS" | grep -q "PROPOSAL_EXECUTOR_RESULT_SUCCESS"; then
    echo "✅ Proposal Executed Successfully."
else
    echo "❌ CRITICAL FAILURE: Execution Failed."
    echo "Raw Log: $(echo $EXEC_LOGS | jq -r '.raw_log')"
    exit 1
fi

# --- 3. VERIFY MEMBERSHIP ---
echo "--- VERIFYING MEMBERSHIP CHANGES ---"

# Query Members
MEMBERS_JSON=$($BINARY query group group-members $GROUP_ID --output json)
echo "$MEMBERS_JSON"

# Check Dave (Should exist)
if echo "$MEMBERS_JSON" | grep -q "$DAVE_ADDR"; then
    echo "✅ SUCCESS: Dave is now a member."
else
    echo "❌ FAILURE: Dave was NOT found in the group."
fi

# Check Carol (Should NOT exist)
if echo "$MEMBERS_JSON" | grep -q "$CAROL_ADDR"; then
    echo "❌ FAILURE: Carol is STILL in the group."
else
    echo "✅ SUCCESS: Carol was removed."
fi