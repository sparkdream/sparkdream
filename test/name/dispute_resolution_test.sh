#!/bin/bash

echo "--- TESTING NAME MODULE: THE HIGH COURT (DISPUTE RESOLUTION) ---"

# --- 0. SETUP & CONFIG ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
DENOM="uspark"
TARGET_NAME="vitalik"

# Ensure jq is installed
if ! command -v jq &> /dev/null; then
    echo "❌ Error: jq is not installed."
    exit 1
fi

ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)

echo "Alice (Council/Squatter): $ALICE_ADDR"
echo "Bob (Claimant):           $BOB_ADDR"
echo "Carol (Council):          $CAROL_ADDR"

# --- 1. SETUP: ALICE REGISTERS NAME ---
echo "--- STEP 1: Alice registers '$TARGET_NAME' ---"
# We try to register. If it fails, we check who owns it.

RES=$($BINARY tx name register-name "$TARGET_NAME" "Original Owner" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json 2>/dev/null)
TX_HASH=$(echo "$RES" | jq -r '.txhash // empty')

if [ ! -z "$TX_HASH" ]; then
    sleep 4
    # Check if successful
    CODE=$($BINARY query tx $TX_HASH --output json | jq -r '.code')
    if [ "$CODE" == "0" ]; then
        echo "✅ Registration TX successful."
    fi
fi

# Verify Ownership (Crucial State Check)
CURRENT_OWNER=$($BINARY query name resolve "$TARGET_NAME" --output json | jq -r '.name_record.owner')
echo "Current Owner: $CURRENT_OWNER"

if [ "$CURRENT_OWNER" != "$ALICE_ADDR" ]; then
    echo "❌ CRITICAL ERROR: '$TARGET_NAME' is owned by $CURRENT_OWNER, not Alice."
    echo "Cannot proceed with test as Alice cannot vote on her own eviction if she isn't the setup."
    # For this test, we assume we need to start fresh or Alice must own it.
    # In a real devnet, you might force a release, but here we exit for safety.
    exit 1
fi
echo "✅ Alice currently owns the name."

# --- 2. FILE DISPUTE ---
echo "--- STEP 2: Bob files a dispute ---"

# Check if dispute already exists
EXISTING_DISPUTE=$($BINARY query name get-dispute "$TARGET_NAME" --output json 2>/dev/null | jq -r '.dispute.claimant // empty')

if [ "$EXISTING_DISPUTE" == "$BOB_ADDR" ]; then
    echo "⚠️  Dispute already pending. Skipping submission."
else
    RES=$($BINARY tx name file-dispute "$TARGET_NAME" --from bob -y --chain-id $CHAIN_ID --keyring-backend test --output json)
    TX_HASH=$(echo "$RES" | jq -r '.txhash')
    sleep 4
    
    # Verify
    QUERY_RES=$($BINARY query tx $TX_HASH --output json)
    CODE=$(echo "$QUERY_RES" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        echo "❌ FAILURE: Failed to file dispute."
        echo "Log: $(echo "$QUERY_RES" | jq -r '.raw_log')"
        exit 1
    fi
    echo "✅ Dispute filed."
fi

# --- 3. COUNCIL PROPOSAL ---
echo "--- STEP 3: Preparing Governance Proposal ---"

# 1. Get Council Group ID from Params
COMMONS_INFO=$($BINARY query commons get-extended-group "Commons Council" --output json)
COMMONS_POLICY=$(echo $COMMONS_INFO | jq -r '.extended_group.policy_address')

if [ -z "$COMMONS_POLICY" ] || [ "$COMMONS_POLICY" == "null" ]; then
    echo "❌ ERROR: No Group Policy found for Commons Council."
    exit 1
fi
echo "Council Policy Address: $COMMONS_POLICY"

# 3. Create Proposal JSON
# Note: The 'authority' in MsgResolveDispute MUST be the Group Policy Address.
echo '{
  "group_policy_address": "'$COMMONS_POLICY'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "High Court Ruling: '$TARGET_NAME'",
  "summary": "Transfer name to Bob (Claimant)",
  "messages": [
    {
      "@type": "/sparkdream.name.v1.MsgResolveDispute",
      "authority": "'$COMMONS_POLICY'", 
      "name": "'$TARGET_NAME'",
      "new_owner": "'$BOB_ADDR'"
    }
  ]
}' > "$PROPOSAL_DIR/resolve_dispute.json"

echo "Submitting Proposal..."
SUBMIT_RES=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/resolve_dispute.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo "$SUBMIT_RES" | jq -r '.txhash')
sleep 4

# Get Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="cosmos.group.v1.EventSubmitProposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ]; then
    echo "❌ ERROR: Failed to submit proposal."
    echo "Logs: $TX_HASH"
    exit 1
fi
echo "✅ Proposal ID: $PROPOSAL_ID"

# --- 4. VOTE & EXECUTE ---
echo "--- STEP 4: Voting & Execution ---"

echo "Alice voting YES..."
$BINARY tx group vote $PROPOSAL_ID $ALICE_ADDR VOTE_OPTION_YES "Justice" --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3
echo "Bob voting YES..."
$BINARY tx group vote $PROPOSAL_ID $BOB_ADDR VOTE_OPTION_YES "Justice" --from bob -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3
echo "Carol voting YES..."
$BINARY tx group vote $PROPOSAL_ID $CAROL_ADDR VOTE_OPTION_YES "Justice" --from carol -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3

# Wait for Voting Period (Child Committee is Fast - 24h in bootstrap, 
# BUT for testing we assumed you might have lowered it or we wait. 
# Since we can't wait 24h, we rely on 'TryExec' if threshold is met immediately)
echo "Votes cast. Attempting Execution..."

EXEC_RES=$($BINARY tx group exec $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 3

# Verify Execution
EXEC_LOGS=$($BINARY query tx $EXEC_HASH --output json)

if echo "$EXEC_LOGS" | grep -q "PROPOSAL_EXECUTOR_RESULT_SUCCESS"; then
    echo "✅ Proposal Executed Successfully."
else
    echo "❌ CRITICAL FAILURE: Execution Failed."
    echo "Raw Log: $(echo $EXEC_LOGS)"
    # Note: If it failed due to 'MinExecutionPeriod' (Timelock), that's expected behavior in Prod 
    # but annoying for scripts. Ensure Child Group has 0 MinExecutionPeriod.
    exit 1
fi

# --- 5. VERIFY RESULTS ---
echo "--- STEP 5: Verifying Ledger State ---"

# 1. Check Owner
FINAL_OWNER=$($BINARY query name resolve "$TARGET_NAME" --output json | jq -r '.name_record.owner')
echo "New Owner: $FINAL_OWNER"

if [ "$FINAL_OWNER" == "$BOB_ADDR" ]; then
    echo "🎉 SUCCESS: Name transferred to Bob."
else
    echo "❌ FAILURE: Owner mismatch. Expected $BOB_ADDR, got $FINAL_OWNER."
    exit 1
fi

# 2. Check Dispute is Cleared
DISPUTE_STATUS=$($BINARY query name get-dispute "$TARGET_NAME" --output json 2>&1 || true)
if echo "$DISPUTE_STATUS" | grep -q "not found"; then
    echo "✅ SUCCESS: Dispute record removed from state."
else
    # Some CLI versions return an empty JSON object, others return an error string
    if [ "$DISPUTE_STATUS" == "{}" ] || [ -z "$DISPUTE_STATUS" ]; then
         echo "✅ SUCCESS: Dispute record is empty."
    else
         echo "⚠️  WARNING: Dispute record might still exist:"
         echo "$DISPUTE_STATUS"
    fi
fi