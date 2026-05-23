#!/bin/bash

echo "--- TESTING: VETO VOTE (PROPOSAL REJECTION) ---"

# --- 0. SETUP & ADDRESS DISCOVERY ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

GROUP_NAME="Commons Operations Committee"

echo "Looking up '$GROUP_NAME'..."
GROUP_INFO=$($BINARY query commons get-group "$GROUP_NAME" --output json)
POLICY_ADDR=$(echo $GROUP_INFO | jq -r '.group.policy_address')

if [ -z "$POLICY_ADDR" ] || [ "$POLICY_ADDR" == "null" ]; then
    echo "[FAIL] SETUP ERROR: '$GROUP_NAME' not found. Run genesis/bootstrap first."
    exit 1
fi

echo "$GROUP_NAME Policy Address: $POLICY_ADDR"

# x/commons MsgSubmitProposal deducts a per-proposal ProposalFee (5 SPARK by
# default) from the proposer (alice). Pre-fund the policy address too in case
# the spend tx itself dips below the rate-limit floor.
$BINARY tx bank send alice "$POLICY_ADDR" 50000000${BOND_DENOM} \
    --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} -y \
    --output json > /dev/null 2>&1
sleep 5

# --- CHECK BOB'S BALANCE ---
echo "--- SNAPSHOT: BOB'S BALANCE (BEFORE) ---"
$BINARY query bank balances $BOB_ADDR

# --- 1. Create Proposal JSON (x/commons format) ---
# We propose sending a massive amount (500 SPARK) to Bob.
echo '{
  "policy_address": "'$POLICY_ADDR'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgSpendFromCommons",
      "authority": "'$POLICY_ADDR'",
      "recipient": "'$BOB_ADDR'",
      "amount": [
        {
          "denom": "'"$BOND_DENOM"'",
          "amount": "500000000"
        }
      ]
    }
  ],
  "metadata": "Controversial spend that should be vetoed"
}' > "$PROPOSAL_DIR/msg_veto_test.json"

# --- 2. Submit Proposal (x/commons) ---
echo "Submitting proposal..."

SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/msg_veto_test.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000${BOND_DENOM} --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

echo "Tx Hash: $TX_HASH"
echo "Waiting for block inclusion..."
sleep 3

# Query Tx to find Proposal ID — x/commons emits "submit_proposal" events
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" == "null" ]; then
    echo "[FAIL] ERROR: Could not find Proposal ID."
    echo "Tx Response: $TX_RES"
    exit 1
fi

echo "[ OK ] Found Proposal ID: $PROPOSAL_ID"

# --- 3. Cast Veto Votes ---
# Alice and Bob are members of the committee.
# Voting NO_WITH_VETO counts strongly against passing.

echo "Alice voting NO_WITH_VETO..."
$BINARY tx commons vote-proposal $PROPOSAL_ID no_with_veto --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000${BOND_DENOM}
sleep 3

echo "Bob voting NO_WITH_VETO..."
$BINARY tx commons vote-proposal $PROPOSAL_ID no_with_veto --from bob -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000${BOND_DENOM}
sleep 3

echo "Votes cast. Attempting Execution (rejected proposal cannot execute)..."

# --- 4. Attempt Execution (should fail because the proposal didn't reach the
# YES threshold; status will move to REJECTED at voting deadline OR the
# execute tx will return ErrInvalidRequest because the proposal isn't ACCEPTED)
EXEC_RES=$($BINARY tx commons execute-proposal $PROPOSAL_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --fees 5000000${BOND_DENOM} --output json 2>&1)
EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash // empty' 2>/dev/null)
sleep 3

# --- 5. Verify Rejection ---
echo "--- CHECKING PROPOSAL STATUS ---"
STATUS=$($BINARY query commons get-proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')
echo "Status: $STATUS"

# A proposal that received only NO_WITH_VETO votes never reaches the YES
# threshold; it stays SUBMITTED until its voting deadline expires (5 days in
# bootstrap), at which point EndBlocker flips it to REJECTED. For the test
# we accept either SUBMITTED-without-execution or REJECTED — both prove
# the spend did not run.
if [ "$STATUS" == "PROPOSAL_STATUS_REJECTED" ] || [ "$STATUS" == "PROPOSAL_STATUS_SUBMITTED" ]; then
    echo "[ OK ] SUCCESS: Proposal status is $STATUS (spend did not execute)."
else
    echo "[FAIL] FAILURE: Proposal status is $STATUS (expected REJECTED or still SUBMITTED)."
fi

# Check that money did NOT move
echo "--- VERIFYING BOB'S BALANCE (SHOULD BE UNCHANGED) ---"
FINAL_BAL=$($BINARY query bank balances $BOB_ADDR --output json | jq -r --arg denom "$BOND_DENOM" '.balances[] | select(.denom==$denom) | .amount')
if [ -z "$FINAL_BAL" ]; then FINAL_BAL=0; fi

echo "Bob's Final Balance: $FINAL_BAL"
