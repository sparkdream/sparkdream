#!bin/bash

echo "--- TESTING FAILURE: UNAUTHORIZED MESSAGE REJECTION ---"

# --- 0. SETUP & ADDRESS DISCOVERY ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

# Discover the Commons Council Address (Group Policy)
COMMONS_COUNCIL_ADDR=$($BINARY query split params --output json | jq -r '.params.commons_council_address')
echo "Target Group Policy: $COMMONS_COUNCIL_ADDR"

# --- 1. Create the "Malicious" Proposal JSON ---
# We try to submit a standard /cosmos.bank.v1beta1.MsgSend.
# Since your GroupWrapper only allows MsgSpendFromCommons and MsgUpdateGroupMembers,
# this MUST fail at the submission stage.

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
}' > msg_fail_spend.json

# --- 2. Submit Proposal & Check for Failure ---
echo "Submitting proposal (Expecting rejection)..."

# We capture the output (stderr included with 2>&1) to check for the error message
OUTPUT=$($BINARY tx group submit-proposal msg_fail_spend.json --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark 2>&1)

sleep 3

# Check if the output contains the specific error from your GroupMsgServerWrapper
if echo "$OUTPUT" | grep -q "not allowed for ''standard'' policy"; then
  echo "✅ FAILURE TEST PASSED: The wrapper correctly rejected the message."
  echo "   Error received: $(echo "$OUTPUT" | grep "not allowed")"
else
  echo "❌ FAILURE TEST FAILED: The message was NOT rejected or a different error occurred."
  echo "   Full Output: $OUTPUT"
fi