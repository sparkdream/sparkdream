#!/bin/bash
# ============================================================================
# x/collect: Pin / Unpin / MakePermanent strict separation
# ============================================================================
# Exercises the rework that splits MsgPinCollection's old "make permanent +
# burn deposit" lifecycle action into:
#   - MsgPinCollection / MsgUnpinCollection — display-only marker, requires
#     the target to already be permanent (ephemeral rejected with
#     ErrCannotPinEphemeral).
#   - MsgMakeCollectionPermanent — lifecycle promotion (sets expires_at=0 +
#     burns escrowed collection/item deposits). Idempotent on permanent.
#
# Uses alice (genesis ESTABLISHED) because pin_min_trust_level=2 ESTABLISHED;
# make_permanent_min_trust_level=1 PROVISIONAL so any active member would
# also work for the make-permanent path.
# ============================================================================

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/test_helpers.sh"
source "$SCRIPT_DIR/.test_env"

echo "========================================================================="
echo "  X/COLLECT - PIN / UNPIN / MAKE-PERMANENT TESTS"
echo "========================================================================="
echo ""

BLOCK_HEIGHT=$(get_block_height)
FUTURE_BLOCK=$((BLOCK_HEIGHT + 100000))

# ----------------------------------------------------------------------------
# Helper: create a TTL collection owned by alice, echo the new id.
# ----------------------------------------------------------------------------
create_ttl_collection() {
    local owner=$1
    local name=$2
    local owner_addr
    owner_addr=$(get_address "$owner")
    local tx_out
    tx_out=$(send_tx collect create-collection \
        nft public false "$FUTURE_BLOCK" "$name" "TTL fixture" "" "" \
        --from "$owner")
    assert_tx_success "Create TTL collection $name" "$tx_out" >/dev/null
    local txhash
    txhash=$(get_txhash "$tx_out")
    local res
    res=$(wait_for_tx "$txhash")
    local id
    id=$(extract_event_attr "$res" "collection_created" "id")
    if [ -z "$id" ]; then
        # Fallback: take the latest collection owned by this address.
        id=$(query collect collections-by-owner "$owner_addr" \
            | jq -r '(.collections[-1].id // 0) | tostring')
    fi
    echo "$id"
}

# ============================================================================
# TEST 1: MakeCollectionPermanent flips lifecycle and does NOT set pinned
# ============================================================================
echo "--- TEST 1: MakeCollectionPermanent flips lifecycle, leaves pinned=false ---"
COLL1_ID=$(create_ttl_collection alice "PinSepColl1")
echo "  Collection: $COLL1_ID"

TX_OUT=$(send_tx collect make-collection-permanent "$COLL1_ID" --from alice)
assert_tx_success "MakeCollectionPermanent succeeds on ephemeral target" "$TX_OUT"

COLL1=$(query collect collection "$COLL1_ID")
EXP=$(echo "$COLL1" | jq -r '.collection.expires_at // "0"')
PINNED=$(echo "$COLL1" | jq -r '.collection.pinned // false')
DEPOSIT_BURNED=$(echo "$COLL1" | jq -r '.collection.deposit_burned // false')
assert_equal "expires_at cleared"          "0"     "$EXP"
assert_equal "pinned marker NOT set"       "false" "$PINNED"
assert_equal "deposit marked burned"       "true"  "$DEPOSIT_BURNED"

# ============================================================================
# TEST 2: PinCollection rejects ephemeral targets
# ============================================================================
echo ""
echo "--- TEST 2: PinCollection rejects ephemeral target ---"
COLL2_ID=$(create_ttl_collection alice "PinSepColl2")

TX_OUT=$(send_tx collect pin-collection "$COLL2_ID" --from alice)
assert_tx_failure "Pin rejects ephemeral with ErrCannotPinEphemeral" "$TX_OUT"

# Sanity: collection is still ephemeral, not pinned.
COLL2=$(query collect collection "$COLL2_ID")
EXP2=$(echo "$COLL2" | jq -r '.collection.expires_at // "0"')
PINNED2=$(echo "$COLL2" | jq -r '.collection.pinned // false')
assert_equal "still ephemeral after rejected pin" "$FUTURE_BLOCK" "$EXP2"
assert_equal "still unpinned after rejected pin"  "false"          "$PINNED2"

# ============================================================================
# TEST 3: PinCollection succeeds on a permanent target (TEST 1's collection)
# ============================================================================
echo ""
echo "--- TEST 3: PinCollection succeeds on permanent target ---"
TX_OUT=$(send_tx collect pin-collection "$COLL1_ID" --from alice)
assert_tx_success "Pin succeeds on permanent collection" "$TX_OUT"

COLL1=$(query collect collection "$COLL1_ID")
PINNED1=$(echo "$COLL1" | jq -r '.collection.pinned // false')
EXP1=$(echo "$COLL1" | jq -r '.collection.expires_at // "0"')
assert_equal "pinned marker now set"     "true" "$PINNED1"
assert_equal "expires_at still 0"        "0"    "$EXP1"

# ============================================================================
# TEST 4: PinCollection is not idempotent (already-pinned errors)
# ============================================================================
echo ""
echo "--- TEST 4: PinCollection on already-pinned target fails ---"
TX_OUT=$(send_tx collect pin-collection "$COLL1_ID" --from alice)
assert_tx_failure "Pin on already-pinned errors (ErrCollectionAlreadyPinned)" "$TX_OUT"

# ============================================================================
# TEST 5: UnpinCollection clears the marker; re-pin works after unpin
# ============================================================================
echo ""
echo "--- TEST 5: UnpinCollection clears marker; subsequent re-pin works ---"
TX_OUT=$(send_tx collect unpin-collection "$COLL1_ID" --from alice)
assert_tx_success "Unpin succeeds" "$TX_OUT"

COLL1=$(query collect collection "$COLL1_ID")
PINNED1=$(echo "$COLL1" | jq -r '.collection.pinned // false')
assert_equal "pinned marker cleared" "false" "$PINNED1"

TX_OUT=$(send_tx collect pin-collection "$COLL1_ID" --from alice)
assert_tx_success "Pin after Unpin succeeds" "$TX_OUT"

# ============================================================================
# TEST 6: UnpinCollection on already-unpinned errors
# ============================================================================
echo ""
echo "--- TEST 6: UnpinCollection on unpinned target fails ---"
COLL6_ID=$(create_ttl_collection alice "PinSepColl6")
TX_OUT=$(send_tx collect make-collection-permanent "$COLL6_ID" --from alice)
assert_tx_success "Promote TEST 6 fixture" "$TX_OUT"

# Permanent, never pinned — Unpin must reject with ErrCollectionNotPinned.
TX_OUT=$(send_tx collect unpin-collection "$COLL6_ID" --from alice)
assert_tx_failure "Unpin on unpinned errors (ErrCollectionNotPinned)" "$TX_OUT"

# ============================================================================
# TEST 7: MakeCollectionPermanent is idempotent on already-permanent
# ============================================================================
echo ""
echo "--- TEST 7: MakeCollectionPermanent is idempotent on permanent target ---"
TX_OUT=$(send_tx collect make-collection-permanent "$COLL6_ID" --from alice)
assert_tx_success "MakePermanent on already-permanent is a no-op success" "$TX_OUT"

# ============================================================================
# TEST 8: MakeCollectionPermanent on non-existent collection fails
# ============================================================================
echo ""
echo "--- TEST 8: MakeCollectionPermanent rejects non-existent target ---"
TX_OUT=$(send_tx collect make-collection-permanent 9999999 --from alice)
assert_tx_failure "MakePermanent on missing collection errors" "$TX_OUT"

# ============================================================================
# TEST 9: MakeCollectionPermanent enforces MaxMakePermanentPerDay
# ============================================================================
# Default cap is 5/day. Alice has already consumed 3 slots:
#   TEST 1: COLL1 promotion (1)
#   TEST 6: COLL6 promotion (2)
#   TEST 7: idempotent re-call on COLL6 (3) — rate-limit gate fires before
#           the idempotent short-circuit, so the slot is consumed
# TEST 8's missing-collection failure consumed zero slots (collection
# lookup fails before the rate-limit gate). So 2 slots remain; we exhaust
# them with COLL9A/COLL9B and assert COLL9C is rejected by the dedicated
# MakePermanent counter, independent of MaxPinsPerDay.
echo ""
echo "--- TEST 9: MakeCollectionPermanent hits MaxMakePermanentPerDay cap ---"

# Sanity check: confirm the param matches our slot accounting before
# we drive the cap from the e2e side.
MAX_MAKE_PERM=$(query collect params | jq -r '.params.max_make_permanent_per_day // "0"')
echo "  max_make_permanent_per_day: $MAX_MAKE_PERM"
if [ "$MAX_MAKE_PERM" != "5" ]; then
    echo "  WARN: default cap changed; this test assumed 5 and that 3 slots were used by TESTS 1/6/7"
fi

COLL9A_ID=$(create_ttl_collection alice "PinSepColl9A")
COLL9B_ID=$(create_ttl_collection alice "PinSepColl9B")
COLL9C_ID=$(create_ttl_collection alice "PinSepColl9C")

TX_OUT=$(send_tx collect make-collection-permanent "$COLL9A_ID" --from alice)
assert_tx_success "MakePermanent slot 4 succeeds" "$TX_OUT"

TX_OUT=$(send_tx collect make-collection-permanent "$COLL9B_ID" --from alice)
assert_tx_success "MakePermanent slot 5 succeeds" "$TX_OUT"

TX_OUT=$(send_tx collect make-collection-permanent "$COLL9C_ID" --from alice)
assert_tx_failure "MakePermanent slot 6 hits MaxMakePermanentPerDay (ErrMaxDailyReactions)" "$TX_OUT"

# COLL9C must still be ephemeral after the rejected MakePermanent.
COLL9C=$(query collect collection "$COLL9C_ID")
EXP9C=$(echo "$COLL9C" | jq -r '.collection.expires_at // "0"')
assert_equal "rejected target stays ephemeral" "$FUTURE_BLOCK" "$EXP9C"

# Pin counter is independent — alice's prior pins (TESTS 3, 5) consumed 3
# of the default 10 pin slots, so a fresh pin on COLL9A (now permanent)
# must still succeed even though MakePermanent is exhausted.
TX_OUT=$(send_tx collect pin-collection "$COLL9A_ID" --from alice)
assert_tx_success "Pin succeeds despite MakePermanent exhaustion (independent counters)" "$TX_OUT"

# Pinned-first result ordering (public-collections, public-collections-by-type,
# collections-by-owner) is covered comprehensively in query_test.sh Tests 8-11;
# not duplicated here to keep this file focused on pin/make-permanent counters.

# ============================================================================
# Summary
# ============================================================================
echo ""
echo "========================================================================="
echo "  PIN / UNPIN / MAKE-PERMANENT TEST SUMMARY"
echo "========================================================================="
echo "  Passed:  $TESTS_PASSED"
echo "  Failed:  $TESTS_FAILED"
echo "  Skipped: $TESTS_SKIPPED"

if [ "$TESTS_FAILED" -gt 0 ]; then
    echo ""
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
fi

echo ""
echo ">>> ALL TESTS PASSED <<<"
exit 0
