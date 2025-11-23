#!/bin/bash

echo "--- TESTING FAILURE: UNAUTHORIZED MESSAGE REJECTION ---"

# --- 0. SETUP & ADDRESS DISCOVERY ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

# Ensure proposals directory exists
mkdir -p proposals

# Discover the Commons Council Address (Group Policy)
COMMONS_COUNCIL_ADDR=$($BINARY query commons params --output json | jq -r '.params.commons_council_address')
echo "Target Group Policy: $COMMONS_COUNCIL_ADDR"

# --- 1. Create the "Malicious" Proposal JSON ---
echo '{
  "group_policy_address": "'$COMMONS_COUNCIL_ADDR'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Unauthorized Proposal",
  "summary": "Trying to bypass the wrapper whitelist",
  "messages": [
    {
      "@type": "/cosmos.bank.v1beta1.MsgSend",
      "from_address": "'$COMMONS_COUNCIL_ADDR'",
      "to_address": "'$ALICE_ADDR'",
      "amount": [{"denom": "uspark", "amount": "1"}] 
    }
  ]
}' > proposals/msg_fail_spend.json

# --- 2. Submit Proposal & Check for Failure ---
echo "Submitting proposal (Expecting rejection)..."

# We pay the spam fee (5000000uspark) to ensure we pass the FeeAnteDecorator 
# and hit the GroupPolicyDecorator where the whitelist logic lives.
OUTPUT=$($BINARY tx group submit-proposal proposals/msg_fail_spend.json --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark 2>&1)

# --- 3. Verification ---

# FIX: We use a regex (grep -E) to match 'standard' OR ''standard'' OR just the unique error text.
# This handles the CLI escaping behavior robustly.
if echo "$OUTPUT" | grep -q "only SpendFromCommons and UpdateGroupMembers allowed"; then
  echo "✅ FAILURE TEST PASSED: The AnteHandler correctly rejected the message."
  echo "   Error received: $(echo "$OUTPUT" | grep "raw_log" | head -n 1)"
elif echo "$OUTPUT" | grep -q "insufficient fee"; then
  echo "❌ TEST FAILED: You didn't pay enough fees to reach the whitelist check."
  echo "   Adjustment: Increase --fees flag."
else
  echo "❌ FAILURE TEST FAILED: The message was NOT rejected with the expected error."
  echo "   Full Output:"
  echo "$OUTPUT"
fi