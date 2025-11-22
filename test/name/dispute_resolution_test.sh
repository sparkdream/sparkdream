#!/bin/bash

echo "--- TESTING NAME MODULE: THE HIGH COURT (DISPUTES) ---"

# --- 0. SETUP & CONFIG ---
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

# Assume Alice is the Council Member/Admin for simplicity of proposal creation
# Assume Bob is the "Claimant" who wants the name "vitalik"

# --- 1. SETUP: Alice squats on "vitalik" ---
echo "--- STEP 1: Alice registers 'vitalik' ---"
RES=$($BINARY tx name register-name "vitalik" "Original Owner" --from alice -y --chain-id $CHAIN_ID --keyring-backend test -o json)
TX_HASH=$(echo "$RES" | jq -r '.txhash')
sleep 3

# Verify Execution
QUERY_RES=$($BINARY query tx $TX_HASH -o json)
CODE=$(echo "$QUERY_RES" | jq -r '.code')

if [ "$CODE" != "0" ]; then
    # Check if it failed because it already exists (which is fine for setup)
    RAW_LOG=$(echo "$QUERY_RES" | jq -r '.raw_log')
    if echo "$RAW_LOG" | grep -q "name already taken"; then
        echo "⚠️  'vitalik' already registered. Proceeding..."
    else
        echo "❌ FAILURE: Setup registration failed."
        echo "Raw Log: $RAW_LOG"
    fi
else
    echo "✅ Registered 'vitalik'."
fi

# Double check ownership
OWNER_1=$($BINARY query name resolve "vitalik" -o json | jq -r '.name_record.owner')
echo "Current Owner: $OWNER_1"

if [ "$OWNER_1" != "$ALICE_ADDR" ]; then
    echo "⚠️  Warning: Owner is not Alice. Tests might behave unexpectedly."
fi

# --- 2. FILE DISPUTE ---
echo "--- STEP 2: Bob files a dispute (Pays Fee) ---"
DISPUTE_FEE=$($BINARY query name params -o json | jq -r '.params.dispute_fee.amount')
echo "Dispute Fee: $DISPUTE_FEE $DENOM"

# File Dispute
RES=$($BINARY tx name file-dispute "vitalik" --from bob -y --chain-id $CHAIN_ID --keyring-backend test -o json)
TX_HASH=$(echo "$RES" | jq -r '.txhash')
sleep 3

# Verify Execution
QUERY_RES=$($BINARY query tx $TX_HASH -o json)
CODE=$(echo "$QUERY_RES" | jq -r '.code')

if [ "$CODE" != "0" ]; then
    echo "❌ FAILURE: Failed to file dispute."
    echo "Raw Log: $(echo "$QUERY_RES" | jq -r '.raw_log')"
    exit 1
fi

# Verify Dispute Exists in State
DISPUTE_CHECK=$($BINARY query name get-dispute "vitalik" -o json | jq -r '.dispute.claimant')
if [ "$DISPUTE_CHECK" == "$BOB_ADDR" ]; then
    echo "✅ SUCCESS: Dispute filed by Bob."
else
    echo "❌ FAILURE: Dispute not found in state."
    exit 1
fi

# --- 3. COUNCIL PROPOSAL (Seize Name) ---
echo "--- STEP 3: Council creates Proposal to resolve dispute ---"

# Get Council Policy Address
COUNCIL_ID=$($BINARY query name params -o json | jq -r '.params.council_group_id')

# Explicitly find the "standard" policy.
POLICY_ADDR=$($BINARY query group group-policies-by-group $COUNCIL_ID -o json | jq -r '.group_policies[] | select(.metadata == "standard") | .address' | head -n 1)

if [ -z "$POLICY_ADDR" ] || [ "$POLICY_ADDR" == "null" ]; then
    echo "⚠️  'standard' policy not found. Checking available policies..."
    $BINARY query group group-policies-by-group $COUNCIL_ID -o json | jq .
    echo "❌ ERROR: Could not find 'standard' Council Policy Address."
    exit 1
fi

echo "Council Policy: $POLICY_ADDR"

# Create JSON for MsgResolveDispute
# FIX: Changed 'creator' to 'authority' to match proto
echo '{
  "group_policy_address": "'$POLICY_ADDR'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "High Court Ruling: Vitalik",
  "summary": "Transfer name to Bob",
  "messages": [
    {
      "@type": "/sparkdream.name.v1.MsgResolveDispute",
      "authority": "'$POLICY_ADDR'", 
      "name": "vitalik",
      "new_owner": "'$BOB_ADDR'"
    }
  ]
}' > proposals/resolve_dispute.json

# Submit Proposal (Alice submits as she is a council member)
# FIX: Added --fees 5000000uspark
SUBMIT_RES=$($BINARY tx group submit-proposal proposals/resolve_dispute.json --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark -o json)
TX_HASH=$(echo "$SUBMIT_RES" | jq -r '.txhash')
sleep 3

# Check execution of submit-proposal
QUERY_RES=$($BINARY query tx $TX_HASH -o json)
CODE=$(echo "$QUERY_RES" | jq -r '.code')

if [ "$CODE" != "0" ]; then
    echo "❌ FAILURE: Failed to submit proposal."
    echo "Raw Log: $(echo "$QUERY_RES" | jq -r '.raw_log')"
    exit 1
fi

# Get Proposal ID using robust parsing (events[]? handles nulls)
PROP_ID=$(echo "$QUERY_RES" | jq -r '.events[]? | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[]? | select(.key=="proposal_id") | .value' | tr -d '"')

if [ -z "$PROP_ID" ] || [ "$PROP_ID" == "null" ]; then
    # Fallback for older SDK versions using logs
    PROP_ID=$(echo "$QUERY_RES" | jq -r '.logs[0].events[]? | select(.type=="cosmos.group.v1.EventSubmitProposal") | .attributes[]? | select(.key=="proposal_id") | .value' | tr -d '"')
fi

echo "Proposal ID: $PROP_ID"

if [ -z "$PROP_ID" ] || [ "$PROP_ID" == "null" ]; then
    echo "❌ Failed to find Proposal ID in logs."
    # Debug output
    echo "$QUERY_RES"
    exit 1
fi

# --- 4. VOTE & EXECUTE ---
echo "--- STEP 4: Voting & Execution ---"
RES=$($BINARY tx group vote $PROP_ID $ALICE_ADDR VOTE_OPTION_YES "Justice" --from alice -y --chain-id $CHAIN_ID --keyring-backend test -o json)
sleep 3

# Check Vote Success
QUERY_RES=$($BINARY query tx $(echo "$RES" | jq -r '.txhash') -o json)
if [ "$(echo "$QUERY_RES" | jq -r '.code')" != "0" ]; then
    echo "❌ Vote Failed: $(echo "$QUERY_RES" | jq -r '.raw_log')"
    exit 1
fi

# Exec
RES=$($BINARY tx group exec $PROP_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test -o json)
TX_HASH=$(echo "$RES" | jq -r '.txhash')
sleep 3

# Check Exec Success
QUERY_RES=$($BINARY query tx $TX_HASH -o json)
CODE=$(echo "$QUERY_RES" | jq -r '.code')

if [ "$CODE" != "0" ]; then
    echo "❌ FAILURE: Proposal Execution Failed."
    echo "Raw Log: $(echo "$QUERY_RES" | jq -r '.raw_log')"
    exit 1
fi

echo "✅ Proposal Executed Successfully."

# --- 5. VERIFY RESULT ---
echo "--- STEP 5: Verification ---"
NEW_OWNER=$($BINARY query name resolve "vitalik" -o json | jq -r '.name_record.owner')

echo "New Owner: $NEW_OWNER"

if [ "$NEW_OWNER" == "$BOB_ADDR" ]; then
    echo "🎉 SUCCESS: The High Court has spoken. Name transferred to Bob."
else
    echo "❌ FAILURE: Name still owned by $NEW_OWNER"
    exit 1
fi

# Verify Dispute Ticket is gone (Burned)
# FIX: Changed 'dispute' to 'get-dispute'
DISPUTE_GONE=$($BINARY query name get-dispute "vitalik" -o json 2>&1 || true)
if echo "$DISPUTE_GONE" | grep -q "not found"; then
    echo "✅ SUCCESS: Dispute ticket closed."
else
    if [ -z "$DISPUTE_GONE" ]; then
         echo "✅ SUCCESS: Dispute ticket closed (Empty response)."
    else
         echo "⚠️ Warning: Dispute ticket might still exist."
         echo "$DISPUTE_GONE"
    fi
fi