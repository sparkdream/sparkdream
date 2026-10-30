#!/bin/bash

echo "--- TESTING: COMMONS SECRET BALLOTS ---"
echo ""
echo "A PROPOSER_CHOICE group (alice, bob) votes a secret-ballot proposal and a"
echo "secret poll through x/shield (PROOF_DOMAIN_GROUP_TREE = 2), and refuses"
echo "open votes on them. The test build skips proof and root verification (no"
echo "verifying key), so the root check is covered by x/shield's"
echo "TestGroupTreeProofs; this script covers the x/commons side end to end."
echo ""

source "$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/_ballot_helpers.sh"

SHIELD_MODULE_ADDR=$($BINARY query auth module-account shield --output json 2>/dev/null | jq -r '.account.value.address // .account.base_account.address // empty')
if [ -z "$SHIELD_MODULE_ADDR" ]; then
    echo "ERROR: Could not resolve shield module address"
    exit 1
fi

# The shield module pays the gas of shielded execs; a fresh chain's
# community pool is empty, so fund it directly.
SHIELD_BAL=$($BINARY query bank balances "$SHIELD_MODULE_ADDR" --output json 2>/dev/null | jq -r --arg d "$BOND_DENOM" '[.balances[] | select(.denom==$d) | .amount][0] // "0"')
if [ "${SHIELD_BAL:-0}" -lt 10000000 ] 2>/dev/null; then
    tx alice bank send "$ALICE_ADDR" "$SHIELD_MODULE_ADDR" 50000000${BOND_DENOM}
fi

DUMMY_PROOF=$(python3 -c "print('aa' * 128)")
DUMMY_MERKLE_ROOT="0000000000000000000000000000000000000000000000000000000000000001"

# shielded_exec INNER_JSON: an immediate-mode group-tree exec with dummy
# proof values (accepted in the test build).
shielded_exec() {
    local RES=$($BINARY tx shield shielded-exec \
        --inner-message "$1" \
        --proof "$DUMMY_PROOF" \
        --nullifier 00$(openssl rand -hex 31) \
        --rate-limit-nullifier 00$(openssl rand -hex 31) \
        --merkle-root "$DUMMY_MERKLE_ROOT" \
        --proof-domain 2 \
        --min-trust-level 0 \
        --exec-mode 0 \
        --from alice --chain-id $CHAIN_ID --keyring-backend test \
        --fees 25000${BOND_DENOM} --gas 500000 -y --output json 2>&1)
    submit_tx_and_wait "$RES"
}

# ---------------------------------------------------------------------------
# SETUP: ZK keys for alice and bob, and a PROPOSER_CHOICE group of the two
# ---------------------------------------------------------------------------
ensure_zk_key() {
    local ACCOUNT=$1 ADDR=$2 KEY_HEX=$3
    local HAS=$($BINARY query rep get-member "$ADDR" --output json 2>/dev/null | jq -r '.member.zk_public_key // ""')
    if [ -n "$HAS" ]; then
        echo "  $ACCOUNT already has a ZK key"
        return
    fi
    tx "$ACCOUNT" rep register-zk-public-key "$(echo "$KEY_HEX" | xxd -r -p | base64)"
    if check_tx_success "$TX_RESULT"; then
        echo "  Registered a ZK key for $ACCOUNT"
    else
        echo "  WARNING: ZK key registration for $ACCOUNT failed: $(raw_log | head -c 200)"
    fi
}
echo "--- SETUP ---"
ensure_zk_key alice "$ALICE_ADDR" "0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b"
ensure_zk_key bob "$BOB_ADDR" "0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c"

COMMONS_POLICY=$($BINARY query commons get-group "Commons Council" --output json | jq -r '.group.policy_address')
GROUP_NAME="Ballot Room $(date +%s)"
cat > "$PROPOSAL_DIR/create_ballot_room.json" <<EOF
{
  "policy_address": "$COMMONS_POLICY",
  "metadata": "Secret ballot test group",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgRegisterGroup",
      "authority": "$COMMONS_POLICY",
      "name": "$GROUP_NAME",
      "description": "Secret ballot test group",
      "members": ["$ALICE_ADDR", "$BOB_ADDR"],
      "member_weights": ["1", "1"],
      "min_members": 2,
      "max_members": 3,
      "term_duration": 3600,
      "voting_period": 600,
      "min_execution_period": 0,
      "max_spend_per_epoch": "1000",
      "update_cooldown": 0,
      "funding_weight": 0,
      "futarchy_enabled": false,
      "vote_threshold": "1",
      "policy_type": "threshold",
      "allowed_messages": ["/sparkdream.commons.v1.MsgSpendFromCommons"],
      "ballot_mode": "BALLOT_MODE_PROPOSER_CHOICE"
    }
  ]
}
EOF
tx alice commons submit-proposal "$PROPOSAL_DIR/create_ballot_room.json"
SETUP_PID=$(extract_event_value "$TX_RESULT" "submit_proposal" "proposal_id")
tx alice commons vote-proposal "$SETUP_PID" yes
tx bob commons vote-proposal "$SETUP_PID" yes
tx alice commons execute-proposal "$SETUP_PID"
check_tx_success "$TX_RESULT" || echo "  ERROR: group setup proposal $SETUP_PID did not execute: $(raw_log | head -c 300)"
GROUP_INFO=$($BINARY query commons get-group "$GROUP_NAME" --output json 2>/dev/null)
GROUP_POLICY=$(echo "$GROUP_INFO" | jq -r '.group.policy_address // empty')

echo "--- TEST 1: The group is registered with its ballot mode ---"
MODE=$(echo "$GROUP_INFO" | jq -r '.group.ballot_mode // empty')
echo "  group=$GROUP_NAME policy=$GROUP_POLICY ballot_mode=$MODE"
if [ -n "$GROUP_POLICY" ] && [ "$MODE" == "BALLOT_MODE_PROPOSER_CHOICE" ]; then
    record_result "Group registered with PROPOSER_CHOICE" "PASS"
else
    record_result "Group registered with PROPOSER_CHOICE" "FAIL"
    print_summary "COMMONS SECRET BALLOT TEST"
fi

# ---------------------------------------------------------------------------
echo "--- TEST 2: A secret-ballot proposal snapshots the voters ---"
cat > "$PROPOSAL_DIR/secret_signal.json" <<EOF
{
  "policy_address": "$GROUP_POLICY",
  "messages": [],
  "metadata": "Should the ballot room meet weekly?",
  "secret_ballot": true
}
EOF
tx alice commons submit-proposal "$PROPOSAL_DIR/secret_signal.json"
SECRET_PID=$(extract_event_value "$TX_RESULT" "submit_proposal" "proposal_id")
PROP=$($BINARY query commons get-proposal "$SECRET_PID" --output json 2>/dev/null)
IS_SECRET=$(echo "$PROP" | jq -r '.proposal.secret_ballot // false')
LEAVES=$($BINARY query commons get-proposal-voter-tree "$SECRET_PID" --output json 2>/dev/null | jq -r '.tree.leaves | length')
echo "  proposal=$SECRET_PID secret_ballot=$IS_SECRET voter_tree_leaves=$LEAVES"
if [ "$IS_SECRET" == "true" ] && [ "$LEAVES" == "2" ]; then
    record_result "Secret ballot snapshots its voters" "PASS"
else
    echo "  $(raw_log | head -c 200)"
    record_result "Secret ballot snapshots its voters" "FAIL"
fi

# ---------------------------------------------------------------------------
echo "--- TEST 3: An open vote on a secret ballot is refused ---"
tx bob commons vote-proposal "$SECRET_PID" yes
expect_refusal "Open vote on secret ballot refused" "ballot mode does not allow this vote"

# ---------------------------------------------------------------------------
echo "--- TEST 4: An anonymous vote counts and passes the threshold ---"
shielded_exec "{\"@type\":\"/sparkdream.commons.v1.MsgAnonymousVoteProposal\",\"voter\":\"$SHIELD_MODULE_ADDR\",\"proposal_id\":\"$SECRET_PID\",\"option\":1}"
PROP=$($BINARY query commons get-proposal "$SECRET_PID" --output json 2>/dev/null)
YES=$(echo "$PROP" | jq -r '.anon_tally.yes_count // "0"')
STATUS=$(echo "$PROP" | jq -r '.proposal.status')
echo "  yes=$YES status=$STATUS"
if check_tx_success "$TX_RESULT" && [ "$YES" == "1" ] && [ "$STATUS" == "PROPOSAL_STATUS_ACCEPTED" ]; then
    record_result "Anonymous vote counted, proposal accepted" "PASS"
else
    echo "  $(raw_log | head -c 200)"
    record_result "Anonymous vote counted, proposal accepted" "FAIL"
fi
TREE_GONE=$($BINARY query commons get-proposal-voter-tree "$SECRET_PID" --output json 2>&1 | grep -c "no open secret ballot")
if [ "$TREE_GONE" -ge 1 ]; then
    record_result "Voter snapshot dropped once voting is over" "PASS"
else
    record_result "Voter snapshot dropped once voting is over" "FAIL"
fi

# ---------------------------------------------------------------------------
echo "--- TEST 5: Without secret_ballot the proposer's choice is open ---"
cat > "$PROPOSAL_DIR/open_signal.json" <<EOF
{
  "policy_address": "$GROUP_POLICY",
  "messages": [],
  "metadata": "Open question"
}
EOF
tx alice commons submit-proposal "$PROPOSAL_DIR/open_signal.json"
OPEN_PID=$(extract_event_value "$TX_RESULT" "submit_proposal" "proposal_id")
IS_SECRET=$($BINARY query commons get-proposal "$OPEN_PID" --output json 2>/dev/null | jq -r '.proposal.secret_ballot // false')
tx bob commons vote-proposal "$OPEN_PID" yes
if [ "$IS_SECRET" == "false" ] && check_tx_success "$TX_RESULT"; then
    record_result "Open proposal in a PROPOSER_CHOICE group" "PASS"
else
    record_result "Open proposal in a PROPOSER_CHOICE group" "FAIL"
fi

# ---------------------------------------------------------------------------
echo "--- TEST 6: A secret poll takes anonymous votes only ---"
tx alice commons open-poll "$GROUP_NAME" 600 "Lunch?" "pizza" "salad" --secret-ballot
POLL_ID=$(extract_event_value "$TX_RESULT" "poll_opened" "poll_id")
tx bob commons vote-poll "$POLL_ID" 1
expect_refusal "Open vote on secret poll refused" "ballot mode does not allow this vote"
shielded_exec "{\"@type\":\"/sparkdream.commons.v1.MsgAnonymousVotePoll\",\"voter\":\"$SHIELD_MODULE_ADDR\",\"poll_id\":\"$POLL_ID\",\"option\":\"1\"}"
TALLIES=$($BINARY query commons get-poll "$POLL_ID" --output json 2>/dev/null | jq -c '[.poll.tallies[] | tonumber]')
echo "  poll=$POLL_ID tallies=$TALLIES"
if check_tx_success "$TX_RESULT" && [ "$TALLIES" == "[0,1]" ]; then
    record_result "Anonymous poll vote counted" "PASS"
else
    echo "  $(raw_log | head -c 200)"
    record_result "Anonymous poll vote counted" "FAIL"
fi

print_summary "COMMONS SECRET BALLOT TEST"
