#!/bin/bash
# ============================================================================
# x/collect: Sentinel self-correct unhide (MsgUnhideContent)
# ============================================================================
# Covers the sentinel self-correct window (MsgUnhideContent — see
# docs/x-collect-spec.md 5.26a):
#
#   Test 1: Happy path — sentinel hides a member-owned tagged collection,
#           then unhides within the window. Status returns to ACTIVE, the
#           HideRecord resolves as self_corrected, and the author's per-tag
#           rep returns to its pre-hide value (the no-mint check: a floored
#           deduction restores nothing, a real deduction restores exactly
#           what was taken). The sentinel's committed bond stays reserved
#           after the unhide (anti hide/unhide cycling) and is only
#           released by the EndBlocker at the original appeal deadline.
#           Also: re-hide after self-correct works (fresh record, second
#           commit) and can itself be self-corrected; appealing a resolved
#           record is rejected. The hide-records-by-sentinel query lists
#           bob's records newest first and is empty for a non-sentinel.
#
#   Test 2: Only the hiding sentinel can unhide — the owner is rejected.
#
#   Test 3: Once appealed, self-correct is off the table — the jury owns
#           the outcome (unhide on an appealed record is rejected).
#
#   Test 4: The window is real — an unhide attempted after
#           sentinel_unhide_window_blocks have elapsed is rejected.
#
# Account roles:
#   carol — member author (PROVISIONAL genesis founder); owns the fixtures
#   bob   — bonded content-sentinel (shared moderation role across forum and
#           collect), performs all hides/unhides
#
# Requires config.yml params overrides (at ~1s/block):
#   collect.hide_expiry_blocks:            "40"  # prod default 100800 ~7 days
#   collect.appeal_cooldown_blocks:        "2"   # prod default 600    ~1 hour
#   collect.appeal_deadline_blocks:        "60"  # prod default 201600 ~14 days
#   collect.sentinel_unhide_window_blocks: "20"  # prod default 14400  ~24 hours
#
# Timing note: Test 1's hide -> unhide gap must stay under the 20-block
# window, so the txs are sent back-to-back with only one status query in
# between (~15 blocks at 1s/block worst case; late-run 2s/block gives even
# more headroom). All longer waits poll block height rather than sleeping a
# fixed wall-clock amount (block rate drifts ~1s -> ~2s late in a run).
# ============================================================================

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/test_helpers.sh"
source "$SCRIPT_DIR/.test_env"

echo "========================================================================="
echo "  X/COLLECT - SENTINEL SELF-CORRECT UNHIDE TESTS"
echo "========================================================================="
echo ""

BOB_ADDR=$(get_address bob)
CAROL_ADDR=$(get_address carol)
if [ -z "$BOB_ADDR" ] || [ -z "$CAROL_ADDR" ]; then
    echo "FAIL: bob or carol key not found in keyring"
    exit 1
fi
echo "Bob (sentinel) address:  $BOB_ADDR"
echo "Carol (author) address:  $CAROL_ADDR"

# ----------------------------------------------------------------------------
# Helpers
# ----------------------------------------------------------------------------
carol_rep_for_tag() {
    local TAG=$1
    $BINARY query rep get-member "$CAROL_ADDR" -o json 2>/dev/null \
        | jq -r --arg t "$TAG" '.member.reputation_scores[$t] // "absent"'
}

bob_committed_bond() {
    $BINARY query rep bonded-role content-sentinel "$BOB_ADDR" -o json 2>/dev/null \
        | jq -r '.bonded_role.total_committed_bond // "0"'
}

# wait_past_block <target_height> <max_seconds> — poll until the chain height
# exceeds target_height. Returns 1 on timeout.
wait_past_block() {
    local target=$1 max=$2 waited=0 h
    while [ "$waited" -lt "$max" ]; do
        h=$(get_block_height)
        if [ -n "$h" ] && [ "$h" -gt "$target" ] 2>/dev/null; then
            return 0
        fi
        sleep 3
        waited=$((waited + 3))
    done
    return 1
}

# poll_committed_back_to <baseline> <max_seconds> — poll bob's
# total_committed_bond until it returns to the given baseline (EndBlocker
# released the retained self-correct commitments). Returns 1 on timeout.
poll_committed_back_to() {
    local baseline=$1 max=$2 waited=0 c
    while [ "$waited" -lt "$max" ]; do
        c=$(bob_committed_bond)
        if [ "$c" = "$baseline" ]; then
            return 0
        fi
        sleep 5
        waited=$((waited + 5))
    done
    return 1
}

# hide_collection <coll_id> — bob hides; sets HIDE_REC_ID from the event.
hide_collection() {
    local coll_id=$1 label=$2
    TX_OUT=$(send_tx collect hide-content "$coll_id" collection spam "spam" --from bob)
    assert_tx_success "$label" "$TX_OUT"
    HIDE_REC_ID=$(extract_event_attr "$TX_RESULT_OUT" "content_hidden" "hide_record_id")
    if [ -z "$HIDE_REC_ID" ]; then
        HIDE_REC_ID=$(query collect hide-records-by-target "$coll_id" collection \
            | jq -r '.hide_records[-1].id // "0"' 2>/dev/null)
    fi
    # proto3 omits zero-valued uint64 from CLI JSON — id 0 is a valid first
    # record, so normalize empty to "0" rather than treating it as a failure.
    HIDE_REC_ID=${HIDE_REC_ID:-0}
}

# ----------------------------------------------------------------------------
# Setup: bond bob as content-sentinel (idempotent; same pattern as
# endorsement_slash_test.sh).
# ----------------------------------------------------------------------------
echo ""
echo "--- Setup: Bond bob as content-sentinel ---"
EXISTING_BOND=$($BINARY query rep bonded-role content-sentinel "$BOB_ADDR" -o json 2>/dev/null \
    | jq -r '.bonded_role.current_bond // "0"')
if [ "$EXISTING_BOND" -gt 0 ] 2>/dev/null; then
    echo "  bob already bonded (current_bond=$EXISTING_BOND), reusing"
else
    BOND_AMOUNT="2500000000"  # 2500 DREAM
    TX_OUT=$(send_tx rep bond-role content-sentinel "$BOND_AMOUNT" --from bob)
    assert_tx_success "Bond bob as content-sentinel" "$TX_OUT"
fi

BOND_STATUS=$($BINARY query rep bonded-role content-sentinel "$BOB_ADDR" -o json 2>/dev/null \
    | jq -r '.bonded_role.bond_status // "unknown"')
assert_equal "bob's bonded-role status is NORMAL" "BONDED_ROLE_STATUS_NORMAL" "$BOND_STATUS"

# Seeded genesis tags (same pair endorsement_slash_test.sh uses) so the hide
# applies a per-tag rep penalty whose snapshot/restore we can observe.
UNHIDE_TAG_A="commons-council"
UNHIDE_TAG_B="technical-council"

# ----------------------------------------------------------------------------
# Test 1: Self-correct happy path + bond retention + rep no-mint + re-hide.
# ----------------------------------------------------------------------------
echo ""
echo "--- Test 1: Sentinel self-correct within the window ---"

COMMITTED_BASE=$(bob_committed_bond)
echo "  bob total_committed_bond baseline: $COMMITTED_BASE"

REP_A_BEFORE=$(carol_rep_for_tag "$UNHIDE_TAG_A")
REP_B_BEFORE=$(carol_rep_for_tag "$UNHIDE_TAG_B")
echo "  carol rep on $UNHIDE_TAG_A before hide: $REP_A_BEFORE"
echo "  carol rep on $UNHIDE_TAG_B before hide: $REP_B_BEFORE"

TX_OUT=$(send_tx collect create-collection \
    nft public false 0 "UnhideColl" "Self-correct fixture" "" "$UNHIDE_TAG_A,$UNHIDE_TAG_B" \
    --from carol)
assert_tx_success "carol creates a tagged ACTIVE collection" "$TX_OUT"
T1_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$T1_COLL_ID" ]; then
    T1_COLL_ID=$(resolve_collection_id "$CAROL_ADDR" "UnhideColl")
fi
echo "  Test 1 collection ID: $T1_COLL_ID"

# Hide then unhide back-to-back — the gap must stay inside the 20-block
# window, so only one quick status probe sits between the two txs.
hide_collection "$T1_COLL_ID" "bob hides carol's collection"
T1_HIDE_REC_1=$HIDE_REC_ID
echo "  Test 1 hide record ID: $HIDE_REC_ID"

T1_STATUS=$(query collect collection "$T1_COLL_ID" | jq -r '.collection.status // empty')
assert_equal "Collection is HIDDEN after hide" "COLLECTION_STATUS_HIDDEN" "$T1_STATUS"

TX_OUT=$(send_tx collect unhide-content "$HIDE_REC_ID" --from bob)
assert_tx_success "bob self-corrects (unhide within window)" "$TX_OUT"

T1_STATUS=$(query collect collection "$T1_COLL_ID" | jq -r '.collection.status // empty')
assert_equal "Collection is ACTIVE after unhide" "COLLECTION_STATUS_ACTIVE" "$T1_STATUS"

# HideRecord closed as self-corrected. proto3 omits false booleans from CLI
# JSON, so `// false` normalizes absence.
HR_JSON=$(query collect hide-record "$HIDE_REC_ID")
HR_RESOLVED=$(echo "$HR_JSON" | jq -r '.hide_record.resolved // false')
HR_SELF=$(echo "$HR_JSON" | jq -r '.hide_record.self_corrected // false')
assert_equal "HideRecord resolved after unhide" "true" "$HR_RESOLVED"
assert_equal "HideRecord marked self_corrected" "true" "$HR_SELF"

# Rep no-mint check: after slash -> restore, carol's per-tag rep must equal
# its pre-hide value. If she had no rep, the deduction floored at zero and
# the restore must add NOTHING (the snapshot records actual amounts, not the
# penalty param) — the entry may exist at 0 but must not exceed the pre-hide
# value. Normalize "absent" to 0 for comparison.
norm_rep() { if [ "$1" = "absent" ]; then echo "0.000000000000000000"; else echo "$1"; fi; }
REP_A_AFTER=$(carol_rep_for_tag "$UNHIDE_TAG_A")
REP_B_AFTER=$(carol_rep_for_tag "$UNHIDE_TAG_B")
echo "  carol rep on $UNHIDE_TAG_A after unhide: $REP_A_AFTER"
echo "  carol rep on $UNHIDE_TAG_B after unhide: $REP_B_AFTER"
assert_equal "carol's $UNHIDE_TAG_A rep unchanged across hide+unhide (no mint)" \
    "$(norm_rep "$REP_A_BEFORE")" "$(norm_rep "$REP_A_AFTER")"
assert_equal "carol's $UNHIDE_TAG_B rep unchanged across hide+unhide (no mint)" \
    "$(norm_rep "$REP_B_BEFORE")" "$(norm_rep "$REP_B_AFTER")"

# Anti-cycling: the commitment is NOT released at unhide time — it stays
# reserved until the original appeal deadline (hide height + 40 blocks).
COMMITTED_AFTER_UNHIDE=$(bob_committed_bond)
echo "  bob total_committed_bond after unhide: $COMMITTED_AFTER_UNHIDE"
COMMIT_DELTA=$((COMMITTED_AFTER_UNHIDE - COMMITTED_BASE))
assert_gt "bob's commitment still reserved after self-correct" "0" "$COMMIT_DELTA"

# Re-hide after self-correct: legitimate (fresh record), but it reserves a
# SECOND commit while the first is still locked — cycling is not free.
hide_collection "$T1_COLL_ID" "bob re-hides after self-correct (fresh record)"
T1_HIDE_REC_2=$HIDE_REC_ID
COMMITTED_AFTER_REHIDE=$(bob_committed_bond)
REHIDE_DELTA=$((COMMITTED_AFTER_REHIDE - COMMITTED_AFTER_UNHIDE))
assert_gt "re-hide reserves a second commit on top of the retained first" \
    "0" "$REHIDE_DELTA"

# Second self-correct (also restores the collection for the appeal test on
# a clean fixture below).
TX_OUT=$(send_tx collect unhide-content "$T1_HIDE_REC_2" --from bob)
assert_tx_success "bob self-corrects the re-hide" "$TX_OUT"

# Appealing a resolved (self-corrected) record must be rejected.
TX_OUT=$(send_tx collect appeal-hide "$T1_HIDE_REC_2" --from carol)
assert_tx_failure "carol cannot appeal a self-corrected (resolved) hide" "$TX_OUT"

# Sentinel moderation history: hide-records-by-sentinel returns bob's hides
# newest first. Earlier suites may have created hides by bob too, so assert
# containment and ordering of the two Test 1 records rather than exact count.
# proto3 omits zero-valued uint64 ids from CLI JSON — normalize with // "0".
BY_SENTINEL_IDS=$(query collect hide-records-by-sentinel "$BOB_ADDR" \
    | jq -r '[.hide_records[] | (.id // "0")] | join(" ")' 2>/dev/null)
echo "  bob's hide record ids (newest first): $BY_SENTINEL_IDS"
FIRST_ID=${BY_SENTINEL_IDS%% *}
assert_equal "hide-records-by-sentinel lists bob's newest hide first" \
    "$T1_HIDE_REC_2" "$FIRST_ID"
case " $BY_SENTINEL_IDS " in
    *" $T1_HIDE_REC_1 "*)
        echo "PASS: hide-records-by-sentinel includes bob's first Test 1 hide"
        TESTS_PASSED=$((TESTS_PASSED + 1))
        ;;
    *)
        echo "FAIL: hide-records-by-sentinel missing record $T1_HIDE_REC_1 (got: $BY_SENTINEL_IDS)"
        TESTS_FAILED=$((TESTS_FAILED + 1))
        ;;
esac

# carol never hid anything — her sentinel history is empty.
CAROL_HIDE_COUNT=$(query collect hide-records-by-sentinel "$CAROL_ADDR" \
    | jq -r '.hide_records | length' 2>/dev/null)
assert_equal "hide-records-by-sentinel is empty for a non-sentinel" "0" "$CAROL_HIDE_COUNT"

# EndBlocker releases the retained commitments at each record's ORIGINAL
# appeal deadline (~40 blocks after its hide; ~40-80s early-run, up to ~160s
# late-run). Poll the committed bond back to baseline.
echo "  Polling up to ~200s for the EndBlocker to release both retained commitments..."
if poll_committed_back_to "$COMMITTED_BASE" 200; then
    echo "PASS: bob's committed bond returned to baseline at the original deadlines"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    echo "FAIL: bob's committed bond still $(bob_committed_bond), baseline $COMMITTED_BASE"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# The collection survived the whole cycle.
T1_STATUS=$(query collect collection "$T1_COLL_ID" | jq -r '.collection.status // empty')
assert_equal "Collection still ACTIVE after both deadlines passed (no deletion)" \
    "COLLECTION_STATUS_ACTIVE" "$T1_STATUS"

# ----------------------------------------------------------------------------
# Test 2 + 3: authorization and appeal guards, on one HIDDEN fixture.
# ----------------------------------------------------------------------------
echo ""
echo "--- Test 2: Owner cannot unhide / Test 3: appealed hide cannot be unhidden ---"

TX_OUT=$(send_tx collect create-collection \
    nft public false 0 "UnhideGuardColl" "Guard fixture" "" "$UNHIDE_TAG_A" \
    --from carol)
assert_tx_success "carol creates guard-test collection" "$TX_OUT"
T2_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$T2_COLL_ID" ]; then
    T2_COLL_ID=$(resolve_collection_id "$CAROL_ADDR" "UnhideGuardColl")
fi

hide_collection "$T2_COLL_ID" "bob hides guard-test collection"
T2_HIDE_REC=$HIDE_REC_ID

# Test 2: the owner (carol) is not the hiding sentinel — rejected.
TX_OUT=$(send_tx collect unhide-content "$T2_HIDE_REC" --from carol)
assert_tx_failure "carol (owner) cannot unhide" "$TX_OUT"

# Test 3: once appealed, the jury owns the outcome. appeal_cooldown_blocks=2
# has long passed by the time the failed-unhide roundtrip (~12s) completes.
TX_OUT=$(send_tx collect appeal-hide "$T2_HIDE_REC" --from carol)
assert_tx_success "carol appeals the hide" "$TX_OUT"

TX_OUT=$(send_tx collect unhide-content "$T2_HIDE_REC" --from bob)
assert_tx_failure "bob cannot self-correct an appealed hide" "$TX_OUT"

# The appeal times out in carol's favor after appeal_deadline_blocks (=60);
# no need to wait for it here — the fixture resolves itself after the suite.

# ----------------------------------------------------------------------------
# Test 4: Window expiry — unhide after 20 blocks is rejected.
# ----------------------------------------------------------------------------
echo ""
echo "--- Test 4: Unhide after the window is rejected ---"

TX_OUT=$(send_tx collect create-collection \
    nft public false 0 "UnhideLateColl" "Window-expiry fixture" "" "" \
    --from carol)
assert_tx_success "carol creates window-expiry collection" "$TX_OUT"
T4_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$T4_COLL_ID" ]; then
    T4_COLL_ID=$(resolve_collection_id "$CAROL_ADDR" "UnhideLateColl")
fi

hide_collection "$T4_COLL_ID" "bob hides window-expiry collection"
T4_HIDE_REC=$HIDE_REC_ID

T4_HIDDEN_AT=$(query collect hide-record "$T4_HIDE_REC" \
    | jq -r '.hide_record.hidden_at // "0"')
echo "  hidden_at=$T4_HIDDEN_AT; waiting past block $((T4_HIDDEN_AT + 20)) (window) ..."

# Wait past hidden_at + window (20) but well before hidden_at + expiry (40):
# the rejection attempt must land inside that ~19-block gap, so poll height
# tightly rather than sleeping.
if wait_past_block $((T4_HIDDEN_AT + 20)) 90; then
    TX_OUT=$(send_tx collect unhide-content "$T4_HIDE_REC" --from bob)
    assert_tx_failure "bob cannot unhide after sentinel_unhide_window_blocks" "$TX_OUT"
else
    echo "FAIL: chain did not pass block $((T4_HIDDEN_AT + 20)) within 90s"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi
# The unappealed record deletes this fixture at hidden_at+40 — expected;
# no cleanup needed.

echo ""
print_summary
exit $?
