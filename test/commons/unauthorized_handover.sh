#!/bin/bash

echo "--- TESTING: HOSTILE HANDOVER ATTEMPT VS EXECUTIVE VETO ---"

# --- 0. SETUP & ADDRESS DISCOVERY ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)
DAVE_ADDR=$($BINARY keys show dave -a --keyring-backend test)
EVE_ADDR=$($BINARY keys show eve -a --keyring-backend test)
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')

# DISCOVER COMMONS COUNCIL
# Get Group ID
GROUP_INFO=$($BINARY query commons get-group "Commons Council" --output json)
GROUP_ID=$(echo $GROUP_INFO | jq -r '.group.group_id // "0"')
COUNCIL_POLICY_ADDR=$(echo $GROUP_INFO | jq -r '.group.policy_address // empty')

if [ -z "$COUNCIL_POLICY_ADDR" ]; then
    echo "[FAIL] SETUP ERROR: Commons Council not found."
    exit 1
fi

# Veto policy address is exposed directly on the Group struct
VETO_POLICY_ADDR=$(echo "$GROUP_INFO" | jq -r '.group.veto_policy_address')

if [ -z "$VETO_POLICY_ADDR" ] || [ "$VETO_POLICY_ADDR" == "null" ]; then
    echo "[FAIL] SETUP ERROR: Veto Policy not found for Commons Council."
    exit 1
fi

# The Technical Council is a sibling of the Commons Council (both answer to
# x/gov), so its veto may cancel a gov proposal against the Commons Council.
TECH_VETO_POLICY_ADDR=$($BINARY query commons get-group "Technical Council" --output json | jq -r '.group.veto_policy_address')
if [ -z "$TECH_VETO_POLICY_ADDR" ] || [ "$TECH_VETO_POLICY_ADDR" == "null" ]; then
    echo "[FAIL] SETUP ERROR: Veto Policy not found for Technical Council."
    exit 1
fi
if ! $BINARY query commons get-council-members "Technical Council" --output json \
    | jq -e --arg a "$ALICE_ADDR" '.members[] | select(.address == $a)' > /dev/null; then
    echo "[FAIL] SETUP ERROR: Alice is not on the Technical Council."
    exit 1
fi

echo "Gov Address:         $GOV_ADDR"
echo "Council Veto Policy: $VETO_POLICY_ADDR"
echo "Tech Veto Policy:    $TECH_VETO_POLICY_ADDR"

# --- 1. ATTACK: SUBMIT HOSTILE GOV PROPOSAL ---
echo "--- ATTACK STARTING: Alice attempts to replace the Council via Governance ---"

# Alice (Validator) proposes to replace the Commons Council with herself, Dave
# and Eve (three seats: the council's min_members binds x/gov too, so this
# would really execute if nobody stops it).
echo '{
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgRenewGroup",
      "authority": "'$GOV_ADDR'",
      "group_name": "Commons Council",
      "new_members": ["'$ALICE_ADDR'", "'$DAVE_ADDR'", "'$EVE_ADDR'"],
      "new_member_weights": ["1", "1", "1"]
    }
  ],
  "deposit": "50000000'"$BOND_DENOM"'",
  "title": "Hostile Handover",
  "summary": "Malicious proposal to hijack the protocol."
}' > "$PROPOSAL_DIR/gov_handover_hostile.json"

# Submit
SUBMIT_RES=$($BINARY tx gov submit-proposal --gas 1000000 "$PROPOSAL_DIR/gov_handover_hostile.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

echo "Waiting for block inclusion (3s)..."
sleep 3

# Get Gov Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
GOV_PROP_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

if [ -z "$GOV_PROP_ID" ] || [ "$GOV_PROP_ID" == "null" ]; then
   GOV_PROP_ID=$(echo $TX_RES | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

if [ -z "$GOV_PROP_ID" ]; then
  echo "[FAIL] ERROR: Failed to create Gov Proposal."
  exit 1
fi

echo "[WARN]  Hostile Gov Proposal ID: $GOV_PROP_ID (Status: VOTING_PERIOD)"

# --- 2. ATTACK: ALICE VOTES YES ---
# Assuming Alice has enough stake to pass it if uncontested.
echo "Alice voting YES (Threatening to pass)..."
$BINARY tx gov vote $GOV_PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3

# submit_veto_proposal POLICY PROPOSER TITLE: submits a group proposal on
# POLICY that emergency-cancels GOV_PROP_ID and prints its proposal id.
submit_veto_proposal() {
    local policy="$1" proposer="$2" title="$3" file="$4"
    local proposer_addr
    proposer_addr=$($BINARY keys show "$proposer" -a --keyring-backend test)
    echo '{
  "policy_address": "'$policy'",
  "proposers": ["'$proposer_addr'"],
  "title": "'"$title"'",
  "summary": "Stopping hostile takeover.",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgEmergencyCancelGovProposal",
      "authority": "'$policy'",
      "proposal_id": '$GOV_PROP_ID'
    }
  ]
}' > "$file"
    local res tx tx_res id
    res=$($BINARY tx commons submit-proposal "$file" --from "$proposer" -y --chain-id $CHAIN_ID --keyring-backend test --output json)
    tx=$(echo "$res" | jq -r '.txhash')
    sleep 3
    tx_res=$($BINARY query tx $tx --output json)
    id=$(echo "$tx_res" | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
    if [ -z "$id" ]; then
        id=$(echo "$tx_res" | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
    fi
    echo "$id"
}

# --- 3. DEFENSE ATTEMPT: THE TARGETED COUNCIL VETOES ITSELF (REFUSED) ---
# A council's veto may not cancel a gov proposal that disciplines that same
# council: gov is the remedy against a captured council.
echo "--- SELF-PROTECTION: Commons Council tries to cancel the proposal against itself ---"
GROUP_PROP_ID=$(submit_veto_proposal "$VETO_POLICY_ADDR" bob "EMERGENCY: KILL PROP $GOV_PROP_ID" "$PROPOSAL_DIR/exec_veto.json")
echo "  Commons Veto Proposal ID: $GROUP_PROP_ID"

# Veto threshold is 0.49: Bob + Carol (2 of 3 seats) clear it.
echo "Bob and Carol voting YES to Veto..."
$BINARY tx commons vote-proposal $GROUP_PROP_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3
$BINARY tx commons vote-proposal $GROUP_PROP_ID yes --from carol -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3

echo "Attempting Execution (Threshold Met, must be refused)..."
EXEC_RES=$($BINARY tx commons execute-proposal $GROUP_PROP_ID --gas 2000000 --from bob -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 3

GROUP_PROP_STATUS=$($BINARY query commons get-proposal $GROUP_PROP_ID --output json | jq -r '.proposal.status')
EXEC_RAW_LOG=$($BINARY query tx $EXEC_TX_HASH --output json 2>/dev/null | jq -r '.raw_log // empty')
if [ "$GROUP_PROP_STATUS" == "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "[FAIL] CRITICAL FAILURE: The Commons Council cancelled a gov proposal against itself."
    exit 1
fi
if ! echo "$EXEC_RAW_LOG" | grep -q "own council"; then
    echo "[FAIL] Self-protection refusal missing (status=$GROUP_PROP_STATUS)."
    echo "Raw Log: $EXEC_RAW_LOG"
    exit 1
fi
echo "[ OK ] Self-protection refused: $GROUP_PROP_STATUS"

GOV_STATUS=$($BINARY query gov proposal $GOV_PROP_ID --output json | jq -r '.proposal.status')
if [ "$GOV_STATUS" != "PROPOSAL_STATUS_VOTING_PERIOD" ]; then
    echo "[FAIL] Hostile Proposal should still be voting after the refused cancel (status=$GOV_STATUS)."
    exit 1
fi
echo "[ OK ] Hostile Proposal still in VOTING_PERIOD"

# --- 4. DEFENSE: A SIBLING COUNCIL EXECUTES THE VETO ---
echo "--- DEFENSE ACTIVATED: Technical Council executing Emergency Cancel ---"
TECH_PROP_ID=$(submit_veto_proposal "$TECH_VETO_POLICY_ADDR" alice "EMERGENCY: KILL PROP $GOV_PROP_ID" "$PROPOSAL_DIR/exec_tech_veto.json")
echo "  Technical Veto Proposal ID: $TECH_PROP_ID"

# Alice holds half the Technical Council weight, which clears the 0.49 veto threshold.
echo "Alice voting YES to Veto..."
$BINARY tx commons vote-proposal $TECH_PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3

echo "Attempting Execution (Threshold Met)..."
EXEC_RES=$($BINARY tx commons execute-proposal $TECH_PROP_ID --gas 2000000 --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_TX_HASH=$(echo $EXEC_RES | jq -r '.txhash')

echo "Waiting for execution block (3s)..."
sleep 3

# --- 5. VERIFICATION ---
echo "--- VERIFICATION RESULTS ---"

# 1. Verify Execution Success via proposal status
TECH_PROP_STATUS=$($BINARY query commons get-proposal $TECH_PROP_ID --output json | jq -r '.proposal.status')
if [ "$TECH_PROP_STATUS" == "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "[ OK ] Veto Execution Logic: SUCCESS (status=$TECH_PROP_STATUS)"
else
    echo "[FAIL] CRITICAL FAILURE: The Veto Group Proposal did not execute (status=$TECH_PROP_STATUS)."
    EXEC_TX_JSON=$($BINARY query tx $EXEC_TX_HASH --output json 2>/dev/null)
    echo "Raw Log: $(echo $EXEC_TX_JSON | jq -r '.raw_log // empty' 2>/dev/null)"
    exit 1
fi

# 2. Check Gov Proposal Status
GOV_STATUS_JSON=$($BINARY query gov proposal $GOV_PROP_ID --output json 2>&1)

if echo "$GOV_STATUS_JSON" | grep -q "not found"; then
  echo "[ OK ] SUCCESS: Hostile Proposal was DELETED from state."
else
  STATUS=$(echo $GOV_STATUS_JSON | jq -r '.proposal.status')
  if [ "$STATUS" == "PROPOSAL_STATUS_FAILED" ] || [ "$STATUS" == "PROPOSAL_STATUS_REJECTED" ]; then
     echo "[ OK ] SUCCESS: Hostile Proposal status is $STATUS."
  elif [ "$STATUS" == "PROPOSAL_STATUS_PASSED" ]; then
     echo "[FAIL] CRITICAL FAILURE: The Hostile Proposal PASSED! Defense failed."
     exit 1
  else
     echo "[FAIL] FAILURE: Hostile Proposal is still $STATUS!"
     exit 1
  fi
fi