#!/bin/bash

echo "--- TESTING: INTERIM COUNCIL (DICTATOR MODE & RESTORATION) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)
# poster1/poster2 are created and funded by setup_test_accounts.sh; this
# test runs first, before any other test adds keys.
POSTER1_ADDR=$($BINARY keys show poster1 -a --keyring-backend test)
POSTER2_ADDR=$($BINARY keys show poster2 -a --keyring-backend test)

GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')
echo "Gov Address: $GOV_ADDR"

# --- 1. PHASE 1: INSTALL AN INTERIM COUNCIL ---
echo "--- PHASE 1: INSTALLING BOB, POSTER1 AND POSTER2 AS THE INTERIM COUNCIL ---"
echo "Alice (Validator) votes to wipe Commons Council and install Bob, Poster1 and Poster2..."

# Using MsgRenewGroup to wipe the slate. The Commons Council's min_members (3)
# binds x/gov too, so the interim council needs three seats.
echo '{
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgRenewGroup",
      "authority": "'$GOV_ADDR'",
      "group_name": "Commons Council",
      "new_members": ["'$BOB_ADDR'", "'$POSTER1_ADDR'", "'$POSTER2_ADDR'"],
      "new_member_weights": ["1", "1", "1"]
    }
  ],
  "deposit": "100000000'"$BOND_DENOM"'",
  "title": "Emergency Interim Council Act",
  "summary": "Replacing the Commons Council with Bob, Poster1 and Poster2.",
  "expedited": true
}' > "$PROPOSAL_DIR/set_bob_dictator.json"

# Submit & Vote
SUBMIT_RES=$($BINARY tx gov submit-proposal --gas 1000000 "$PROPOSAL_DIR/set_bob_dictator.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 3

# Get Prop ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
PROP_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
if [ -z "$PROP_ID" ] || [ "$PROP_ID" == "null" ]; then
   PROP_ID=$(echo $TX_RES | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

echo "Dictator Prop ID: $PROP_ID"
$BINARY tx gov vote $PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test

echo "Waiting for Expedited Voting (45s)..."
sleep 45

# Verify the interim council is exactly Bob, Poster1 and Poster2
# 1. Get Group ID
GROUP_INFO=$($BINARY query commons get-group "Commons Council" --output json)
GROUP_ID=$(echo $GROUP_INFO | jq -r '.group.group_id // "0"')
POLICY_ADDR=$(echo $GROUP_INFO | jq -r '.group.policy_address')

# 2. Check Members (x/commons get-council-members returns {"members": [...]} of Member)
MEMBERS_JSON=$($BINARY query commons get-council-members "Commons Council" --output json)
COUNT=$(echo $MEMBERS_JSON | jq '.members | length')
MEMBER_ADDRS=$(echo $MEMBERS_JSON | jq -r '.members[].address' | sort | tr '\n' ' ')
EXPECTED_ADDRS=$(printf '%s\n' "$BOB_ADDR" "$POSTER1_ADDR" "$POSTER2_ADDR" | sort | tr '\n' ' ')

if [ "$COUNT" == "3" ] && [ "$MEMBER_ADDRS" == "$EXPECTED_ADDRS" ]; then
    echo "[ OK ] SUCCESS: Bob, Poster1 and Poster2 are now the interim council."
else
    echo "[FAIL] FAILURE: Membership update failed. Count: $COUNT"
    exit 1
fi

# --- 2. ATTACK & VETO (Interim Council Veto) ---
echo "--- PHASE 2: INTERIM COUNCIL VETOES MALICIOUS PROPOSAL ---"

# Carol creates malicious proposal
echo '{
  "messages": [
    {
      "@type": "/cosmos.bank.v1beta1.MsgSend",
      "from_address": "'$GOV_ADDR'",
      "to_address": "'$CAROL_ADDR'",
      "amount": [{"denom": "'"$BOND_DENOM"'", "amount": "1"}]
    }
  ],
  "deposit": "50000000'"$BOND_DENOM"'",
  "title": "Steal Funds",
  "summary": "Trying to steal while the interim council is in charge."
}' > "$PROPOSAL_DIR/bad_prop_bob.json"

SUBMIT_RES=$($BINARY tx gov submit-proposal --gas 1000000 "$PROPOSAL_DIR/bad_prop_bob.json" --from carol -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 3
BAD_PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
echo "Bad Prop ID: $BAD_PROP_ID"

echo "Discovering Veto Policy..."

# 1. DISCOVER VETO POLICY for Commons Council (exposed directly on Group)
VETO_POLICY_ADDR=$($BINARY query commons get-group "Commons Council" --output json | jq -r '.group.veto_policy_address')

if [ -z "$VETO_POLICY_ADDR" ] || [ "$VETO_POLICY_ADDR" == "null" ]; then
    echo "[FAIL] ERROR: Could not find Veto Policy for Group $GROUP_ID"
    exit 1
fi

echo "Using Veto Policy: $VETO_POLICY_ADDR"

# 2. Bob submits Veto via VETO Policy
echo "Bob submits Veto via Group Proposal..."
echo '{
  "policy_address": "'$VETO_POLICY_ADDR'",
  "proposers": ["'$BOB_ADDR'"],
  "title": "FAST VETO",
  "summary": "Immediate execution.",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgEmergencyCancelGovProposal",
      "authority": "'$VETO_POLICY_ADDR'",
      "proposal_id": '$BAD_PROP_ID'
    }
  ]
}' > "$PROPOSAL_DIR/fast_veto.json"

# Submit Group Proposal
SUBMIT_GROUP=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/fast_veto.json" --from bob -y --chain-id $CHAIN_ID --keyring-backend test --output json)
GROUP_TX=$(echo $SUBMIT_GROUP | jq -r '.txhash')
sleep 3

GROUP_PROP_ID=$(echo $($BINARY query tx $GROUP_TX --output json) | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
if [ -z "$GROUP_PROP_ID" ] || [ "$GROUP_PROP_ID" == "null" ]; then
   GROUP_PROP_ID=$(echo $($BINARY query tx $GROUP_TX --output json) | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi
echo "Group Prop ID: $GROUP_PROP_ID"

# Bob and Poster1 vote YES (2 of 3 seats clears the 0.49 veto threshold)
$BINARY tx commons vote-proposal $GROUP_PROP_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3
$BINARY tx commons vote-proposal $GROUP_PROP_ID yes --from poster1 -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3
$BINARY tx commons execute-proposal $GROUP_PROP_ID --gas 2000000 --from bob -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3

# Verify Kill
STATUS=$($BINARY query gov proposal $BAD_PROP_ID --output json | jq -r '.proposal.status')
if [ "$STATUS" == "PROPOSAL_STATUS_FAILED" ] || [ "$STATUS" == "PROPOSAL_STATUS_REJECTED" ]; then
    echo "[ OK ] SUCCESS: The interim council vetoed using Veto Policy."
else
    echo "[FAIL] FAILURE: Proposal is still $STATUS"
    exit 1
fi

# --- 3. RESTORATION: EXPAND GROUP ---
echo "--- PHASE 3: RESTORING THE REPUBLIC ---"
echo "Bob (or Gov) proposes to restore Alice, Bob and Carol..."

# The interim council cannot do this itself (it is not the Commons Council's
# parent), so we must use a Gov Proposal again.

echo '{
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgRenewGroup",
      "authority": "'$GOV_ADDR'",
      "group_name": "Commons Council",
      "new_members": ["'$ALICE_ADDR'", "'$BOB_ADDR'", "'$CAROL_ADDR'"],
      "new_member_weights": ["1", "1", "1"]
    }
  ],
  "deposit": "100000000'"$BOND_DENOM"'",
  "title": "Restore Democracy",
  "summary": "Restoring the original council.",
  "expedited": true
}' > "$PROPOSAL_DIR/restore_council.json"

# Submit Proposal
SUBMIT_RES=$($BINARY tx gov submit-proposal --gas 1000000 "$PROPOSAL_DIR/restore_council.json" --from bob -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 3
RESTORE_PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
if [ -z "$RESTORE_PROP_ID" ] || [ "$RESTORE_PROP_ID" == "null" ]; then
   RESTORE_PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

echo "Restoration Prop ID: $RESTORE_PROP_ID"

# Alice Votes Yes to ratify
$BINARY tx gov vote $RESTORE_PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test

echo "Waiting for Expedited Voting (45s)..."
sleep 45

# Verify Final State
FINAL_MEMBERS=$($BINARY query commons get-council-members "Commons Council" --output json)
FINAL_COUNT=$(echo $FINAL_MEMBERS | jq '.members | length')
FINAL_ADDRS=$(echo $FINAL_MEMBERS | jq -r '.members[].address' | sort | tr '\n' ' ')
RESTORED_ADDRS=$(printf '%s\n' "$ALICE_ADDR" "$BOB_ADDR" "$CAROL_ADDR" | sort | tr '\n' ' ')

if [ "$FINAL_ADDRS" == "$RESTORED_ADDRS" ]; then
    echo "[ OK ] SUCCESS: The Republic is Restored. Alice, Bob and Carol are back."
else
    echo "[FAIL] FAILURE: Restoration failed. Count: $FINAL_COUNT"
    exit 1
fi