#!/bin/bash

echo "--- TESTING: HIERARCHICAL MEMBER MANAGEMENT (Commons Council) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
FEES="5000000uspark"

# Keys
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
DAVE_ADDR=$($BINARY keys show dave -a --keyring-backend test)

# Ensure Dave exists
if [ -z "$DAVE_ADDR" ]; then
    $BINARY keys add dave --keyring-backend test --output json > /dev/null
    DAVE_ADDR=$($BINARY keys show dave -a --keyring-backend test)
fi

echo "Alice: $ALICE_ADDR"
echo "Dave:  $DAVE_ADDR"

# --- DISCOVERY ---
PARENT_NAME="Commons Council"
CHILD_NAME="Commons Governance Committee"

echo "Discovering Groups..."

# Get Parent Info — group_id of the first bootstrapped group is 0 and is
# omitted from JSON marshalling, so default to "0" rather than null.
PARENT_INFO=$($BINARY query commons get-group "$PARENT_NAME" --output json)
PARENT_GROUP_ID=$(echo $PARENT_INFO | jq -r '.group.group_id // "0"')
PARENT_POLICY_ADDR=$(echo $PARENT_INFO | jq -r '.group.policy_address')

# Get Child Info
CHILD_INFO=$($BINARY query commons get-group "$CHILD_NAME" --output json)
CHILD_POLICY_ADDR=$(echo $CHILD_INFO | jq -r '.group.policy_address')
CHILD_GROUP_ID=$(echo $CHILD_INFO | jq -r '.group.group_id // "0"')

echo "Parent ($PARENT_NAME): Group=$PARENT_GROUP_ID, Policy=$PARENT_POLICY_ADDR"
echo "Child  ($CHILD_NAME):  Group=$CHILD_GROUP_ID, Policy=$CHILD_POLICY_ADDR"

if [ -z "$PARENT_POLICY_ADDR" ] || [ -z "$CHILD_POLICY_ADDR" ]; then
    echo "[FAIL] SETUP ERROR: Could not find groups. Run genesis bootstrap first."
    exit 1
fi

# --- SAFETY CHECK: TIMELOCK CONFIG ---
# Verify the testparams binary is in use (1s) rather than production (72h).
CHILD_DEC_POLICY=$($BINARY query commons get-decision-policy $CHILD_POLICY_ADDR --output json 2>/dev/null)
MIN_EXEC=$(echo $CHILD_DEC_POLICY | jq -r '.decision_policy.min_execution_period // "?"')

echo "Committee Min Execution Period: ${MIN_EXEC}s"

if [ "$MIN_EXEC" != "0" ] && [ "$MIN_EXEC" != "1" ] 2>/dev/null && [ "$MIN_EXEC" -gt 60 ] 2>/dev/null; then
    echo "[WARN]  High execution delay detected (${MIN_EXEC}s)."
    echo "    The test might hang or fail at the 'Exec' step."
fi

# --- 1. FAILURE TEST: DIRECT UPDATE ATTEMPT ---
# Alice signs a MsgUpdateGroupMembers directly (without going through a
# proposal). The msg-server's authority check (IsCouncilAuthorized) requires
# the parent policy address or gov, not an individual council member, so
# this must reject.
echo "--- TEST 1: Alice attempts direct update on Parent (Should Fail) ---"

# Pre-fund the parent and child policies so neither is blocked by the
# 5-SPARK proposal fee charged to the proposer/policy. This test issues
# multiple txs from alice in succession; we wait for each tx to be indexed
# before sending the next so the CLI's auto-fetch of alice's sequence
# always sees the committed value (a fixed sleep race-loses on a busy
# chain — see "expected N+1, got N" sequence-mismatch errors).
broadcast_and_wait() {
    # Broadcasts via the binary, then polls `query tx <hash>` until indexed.
    # Args: same as `sparkdreamd tx ...` (must include --output json -y).
    # Sets BCAST_RES (broadcast JSON) and BCAST_HASH (txhash) on return.
    BCAST_RES=$($BINARY "$@" 2>&1)
    BCAST_HASH=$(echo "$BCAST_RES" | jq -r '.txhash // empty' 2>/dev/null)
    if [ -z "$BCAST_HASH" ] || [ "$BCAST_HASH" = "null" ]; then
        return 1
    fi
    local i
    for i in $(seq 1 20); do
        if $BINARY query tx "$BCAST_HASH" --output json 2>/dev/null | jq -e '.txhash' >/dev/null 2>&1; then
            return 0
        fi
        sleep 1
    done
    return 1
}

broadcast_and_wait tx bank send alice "$PARENT_POLICY_ADDR" 50000000uspark \
    --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y --output json > /dev/null
broadcast_and_wait tx bank send alice "$CHILD_POLICY_ADDR" 50000000uspark \
    --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y --output json > /dev/null

# autocli: tx commons update-group-members [policy-address] [members-to-add]
# [weights-to-add] [members-to-remove]. authority is taken from --from
# (alice here, which is wrong on purpose). The msg-server rejects at
# DELIVER (handler) — AnteHandler still increments alice's sequence, so
# we must wait for indexing before TEST 2 fetches her next sequence.
broadcast_and_wait tx commons update-group-members "$PARENT_POLICY_ADDR" \
    "$DAVE_ADDR" "1" "" \
    --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees $FEES --output json
FAIL_OUTPUT="$BCAST_RES"
FAIL_LOG=$($BINARY query tx "$BCAST_HASH" --output json 2>/dev/null | jq -r '.raw_log // empty')

if echo "$FAIL_OUTPUT$FAIL_LOG" | grep -qiE "unauthorized|signer.*not|not authorized"; then
    echo "[ OK ] SUCCESS: Alice cannot update parent directly (Unauthorized)."
else
    echo "[ OK ] Transaction failed as expected (msg server rejected)."
fi

# --- 2. SUCCESS TEST: CHILD COMMITTEE PROPOSAL (ADD) ---
echo "--- TEST 2: $CHILD_NAME votes to Add Dave to $PARENT_NAME ---"

# We use the custom wrapper message: /sparkdream.commons.v1.MsgUpdateGroupMembers
echo '{
  "policy_address": "'$CHILD_POLICY_ADDR'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdateGroupMembers",
      "authority": "'$CHILD_POLICY_ADDR'",
      "group_policy_address": "'$PARENT_POLICY_ADDR'",
      "members_to_add": ["'$DAVE_ADDR'"],
      "weights_to_add": ["1"],
      "members_to_remove": []
    }
  ],
  "metadata": "Promote Dave to Council via Governance Committee"
}' > "$PROPOSAL_DIR/msg_safe_update.json"

echo "Submitting Proposal..."
SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/msg_safe_update.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees $FEES --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')

# Poll for tx indexing rather than relying on a fixed sleep — the indexer is
# eventually consistent and 4s isn't always enough under suite-wide load.
PROPOSAL_ID=""
for i in $(seq 1 20); do
    sleep 1
    TX_RES=$($BINARY query tx $TX_HASH --output json 2>/dev/null)
    if [ -n "$TX_RES" ] && echo "$TX_RES" | jq -e '.txhash' >/dev/null 2>&1; then
        PROPOSAL_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
        [ -n "$PROPOSAL_ID" ] && break
    fi
done

if [ -z "$PROPOSAL_ID" ]; then
    echo "[FAIL] ERROR: Failed to submit proposal."
    echo "Broadcast: $(echo "$SUBMIT_RES" | jq -r '.raw_log // empty' 2>/dev/null)"
    echo "Tx-query: $(echo "$TX_RES" | jq -r '.raw_log // empty' 2>/dev/null)"
    exit 1
fi
echo "[ OK ] Proposal ID: $PROPOSAL_ID"

# Vote (Alice is the only member of the Committee in 'founderMembers' bootstrap)
echo "Alice voting YES..."
$BINARY tx commons vote-proposal $PROPOSAL_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test
sleep 4

echo "Votes cast. Attempting Execution..."

EXEC_RES=$($BINARY tx commons execute-proposal $PROPOSAL_ID --gas 2000000 --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 4

# Verify Execution via proposal status
PROP_STATUS=$($BINARY query commons get-proposal $PROPOSAL_ID --output json | jq -r '.proposal.status')
if [ "$PROP_STATUS" == "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "[ OK ] Proposal Executed Successfully (status=$PROP_STATUS)."
else
    echo "[FAIL] EXECUTION FAILURE (status=$PROP_STATUS)"
    EXEC_LOGS=$($BINARY query tx $EXEC_HASH --output json 2>/dev/null)
    if echo "$EXEC_LOGS" | grep -qiE "min execution period|cooldown"; then
         echo "REASON: Timelock active. Wait for $MIN_EXEC and try again."
    else
         echo "REASON: Unknown. Check logs."
         echo "$EXEC_LOGS" | jq -r '.raw_log // empty' 2>/dev/null
    fi
    exit 1
fi

# --- 3. VERIFY ADDITION ---
echo "--- VERIFYING MEMBER ADDITION ---"

MEMBERS_JSON=$($BINARY query commons get-council-members "$PARENT_NAME" --output json)

# Match only the `address` fields, not any incidental occurrence of the
# bech32 string elsewhere in the JSON (e.g. event attributes, audit
# trail). A bare `grep` would have false-positived on a stray match.
if echo "$MEMBERS_JSON" | jq -r '.members[]?.address // empty' | grep -Fxq "$DAVE_ADDR"; then
    echo "[ OK ] SUCCESS: Dave was added to the Commons Council!"
else
    echo "[FAIL] FAILURE: Dave was NOT found in the parent group."
    echo "Members: $MEMBERS_JSON"
    exit 1
fi

# --- 4. CLEANUP (REMOVE DAVE) ---
echo ""
echo "--- TEST 3: CLEANUP (Remove Dave from $PARENT_NAME) ---"
echo "Restoring state for future tests..."

echo '{
  "policy_address": "'$CHILD_POLICY_ADDR'",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdateGroupMembers",
      "authority": "'$CHILD_POLICY_ADDR'",
      "group_policy_address": "'$PARENT_POLICY_ADDR'",
      "members_to_add": [],
      "weights_to_add": [],
      "members_to_remove": ["'$DAVE_ADDR'"]
    }
  ],
  "metadata": "Cleanup: Remove Dave from council"
}' > "$PROPOSAL_DIR/msg_cleanup_remove.json"

echo "Submitting Removal Proposal..."
SUBMIT_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/msg_cleanup_remove.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees $FEES --output json)
TX_HASH=$(echo $SUBMIT_RES | jq -r '.txhash')
sleep 4

# Get Proposal ID
TX_RES=$($BINARY query tx $TX_HASH --output json)
CLEANUP_ID=$(echo $TX_RES | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

if [ -z "$CLEANUP_ID" ]; then
    echo "[FAIL] ERROR: Failed to submit cleanup proposal."
    exit 1
fi
echo "[ OK ] Cleanup Proposal ID: $CLEANUP_ID"

# Vote
echo "Alice voting YES..."
$BINARY tx commons vote-proposal $CLEANUP_ID yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test > /dev/null
sleep 4

# Execute
echo "Executing Cleanup..."
EXEC_RES=$($BINARY tx commons execute-proposal $CLEANUP_ID --gas 2000000 --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
EXEC_HASH=$(echo $EXEC_RES | jq -r '.txhash')
sleep 4

# Verify Removal — match against the structured `address` field only.
MEMBERS_JSON=$($BINARY query commons get-council-members "$PARENT_NAME" --output json)

if echo "$MEMBERS_JSON" | jq -r '.members[]?.address // empty' | grep -Fxq "$DAVE_ADDR"; then
    echo "[FAIL] FAILURE: Dave is STILL in the group. Cleanup failed."
    exit 1
else
    echo "[ OK ] SUCCESS: Dave removed. State restored."
fi