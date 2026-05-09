#!/bin/bash

echo "--- TESTING: POLICY PERMISSIONS (RATCHET DOWN & GOV OVERRIDE) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)

mkdir -p proposals

# robust Address Lookup
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')

# DISCOVER COUNCIL (Commons Council Standard Policy)
COUNCIL_INFO=$($BINARY query commons get-group "Commons Council" --output json)
COUNCIL_ADDR=$(echo $COUNCIL_INFO | jq -r '.group.policy_address')

echo "Gov Address:     $GOV_ADDR"
echo "Council Address: $COUNCIL_ADDR"

if [ -z "$COUNCIL_ADDR" ] || [ "$COUNCIL_ADDR" == "null" ]; then
    echo "[FAIL] SETUP ERROR: Council Address not found. Run group_setup.sh first."
    exit 1
fi

# Capture the initial allowed_messages so we can guarantee restoration on
# every exit path (success, mid-test failure, ctrl-c). Downstream tests
# (anon_test, social_veto_vote_test, executive_veto_test, etc.) all assume
# Commons Council can submit MsgSpendFromCommons, so leaving the policy in
# a ratcheted-down state cascades failures across the whole suite.
INITIAL_PERMS_JSON=$($BINARY query commons get-policy-permissions $COUNCIL_ADDR --output json 2>/dev/null)
INITIAL_PERMS=$(echo "$INITIAL_PERMS_JSON" | jq -r '.policy_permissions.allowed_messages | join(",")' 2>/dev/null)

restore_council_perms() {
    # Idempotent restore via gov proposal — even if the body of this test
    # exits early, downstream commons tests need the original permission
    # set back. The gov proposal works regardless of council state because
    # gov is the immutable supreme authority for MsgUpdatePolicyPermissions.
    [ -z "$INITIAL_PERMS" ] && return 0
    local now_perms
    now_perms=$($BINARY query commons get-policy-permissions $COUNCIL_ADDR --output json 2>/dev/null \
        | jq -r '.policy_permissions.allowed_messages | join(",")' 2>/dev/null)
    if [ "$now_perms" = "$INITIAL_PERMS" ]; then
        return 0
    fi
    echo "  [trap] restoring Commons Council permissions to baseline..."
    local msgs_json
    msgs_json=$(echo "$INITIAL_PERMS_JSON" | jq -c '.policy_permissions.allowed_messages')
    cat > "$PROPOSAL_DIR/restore_baseline.json" <<RESTORE_JSON
{
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdatePolicyPermissions",
      "authority": "$GOV_ADDR",
      "policy_address": "$COUNCIL_ADDR",
      "allowed_messages": $msgs_json
    }
  ],
  "metadata": "Restore Commons Council permissions to baseline",
  "deposit": "100000000uspark",
  "title": "Restore Council Permissions",
  "summary": "Restore baseline permissions",
  "expedited": true
}
RESTORE_JSON
    local res
    res=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/restore_baseline.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 400000 --output json 2>/dev/null)
    local h
    h=$(echo "$res" | jq -r '.txhash // empty' 2>/dev/null)
    sleep 3
    local pid
    pid=$($BINARY query tx "$h" --output json 2>/dev/null | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' 2>/dev/null | tr -d '"' | head -1)
    [ -z "$pid" ] && return 0
    $BINARY tx gov vote "$pid" yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json >/dev/null 2>&1
    sleep 45
}
trap restore_council_perms EXIT

# --- 1. BASELINE CHECK ---
echo "--- STEP 1: VERIFY INITIAL PERMISSIONS ---"
PERMS_JSON=$($BINARY query commons show-policy-permissions $COUNCIL_ADDR --output json)
echo "Current Permissions:"
echo "$PERMS_JSON" | jq -r '.policy_permissions.allowed_messages[]'

# Check if MsgSpendFromCommons is currently allowed
if echo "$PERMS_JSON" | grep -q "MsgSpendFromCommons"; then
    echo "[ OK ] MsgSpendFromCommons is currently ALLOWED."
else
    echo "[FAIL] SETUP ERROR: MsgSpendFromCommons should be allowed at start."
    exit 1
fi

# --- 2. SELF-REGULATION (RATCHET DOWN) ---
echo "--- STEP 2: COUNCIL VOLUNTARILY REMOVES SPEND PERMISSION ---"

# We create a new list that EXCLUDES Spend but KEEPS UpdatePolicyPermissions
echo '{
  "policy_address": "'$COUNCIL_ADDR'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdatePolicyPermissions",
      "authority": "'$COUNCIL_ADDR'",
      "policy_address": "'$COUNCIL_ADDR'",
      "allowed_messages": [
        "/sparkdream.commons.v1.MsgDeleteGroup",
        "/sparkdream.commons.v1.MsgRegisterGroup",
        "/sparkdream.commons.v1.MsgRenewGroup",
        "/sparkdream.commons.v1.MsgUpdateGroupConfig",
        "/sparkdream.commons.v1.MsgUpdateGroupMembers",
        "/sparkdream.commons.v1.MsgUpdatePolicyPermissions",
        "/sparkdream.commons.v1.MsgVoteProposal",
        "/sparkdream.name.v1.MsgResolveDispute"
      ]
    }
  ],
  "metadata": "Self-restriction: we voluntarily give up the power to spend"
}' > "$PROPOSAL_DIR/msg_ratchet_down.json"

# Submit, Vote, Exec
echo "Submitting Ratchet Down Proposal..."
SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/msg_ratchet_down.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 3
PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

echo "Proposal ID: $PROP_ID"
$BINARY tx commons vote-proposal $PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
$BINARY tx commons vote-proposal $PROP_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test

# Threshold met by 2 yes-votes — early acceptance flips status to ACCEPTED
# immediately. min_execution_period is 1s under testparams. Brief sleep,
# then execute.
sleep 5

echo "Executing Ratchet Down..."
EXEC_RES=$($BINARY tx commons execute-proposal $PROP_ID --gas 2000000 --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash // empty' 2>/dev/null)
sleep 3

# Confirm the proposal actually executed
EXEC_STATUS=$($BINARY query commons get-proposal $PROP_ID --output json | jq -r '.proposal.status')
if [ "$EXEC_STATUS" != "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "[FAIL] Ratchet-down proposal did not execute (status=$EXEC_STATUS)."
    [ -n "$EXEC_HASH" ] && echo "Tx raw_log: $($BINARY query tx $EXEC_HASH --output json 2>/dev/null | jq -r '.raw_log // empty')"
    exit 1
fi

# Verify Removal
NEW_PERMS=$($BINARY query commons get-policy-permissions $COUNCIL_ADDR --output json)
if echo "$NEW_PERMS" | grep -q "MsgSpendFromCommons"; then
    echo "[FAIL] FAILURE: MsgSpendFromCommons is STILL in the list."
    echo "Permissions: $NEW_PERMS"
    exit 1
else
    echo "[ OK ] SUCCESS: MsgSpendFromCommons successfully removed."
fi

# --- 3. ENFORCEMENT CHECK ---
echo "--- STEP 3: VERIFY COUNCIL CANNOT SPEND ---"

echo '{
  "policy_address": "'$COUNCIL_ADDR'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgSpendFromCommons",
      "authority": "'$COUNCIL_ADDR'",
      "recipient": "'$ALICE_ADDR'",
      "amount": [{"denom": "uspark", "amount": "1"}]
    }
  ],
  "metadata": "Illegal spend attempt after removing permission"
}' > "$PROPOSAL_DIR/msg_illegal_spend.json"

# x/commons rejects the disallowed message inside SubmitProposal handler;
# the rejection appears as a non-zero tx code with "not allowed for policy"
# in the on-chain raw_log (not in the broadcast output, which just shows
# the txhash).
ILLEGAL_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/msg_illegal_spend.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json 2>&1)
ILLEGAL_HASH=$(echo "$ILLEGAL_RES" | jq -r '.txhash // empty' 2>/dev/null)
sleep 3
ILLEGAL_LOG=""
if [ -n "$ILLEGAL_HASH" ]; then
    ILLEGAL_LOG=$($BINARY query tx "$ILLEGAL_HASH" --output json 2>/dev/null | jq -r '.raw_log // empty' 2>/dev/null)
fi

if echo "$ILLEGAL_RES$ILLEGAL_LOG" | grep -qiE "MsgSpendFromCommons not allowed for policy|not allowed for policy"; then
    echo "[ OK ] SUCCESS: SubmitProposal rejected the disallowed Spend."
else
    echo "[FAIL] FAILURE: Spend attempt was NOT blocked."
    echo "Broadcast: $ILLEGAL_RES" | head -10
    echo "Tx raw_log: $ILLEGAL_LOG"
    exit 1
fi

# --- 4. UNAUTHORIZED EXPANSION (RATCHET CHECK) ---
echo "--- STEP 4: COUNCIL TRIES TO ADD PERMISSION BACK (SHOULD FAIL) ---"

# Council tries to add MsgSpendFromCommons back
echo '{
  "policy_address": "'$COUNCIL_ADDR'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdatePolicyPermissions",
      "authority": "'$COUNCIL_ADDR'",
      "policy_address": "'$COUNCIL_ADDR'",
      "allowed_messages": [
        "/sparkdream.commons.v1.MsgDeleteGroup",
        "/sparkdream.commons.v1.MsgRegisterGroup",
        "/sparkdream.commons.v1.MsgRenewGroup",
        "/sparkdream.commons.v1.MsgSpendFromCommons",
        "/sparkdream.commons.v1.MsgUpdateGroupConfig",
        "/sparkdream.commons.v1.MsgUpdateGroupMembers",
        "/sparkdream.commons.v1.MsgUpdatePolicyPermissions",
        "/sparkdream.commons.v1.MsgVoteProposal",
        "/sparkdream.name.v1.MsgResolveDispute"
      ]
    }
  ],
  "metadata": "Sneaky expansion: trying to add spend permission back"
}' > "$PROPOSAL_DIR/msg_sneaky_expansion.json"

# 1. Submission: WILL SUCCEED (because UpdatePolicyPermissions is allowed)
SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/msg_sneaky_expansion.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 3
PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

echo "Sneaky Proposal ID: $PROP_ID"
$BINARY tx commons vote-proposal $PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 3
$BINARY tx commons vote-proposal $PROP_ID yes --from bob -y --chain-id $CHAIN_ID --keyring-backend test
# Early acceptance — threshold met by 2 yes-votes, no need to wait the
# voting deadline. min_execution_period=1s under testparams.
sleep 5

# 2. Execution: MUST FAIL with "ratchet down violation". x/commons reverts
# state on inner-msg failure, so the proposal stays ACCEPTED rather than
# flipping to FAILED. We assert the broadcast captures the violation.
echo "Executing Sneaky Expansion..."
EXEC_RES=$($BINARY tx commons execute-proposal $PROP_ID --gas 2000000 --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json 2>&1)
EXEC_HASH=$(echo "$EXEC_RES" | jq -r '.txhash // empty' 2>/dev/null)
sleep 3
EXEC_RAW=""
if [ -n "$EXEC_HASH" ]; then
    EXEC_RAW=$($BINARY query tx "$EXEC_HASH" --output json 2>/dev/null | jq -r '.raw_log // empty' 2>/dev/null)
fi

if echo "$EXEC_RES$EXEC_RAW" | grep -qiE "ratchet down violation"; then
    echo "[ OK ] SUCCESS: Execution failed with 'ratchet down violation'."
else
    SNEAK_STATUS=$($BINARY query commons get-proposal $PROP_ID --output json | jq -r '.proposal.status')
    if [ "$SNEAK_STATUS" == "PROPOSAL_STATUS_ACCEPTED" ] || [ "$SNEAK_STATUS" == "PROPOSAL_STATUS_FAILED" ]; then
        echo "[ OK ] SUCCESS: Sneaky expansion did not execute (status=$SNEAK_STATUS)."
    else
        echo "[FAIL] CRITICAL FAILURE: The Council was able to expand its own permissions! (status=$SNEAK_STATUS)"
        echo "Broadcast: $EXEC_RES" | head -10
        echo "Tx raw_log: $EXEC_RAW"
        exit 1
    fi
fi

# --- 5. SUPREME AUTHORITY RESTORATION ---
echo "--- STEP 5: GOVERNANCE RESTORES THE PERMISSION ---"

# x/gov (Community) proposes to fix the permissions.
echo '{
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdatePolicyPermissions",
      "authority": "'$GOV_ADDR'",
      "policy_address": "'$COUNCIL_ADDR'",
      "allowed_messages": [
        "/sparkdream.commons.v1.MsgDeleteGroup",
        "/sparkdream.commons.v1.MsgRegisterGroup",
        "/sparkdream.commons.v1.MsgRenewGroup",
        "/sparkdream.commons.v1.MsgSpendFromCommons",
        "/sparkdream.commons.v1.MsgUpdateGroupConfig",
        "/sparkdream.commons.v1.MsgUpdateGroupMembers",
        "/sparkdream.commons.v1.MsgUpdatePolicyPermissions",
        "/sparkdream.commons.v1.MsgVoteProposal",
        "/sparkdream.name.v1.MsgResolveDispute"
      ]
    }
  ],
  "metadata": "Restore spend powers via gov",
  "deposit": "100000000uspark",
  "title": "Restore Spend Powers",
  "summary": "Community restores spending power to the council.",
  "expedited": true
}' > "$PROPOSAL_DIR/gov_restore_perms.json"

SUBMIT_RES=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/gov_restore_perms.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 400000 --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 3
GOV_PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

if [ -z "$GOV_PROP_ID" ] || [ "$GOV_PROP_ID" == "null" ]; then
    # Fallback
    GOV_PROP_ID=$(echo $($BINARY query tx $TX_HASH --output json) | jq -r '.logs[0].events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
fi

echo "Gov Proposal ID: $GOV_PROP_ID"

# Vote YES
$BINARY tx gov vote $GOV_PROP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test

echo "Waiting for Expedited Voting (40s)..."
sleep 45

# --- 6. FINAL VERIFICATION ---
echo "--- STEP 6: VERIFY RESTORATION ---"

FINAL_PERMS=$($BINARY query commons show-policy-permissions $COUNCIL_ADDR --output json)

if echo "$FINAL_PERMS" | grep -q "MsgSpendFromCommons"; then
    echo "[ OK ] GRAND SUCCESS: Governance successfully restored the spending permission."
else
    echo "[FAIL] FAILURE: Permission was not restored."
    exit 1
fi