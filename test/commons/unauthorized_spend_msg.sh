#!/bin/bash

echo "--- TESTING FAILURE: UNAUTHORIZED MESSAGE REJECTION ---"

# --- 0. SETUP & ADDRESS DISCOVERY ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)

# Ensure proposals directory exists
mkdir -p proposals

# DISCOVERY: Find the Commons Council Policy Address
# We target the main "Commons Council" because its allowlist does NOT include MsgSend.
# (It only includes MsgSpendFromCommons, MsgResolveDispute, etc.)
GROUP_NAME="Commons Council"

echo "Looking up '$GROUP_NAME'..."
GROUP_INFO=$($BINARY query commons get-extended-group "$GROUP_NAME" --output json)
POLICY_ADDR=$(echo $GROUP_INFO | jq -r '.extended_group.policy_address')

if [ -z "$POLICY_ADDR" ] || [ "$POLICY_ADDR" == "null" ]; then
    echo "❌ SETUP ERROR: '$GROUP_NAME' not found. Run genesis/bootstrap first."
    exit 1
fi

echo "Target Group Policy: $POLICY_ADDR"

# --- 1. Create the "Malicious" Proposal JSON ---
# We try to use /cosmos.bank.v1beta1.MsgSend.
# This is NOT in the StandardPermissions list for Commons Council.
echo '{
  "group_policy_address": "'$POLICY_ADDR'",
  "proposers": ["'$ALICE_ADDR'"],
  "title": "Unauthorized Proposal",
  "summary": "Trying to bypass the wrapper whitelist",
  "messages": [
    {
      "@type": "/cosmos.bank.v1beta1.MsgSend",
      "from_address": "'$POLICY_ADDR'",
      "to_address": "'$ALICE_ADDR'",
      "amount": [{"denom": "uspark", "amount": "1"}] 
    }
  ]
}' > "$PROPOSAL_DIR/msg_fail_spend.json"

# --- 2. Submit Proposal & Check for Failure ---
echo "Submitting proposal (Expecting rejection)..."

# We pay the spam fee to ensure we pass the FeeAnteDecorator 
# and hit the GroupPolicyDecorator where the whitelist logic lives.
OUTPUT=$($BINARY tx group submit-proposal "$PROPOSAL_DIR/msg_fail_spend.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark 2>&1)

# --- 3. Verification ---

# The AnteHandler code says: "msg type %s not allowed for policy %s"
if echo "$OUTPUT" | grep -q "msg /cosmos.bank.v1beta1.MsgSend not allowed for policy"; then
  echo "✅ FAILURE TEST PASSED: The AnteHandler correctly rejected the message."
  echo "   Error received match: 'not allowed for policy'"
elif echo "$OUTPUT" | grep -q "insufficient fee"; then
  echo "❌ TEST FAILED: You didn't pay enough fees to reach the whitelist check."
  echo "   Adjustment: Increase --fees flag."
else
  echo "❌ FAILURE TEST FAILED: The message was NOT rejected with the expected error."
  echo "   Full Output:"
  echo "$OUTPUT"
fi