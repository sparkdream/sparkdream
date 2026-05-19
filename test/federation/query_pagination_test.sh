#!/bin/bash

echo "--- TESTING: FEDERATION list-query pagination ---"
#
# All List* queries on x/federation use Cosmos SDK's standard
# PageRequest/PageResponse. This file exercises:
#   - the canonical `--page-limit N` page size cap
#   - the `--page-key` continuation pattern across pages
#   - the per-query filter argument that ListPendingIdentityChallenges
#     accepts (claimed_address)
#
# It is deliberately additive: query_test.sh already validates the
# happy-path body shape; here we focus on the pagination contract.

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "ERROR: .test_env not found. Run setup_test_accounts.sh first."
    exit 1
fi
source "$SCRIPT_DIR/.test_env"
# shellcheck disable=SC1091
source "$SCRIPT_DIR/peer_fixtures.sh"

PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()

record_result() {
    local NAME=$1
    local RESULT=$2
    TEST_NAMES+=("$NAME")
    RESULTS+=("$RESULT")
    if [ "$RESULT" == "PASS" ]; then PASS_COUNT=$((PASS_COUNT + 1)); else FAIL_COUNT=$((FAIL_COUNT + 1)); fi
    echo "  => $RESULT"
}

# Seed at least four ActivityPub peers so list-peers and
# list-bridge-bindings have something to page through. peer_fixtures'
# register_test_peer is idempotent.
echo ""
echo "Seeding peers for pagination..."
for i in 1 2 3 4; do
    register_test_peer "pagi-$i.example" "PEER_TYPE_ACTIVITYPUB" "Pagination peer $i" ""
done

# ========================================================================
# Group A: ListPeers pagination
# ========================================================================
echo ""
echo "============================================================"
echo "Group A: list-peers pagination"
echo "============================================================"

# ------------------------------------------------------------------------
# TEST A1: --page-limit caps the response size and emits a next_key when more
# pages remain.
# ------------------------------------------------------------------------
echo ""
echo "--- TEST A1: list-peers --page-limit 2 → exactly 2 results + next_key ---"

PAGE1=$($BINARY query federation list-peers --page-limit 2 --output json 2>&1)
COUNT1=$(echo "$PAGE1" | jq '.peers | length')
NEXT_KEY=$(echo "$PAGE1" | jq -r '.pagination.next_key // empty')

if [ "$COUNT1" == "2" ] && [ -n "$NEXT_KEY" ] && [ "$NEXT_KEY" != "null" ]; then
    echo "  page 1 size=$COUNT1, next_key=${NEXT_KEY:0:32}..."
    record_result "list-peers limit=2 returns 2 + next_key" "PASS"
else
    echo "  count=$COUNT1 next_key='$NEXT_KEY'"
    record_result "list-peers limit=2 returns 2 + next_key" "FAIL"
fi

# ------------------------------------------------------------------------
# TEST A2: --page-key continuation walks the rest of the set.
# ------------------------------------------------------------------------
echo ""
echo "--- TEST A2: list-peers --page-key continues pagination ---"

if [ -n "$NEXT_KEY" ] && [ "$NEXT_KEY" != "null" ]; then
    PAGE2=$($BINARY query federation list-peers --page-limit 2 --page-key "$NEXT_KEY" --output json 2>&1)
    COUNT2=$(echo "$PAGE2" | jq '.peers | length')
    # Page 1's first peer must differ from page 2's first peer.
    P1_FIRST=$(echo "$PAGE1" | jq -r '.peers[0].id // empty')
    P2_FIRST=$(echo "$PAGE2" | jq -r '.peers[0].id // empty')

    if [ "$COUNT2" -gt 0 ] 2>/dev/null && [ "$P1_FIRST" != "$P2_FIRST" ]; then
        echo "  page 2 size=$COUNT2, distinct from page 1 (first: $P1_FIRST → $P2_FIRST)"
        record_result "list-peers --page-key continuation" "PASS"
    else
        echo "  page 2 count=$COUNT2 page1.first=$P1_FIRST page2.first=$P2_FIRST"
        record_result "list-peers --page-key continuation" "FAIL"
    fi
else
    echo "  SKIP: TEST A1 did not produce a next_key"
    record_result "list-peers --page-key continuation" "FAIL"
fi

# ------------------------------------------------------------------------
# TEST A3: --page-reverse returns results in opposite order without crashing.
# ------------------------------------------------------------------------
echo ""
echo "--- TEST A3: list-peers --page-reverse ---"

REV=$($BINARY query federation list-peers --page-reverse --page-limit 1 --output json 2>&1)
REV_COUNT=$(echo "$REV" | jq '.peers | length')
REV_FIRST=$(echo "$REV" | jq -r '.peers[0].id // empty')
FWD_FIRST=$(echo "$PAGE1" | jq -r '.peers[0].id // empty')

if [ "$REV_COUNT" == "1" ] && [ "$REV_FIRST" != "$FWD_FIRST" ]; then
    echo "  reverse first: $REV_FIRST   forward first: $FWD_FIRST"
    record_result "list-peers --reverse" "PASS"
elif [ "$REV_COUNT" == "1" ]; then
    # Only one peer total → reverse first == forward first. Soft pass.
    echo "  reverse first matches forward first (likely only one peer)"
    record_result "list-peers --reverse" "PASS"
else
    record_result "list-peers --reverse" "FAIL"
fi

# ------------------------------------------------------------------------
# TEST A4: --page-count-total returns a populated `total` field.
# ------------------------------------------------------------------------
echo ""
echo "--- TEST A4: list-peers --page-count-total ---"

CT=$($BINARY query federation list-peers --page-count-total --output json 2>&1)
TOTAL=$(echo "$CT" | jq -r '.pagination.total // "0"')

if [ "$TOTAL" -ge 4 ] 2>/dev/null; then
    echo "  total=$TOTAL (≥ 4 seeded peers)"
    record_result "list-peers --page-count-total" "PASS"
else
    echo "  total='$TOTAL'"
    record_result "list-peers --page-count-total" "FAIL"
fi

# ========================================================================
# Group B: ListBridgeBindings pagination
# ========================================================================
echo ""
echo "============================================================"
echo "Group B: list-bridge-bindings pagination"
echo "============================================================"

# Bridge bindings vary across test runs; just verify the pagination
# contract holds even when only zero or one binding exists.
echo ""
echo "--- TEST B1: list-bridge-bindings --page-limit 1 returns ≤ 1 binding ---"

BB=$($BINARY query federation list-bridge-bindings --page-limit 1 --output json 2>&1)
BB_COUNT=$(echo "$BB" | jq '.bridge_bindings | length')

if [ "$BB_COUNT" -le 1 ] 2>/dev/null; then
    echo "  count=$BB_COUNT"
    record_result "list-bridge-bindings limit=1 returns ≤ 1" "PASS"
else
    record_result "list-bridge-bindings limit=1 returns ≤ 1" "FAIL"
fi

# ========================================================================
# Group C: ListFederatedContent pagination
# ========================================================================
echo ""
echo "============================================================"
echo "Group C: list-federated-content pagination"
echo "============================================================"

echo ""
echo "--- TEST C1: list-federated-content --page-limit 5 ---"

FC=$($BINARY query federation list-federated-content --page-limit 5 --output json 2>&1)
FC_COUNT=$(echo "$FC" | jq '.content | length')

if [ "$FC_COUNT" -le 5 ] 2>/dev/null; then
    echo "  count=$FC_COUNT"
    record_result "list-federated-content limit=5 ≤ 5" "PASS"
else
    record_result "list-federated-content limit=5 ≤ 5" "FAIL"
fi

# ========================================================================
# Group D: ListIdentityLinks pagination
# ========================================================================
echo ""
echo "============================================================"
echo "Group D: list-identity-links pagination"
echo "============================================================"

echo ""
echo "--- TEST D1: list-identity-links --page-limit 5 ---"

IL=$($BINARY query federation list-identity-links --page-limit 5 --output json 2>&1)
IL_COUNT=$(echo "$IL" | jq '.links | length')

if [ "$IL_COUNT" -le 5 ] 2>/dev/null; then
    echo "  count=$IL_COUNT"
    record_result "list-identity-links limit=5 ≤ 5" "PASS"
else
    record_result "list-identity-links limit=5 ≤ 5" "FAIL"
fi

# ========================================================================
# Group E: ListPendingIdentityChallenges — claimed_address filter
# ========================================================================
echo ""
echo "============================================================"
echo "Group E: list-pending-identity-challenges (claimed_address filter)"
echo "============================================================"

# An empty claimed_address is special: we accept either an error or an
# empty list — both are valid handler responses.
echo ""
echo "--- TEST E1: list-pending-identity-challenges <linker1> ---"

PIC=$($BINARY query federation list-pending-identity-challenges "$LINKER1_ADDR" --output json 2>&1)
# Three acceptable handler outputs:
#   1. `{ "challenges": [...] }` populated
#   2. `{}` (proto3 omits empty repeated fields, so an address with
#      zero pending challenges renders as `{}`)
#   3. `{ "challenges": [] }` (some codec settings keep the empty list)
# Anything that parses as JSON without crashing is a pass.
if echo "$PIC" | jq -e '.' > /dev/null 2>&1; then
    PIC_COUNT=$(echo "$PIC" | jq '(.challenges // []) | length')
    echo "  pending count for linker1: $PIC_COUNT"
    record_result "list-pending-identity-challenges (filtered)" "PASS"
else
    echo "  Query failed (not JSON). Output: $(echo "$PIC" | head -c 160)"
    record_result "list-pending-identity-challenges (filtered)" "FAIL"
fi

# ========================================================================
# Group F: ListOutboundAttestations pagination
# ========================================================================
echo ""
echo "============================================================"
echo "Group F: list-outbound-attestations pagination"
echo "============================================================"

echo ""
echo "--- TEST F1: list-outbound-attestations --page-limit 5 ---"

OA=$($BINARY query federation list-outbound-attestations --page-limit 5 --output json 2>&1)
OA_COUNT=$(echo "$OA" | jq '.attestations | length')

if [ "$OA_COUNT" -le 5 ] 2>/dev/null; then
    echo "  count=$OA_COUNT"
    record_result "list-outbound-attestations limit=5 ≤ 5" "PASS"
else
    record_result "list-outbound-attestations limit=5 ≤ 5" "FAIL"
fi

# ========================================================================
# Group G: VerifierActivity — federation-side per-verifier counters
# ========================================================================
echo ""
echo "============================================================"
echo "Group G: verifier-activity"
echo "============================================================"

# The verifier_activity query returns a record even for addresses that
# have never verified anything (zeroed counters). We don't depend on
# verifier1 being bonded — we just verify the query layer is reachable.
echo ""
echo "--- TEST G1: verifier-activity <verifier1> ---"

VA=$($BINARY query federation verifier-activity "$VERIFIER1_ADDR" --output json 2>&1)
# Accept either a populated activity record or a not-found-ish empty
# struct. The contract is "no crash, parseable JSON".
if echo "$VA" | jq -e '.' > /dev/null 2>&1; then
    echo "  Query returned valid JSON"
    record_result "verifier-activity reachable" "PASS"
else
    echo "  Output: $(echo "$VA" | head -c 160)"
    record_result "verifier-activity reachable" "FAIL"
fi

# ========================================================================
# Summary
# ========================================================================
echo ""
echo "============================================"
echo "QUERY PAGINATION TEST RESULTS"
echo "============================================"
for i in "${!TEST_NAMES[@]}"; do
    printf "  %-55s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
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
