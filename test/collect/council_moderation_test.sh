#!/bin/bash
# ============================================================================
# x/collect: Council moderation path + shared content-sentinel accountability
# ============================================================================
# Covers the council (gov-authority) moderation pair and the cross-module
# RoleActivity accountability (see docs/x-collect-spec.md 5.25/5.26a and the
# RoleActivity section of docs/x-rep-spec.md):
#
#   Test 1: Council hide via --authority council (alice, the e2e council
#           authority). HideRecord written with the empty-sentinel gov
#           marker and zero committed bond; no sentinel bond moves.
#   Test 2: Council hides ARE appealable (deviation from forum's gov
#           hides): the owner's appeal-hide tx succeeds.
#   Test 3: Council unhide overrides a SENTINEL hide with no window: content
#           back to ACTIVE and the sentinel's committed bond released
#           IMMEDIATELY (no waiting for the original appeal deadline —
#           contrast with the self-correct retention in
#           sentinel_unhide_test.sh).
#   Test 4: Authority rejections: --authority council by a non-council
#           account fails; --authority sentinel by a non-sentinel fails.
#   Test 5: Cross-module accountability projection: bob's collect hides are
#           visible through `query forum get-sentinel-activity`
#           (total_collect_hides), proving collect reports into rep's shared
#           RoleActivity and forum's query projects it.
#
# Account roles:
#   alice — council authority (Ops Committee / founder in the e2e genesis);
#           NOT a bonded sentinel in this suite's flows
#   bob   — bonded content sentinel (shared role across forum + collect)
#   carol — member content owner (fixtures + appellant)
#
# Requires config.yml params overrides (documented in
# endorsement_slash_test.sh): hide_expiry_blocks=40,
# appeal_cooldown_blocks=2, appeal_deadline_blocks=60,
# sentinel_unhide_window_blocks=20.
# ============================================================================

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/test_helpers.sh"
source "$SCRIPT_DIR/.test_env"

echo "========================================================================="
echo "  X/COLLECT - COUNCIL MODERATION + SHARED ACCOUNTABILITY TESTS"
echo "========================================================================="
echo ""

ALICE_ADDR=$(get_address alice)
BOB_ADDR=$(get_address bob)
CAROL_ADDR=$(get_address carol)
if [ -z "$ALICE_ADDR" ] || [ -z "$BOB_ADDR" ] || [ -z "$CAROL_ADDR" ]; then
    echo "FAIL: alice/bob/carol key missing from keyring"
    exit 1
fi

# Sanity: the CLI must expose the --authority flag on hide-content.
if ! $BINARY tx collect hide-content --help 2>&1 | grep -q -- '--authority'; then
    echo "FATAL: 'collect hide-content' is missing the --authority flag." >&2
    exit 1
fi

bob_committed_bond() {
    $BINARY query rep bonded-role content-sentinel "$BOB_ADDR" -o json 2>/dev/null \
        | jq -r '.bonded_role.total_committed_bond // "0"'
}

# wait_for_bond_drain <max_seconds> — poll until bob's committed bond is 0.
# The preceding sentinel_unhide_test suite deliberately RETAINS self-correct
# commitments until each hide's original appeal deadline (~40 blocks) and
# leaves an appeal to time out (~60 blocks); if those release mid-test, any
# before/after equality on bob's committed bond is racy. Draining to zero
# first makes the baselines deterministic. Warns (rather than fails) on
# timeout so an unrelated stuck commitment surfaces as the real assertion
# diff below, not as an opaque drain failure.
wait_for_bond_drain() {
    local max=$1 waited=0 c
    while [ "$waited" -lt "$max" ]; do
        c=$(bob_committed_bond)
        if [ "$c" = "0" ]; then
            return 0
        fi
        sleep 5
        waited=$((waited + 5))
    done
    echo "  WARN: bob's committed bond still $(bob_committed_bond) after ${max}s; baselines may be racy"
    return 1
}

# create_carol_collection <name> — sets COLL_ID to the new collection id.
# (Sets a global rather than echoing: assert_* helpers print and mutate the
# pass/fail counters, which a command-substitution subshell would swallow.)
create_carol_collection() {
    local name=$1
    TX_OUT=$(send_tx collect create-collection \
        nft public false 0 "$name" "Council-mod fixture" "" "" --from carol)
    assert_tx_success "carol creates collection $name" "$TX_OUT"
    COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
    if [ -z "$COLL_ID" ]; then
        COLL_ID=$(resolve_collection_id "$CAROL_ADDR" "$name")
    fi
}

# ----------------------------------------------------------------------------
# Setup: bond bob as content-sentinel (idempotent — mirrors
# endorsement_slash_test.sh / sentinel_unhide_test.sh).
# ----------------------------------------------------------------------------
echo ""
echo "--- Setup: Bond bob as content-sentinel ---"
EXISTING_BOND=$($BINARY query rep bonded-role content-sentinel "$BOB_ADDR" -o json 2>/dev/null \
    | jq -r '.bonded_role.current_bond // "0"')
if [ "$EXISTING_BOND" -gt 0 ] 2>/dev/null; then
    echo "  bob already bonded (current_bond=$EXISTING_BOND), reusing"
else
    TX_OUT=$(send_tx rep bond-role content-sentinel "2500000000" --from bob)
    assert_tx_success "Bond bob as content-sentinel" "$TX_OUT"
fi

# ----------------------------------------------------------------------------
# Test 1: Council hide — gov marker, zero bond.
# ----------------------------------------------------------------------------
echo ""
echo "--- Test 1: Council hide (--authority council) ---"

# Drain retained commitments left by the sentinel_unhide suite so the
# before/after bond comparisons below are deterministic (see helper).
echo "  Waiting up to ~240s for bob's outstanding commitments to drain..."
wait_for_bond_drain 240

create_carol_collection "CouncilHideColl"
T1_COLL_ID=$COLL_ID
COMMITTED_BASE=$(bob_committed_bond)

TX_OUT=$(send_tx collect hide-content "$T1_COLL_ID" collection spam "spam" \
    --authority council --from alice)
assert_tx_success "alice council-hides carol's collection" "$TX_OUT"
T1_HIDE_REC=$(extract_event_attr "$TX_RESULT_OUT" "content_hidden" "hide_record_id")
T1_HIDE_REC=${T1_HIDE_REC:-0}

T1_STATUS=$(query collect collection "$T1_COLL_ID" | jq -r '.collection.status // empty')
assert_equal "Collection HIDDEN after council hide" "COLLECTION_STATUS_HIDDEN" "$T1_STATUS"

HR_JSON=$(query collect hide-record "$T1_HIDE_REC")
HR_SENTINEL=$(echo "$HR_JSON" | jq -r '.hide_record.sentinel // ""')
HR_COMMITTED=$(echo "$HR_JSON" | jq -r '.hide_record.committed_amount // "0"')
assert_equal "Council hide carries the empty-sentinel gov marker" "" "$HR_SENTINEL"
assert_equal "Council hide commits no bond" "0" "$HR_COMMITTED"
assert_equal "No sentinel bond moved on a council hide" \
    "$COMMITTED_BASE" "$(bob_committed_bond)"

AUTHORITY_ATTR=$(extract_event_attr "$TX_RESULT_OUT" "content_hidden" "authority")
assert_equal "content_hidden event carries authority=council" "council" "$AUTHORITY_ATTR"

# ----------------------------------------------------------------------------
# Test 2: Council hides are appealable (unlike forum's gov hides).
# ----------------------------------------------------------------------------
echo ""
echo "--- Test 2: Council hide is appealable ---"

# Wait past appeal_cooldown_blocks (=2; ~12s of tx pacing already elapsed,
# but be explicit and cheap about it).
sleep 6

TX_OUT=$(send_tx collect appeal-hide "$T1_HIDE_REC" --from carol)
assert_tx_success "carol appeals the COUNCIL hide" "$TX_OUT"

HR_APPEALED=$(query collect hide-record "$T1_HIDE_REC" | jq -r '.hide_record.appealed // false')
assert_equal "Council hide record marked appealed" "true" "$HR_APPEALED"
# The appeal times out in carol's favor after appeal_deadline_blocks (=60);
# no need to wait for the restore here.

# ----------------------------------------------------------------------------
# Test 3: Council unhide overrides a sentinel hide — immediate bond release.
# ----------------------------------------------------------------------------
echo ""
echo "--- Test 3: Council unhide of a sentinel hide releases the bond immediately ---"

create_carol_collection "CouncilOverrideColl"
T3_COLL_ID=$COLL_ID
COMMITTED_BEFORE_HIDE=$(bob_committed_bond)

TX_OUT=$(send_tx collect hide-content "$T3_COLL_ID" collection spam "spam" --from bob)
assert_tx_success "bob sentinel-hides carol's collection" "$TX_OUT"
T3_HIDE_REC=$(extract_event_attr "$TX_RESULT_OUT" "content_hidden" "hide_record_id")
T3_HIDE_REC=${T3_HIDE_REC:-0}

COMMITTED_AFTER_HIDE=$(bob_committed_bond)
HIDE_DELTA=$((COMMITTED_AFTER_HIDE - COMMITTED_BEFORE_HIDE))
assert_gt "bob's bond reserved by the sentinel hide" "0" "$HIDE_DELTA"

TX_OUT=$(send_tx collect unhide-content "$T3_HIDE_REC" --from alice)
assert_tx_success "alice council-unhides bob's sentinel hide" "$TX_OUT"

T3_STATUS=$(query collect collection "$T3_COLL_ID" | jq -r '.collection.status // empty')
assert_equal "Collection ACTIVE after council unhide" "COLLECTION_STATUS_ACTIVE" "$T3_STATUS"

# Contrast with self-correct: the council override releases bob's committed
# bond IMMEDIATELY (no retention until the original appeal deadline).
assert_equal "bob's committed bond released immediately by the council unhide" \
    "$COMMITTED_BEFORE_HIDE" "$(bob_committed_bond)"

IS_COUNCIL_ATTR=$(extract_event_attr "$TX_RESULT_OUT" "content_unhidden" "is_council")
assert_equal "content_unhidden event carries is_council=true" "true" "$IS_COUNCIL_ATTR"

# ----------------------------------------------------------------------------
# Test 4: Authority rejections.
# ----------------------------------------------------------------------------
echo ""
echo "--- Test 4: Forced-authority rejections ---"

create_carol_collection "AuthorityRejectColl"
T4_COLL_ID=$COLL_ID

# bob is a sentinel, not council: forcing council must fail.
TX_OUT=$(send_tx collect hide-content "$T4_COLL_ID" collection spam "spam" \
    --authority council --from bob)
assert_tx_failure "bob (non-council) cannot force --authority council" "$TX_OUT"

# carol is neither: forcing sentinel must fail.
TX_OUT=$(send_tx collect hide-content "$T4_COLL_ID" collection spam "spam" \
    --authority sentinel --from carol)
assert_tx_failure "carol (non-sentinel) cannot force --authority sentinel" "$TX_OUT"

T4_STATUS=$(query collect collection "$T4_COLL_ID" | jq -r '.collection.status // empty')
assert_equal "Collection still ACTIVE after rejected hides" \
    "COLLECTION_STATUS_ACTIVE" "$T4_STATUS"

# ----------------------------------------------------------------------------
# Test 5: Cross-module accountability — collect hides visible through
# forum's get-sentinel-activity projection (rep-owned RoleActivity).
# ----------------------------------------------------------------------------
echo ""
echo "--- Test 5: Collect hides project into forum get-sentinel-activity ---"

# bob performed at least one sentinel-path collect hide in Test 3 (plus any
# from earlier suites in the same run). proto3 omits zero-valued uint64, so
# normalize with // "0".
SA_JSON=$($BINARY query forum get-sentinel-activity "$BOB_ADDR" --output json 2>/dev/null)
TOTAL_COLLECT_HIDES=$(echo "$SA_JSON" | jq -r '.sentinel_activity.total_collect_hides // "0"')
assert_gt "forum projection shows bob's collect hides (total_collect_hides)" \
    "0" "$TOTAL_COLLECT_HIDES"

echo ""
print_summary
exit $?
