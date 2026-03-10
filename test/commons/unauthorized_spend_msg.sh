#!/bin/bash

echo "--- TESTING FAILURE: UNAUTHORIZED MESSAGE REJECTION ---"

# --- 0. SETUP & ADDRESS DISCOVERY ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)

# DISCOVERY: Find the Commons Council Policy Address
# We target the main "Commons Council" because its allowlist does NOT include MsgSend.
# (It only includes MsgSpendFromCommons, MsgResolveDispute, etc.)
GROUP_NAME="Commons Council"

echo "Looking up '$GROUP_NAME'..."
GROUP_INFO=$($BINARY query commons get-group "$GROUP_NAME" --output json)
POLICY_ADDR=$(echo $GROUP_INFO | jq -r '.group.policy_address')

if [ -z "$POLICY_ADDR" ] || [ "$POLICY_ADDR" == "null" ]; then
    echo "SETUP ERROR: '$GROUP_NAME' not found. Run genesis/bootstrap first."
    exit 1
fi

echo "Target Group Policy: $POLICY_ADDR"

# --- 1. Create the "Malicious" Proposal JSON ---
# We try to use /cosmos.bank.v1beta1.MsgSend.
# This is NOT in the StandardPermissions list for Commons Council.
echo '{
  "policy_address": "'$POLICY_ADDR'",
  "messages": [
    {
      "@type": "/cosmos.bank.v1beta1.MsgSend",
      "from_address": "'$POLICY_ADDR'",
      "to_address": "'$ALICE_ADDR'",
      "amount": [{"denom": "uspark", "amount": "1"}]
    }
  ],
  "metadata": "Trying to bypass the wrapper whitelist"
}' > "$PROPOSAL_DIR/msg_fail_spend.json"

# --- 2. Submit Proposal & Check for Failure ---
echo "Submitting proposal (Expecting rejection)..."

# The permission check now happens in the MsgServer's SubmitProposal handler.
# The tx will be broadcast but the message execution will fail with a non-zero code.
OUTPUT=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/msg_fail_spend.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json 2>&1)

# --- 3. Verification ---
# The MsgServer SubmitProposal handler checks permissions and returns:
#   "msg /cosmos.bank.v1beta1.MsgSend not allowed for policy <addr>"
# This results in a failed tx (non-zero code) with the error in raw_log.

# Check for non-zero code (tx failure)
TX_CODE=$(echo "$OUTPUT" | jq -r '.code // empty' 2>/dev/null)

if [ -n "$TX_CODE" ] && [ "$TX_CODE" != "0" ]; then
    RAW_LOG=$(echo "$OUTPUT" | jq -r '.raw_log // empty' 2>/dev/null)
    if echo "$RAW_LOG" | grep -q "not allowed for policy"; then
        echo "FAILURE TEST PASSED: The message server correctly rejected the unauthorized message."
        echo "   Error: $RAW_LOG"
    else
        echo "FAILURE TEST PASSED: Tx failed with code $TX_CODE."
        echo "   Raw log: $RAW_LOG"
    fi
elif echo "$OUTPUT" | grep -q "not allowed for policy"; then
    echo "FAILURE TEST PASSED: The message server correctly rejected the unauthorized message."
    echo "   Error match: 'not allowed for policy'"
elif echo "$OUTPUT" | grep -q "insufficient fee"; then
    echo "TEST FAILED: You didn't pay enough fees to reach the whitelist check."
    echo "   Adjustment: Increase --fees flag."
else
    # It's possible the tx was accepted (code=0) but we need to check the tx hash
    TX_HASH=$(echo "$OUTPUT" | jq -r '.txhash // empty' 2>/dev/null)
    if [ -n "$TX_HASH" ]; then
        sleep 5
        TX_RES=$($BINARY query tx $TX_HASH --output json 2>&1)
        TX_RES_CODE=$(echo "$TX_RES" | jq -r '.code // 0')
        if [ "$TX_RES_CODE" != "0" ]; then
            RAW_LOG=$(echo "$TX_RES" | jq -r '.raw_log // empty')
            if echo "$RAW_LOG" | grep -q "not allowed for policy"; then
                echo "FAILURE TEST PASSED: The message server correctly rejected the unauthorized message."
                echo "   Error: $RAW_LOG"
            else
                echo "FAILURE TEST PASSED: Tx failed with code $TX_RES_CODE."
                echo "   Raw log: $RAW_LOG"
            fi
        else
            echo "FAILURE TEST FAILED: The message was NOT rejected. Tx succeeded."
            echo "   Full Output:"
            echo "$OUTPUT"
        fi
    else
        echo "FAILURE TEST FAILED: The message was NOT rejected with the expected error."
        echo "   Full Output:"
        echo "$OUTPUT"
    fi
fi
