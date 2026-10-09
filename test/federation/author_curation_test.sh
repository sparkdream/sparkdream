#!/bin/bash

echo "--- TESTING: FEDERATION AUTHOR CURATION (allowed_identities + curated collection) ---"

# ========================================================================
# Covers PeerPolicy's two author gates on bridged content
# (MsgSubmitFederatedContent). An author is anchored only when both pass:
#   - allowed_identities: "*" = any author, empty = nobody (2385)
#   - curation: an x/collect collection's active link items (2386)
# and the curation list being owned by the Operations Committee (its policy
# address owns collections as a member does) with a member collaborating.
#
# Self-contained: its own peer, bridge and collection, through the
# idempotent helpers in peer_fixtures.sh.
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

PEER="curation.example"
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
    local TXHASH=$1; local MAX=30; local A=0
    while [ $A -lt $MAX ]; do
        RESULT=$($BINARY q tx $TXHASH --output json 2>/dev/null)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then echo "$RESULT"; return 0; fi
        A=$((A + 1)); sleep 1
    done
    echo "ERROR: tx $TXHASH not found" >&2; return 1
}

# run_tx <tx json>: sets TX_CODE (DeliverTx, or CheckTx if it never got
# further) and TX_RESULT. Call directly, never inside $(...).
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

# committee <file> <label>: an Operations Committee proposal, voted and
# executed. Sets PROP_STATUS.
committee() {
    local FILE=$1
    run_tx "$($BINARY tx commons submit-proposal "$FILE" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 1000000 --fees 5000000${BOND_DENOM} --output json)"
    local ID=$(echo "$TX_RESULT" | jq -r '[.events[]? | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value][0] // empty' | tr -d '"')
    if [ -z "$ID" ]; then PROP_STATUS="not-submitted (code $TX_CODE)"; return; fi
    for VOTER in alice bob; do
        run_tx "$($BINARY tx commons vote-proposal "$ID" yes --from $VOTER -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000${BOND_DENOM} --output json 2>/dev/null)"
    done
    run_tx "$($BINARY tx commons execute-proposal "$ID" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000${BOND_DENOM} --gas 2000000 --output json)"
    PROP_STATUS=$($BINARY query commons get-proposal "$ID" --output json 2>/dev/null | jq -r '.proposal.status // "?"')
}

# set_policy <allowed_identities json> <curation json or null>
set_policy() {
    local FILE="$PROPOSAL_DIR/curation_policy_$RANDOM.json"
    cat > "$FILE" <<JSON
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
      "inbound_rate_limit_per_epoch": 1000,
      "outbound_rate_limit_per_epoch": 0,
      "allow_reputation_queries": false,
      "accept_reputation_attestations": false,
      "require_review": false,
      "blocked_identities": [],
      "allowed_identities": $1,
      "curation": $2
    }
  }],
  "metadata": "Author curation test: policy"
}
JSON
    committee "$FILE"
}

sha256_base64() {
    echo -n "$1" | sha256sum | awk '{print $1}' | xxd -r -p | base64 -w0
}

# submit <creator_identity>: a bridged post by that author
submit() {
    local SEED="$RANDOM$RANDOM"
    run_tx "$($BINARY tx federation submit-federated-content \
        "$PEER" "cur-$SEED" "blog_post" "$1" "Curation" "" \
        "curation body $SEED" "https://$PEER/users/x/statuses/$SEED" "$(date +%s)" \
        --content-hash "$(sha256_base64 "curation $SEED")" \
        --license CC0-1.0 \
        --from $OPERATOR_KEY --chain-id $CHAIN_ID --keyring-backend test \
        --gas 500000 --fees 5000${BOND_DENOM} -y --output json)"
}

expect_code() {  # <label> <want> (after submit)
    echo "  code=$TX_CODE (want $2)"
    if [ "$TX_CODE" == "$2" ]; then record_result "$1" "PASS"; else record_result "$1" "FAIL"; fi
}

# ========================================================================
# Setup: peer, bridge
# ========================================================================
echo ""
echo "--- SETUP ---"
register_test_peer "$PEER" "PEER_TYPE_ACTIVITYPUB" "Curation test peer" "" || { echo "ERROR: peer setup failed"; exit 1; }
register_test_bridge "$OPERATOR_KEY" "$OPERATOR_ADDR" "$PEER" "activitypub" "https://bridge.$PEER" || { echo "ERROR: bridge setup failed"; exit 1; }

# ========================================================================
# TEST 1: nothing configured admits nobody
# ========================================================================
echo ""
echo "--- TEST 1: empty allowed_identities admits nobody ---"
set_policy '[]' null
echo "  policy proposal: $PROP_STATUS"
submit "@alice@$PEER"
expect_code "Empty gate refuses (2385)" "2385"

# ========================================================================
# TEST 2: an explicit allowlist
# ========================================================================
echo ""
echo "--- TEST 2: allowed_identities lets in exactly the listed authors ---"
set_policy "[\"@alice@$PEER\", \"https://$PEER/@bob\"]" null
echo "  policy proposal: $PROP_STATUS"
submit "@alice@$PEER"; expect_code "Listed handle admitted" "0"
submit "@Bob@$PEER"; expect_code "Listed by profile URL, any case, admitted" "0"
submit "@mallory@$PEER"; expect_code "Unlisted author refused (2385)" "2385"

# ========================================================================
# TEST 3: a committee-owned curation collection with a member collaborator
# ========================================================================
echo ""
echo "--- TEST 3: the Operations Committee owns the curation collection ---"
# the committee pays the collection deposit from its own account
run_tx "$($BINARY tx bank send alice "$OPS_POLICY" 20000000${BOND_DENOM} --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} -y --output json)"
BEFORE=$($BINARY query collect collections-by-owner "$OPS_POLICY" --output json 2>/dev/null | jq -r '[.collections[]?.id // "0"] | length')
cat > "$PROPOSAL_DIR/curation_collection.json" <<JSON
{
  "policy_address": "$OPS_POLICY",
  "messages": [{
    "@type": "/sparkdream.collect.v1.MsgCreateCollection",
    "creator": "$OPS_POLICY",
    "type": "COLLECTION_TYPE_LINK",
    "visibility": "VISIBILITY_PUBLIC",
    "name": "curation-example-authors",
    "description": "Authors whose curation.example posts may be anchored"
  }],
  "metadata": "Author curation test: create the curation collection"
}
JSON
committee "$PROPOSAL_DIR/curation_collection.json"
echo "  create proposal: $PROP_STATUS"
COLL=$($BINARY query collect collections-by-owner "$OPS_POLICY" --output json 2>/dev/null | jq -r '[.collections[]? | select(.name=="curation-example-authors") | (.id // "0")] | last // empty')
STATUS=$($BINARY query collect collection "$COLL" --output json 2>/dev/null | jq -r '.collection.status // empty')
EXPIRES=$($BINARY query collect collection "$COLL" --output json 2>/dev/null | jq -r '.collection.expires_at // "0"')
echo "  collection=$COLL status=$STATUS expires_at=$EXPIRES"
if [ -n "$COLL" ] && [ "$STATUS" == "COLLECTION_STATUS_ACTIVE" ] && [ "$EXPIRES" == "0" ]; then
    record_result "Committee-owned collection is ACTIVE and permanent" "PASS"
else
    record_result "Committee-owned collection is ACTIVE and permanent" "FAIL"
fi

cat > "$PROPOSAL_DIR/curation_collaborator.json" <<JSON
{
  "policy_address": "$OPS_POLICY",
  "messages": [{
    "@type": "/sparkdream.collect.v1.MsgAddCollaborator",
    "creator": "$OPS_POLICY",
    "collection_id": "$COLL",
    "address": "$BOB_ADDR",
    "role": "COLLABORATOR_ROLE_EDITOR"
  }],
  "metadata": "Author curation test: bob curates"
}
JSON
committee "$PROPOSAL_DIR/curation_collaborator.json"
echo "  collaborator proposal: $PROP_STATUS"

# bob, a member collaborator, curates: carol by handle, dave by profile URL
for AUTHOR in "@carol@$PEER" "https://$PEER/@dave"; do
    run_tx "$($BINARY tx collect add-item "$COLL" 0 "$AUTHOR" "" "" link \
        --link "{\"uri\":\"$AUTHOR\"}" --from bob --chain-id $CHAIN_ID --keyring-backend test \
        --gas 500000 --fees 5000${BOND_DENOM} -y --output json)"
    echo "  add $AUTHOR: code=$TX_CODE"
done
ITEMS=$($BINARY query collect items "$COLL" --output json 2>/dev/null | jq -r '[.items[]? | select(.reference_type=="REFERENCE_TYPE_LINK")] | length')
echo "  link items: $ITEMS"
if [ "$ITEMS" == "2" ]; then record_result "Member collaborator curates the list" "PASS"; else record_result "Member collaborator curates the list" "FAIL"; fi

# ========================================================================
# TEST 4: "*" plus the collection: the collection alone decides
# ========================================================================
echo ""
echo "--- TEST 4: collection-only curation ---"
set_policy '["*"]' "{\"collection_id\": \"$COLL\"}"
echo "  policy proposal: $PROP_STATUS"
CUR=$($BINARY query federation get-peer-policy "$PEER" --output json | jq -r '.policy.curation.collection_id // "0"')
echo "  policy curation collection: $CUR"
submit "@carol@$PEER"; expect_code "Curated by handle admitted" "0"
submit "@dave@$PEER"; expect_code "Curated by profile URL admitted" "0"
submit "@alice@$PEER"; expect_code "Uncurated author refused (2386)" "2386"

# ========================================================================
# TEST 5: both gates: listed AND curated
# ========================================================================
echo ""
echo "--- TEST 5: allowlist and collection together ---"
set_policy "[\"@carol@$PEER\", \"@alice@$PEER\"]" "{\"collection_id\": \"$COLL\"}"
echo "  policy proposal: $PROP_STATUS"
submit "@carol@$PEER"; expect_code "Listed and curated admitted" "0"
submit "@alice@$PEER"; expect_code "Listed but not curated refused (2386)" "2386"
submit "@dave@$PEER"; expect_code "Curated but not listed refused (2385)" "2385"

# ========================================================================
# TEST 6: the committee curates by proposal too (no curator needed)
# ========================================================================
echo ""
echo "--- TEST 6: committee adds and removes an author by proposal ---"
set_policy '["*"]' "{\"collection_id\": \"$COLL\"}"
echo "  policy proposal: $PROP_STATUS"
submit "@erin@$PEER"; expect_code "Before: uncurated author refused (2386)" "2386"

cat > "$PROPOSAL_DIR/curation_add_item.json" <<JSON
{
  "policy_address": "$OPS_POLICY",
  "messages": [{
    "@type": "/sparkdream.collect.v1.MsgAddItem",
    "creator": "$OPS_POLICY",
    "collection_id": "$COLL",
    "position": "0",
    "title": "@erin@$PEER",
    "reference_type": "REFERENCE_TYPE_LINK",
    "link": {"uri": "@erin@$PEER"}
  }],
  "metadata": "Author curation test: the committee adds an author"
}
JSON
committee "$PROPOSAL_DIR/curation_add_item.json"
echo "  add proposal: $PROP_STATUS"
submit "@erin@$PEER"; expect_code "Committee-added author admitted" "0"

ITEM=$($BINARY query collect items "$COLL" --output json 2>/dev/null | jq -r --arg u "@erin@$PEER" '[.items[]? | select(.link.uri == $u) | (.id // "0")] | first // empty')
echo "  item: $ITEM"
cat > "$PROPOSAL_DIR/curation_remove_item.json" <<JSON
{
  "policy_address": "$OPS_POLICY",
  "messages": [{
    "@type": "/sparkdream.collect.v1.MsgRemoveItem",
    "creator": "$OPS_POLICY",
    "id": "$ITEM"
  }],
  "metadata": "Author curation test: the committee removes an author"
}
JSON
committee "$PROPOSAL_DIR/curation_remove_item.json"
echo "  remove proposal: $PROP_STATUS"
submit "@erin@$PEER"; expect_code "Committee-removed author refused (2386)" "2386"

# ========================================================================
# Summary
# ========================================================================
echo ""
echo "============================================"
echo "AUTHOR CURATION TEST RESULTS"
echo "============================================"
for i in "${!TEST_NAMES[@]}"; do
    printf "  %-60s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done
echo ""
echo "  Passed: $PASS_COUNT / $((PASS_COUNT + FAIL_COUNT))"
if [ "$FAIL_COUNT" -gt 0 ]; then
    echo ">>> SOME AUTHOR CURATION TESTS FAILED <<<"
    exit 1
fi
echo ">>> ALL AUTHOR CURATION TESTS PASSED <<<"
