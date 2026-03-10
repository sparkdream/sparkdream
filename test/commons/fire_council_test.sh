#!/bin/bash

echo "--- TESTING: CONSTITUTIONAL REMOVAL (FIRING THE COMMONS COUNCIL) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

# Gov Address
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')
echo "Gov Module Address: $GOV_ADDR"

# Discover Veto Policy for Commons Council
VETO_POLICY_ADDR=$($BINARY query commons get-group "Commons Council" --output json | jq -r '.group.veto_policy_address')

echo "Veto Policy Address: $VETO_POLICY_ADDR"

# Check who is currently in the council (Should be Alice/Bob/Carol from Genesis)
MEMBER_COUNT=$($BINARY query commons get-council-members "Commons Council" --output json | jq '.members | length')
echo "Current Member Count: $MEMBER_COUNT"

# --- 1. ATTACK: Validators Vote to Fire the Council ---
echo "--- PHASE 1: THE CONSTITUTIONAL COUP ---"
echo "Alice submits EXPEDITED proposal to WIPE membership and install Bob as Dictator..."

# We use MsgRenewGroup signed by x/gov
echo '{
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgRenewGroup",
      "authority": "'$GOV_ADDR'",
      "group_name": "Commons Council",
      "new_members": ["'$BOB_ADDR'"],
      "new_member_weights": ["1"]
    }
  ],
  "deposit": "100000000uspark",
  "title": "FIRE THE COUNCIL",
  "summary": "The Council has gone rogue. We are wiping the slate via Expedited Proposal.",
  "expedited": true
}' > "$PROPOSAL_DIR/fire_council.json"

# Submit EXPEDITED Proposal
SUBMIT_RES=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/fire_council.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

echo "Submitted Expedited Prop. Hash: $TX_HASH"
sleep 5

# Get Gov Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
GOV_PROP_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
if [ -z "$GOV_PROP_ID" ] || [ "$GOV_PROP_ID" == "null" ]; then
   GOV_PROP_ID=$(echo $TX_RES | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

echo "Expedited Gov Proposal ID: $GOV_PROP_ID"

# ALICE VOTES YES (Super-Majority)
echo "Alice votes YES..."
$BINARY tx gov vote $GOV_PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5

# --- 2. DEFENSE: Council Tries to Veto ---
echo "--- PHASE 2: THE FAILED DEFENSE ---"
echo "Council panics and tries to Veto the proposal..."

echo '{
  "policy_address": "'$VETO_POLICY_ADDR'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgEmergencyCancelGovProposal",
      "authority": "'$VETO_POLICY_ADDR'",
      "proposal_id": '$GOV_PROP_ID'
    }
  ],
  "metadata": "Trying to kill the proposal that fires us."
}' > "$PROPOSAL_DIR/msg_fail_veto.json"

# Submit Commons Proposal
SUBMIT_GROUP_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/msg_fail_veto.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
GROUP_TX_HASH=$(echo $SUBMIT_GROUP_RES | jq -r '.txhash')
sleep 5

# Get Commons Proposal ID
GROUP_TX_RES=$($BINARY query tx $GROUP_TX_HASH --output json)
GROUP_PROP_ID=$(echo $GROUP_TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
if [ -z "$GROUP_PROP_ID" ] || [ "$GROUP_PROP_ID" == "null" ]; then
   GROUP_PROP_ID=$(echo $GROUP_TX_RES | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi
echo "Commons Proposal ID: $GROUP_PROP_ID"

# Vote to Veto (Consensus)
echo "Council votes YES to Veto..."
$BINARY tx commons vote-proposal $GROUP_PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5
$BINARY tx commons vote-proposal $GROUP_PROP_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test
sleep 5

# EXECUTE VETO -> THIS MUST FAIL (Constitutional Protection)
echo "Attempting to Execute Veto (Expect Failure)..."
EXEC_RES=$($BINARY tx commons execute-proposal $GROUP_PROP_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --output json)
EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 5

# --- 3. VERIFY VETO FAILURE ---
echo "--- VERIFYING VETO FAILURE ---"
EXEC_TX_JSON=$($BINARY query tx $EXEC_TX_HASH --output json)
EXEC_CODE=$(echo $EXEC_TX_JSON | jq -r '.code')

if [ "$EXEC_CODE" != "0" ]; then
    if echo "$EXEC_TX_JSON" | grep -q "Constitutional Protection"; then
        echo "SUCCESS: The Code Exception worked! (Constitutional Protection)"
    else
        echo "SUCCESS: Veto Execution Failed as expected."
        echo "   Detail: $(echo $EXEC_TX_JSON | jq -r '.raw_log // empty')"
    fi
else
    # Check if the proposal status shows failure
    PROP_STATUS=$($BINARY query commons get-proposal $GROUP_PROP_ID --output json | jq -r '.proposal.status')
    if [ "$PROP_STATUS" == "PROPOSAL_STATUS_FAILED" ]; then
        echo "SUCCESS: Veto Proposal executed but inner message FAILED (Constitutional Protection)."
    else
        echo "FAILURE: The Veto Execution did NOT fail as expected."
        echo "   Full Logs: $(echo $EXEC_TX_JSON | jq -r '.raw_log')"
        exit 1
    fi
fi

# --- 4. VERIFY GOV SUCCESS ---
echo "--- WAITING FOR GOV PROPOSAL TO PASS ---"
echo "Waiting 45s for Expedited Voting Period to end..."
sleep 45

echo "--- VERIFYING NEW REGIME ---"

# Check Gov Prop Status
GOV_STATUS=$($BINARY query gov proposal $GOV_PROP_ID --output json | jq -r '.proposal.status')
echo "Gov Prop Status: $GOV_STATUS"

if [ "$GOV_STATUS" != "PROPOSAL_STATUS_PASSED" ]; then
    echo "FAILURE: Gov Proposal did not pass (Status: $GOV_STATUS)."
    exit 1
fi

# Check Membership via commons query
NEW_MEMBERS=$($BINARY query commons get-council-members "Commons Council" --output json)
NEW_MEMBER_COUNT=$(echo $NEW_MEMBERS | jq '.members | length')
NEW_MEMBER_ADDR=$(echo $NEW_MEMBERS | jq -r '.members[0].address')

echo "New Member Count: $NEW_MEMBER_COUNT"
echo "New Member Addr:  $NEW_MEMBER_ADDR"

if [ "$NEW_MEMBER_COUNT" == "1" ] && [ "$NEW_MEMBER_ADDR" == "$BOB_ADDR" ]; then
    echo "GRAND SUCCESS: The Council has been fired. Bob is the new Dictator."
else
    echo "FAILURE: Membership was not updated correctly."
fi
