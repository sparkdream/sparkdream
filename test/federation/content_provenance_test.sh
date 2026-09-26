#!/bin/bash

echo "--- TESTING: FEDERATION CONTENT PROVENANCE (content_hosts, creator host, supersedes, SUPERSEDED) ---"

# ========================================================================
# Covers MsgSubmitFederatedContent's inbound provenance and edit rules on an
# ActivityPub peer:
#   - content_uri must be on the peer's host or a policy content_host (2382)
#   - creator_identity (@user@host) likewise (2384)
#   - supersedes retires the same operator's pending record of the same
#     content_uri to SUPERSEDED, once (2383 on a second attempt)
#   - SUPERSEDED is terminal for MsgModerateContent (2354)
#
# Self-contained: registers and activates its own peer, policy and bridge
# through the idempotent helpers in peer_fixtures.sh.
# ========================================================================
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "ERROR: .test_env not found."; exit 1
fi
source "$SCRIPT_DIR/.test_env"
source "$SCRIPT_DIR/peer_fixtures.sh"

PEER="provenance.example"
ALIAS_HOST="social.provenance.example"
OPERATOR_KEY="operator2"
OPERATOR_ADDR="$OPERATOR2_ADDR"

PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()

record_result() {
    local NAME=$1; local RESULT=$2
    TEST_NAMES+=("$NAME"); RESULTS+=("$RESULT")
    if [ "$RESULT" == "PASS" ]; then PASS_COUNT=$((PASS_COUNT + 1)); else FAIL_COUNT=$((FAIL_COUNT + 1)); fi
    echo "  => $RESULT"
}

wait_for_tx() {
    local TXHASH=$1; local MAX=20; local A=0
    while [ $A -lt $MAX ]; do
        RESULT=$($BINARY q tx $TXHASH --output json 2>/dev/null)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then echo "$RESULT"; return 0; fi
        A=$((A + 1)); sleep 1
    done
    echo "ERROR: tx $TXHASH not found" >&2; return 1
}

# run_tx <tx json>: waits for the broadcast and sets TX_CODE (the DeliverTx
# code, or the CheckTx code if it never got that far) and TX_RESULT (the
# included tx). Call it directly, never inside $(...): a subshell would
# drop both variables.
run_tx() {
    local TX_RES=$1
    TX_RESULT="$TX_RES"
    local TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')
    if [ -z "$TXHASH" ]; then TX_CODE="no-txhash"; return; fi
    TX_CODE=$(echo "$TX_RES" | jq -r '.code // "0"')
    [ "$TX_CODE" != "0" ] && return
    TX_RESULT=$(wait_for_tx "$TXHASH") || { TX_CODE="not-included"; return; }
    TX_CODE=$(echo "$TX_RESULT" | jq -r '.code // "0"')
}

content_id_of() {
    echo "$1" | jq -r '[.events[] | select(.type=="federated_content_received").attributes[] | select(.key=="content_id").value][0] // empty'
}

sha256_base64() {
    echo -n "$1" | sha256sum | awk '{print $1}' | xxd -r -p | base64 -w0
}

# submit <content_uri> <creator_identity> <body seed> [supersedes id]
submit() {
    local URI=$1 CREATOR=$2 SEED=$3 SUPERSEDES=${4:-}
    local EXTRA=()
    [ -n "$SUPERSEDES" ] && EXTRA=(--supersedes "{\"content_id\":\"$SUPERSEDES\"}")
    $BINARY tx federation submit-federated-content \
        "$PEER" "prov-$SEED" "blog_post" "$CREATOR" "Provenance" "" \
        "provenance body $SEED" "$URI" "$(date +%s)" \
        --content-hash "$(sha256_base64 "provenance $SEED $RANDOM")" \
        "${EXTRA[@]}" \
        --from $OPERATOR_KEY --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json
}

content_field() {
    $BINARY query federation get-federated-content "$1" --output json 2>/dev/null | jq -r "$2"
}

# ========================================================================
# Setup: peer, policy (with content_hosts), bridge
# ========================================================================
echo ""
echo "--- SETUP ---"
register_test_peer "$PEER" "PEER_TYPE_ACTIVITYPUB" "Provenance test peer" "" || { echo "ERROR: peer setup failed"; exit 1; }

# The policy is stored whole: write every field, content_hosts included.
POLICY_FILE="$PROPOSAL_DIR/provenance_policy.json"
cat > "$POLICY_FILE" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [{
    "@type": "/sparkdream.federation.v1.MsgUpdatePeerPolicy",
    "authority": "$OPS_POLICY",
    "peer_id": "$PEER",
    "policy": {
      "peer_id": "$PEER",
      "outbound_content_types": [],
      "inbound_content_types": ["blog_post", "blog_reply"],
      "min_outbound_trust_level": 0,
      "inbound_rate_limit_per_epoch": 100,
      "outbound_rate_limit_per_epoch": 100,
      "allow_reputation_queries": false,
      "accept_reputation_attestations": false,
      "require_review": false,
      "blocked_identities": [],
      "allowed_identities": ["*"],
      "content_hosts": ["$ALIAS_HOST"]
    }
  }],
  "metadata": "Provenance test: policy with content_hosts"
}
EOF
TX_RES=$($BINARY tx commons submit-proposal "$POLICY_FILE" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000${BOND_DENOM} --output json)
run_tx "$TX_RES"; CODE=$TX_CODE
PROP_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"')
for VOTER in alice bob; do
    $BINARY tx commons vote-proposal "$PROP_ID" yes --from $VOTER -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000${BOND_DENOM} --output json > /dev/null 2>&1
    sleep 2
done
TX_RES=$($BINARY tx commons execute-proposal "$PROP_ID" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000${BOND_DENOM} --gas 2000000 --output json)
run_tx "$TX_RES"

register_test_bridge "$OPERATOR_KEY" "$OPERATOR_ADDR" "$PEER" "activitypub" "https://bridge.$PEER" || { echo "ERROR: bridge setup failed"; exit 1; }

# ========================================================================
# TEST 1: policy content_hosts applied
# ========================================================================
echo ""
echo "--- TEST 1: MsgUpdatePeerPolicy stores content_hosts ---"
HOSTS=$($BINARY query federation get-peer-policy "$PEER" --output json | jq -c '.policy.content_hosts // []')
echo "  content_hosts: $HOSTS"
if [ "$HOSTS" == "[\"$ALIAS_HOST\"]" ]; then record_result "Policy content_hosts stored" "PASS"; else record_result "Policy content_hosts stored" "FAIL"; fi

# ========================================================================
# TEST 2: content_uri on another instance's host is refused (2382)
# ========================================================================
echo ""
echo "--- TEST 2: content_uri on a foreign host rejected ---"
run_tx "$(submit "https://aurora.example/users/a/statuses/1" "@a@$PEER" foreign-uri)"; CODE=$TX_CODE
echo "  code=$CODE"
if [ "$CODE" == "2382" ]; then record_result "Foreign content_uri rejected (2382)" "PASS"; else record_result "Foreign content_uri rejected (2382)" "FAIL"; fi

# ========================================================================
# TEST 3: creator_identity on another instance's host is refused (2384)
# ========================================================================
echo ""
echo "--- TEST 3: creator_identity on a foreign host rejected ---"
run_tx "$(submit "https://$PEER/users/a/statuses/2" "@zed@aurora.example" foreign-creator)"; CODE=$TX_CODE
echo "  code=$CODE"
if [ "$CODE" == "2384" ]; then record_result "Foreign creator_identity rejected (2384)" "PASS"; else record_result "Foreign creator_identity rejected (2384)" "FAIL"; fi

# ========================================================================
# TEST 4: a policy content_host is accepted for both
# ========================================================================
echo ""
echo "--- TEST 4: content_hosts entry accepted ---"
run_tx "$(submit "https://$ALIAS_HOST/users/a/statuses/3" "@a@$ALIAS_HOST" alias-host)"; CODE=$TX_CODE
echo "  code=$CODE"
if [ "$CODE" == "0" ]; then record_result "content_hosts host accepted" "PASS"; else record_result "content_hosts host accepted" "FAIL"; fi

# ========================================================================
# TEST 5: supersedes retires the pending predecessor
# ========================================================================
echo ""
echo "--- TEST 5: supersedes retires a pending predecessor to SUPERSEDED ---"
EDIT_URI="https://$PEER/users/a/statuses/$RANDOM$RANDOM"
run_tx "$(submit "$EDIT_URI" "@a@$PEER" v1)"; CODE=$TX_CODE
V1=$(content_id_of "$TX_RESULT")
run_tx "$(submit "$EDIT_URI" "@a@$PEER" v2 "$V1")"; CODE2=$TX_CODE
V2=$(content_id_of "$TX_RESULT")
V1_STATUS=$(content_field "$V1" '.content.status // "FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION"')
V1_BY=$(content_field "$V1" '.content.superseded_by // "0"')
V2_REF=$(content_field "$V2" '.content.supersedes.content_id // "0"')
echo "  v1=$V1 (code $CODE) status=$V1_STATUS superseded_by=$V1_BY"
echo "  v2=$V2 (code $CODE2) supersedes=$V2_REF"
if [ "$CODE" == "0" ] && [ "$CODE2" == "0" ] && [ -n "$V1" ] && [ -n "$V2" ] && \
   [ "$V1_STATUS" == "FEDERATED_CONTENT_STATUS_SUPERSEDED" ] && [ "$V1_BY" == "$V2" ] && [ "$V2_REF" == "$V1" ]; then
    record_result "Supersede retires predecessor" "PASS"
else
    record_result "Supersede retires predecessor" "FAIL"
fi
EVENT=$(echo "$TX_RESULT" | jq -r '[.events[] | select(.type=="content_superseded")] | length')
if [ "$EVENT" == "1" ]; then record_result "content_superseded emitted" "PASS"; else record_result "content_superseded emitted" "FAIL"; fi

# ========================================================================
# TEST 6: a record is superseded once (2383)
# ========================================================================
echo ""
echo "--- TEST 6: superseding an already-superseded record rejected ---"
run_tx "$(submit "$EDIT_URI" "@a@$PEER" v2-again "$V1")"; CODE=$TX_CODE
echo "  code=$CODE"
if [ "$CODE" == "2383" ]; then record_result "Second supersede rejected (2383)" "PASS"; else record_result "Second supersede rejected (2383)" "FAIL"; fi

# ========================================================================
# TEST 7: SUPERSEDED is terminal for moderation (2354)
# ========================================================================
echo ""
echo "--- TEST 7: MsgModerateContent refuses SUPERSEDED ---"
TX_RES=$($BINARY tx federation moderate-content "$V1" "e2e: moderation must not revive a superseded record" \
    --new-status verified --from alice --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000${BOND_DENOM} -y --output json)
run_tx "$TX_RES"; CODE=$TX_CODE
echo "  code=$CODE"
if [ "$CODE" == "2354" ]; then record_result "Moderating SUPERSEDED rejected (2354)" "PASS"; else record_result "Moderating SUPERSEDED rejected (2354)" "FAIL"; fi

# ========================================================================
# Summary
# ========================================================================
echo ""
echo "============================================"
echo "CONTENT PROVENANCE TEST RESULTS"
echo "============================================"
for i in "${!TEST_NAMES[@]}"; do
    printf "  %-45s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done
echo ""
echo "  Passed: $PASS_COUNT / $((PASS_COUNT + FAIL_COUNT))"

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL TESTS PASSED <<<"
    exit 0
fi
