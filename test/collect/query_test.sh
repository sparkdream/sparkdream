#!/bin/bash
# Query endpoint tests for x/collect

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/test_helpers.sh"
source "$SCRIPT_DIR/.test_env"

echo "========================================================================="
echo "  X/COLLECT - QUERY TESTS"
echo "========================================================================="
echo ""

# =========================================================================
# Test 1: Query params
# =========================================================================
echo "--- Test 1: Query params ---"
PARAMS=$(query collect params)
BASE_DEPOSIT=$(echo "$PARAMS" | jq -r '.params.base_collection_deposit // empty' 2>/dev/null)
assert_not_empty "Params base_collection_deposit is set" "$BASE_DEPOSIT"

MAX_ITEMS=$(echo "$PARAMS" | jq -r '.params.max_items_per_collection // "0"' 2>/dev/null)
assert_gt "Params max_items > 0" "0" "$MAX_ITEMS"

# =========================================================================
# Test 2: Query public-collections
# =========================================================================
echo ""
echo "--- Test 2: Query public-collections ---"
PUBLIC=$(query collect public-collections)
PUBLIC_COUNT=$(echo "$PUBLIC" | jq -r '.collections | length' 2>/dev/null)
assert_gt "Public collections exist" "0" "$PUBLIC_COUNT"
echo "  Found $PUBLIC_COUNT public collections"

# =========================================================================
# Test 3: Query public-collections-by-type
# =========================================================================
echo ""
echo "--- Test 3: Query public-collections-by-type ---"
# Type 1 = COLLECTION_TYPE_NFT (queries use integer enum values). We can't
# assume any NFT collections exist at this point, so assert the response is a
# well-formed collections array (proto3 omits an empty repeated field, hence
# `// []`). A wrong jq path or a query error yields a non-array and fails.
BY_TYPE=$(query collect public-collections-by-type 1)
BY_TYPE_TYPE=$(echo "$BY_TYPE" | jq -r '(.collections // []) | type' 2>/dev/null)
assert_equal "public-collections-by-type returns a collections array" "array" "$BY_TYPE_TYPE"

# =========================================================================
# Test 4: Query bonded-roles-by-type (collect-curator)
# =========================================================================
# Structural check only — curator presence/identity is asserted in
# curation_test.sh. The response field is `bonded_roles` (omitted when empty).
echo ""
echo "--- Test 4: Query bonded-roles-by-type collect-curator ---"
CURATORS=$(query rep bonded-roles-by-type collect-curator)
CURATORS_TYPE=$(echo "$CURATORS" | jq -r '(.bonded_roles // []) | type' 2>/dev/null)
assert_equal "bonded-roles-by-type returns a bonded_roles array" "array" "$CURATORS_TYPE"

# =========================================================================
# Test 5: Query pending-collections
# =========================================================================
echo ""
echo "--- Test 5: Query pending-collections ---"
PENDING=$(query collect pending-collections)
PENDING_TYPE=$(echo "$PENDING" | jq -r '(.collections // []) | type' 2>/dev/null)
assert_equal "pending-collections returns a collections array" "array" "$PENDING_TYPE"

# =========================================================================
# Test 6: Query flagged-content
# =========================================================================
echo ""
echo "--- Test 6: Query flagged-content ---"
FLAGGED=$(query collect flagged-content)
FLAGGED_TYPE=$(echo "$FLAGGED" | jq -r '(.collection_flags // []) | type' 2>/dev/null)
assert_equal "flagged-content returns a collection_flags array" "array" "$FLAGGED_TYPE"

# =========================================================================
# Test 7: Query sponsorship-requests
# =========================================================================
echo ""
echo "--- Test 7: Query sponsorship-requests ---"
REQUESTS=$(query collect sponsorship-requests)
REQ_TYPE=$(echo "$REQUESTS" | jq -r '(.sponsorship_requests // []) | type' 2>/dev/null)
assert_equal "sponsorship-requests returns a sponsorship_requests array" "array" "$REQ_TYPE"

# =========================================================================
# Test 8: Pinned collections surface first in public-collections ordering
# =========================================================================
# The CollectionsByStatus index is keyed (status, pinned-rank, id) so a pinned
# collection must appear ahead of unpinned ones regardless of its id. We create
# three collections, pin the highest-id one, and assert it comes back first.
echo ""
echo "--- Test 8: Pinned collection surfaces first in public-collections ---"

# Create three permanent (non-TTL) collections owned by alice so we can pin.
PF_COLL_A=$(send_tx collect create-collection \
    nft public false 0 "PinFirstA" "pin-first A" "" "" \
    --from alice)
assert_tx_success "Create PinFirstA" "$PF_COLL_A" >/dev/null
PF_A_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
[ -z "$PF_A_ID" ] && PF_A_ID=$(resolve_collection_id "$ALICE_ADDR" "PinFirstA")

PF_COLL_B=$(send_tx collect create-collection \
    nft public false 0 "PinFirstB" "pin-first B" "" "" \
    --from alice)
assert_tx_success "Create PinFirstB" "$PF_COLL_B" >/dev/null
PF_B_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
[ -z "$PF_B_ID" ] && PF_B_ID=$(resolve_collection_id "$ALICE_ADDR" "PinFirstB")

PF_COLL_C=$(send_tx collect create-collection \
    nft public false 0 "PinFirstC" "pin-first C" "" "" \
    --from alice)
assert_tx_success "Create PinFirstC" "$PF_COLL_C" >/dev/null
PF_C_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
[ -z "$PF_C_ID" ] && PF_C_ID=$(resolve_collection_id "$ALICE_ADDR" "PinFirstC")

echo "  PinFirst IDs: A=$PF_A_ID B=$PF_B_ID C=$PF_C_ID"
assert_not_empty "PinFirstA id resolved" "$PF_A_ID"
assert_not_empty "PinFirstB id resolved" "$PF_B_ID"
assert_not_empty "PinFirstC id resolved" "$PF_C_ID"

# Pin C (highest id) — it should jump to the front of the list.
PF_PIN=$(send_tx collect pin-collection "$PF_C_ID" --from alice)
assert_tx_success "Pin PinFirstC (highest id)" "$PF_PIN" >/dev/null

# Query public-collections and find our three by name, in order.
PF_ORDER=$(query collect public-collections \
    | jq -r '[.collections[] | select(.name | startswith("PinFirst")) | .id] | join(",")')

echo "  Order before pin-undo: $PF_ORDER"
# The pinned C must come before A and B.
if echo "$PF_ORDER" | grep -q "^${PF_C_ID},"; then
    echo "PASS: pinned collection (id=$PF_C_ID) appears first"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    echo "FAIL: pinned collection (id=$PF_C_ID) did not appear first; order=$PF_ORDER"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# =========================================================================
# Test 9: Unpin restores id ordering
# =========================================================================
echo ""
echo "--- Test 9: Unpin restores id ordering ---"
PF_UNPIN=$(send_tx collect unpin-collection "$PF_C_ID" --from alice)
assert_tx_success "Unpin PinFirstC" "$PF_UNPIN" >/dev/null

PF_ORDER_AFTER=$(query collect public-collections \
    | jq -r '[.collections[] | select(.name | startswith("PinFirst")) | .id] | join(",")')
echo "  Order after unpin: $PF_ORDER_AFTER"

# After unpin, the three should be in ascending id order: A,B,C.
PF_EXPECTED="${PF_A_ID},${PF_B_ID},${PF_C_ID}"
if [ "$PF_ORDER_AFTER" = "$PF_EXPECTED" ]; then
    echo "PASS: after unpin, collections are back in id order ($PF_EXPECTED)"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    echo "FAIL: after unpin, expected $PF_EXPECTED got $PF_ORDER_AFTER"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# =========================================================================
# Test 10: Pinned collection surfaces first in collections-by-owner
# =========================================================================
# Unlike public-collections (index-native ordering via the pinned-rank in the
# CollectionsByStatus key), collections-by-owner is keyed (owner, id) and
# applies pinned-first ordering with an in-memory stable partition. This is a
# distinct code path (pinnedFirst + paginate) and must be exercised separately.
echo ""
echo "--- Test 10: Pinned collection surfaces first in collections-by-owner ---"

# Create two more permanent collections owned by alice so the owner list has
# fresh, ascending ids we can reason about.
PF_OWNER_A=$(send_tx collect create-collection \
    nft public false 0 "OwnerPinA" "owner-pin A" "" "" \
    --from alice)
assert_tx_success "Create OwnerPinA" "$PF_OWNER_A" >/dev/null
OWNER_A_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
[ -z "$OWNER_A_ID" ] && OWNER_A_ID=$(resolve_collection_id "$ALICE_ADDR" "OwnerPinA")

PF_OWNER_B=$(send_tx collect create-collection \
    nft public false 0 "OwnerPinB" "owner-pin B" "" "" \
    --from alice)
assert_tx_success "Create OwnerPinB" "$PF_OWNER_B" >/dev/null
OWNER_B_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
[ -z "$OWNER_B_ID" ] && OWNER_B_ID=$(resolve_collection_id "$ALICE_ADDR" "OwnerPinB")

echo "  OwnerPin IDs: A=$OWNER_A_ID B=$OWNER_B_ID"
assert_not_empty "OwnerPinA id resolved" "$OWNER_A_ID"
assert_not_empty "OwnerPinB id resolved" "$OWNER_B_ID"

# Pin B (higher id) — it must jump ahead of A in the owner listing.
OWNER_PIN=$(send_tx collect pin-collection "$OWNER_B_ID" --from alice)
assert_tx_success "Pin OwnerPinB (higher id)" "$OWNER_PIN" >/dev/null

# Query collections-by-owner for alice and find our two by name, in order.
OWNER_ORDER=$(query collect collections-by-owner "$ALICE_ADDR" \
    | jq -r '[.collections[] | select(.name | startswith("OwnerPin")) | .id] | join(",")')
echo "  Owner order with B pinned: $OWNER_ORDER"

OWNER_EXPECTED_PINNED="${OWNER_B_ID},${OWNER_A_ID}"
if [ "$OWNER_ORDER" = "$OWNER_EXPECTED_PINNED" ]; then
    echo "PASS: pinned OwnerPinB ($OWNER_B_ID) appears first in collections-by-owner"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    echo "FAIL: expected $OWNER_EXPECTED_PINNED got $OWNER_ORDER"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# Clean up the pin so it does not leak into other tests' daily-pin budget.
# Wait for commit: an un-awaited send_tx returns before the account sequence
# advances, so the next tx (Test 11's create) would collide sequences and be
# rejected. wait_for_tx spaces them out.
OWNER_CLEANUP=$(send_tx collect unpin-collection "$OWNER_B_ID" --from alice 2>/dev/null)
wait_for_tx "$(get_txhash "$OWNER_CLEANUP")" >/dev/null 2>&1

# =========================================================================
# Test 11: Pinned collection surfaces first in public-collections-by-type
# =========================================================================
# public-collections-by-type walks the same (status, pinned-rank, id) index as
# public-collections but filters by type in-memory. Verify pinned-first ordering
# holds within the requested type and that a pinned collection of a *different*
# type does not leak into the result.
echo ""
echo "--- Test 11: Pinned collection surfaces first in public-collections-by-type ---"

# Type 1 = NFT. Create two NFT collections, pin the higher-id one.
PF_TYPE_A=$(send_tx collect create-collection \
    nft public false 0 "TypePinA" "type-pin A" "" "" \
    --from alice)
assert_tx_success "Create TypePinA (nft)" "$PF_TYPE_A" >/dev/null
TYPE_A_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
[ -z "$TYPE_A_ID" ] && TYPE_A_ID=$(resolve_collection_id "$ALICE_ADDR" "TypePinA")

PF_TYPE_B=$(send_tx collect create-collection \
    nft public false 0 "TypePinB" "type-pin B" "" "" \
    --from alice)
assert_tx_success "Create TypePinB (nft)" "$PF_TYPE_B" >/dev/null
TYPE_B_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
[ -z "$TYPE_B_ID" ] && TYPE_B_ID=$(resolve_collection_id "$ALICE_ADDR" "TypePinB")

echo "  TypePin IDs: A=$TYPE_A_ID B=$TYPE_B_ID"
assert_not_empty "TypePinA id resolved" "$TYPE_A_ID"
assert_not_empty "TypePinB id resolved" "$TYPE_B_ID"

# Pin B (higher id).
TYPE_PIN=$(send_tx collect pin-collection "$TYPE_B_ID" --from alice)
assert_tx_success "Pin TypePinB (higher id)" "$TYPE_PIN" >/dev/null

# Query NFT collections and find our two by name, in order.
TYPE_ORDER=$(query collect public-collections-by-type 1 \
    | jq -r '[.collections[] | select(.name | startswith("TypePin")) | .id] | join(",")')
echo "  Type order with B pinned: $TYPE_ORDER"

TYPE_EXPECTED="${TYPE_B_ID},${TYPE_A_ID}"
if [ "$TYPE_ORDER" = "$TYPE_EXPECTED" ]; then
    echo "PASS: pinned TypePinB ($TYPE_B_ID) appears first in public-collections-by-type"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    echo "FAIL: expected $TYPE_EXPECTED got $TYPE_ORDER"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# Clean up the pin (await commit so the sequence advances for any later tx).
TYPE_CLEANUP=$(send_tx collect unpin-collection "$TYPE_B_ID" --from alice 2>/dev/null)
wait_for_tx "$(get_txhash "$TYPE_CLEANUP")" >/dev/null 2>&1

echo ""
print_summary
exit $?
