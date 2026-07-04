#!/bin/bash
# ============================================================================
# x/collect: Endorsement slash + deferral regression + rep deduction
# ============================================================================
# Four tests covering the slash semantics around sentinel hides:
#
#   Test 1: Unappealed sentinel hide → endorser DREAM is BURNED and the
#           endorser's per-tag reputation is DEDUCTED on each of the
#           collection's tags. Closes the original loophole where
#           deleteCollectionFull's blanket unlock-on-endorsed-delete refunded
#           the endorser whenever the owner declined to appeal (rational
#           bad-faith owners never appeal a justified hide). The rep half
#           covers the side-effect added when EndorserRepPenalty was wired
#           into the same slash path so a slashed voucher's tag-space
#           authority degrades alongside their DREAM stake.
#
#   Test 2: MsgDeleteCollection on a HIDDEN collection is REJECTED.
#           Closes the forfeit-by-delete escape hatch where the owner could
#           simply delete the collection after a sentinel hide to dodge the
#           slash decision. The owner's only recourse for a HIDDEN collection
#           is MsgAppealHide.
#
#   Test 3: Appeal in flight at TTL → §10.1 DEFERS deletion until resolution.
#           Closes the second escape hatch where the owner could file a
#           frivolous appeal and let TTL race past the appeal deadline,
#           leaving the endorser refunded under the prior suppress-on-appeal
#           rule. Deferral honors the appeal process: the endorser pays (or
#           doesn't) based on the actual verdict, not which timer fired.
#
#   Test 4: Sentinel hide on a MEMBER-owned tagged collection deducts the
#           AUTHOR's per-tag rep alongside SlashAuthorBond. Mirrors Test 1's
#           rep half but on the author-slash path (eager, at hide time)
#           instead of the endorser-slash path (deferred, at delete time).
#
# Account roles:
#   collect_slash_owner — dedicated fresh non-member collection owner for
#                Tests 1-3 (PENDING status until endorsed), created + funded
#                in-test so its tiered collection cap is never a factor
#   nonmember1 — (no longer used here; superseded by collect_slash_owner)
#   alice      — endorser (CORE genesis trust, satisfies min_sponsor_trust)
#   bob        — bonded content-sentinel (ESTABLISHED genesis trust + 25k DREAM,
#                bondable without rep bootstrap)
#
# Requires config.yml params overrides (at ~1s/block these are ~40s/~2s/~60s):
#   collect.hide_expiry_blocks:     "40"   # prod default 100800 ~7 days
#   collect.appeal_cooldown_blocks: "2"    # prod default 600    ~1 hour
#   collect.appeal_deadline_blocks: "60"   # prod default 201600 ~14 days
# ============================================================================

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/test_helpers.sh"
source "$SCRIPT_DIR/.test_env"

echo "========================================================================="
echo "  X/COLLECT - ENDORSEMENT SLASH (UNAPPEALED-HIDE) TESTS"
echo "========================================================================="
echo ""

BOB_ADDR=$(get_address bob)
if [ -z "$BOB_ADDR" ]; then
    echo "FAIL: bob key not found in keyring"
    exit 1
fi
echo "Bob address: $BOB_ADDR"

# ----------------------------------------------------------------------------
# Helpers: dream_balance and staked_dream readers.
#
# Note on signal choice: dream_balance is affected by the periodic decay that
# ApplyPendingDecay applies inside Lock/Unlock/Burn, so strict balance equality
# isn't reliable. staked_dream is the cleanest signal — LockDREAM bumps it by
# exactly the lock amount and Unlock/slash decrement it by exactly the unlock
# amount. Staked decay (~0.05%/epoch, epoch ≈ 10 blocks) is applied lazily on
# every read and scales with the WHOLE staked balance — which, by the time this
# suite runs after the earlier collect tests, is on the order of ~1.6k DREAM.
# Over this test's longest measurement window (~80 blocks ≈ 8 epochs) that is
# ~0.4% ≈ 6-7 DREAM, so the ~100 DREAM lock/unlock deltas are asserted with a
# generous 15M (15 DREAM) slack rather than a tight ±1M.
# ----------------------------------------------------------------------------
alice_dream_balance() {
    $BINARY query rep get-member "$ALICE_ADDR" -o json 2>/dev/null \
        | jq -r '.member.dream_balance // "0"'
}

alice_staked_dream() {
    $BINARY query rep get-member "$ALICE_ADDR" -o json 2>/dev/null \
        | jq -r '.member.staked_dream // "0"'
}

# ----------------------------------------------------------------------------
# Helper: read alice's reputation_scores entry for a specific tag.
# Returns "absent" if the map has no entry for that tag, otherwise the
# stringified LegacyDec score (e.g. "0.000000000000000000"). Used to assert
# the per-tag rep deduction side-effect on the endorser slash flow.
# ----------------------------------------------------------------------------
alice_rep_for_tag() {
    local TAG=$1
    $BINARY query rep get-member "$ALICE_ADDR" -o json 2>/dev/null \
        | jq -r --arg t "$TAG" '.member.reputation_scores[$t] // "absent"'
}

# ----------------------------------------------------------------------------
# poll_collection_deleted <collection_id> <max_seconds>
#
# Polls the collection query until it is gone — a "not found" error string OR a
# response that no longer decodes to a status. Returns 0 once deleted, 1 on
# timeout. Used in place of fixed sleeps because this suite's block rate
# degrades from ~1s/block early to ~2s/block late (LevelDB growth), so
# block-count windows like hide_expiry_blocks / appeal_deadline_blocks map to a
# moving wall-clock target — a fixed sleep that is long enough early is too
# short late. Polling tolerates the drift.
# ----------------------------------------------------------------------------
poll_collection_deleted() {
    local id=$1 max=$2 waited=0 q status
    while [ "$waited" -lt "$max" ]; do
        q=$(query collect collection "$id" 2>&1)
        if echo "$q" | grep -qE "not found|NotFound"; then
            return 0
        fi
        status=$(echo "$q" | jq -r '.collection.status // empty' 2>/dev/null)
        if [ -z "$status" ]; then
            return 0
        fi
        sleep 6
        waited=$((waited + 6))
    done
    return 1
}

# ----------------------------------------------------------------------------
# Bond bob as a forum sentinel (idempotent).
#
# Bonding is the gate that lets bob call hide-content. 2500 micro-DREAM bond
# is the same amount forum/sentinel_test.sh uses — comfortably above the
# 500 DREAM content-sentinel minimum. Skipped if bob is already bonded from a
# prior test run.
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

# Confirm the bonded-role status — hide-content will reject if not NORMAL.
BOND_STATUS=$($BINARY query rep bonded-role content-sentinel "$BOB_ADDR" -o json 2>/dev/null \
    | jq -r '.bonded_role.bond_status // "unknown"')
assert_equal "bob's bonded-role status is NORMAL" "BONDED_ROLE_STATUS_NORMAL" "$BOND_STATUS"

# ----------------------------------------------------------------------------
# Dedicated fresh non-member collection owner for Tests 1-3.
#
# Each of these tests creates a PENDING TTL collection owned by a non-member.
# With hide_expiry_blocks=40 a HIDDEN collection lingers ~40s before §10.3
# prunes it, so Test 2's fixture is still alive when Test 3 creates. Reusing
# the suite-shared `nonmember1` (which already carries collections from earlier
# collect tests) would push it over its tiered collection cap
# (MaxCollectionsBase=5) and trip ErrMaxCollections (1103). A fresh owner starts
# at zero and never holds more than two collections at once (Test 1's is deleted
# before Test 2's, Test 3 adds one more), so the cap is never a factor
# regardless of suite ordering.
# ----------------------------------------------------------------------------
SLASH_OWNER_ACCT="collect_slash_owner"
if ! $BINARY keys show "$SLASH_OWNER_ACCT" --keyring-backend "$KEYRING" >/dev/null 2>&1; then
    $BINARY keys add "$SLASH_OWNER_ACCT" --keyring-backend "$KEYRING" >/dev/null 2>&1
fi
SLASH_OWNER_ADDR=$(get_address "$SLASH_OWNER_ACCT")
echo "  Slash-test collection owner: $SLASH_OWNER_ADDR"
# Fund with SPARK for tx fees + collection creation (mirrors setup's nonmember1
# funding; non-member creation needs no DREAM). Re-funding on rerun is harmless.
TX_OUT=$(send_tx bank send alice "$SLASH_OWNER_ADDR" "200000000${BOND_DENOM}" --from alice)
assert_tx_success "Fund slash-test collection owner with SPARK" "$TX_OUT"

# ----------------------------------------------------------------------------
# Test 1: Unappealed sentinel hide on an endorsed collection burns the
# endorser's DREAM stake.
# ----------------------------------------------------------------------------
echo ""
echo "--- Test 1: Unappealed sentinel hide → endorser DREAM slashed ---"

BLOCK_HEIGHT=$(get_block_height)
# Pick a TTL well past hide_expiry (=40 in test config) so the §10.3 unappealed
# hide path wins the prune race. Both delete paths now slash via
# deleteCollectionFull, so either ordering would work — but having §10.3 win
# keeps this test's scope to the originally-reported loophole.
FUTURE_BLOCK=$((BLOCK_HEIGHT + 5000))

# Tag the collection so the slash path's per-tag rep deduction can be
# observed end-to-end. commons-council and technical-council are seeded in
# the rep tag_map at genesis (config.yml ~L300) and are not reserved — same
# pair tag_test.sh uses for positive attachment.
SLASH_TAG_A="commons-council"
SLASH_TAG_B="technical-council"

# Capture baseline staked_dream BEFORE endorsement. LockDREAM in endorse
# bumps staked by exactly EndorsementDreamStake; the slash path on hide
# expiry calls UnlockDREAM+BurnDREAM, dropping staked back. dream_balance
# is also captured for diagnostics but isn't strictly compared (decay).
STAKED_BEFORE=$(alice_staked_dream)
BAL_BEFORE=$(alice_dream_balance)
echo "  Alice staked_dream before endorse: $STAKED_BEFORE"
echo "  Alice DREAM before endorse: $BAL_BEFORE"

# Capture baseline per-tag rep on the slash tags so we can confirm
# DeductReputation fired during the §10.3 prune. The deduction floors at
# zero, so any pre-existing rep on these tags drops by EndorserRepPenalty
# (default 10) or to 0 — and if Alice has no entry yet, the deduction CREATES
# the map entry at "0.000000000000000000" (floor-of-zero-minus-10). The
# "entry exists after slash" signal is what we assert.
REP_A_BEFORE=$(alice_rep_for_tag "$SLASH_TAG_A")
REP_B_BEFORE=$(alice_rep_for_tag "$SLASH_TAG_B")
echo "  Alice rep on $SLASH_TAG_A before slash: $REP_A_BEFORE"
echo "  Alice rep on $SLASH_TAG_B before slash: $REP_B_BEFORE"

# nonmember1 creates a PENDING TTL collection seeking endorsement.
TX_OUT=$(send_tx collect create-collection \
    nft public false "$FUTURE_BLOCK" "SlashColl" "Bad content fixture" "" "$SLASH_TAG_A,$SLASH_TAG_B" \
    --from "$SLASH_OWNER_ACCT")
assert_tx_success "nonmember1 creates PENDING TTL collection" "$TX_OUT"

SLASH_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$SLASH_COLL_ID" ]; then
    DATA=$(query collect collections-by-owner "$SLASH_OWNER_ADDR")
    SLASH_COLL_ID=$(echo "$DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi
echo "  Slash-test collection ID: $SLASH_COLL_ID"

TX_OUT=$(send_tx collect set-seeking-endorsement "$SLASH_COLL_ID" true --from "$SLASH_OWNER_ACCT")
assert_tx_success "nonmember1 sets seeking-endorsement=true" "$TX_OUT"

# alice endorses — this locks 100 DREAM via repKeeper.LockDREAM, which bumps
# staked_dream by exactly the lock amount.
TX_OUT=$(send_tx collect endorse-collection "$SLASH_COLL_ID" --from alice)
assert_tx_success "alice endorses (locks 100 DREAM)" "$TX_OUT"

STAKED_AFTER_ENDORSE=$(alice_staked_dream)
ENDORSE_LOCK_DELTA=$((STAKED_AFTER_ENDORSE - STAKED_BEFORE))
# Allow 15M slack for staked decay applied during the lock (see header note).
assert_gt "Alice staked_dream increased by ~100 DREAM on endorse" \
    "85000000" "$ENDORSE_LOCK_DELTA"

# bob (bonded sentinel) hides the collection. autocli expects enum string
# values for target-type (FlagTargetType) and reason-code (ModerationReason).
TX_OUT=$(send_tx collect hide-content "$SLASH_COLL_ID" collection spam "spam" --from bob)
assert_tx_success "bob hides the endorsed collection" "$TX_OUT"

# Confirm status flipped to HIDDEN — the slash branch in deleteCollectionFull
# keys on this status.
HIDDEN_STATUS=$(query collect collection "$SLASH_COLL_ID" \
    | jq -r '.collection.status // empty' 2>/dev/null)
assert_equal "Collection status is HIDDEN after sentinel hide" \
    "COLLECTION_STATUS_HIDDEN" "$HIDDEN_STATUS"

# Poll past hide_expiry_blocks (=40) for §10.3 to fire and delete+slash the
# unappealed hide. The owner deliberately never appeals. Polling (vs a fixed
# sleep) absorbs the suite's late-run block-rate slowdown (~2s/block).
echo "  Polling up to ~150s for hide-expiry EndBlocker pass..."
if poll_collection_deleted "$SLASH_COLL_ID" 150; then
    echo "PASS: collection deleted after unappealed hide expiry"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    POST_STATUS=$(query collect collection "$SLASH_COLL_ID" | jq -r '.collection.status // empty' 2>/dev/null)
    echo "FAIL: collection still queryable, status=$POST_STATUS"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# THE KEY ASSERTION: the slash path UnlockDREAM+BurnDREAMs the 100 DREAM lock,
# so staked_dream must return to its pre-endorse level. Pre-fix, UnlockDREAM
# would have fired instead (else-branch) and staked would also drop — but
# dream_balance would be left alone. The rep deduction below distinguishes
# the two branches definitively (no rep deduction on the else-branch).
STAKED_AFTER_SLASH=$(alice_staked_dream)
BAL_AFTER_SLASH=$(alice_dream_balance)
echo "  Alice staked_dream after expiry: $STAKED_AFTER_SLASH"
echo "  Alice DREAM after expiry: $BAL_AFTER_SLASH"
SLASH_UNLOCK_DELTA=$((STAKED_AFTER_ENDORSE - STAKED_AFTER_SLASH))
assert_gt "Alice staked_dream decreased by ~100 DREAM on slash unlock" \
    "85000000" "$SLASH_UNLOCK_DELTA"

# Rep-deduction side-effect: §10.3 routed through deleteCollectionFull, which
# burns the endorser's stake AND fires DeductReputation per collection tag.
# Pre-fix, no rep call fired here at all. The deduction either drops a
# pre-existing score by EndorserRepPenalty (floored at zero) or — if Alice
# had no entry — creates one at "0.000000000000000000". Either way, the
# tag entry MUST exist in her reputation_scores map after the slash.
REP_A_AFTER=$(alice_rep_for_tag "$SLASH_TAG_A")
REP_B_AFTER=$(alice_rep_for_tag "$SLASH_TAG_B")
echo "  Alice rep on $SLASH_TAG_A after slash: $REP_A_AFTER"
echo "  Alice rep on $SLASH_TAG_B after slash: $REP_B_AFTER"
if [ "$REP_A_AFTER" = "absent" ]; then
    echo "FAIL: Alice's reputation_scores has no entry for $SLASH_TAG_A — DeductReputation didn't fire on the endorser slash path"
    TESTS_FAILED=$((TESTS_FAILED + 1))
else
    echo "PASS: Alice's $SLASH_TAG_A rep entry present after slash (value=$REP_A_AFTER)"
    TESTS_PASSED=$((TESTS_PASSED + 1))
fi
if [ "$REP_B_AFTER" = "absent" ]; then
    echo "FAIL: Alice's reputation_scores has no entry for $SLASH_TAG_B — DeductReputation didn't fire on the endorser slash path"
    TESTS_FAILED=$((TESTS_FAILED + 1))
else
    echo "PASS: Alice's $SLASH_TAG_B rep entry present after slash (value=$REP_B_AFTER)"
    TESTS_PASSED=$((TESTS_PASSED + 1))
fi

# ----------------------------------------------------------------------------
# Test 2: MsgDeleteCollection on a HIDDEN collection is rejected.
#
# Without this gate the owner could simply delete a sentinel-hidden
# collection to escape the slash decision (forfeit-by-delete). The only
# recourse for a hidden collection's owner must be MsgAppealHide; if the
# appeal upholds, status restores to ACTIVE and delete becomes available.
# ----------------------------------------------------------------------------
echo ""
echo "--- Test 2: Owner cannot delete a HIDDEN collection ---"

BAL_BEFORE_T2=$(alice_dream_balance)
STAKED_BEFORE_T2=$(alice_staked_dream)

BLOCK_HEIGHT=$(get_block_height)
T2_TTL=$((BLOCK_HEIGHT + 1000))  # Long TTL — the test resolves before TTL.

TX_OUT=$(send_tx collect create-collection \
    nft public false "$T2_TTL" "DeleteBlockColl" "Hide-and-try-delete fixture" "" "" \
    --from "$SLASH_OWNER_ACCT")
assert_tx_success "nonmember1 creates PENDING TTL collection (Test 2)" "$TX_OUT"
T2_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$T2_COLL_ID" ]; then
    DATA=$(query collect collections-by-owner "$SLASH_OWNER_ADDR")
    T2_COLL_ID=$(echo "$DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi

TX_OUT=$(send_tx collect set-seeking-endorsement "$T2_COLL_ID" true --from "$SLASH_OWNER_ACCT")
assert_tx_success "nonmember1 sets seeking-endorsement=true (Test 2)" "$TX_OUT"

TX_OUT=$(send_tx collect endorse-collection "$T2_COLL_ID" --from alice)
assert_tx_success "alice endorses (Test 2)" "$TX_OUT"

TX_OUT=$(send_tx collect hide-content "$T2_COLL_ID" collection spam "spam" --from bob)
assert_tx_success "bob hides the endorsed collection (Test 2)" "$TX_OUT"

# Owner attempts MsgDeleteCollection — must be rejected.
TX_OUT=$(send_tx collect delete-collection "$T2_COLL_ID" --from "$SLASH_OWNER_ACCT")
assert_tx_failure "nonmember1 cannot delete a HIDDEN collection" "$TX_OUT"

# Collection still exists (owner's delete failed).
T2_STATUS=$(query collect collection "$T2_COLL_ID" | jq -r '.collection.status // empty')
assert_equal "Collection still HIDDEN after rejected delete" \
    "COLLECTION_STATUS_HIDDEN" "$T2_STATUS"

# Alice's lock is intact, no slash or unlock has fired yet (Test 2 leaves the
# collection in HIDDEN; Test 3 wraps it up via timeout). staked_dream should
# be up by ~100 DREAM from the T2 endorse and not back down.
BAL_AFTER_T2=$(alice_dream_balance)
STAKED_AFTER_T2=$(alice_staked_dream)
T2_STAKED_DELTA=$((STAKED_AFTER_T2 - STAKED_BEFORE_T2))
assert_gt "Alice staked_dream up by ~100 DREAM (lock intact post-rejected-delete)" \
    "85000000" "$T2_STAKED_DELTA"

# Resolve Test 2's HIDDEN collection before Test 3 begins. With
# hide_expiry_blocks=40 its unappealed hide finalizes (§10.3) and BURNS Alice's
# Test-2 endorsement lock. If that slash landed mid-Test-3 it would cancel out
# Test 3's own +100 DREAM lock and corrupt the staked_dream deltas. Polling
# until it is gone makes Test 3's baseline clean. Cleanup, not an assertion.
echo "  Polling up to ~150s for Test 2's hide to finalize (clean baseline for Test 3)..."
if ! poll_collection_deleted "$T2_COLL_ID" 150; then
    echo "  WARN: Test 2 collection $T2_COLL_ID still present; Test 3 staked deltas may be noisy"
fi

# ----------------------------------------------------------------------------
# Test 3: Appeal in flight when TTL fires → §10.1 defers deletion until the
# appeal resolves. Mirrors the unit tests
# TestPruneExpiredCollections_HiddenEndorsed_AppealPending_DefersDeletion and
# TestPruneExpiredCollections_HiddenEndorsed_AppealUpheld_Unlocks.
#
# Sequence:
#   (a) Create short-TTL endorsed collection, sentinel hides it.
#   (b) Owner appeals after appeal_cooldown_blocks (=2).
#   (c) TTL elapses while appeal is still in flight — §10.1 must SKIP this
#       collection and emit collection_expiry_deferred (no delete, no burn).
#   (d) appeal_deadline_blocks (=60) elapses → §10.3a restores status to
#       ACTIVE (appellant wins by timeout — jury never ruled).
#   (e) Next §10.1 pass deletes the now-ACTIVE collection via the normal
#       unlock path. Alice's DREAM balance returns to the Test-3 baseline.
#
# Pre-deferral, step (c) would have deleted the collection and either
# burned the endorser (the simple HIDDEN→burn rule) or trivially unlocked
# them (the suppress-on-appeal rule, which was just a paid escape hatch).
# Deferral honors the appeal process: the endorser pays (or doesn't) based
# on the actual verdict instead of which timer fired first.
# ----------------------------------------------------------------------------
echo ""
echo "--- Test 3: Appeal-in-flight defers TTL deletion until resolution ---"

BAL_BEFORE_T3=$(alice_dream_balance)
STAKED_BEFORE_T3=$(alice_staked_dream)
echo "  Alice DREAM before Test 3: $BAL_BEFORE_T3"
echo "  Alice staked_dream before Test 3: $STAKED_BEFORE_T3"

BLOCK_HEIGHT=$(get_block_height)
# At ~1s/block, the 4 setup txs (create/seek/endorse/hide, ~6s each via
# TX_WAIT) + 18s appeal-cooldown sleep + appeal tx put us at ~+44 blocks when
# the appeal lands. TTL must fire AFTER the appeal (so §10.1 sees an in-flight
# appeal and DEFERS rather than deletes) but BEFORE the ~30s deferral check at
# ~+78. TTL = +60 sits squarely in that window (16-block margin on each side),
# and the appeal_deadline (appeal_block + 60 ≈ +104) stays well past the check
# so the collection is still HIDDEN-deferred when we assert it.
T3_TTL=$((BLOCK_HEIGHT + 60))

TX_OUT=$(send_tx collect create-collection \
    nft public false "$T3_TTL" "DeferredColl" "Appeal-defers-TTL fixture" "" "" \
    --from "$SLASH_OWNER_ACCT")
assert_tx_success "nonmember1 creates PENDING TTL collection (Test 3)" "$TX_OUT"
T3_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$T3_COLL_ID" ]; then
    DATA=$(query collect collections-by-owner "$SLASH_OWNER_ADDR")
    T3_COLL_ID=$(echo "$DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi

TX_OUT=$(send_tx collect set-seeking-endorsement "$T3_COLL_ID" true --from "$SLASH_OWNER_ACCT")
assert_tx_success "nonmember1 sets seeking-endorsement=true (Test 3)" "$TX_OUT"

TX_OUT=$(send_tx collect endorse-collection "$T3_COLL_ID" --from alice)
assert_tx_success "alice endorses (Test 3, locks 100 DREAM)" "$TX_OUT"

TX_OUT=$(send_tx collect hide-content "$T3_COLL_ID" collection spam "spam" --from bob)
assert_tx_success "bob hides the endorsed collection (Test 3)" "$TX_OUT"

T3_HIDE_REC=$(extract_event_attr "$TX_RESULT_OUT" "content_hidden" "hide_record_id")
if [ -z "$T3_HIDE_REC" ]; then
    T3_HIDE_REC=$(query collect hide-records-by-target "$T3_COLL_ID" collection \
        | jq -r '.hide_records[-1].id // empty' 2>/dev/null)
fi
echo "  Test 3 hide record ID: $T3_HIDE_REC"

# Wait past appeal_cooldown_blocks (=2 → ~12s with TX_WAIT=6).
echo "  Waiting ~18s for appeal cooldown..."
sleep 18

TX_OUT=$(send_tx collect appeal-hide "$T3_HIDE_REC" --from "$SLASH_OWNER_ACCT")
assert_tx_success "nonmember1 files appeal" "$TX_OUT"

# Give §10.1 a window to encounter the TTL while the appeal is in flight and
# DEFER (not delete). The collection stays HIDDEN throughout the appeal, so this
# assertion holds regardless of exactly when within the window the TTL fires;
# the definitive deferral evidence is that deletion only happens AFTER the
# appeal-deadline timeout below, not here.
echo "  Waiting ~30s before asserting the collection is still HIDDEN (deferred)..."
sleep 30

# Collection MUST still exist — deferred.
T3_STATUS=$(query collect collection "$T3_COLL_ID" | jq -r '.collection.status // empty')
assert_equal "Collection still HIDDEN (TTL deferred by in-flight appeal)" \
    "COLLECTION_STATUS_HIDDEN" "$T3_STATUS"

# Alice's lock still held — Test 3 endorse bumped staked by 100M and neither
# slash nor unlock has fired yet. staked_dream should still be up ~100 DREAM
# from the pre-Test-3 baseline.
BAL_DURING_T3=$(alice_dream_balance)
STAKED_DURING_T3=$(alice_staked_dream)
T3_DEFERRAL_DELTA=$((STAKED_DURING_T3 - STAKED_BEFORE_T3))
assert_gt "Alice staked_dream up by ~100 DREAM during deferral (lock held)" \
    "85000000" "$T3_DEFERRAL_DELTA"

# Now poll for appeal_deadline_blocks (=60) to elapse so §10.3a fires its
# timeout-favors-appellant branch, restoring status to ACTIVE, after which the
# next §10.1 pass deletes via the normal unlock path. The deadline is set to
# appeal_block + 60; at the suite's late-run ~2s/block that is ~120s of
# wall-clock after the appeal, so poll generously rather than sleep a fixed 75s.
echo "  Polling up to ~200s for appeal-deadline timeout + subsequent §10.1 delete..."
if poll_collection_deleted "$T3_COLL_ID" 200; then
    echo "PASS: collection deleted after appeal-deadline timeout"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    POST_STATUS=$(query collect collection "$T3_COLL_ID" | jq -r '.collection.status // empty' 2>/dev/null)
    echo "FAIL: collection still queryable post-timeout, status=$POST_STATUS"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# Appeal timed out in Alice's favor (sentinel didn't win), so the endorsement
# stake was UNLOCKED — staked_dream should drop back to the pre-Test-3 level
# (within decay slack). Pre-deferral, the TTL-during-appeal race would have
# burned the stake here.
BAL_AFTER_T3=$(alice_dream_balance)
STAKED_AFTER_T3=$(alice_staked_dream)
echo "  Alice DREAM after Test 3 resolution: $BAL_AFTER_T3"
echo "  Alice staked_dream after Test 3 resolution: $STAKED_AFTER_T3"
T3_UNLOCK_DELTA=$((STAKED_DURING_T3 - STAKED_AFTER_T3))
assert_gt "Alice staked_dream dropped by ~100 DREAM on appeal-timeout unlock" \
    "85000000" "$T3_UNLOCK_DELTA"

# ----------------------------------------------------------------------------
# Test 4: Sentinel hide on a MEMBER-owned tagged collection deducts the
# author's per-tag reputation alongside SlashAuthorBond. Tests 1–3 cover the
# endorser slash path on non-member content; this test exercises the parallel
# author rep-penalty path on member content.
#
# collector1 (PROVISIONAL trust, invited by alice in setup_test_accounts.sh)
# is the author. The hide fires SlashAuthorBond (best-effort — collector1 may
# have no bond) AND DeductReputation on each of the collection's tags.
#
# We check that collector1's reputation_scores map gains entries for the
# slash tags after the hide. The author has no pre-existing rep on these
# tags, so the deduction floor-at-zero behavior CREATES the map entries at
# "0.000000000000000000". Absence of those entries would mean the rep
# deduction never fired.
# ----------------------------------------------------------------------------
echo ""
echo "--- Test 4: Sentinel hide on member-owned collection deducts author rep ---"

# Author is carol — a PROVISIONAL genesis founder (key seeded from the
# config.yml mnemonic) with an empty reputation_scores map and zero outstanding
# collections from prior tests. Using carol avoids collector1's accumulated
# tiered-collection-limit pressure from earlier suite tests AND gives a clean
# "rep entry should be absent then present" signal.
CAROL_ADDR=$(get_address carol)
if [ -z "$CAROL_ADDR" ]; then
    echo "SKIP: carol key not found in keyring; skipping author-rep e2e"
else
    AUTHOR_TAG_A="commons-council"
    AUTHOR_TAG_B="ecosystem-council"

    # Capture pre-hide rep state for assertion contrast.
    AUTHOR_REP_A_BEFORE=$($BINARY query rep get-member "$CAROL_ADDR" -o json 2>/dev/null \
        | jq -r --arg t "$AUTHOR_TAG_A" '.member.reputation_scores[$t] // "absent"')
    AUTHOR_REP_B_BEFORE=$($BINARY query rep get-member "$CAROL_ADDR" -o json 2>/dev/null \
        | jq -r --arg t "$AUTHOR_TAG_B" '.member.reputation_scores[$t] // "absent"')
    echo "  carol rep on $AUTHOR_TAG_A before hide: $AUTHOR_REP_A_BEFORE"
    echo "  carol rep on $AUTHOR_TAG_B before hide: $AUTHOR_REP_B_BEFORE"

    # carol creates an ACTIVE tagged collection (no TTL, no endorsement).
    TX_OUT=$(send_tx collect create-collection \
        nft public false 0 "AuthorSlashColl" "Author rep deduction fixture" "" "$AUTHOR_TAG_A,$AUTHOR_TAG_B" \
        --from carol)
    assert_tx_success "carol creates a tagged ACTIVE collection" "$TX_OUT"

    AUTHOR_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
    if [ -z "$AUTHOR_COLL_ID" ]; then
        DATA=$(query collect collections-by-owner "$CAROL_ADDR")
        AUTHOR_COLL_ID=$(echo "$DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
    fi
    echo "  Author-slash collection ID: $AUTHOR_COLL_ID"

    # bob (bonded sentinel from the setup section above) hides the collection.
    TX_OUT=$(send_tx collect hide-content "$AUTHOR_COLL_ID" collection spam "spam" --from bob)
    assert_tx_success "bob hides the member-owned collection" "$TX_OUT"

    HIDDEN_STATUS=$(query collect collection "$AUTHOR_COLL_ID" \
        | jq -r '.collection.status // empty' 2>/dev/null)
    assert_equal "Member-owned collection is HIDDEN after sentinel hide" \
        "COLLECTION_STATUS_HIDDEN" "$HIDDEN_STATUS"

    # The rep deduction fires synchronously with MsgHideContent — no need to
    # wait on EndBlocker passes. Query right after.
    AUTHOR_REP_A_AFTER=$($BINARY query rep get-member "$CAROL_ADDR" -o json 2>/dev/null \
        | jq -r --arg t "$AUTHOR_TAG_A" '.member.reputation_scores[$t] // "absent"')
    AUTHOR_REP_B_AFTER=$($BINARY query rep get-member "$CAROL_ADDR" -o json 2>/dev/null \
        | jq -r --arg t "$AUTHOR_TAG_B" '.member.reputation_scores[$t] // "absent"')
    echo "  carol rep on $AUTHOR_TAG_A after hide: $AUTHOR_REP_A_AFTER"
    echo "  carol rep on $AUTHOR_TAG_B after hide: $AUTHOR_REP_B_AFTER"

    if [ "$AUTHOR_REP_A_AFTER" = "absent" ]; then
        echo "FAIL: carol's reputation_scores has no entry for $AUTHOR_TAG_A — author rep deduction didn't fire"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    else
        echo "PASS: carol's $AUTHOR_TAG_A rep entry present after hide (value=$AUTHOR_REP_A_AFTER)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    fi
    if [ "$AUTHOR_REP_B_AFTER" = "absent" ]; then
        echo "FAIL: carol's reputation_scores has no entry for $AUTHOR_TAG_B — author rep deduction didn't fire"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    else
        echo "PASS: carol's $AUTHOR_TAG_B rep entry present after hide (value=$AUTHOR_REP_B_AFTER)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    fi
fi

echo ""
print_summary
exit $?
