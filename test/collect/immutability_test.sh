#!/bin/bash
# Immutability enforcement tests for x/collect
# Tests: endorsed collection mutations are blocked

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/test_helpers.sh"
source "$SCRIPT_DIR/.test_env"

echo "========================================================================="
echo "  X/COLLECT - IMMUTABILITY ENFORCEMENT TESTS"
echo "========================================================================="
echo ""

BLOCK_HEIGHT=$(get_block_height)
FUTURE_BLOCK=$((BLOCK_HEIGHT + 5000))

# =========================================================================
# Setup: Create a PENDING collection, add items, endorse it
# =========================================================================
echo "--- Setup: Create and endorse a collection with items ---"

# Create a PENDING TTL collection for nonmember1
TX_OUT=$(send_tx collect create-collection \
    nft public false "$FUTURE_BLOCK" "ImmutableColl" "For immutability tests" "" "" \
    --from nonmember1)
assert_tx_success "Create collection for immutability test" "$TX_OUT"

IMMUT_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$IMMUT_COLL_ID" ]; then
    DATA=$(query collect collections-by-owner "$NONMEMBER1_ADDR")
    IMMUT_COLL_ID=$(echo "$DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi
echo "  Collection ID: $IMMUT_COLL_ID"

# Add 2 items
TX_OUT=$(send_tx collect add-item "$IMMUT_COLL_ID" 0 "ImmutItem1" "First item" "" unspecified --from nonmember1)
assert_tx_success "Add item 1 to immutable collection" "$TX_OUT"
IMMUT_ITEM1_ID=$(extract_event_attr "$TX_RESULT_OUT" "item_added" "id")
if [ -z "$IMMUT_ITEM1_ID" ]; then
    ITEMS_DATA=$(query collect items "$IMMUT_COLL_ID")
    IMMUT_ITEM1_ID=$(echo "$ITEMS_DATA" | jq -r '.items[0].id // empty' 2>/dev/null)
fi

TX_OUT=$(send_tx collect add-item "$IMMUT_COLL_ID" 1 "ImmutItem2" "Second item" "" unspecified --from nonmember1)
assert_tx_success "Add item 2 to immutable collection" "$TX_OUT"
IMMUT_ITEM2_ID=$(extract_event_attr "$TX_RESULT_OUT" "item_added" "id")
if [ -z "$IMMUT_ITEM2_ID" ]; then
    ITEMS_DATA=$(query collect items "$IMMUT_COLL_ID")
    IMMUT_ITEM2_ID=$(echo "$ITEMS_DATA" | jq -r '.items[1].id // empty' 2>/dev/null)
fi

echo "  Item IDs: $IMMUT_ITEM1_ID, $IMMUT_ITEM2_ID"

# Set seeking endorsement
TX_OUT=$(send_tx collect set-seeking-endorsement "$IMMUT_COLL_ID" true --from nonmember1)
assert_tx_success "Set seeking endorsement" "$TX_OUT"

# Alice endorses the collection (locks 100 DREAM); requires TRUST_LEVEL_ESTABLISHED or above.
# Alice has TRUST_LEVEL_CORE and sufficient DREAM.
TX_OUT=$(send_tx collect endorse-collection "$IMMUT_COLL_ID" --from alice)
assert_tx_success "Endorse collection" "$TX_OUT"

# Verify immutability
COLL_QUERY=$(query collect collection "$IMMUT_COLL_ID")
IMMUTABLE=$(echo "$COLL_QUERY" | jq -r '.collection.immutable // "false"' 2>/dev/null)
STATUS=$(echo "$COLL_QUERY" | jq -r '.collection.status // empty' 2>/dev/null)
assert_equal "Collection is immutable" "true" "$IMMUTABLE"
assert_equal "Collection status is ACTIVE" "COLLECTION_STATUS_ACTIVE" "$STATUS"

# =========================================================================
# Test 1: Cannot add item to immutable collection
# =========================================================================
echo ""
echo "--- Test 1: Cannot add item to immutable collection ---"
TX_OUT=$(send_tx collect add-item "$IMMUT_COLL_ID" 0 "NewItem" "Should fail" "" unspecified --from nonmember1)
assert_tx_failure "Cannot add item to immutable collection" "$TX_OUT"

# =========================================================================
# Test 2: Cannot update item in immutable collection
# =========================================================================
echo ""
echo "--- Test 2: Cannot update item in immutable collection ---"
TX_OUT=$(send_tx collect update-item "$IMMUT_ITEM1_ID" "Updated" "Should fail" "" unspecified --from nonmember1)
assert_tx_failure "Cannot update item in immutable collection" "$TX_OUT"

# =========================================================================
# Test 3: Cannot remove item from immutable collection
# =========================================================================
echo ""
echo "--- Test 3: Cannot remove item from immutable collection ---"
TX_OUT=$(send_tx collect remove-item "$IMMUT_ITEM1_ID" --from nonmember1)
assert_tx_failure "Cannot remove item from immutable collection" "$TX_OUT"

# =========================================================================
# Test 4: Cannot reorder item in immutable collection
# =========================================================================
echo ""
echo "--- Test 4: Cannot reorder item in immutable collection ---"
TX_OUT=$(send_tx collect reorder-item "$IMMUT_ITEM1_ID" 1 --from nonmember1)
assert_tx_failure "Cannot reorder item in immutable collection" "$TX_OUT"

# =========================================================================
# Test 5: Cannot add collaborator to immutable collection
# =========================================================================
echo ""
echo "--- Test 5: Cannot add collaborator to immutable collection ---"
TX_OUT=$(send_tx collect add-collaborator "$IMMUT_COLL_ID" "$COLLECTOR2_ADDR" editor --from nonmember1)
assert_tx_failure "Cannot add collaborator to immutable collection" "$TX_OUT"

# =========================================================================
# Test 6: Cannot update immutable collection metadata
# =========================================================================
echo ""
echo "--- Test 6: Cannot update immutable collection ---"
TX_OUT=$(send_tx collect update-collection \
    "$IMMUT_COLL_ID" nft 0 "NewName" "New description" "" "" \
    --from nonmember1)
assert_tx_failure "Cannot update immutable collection" "$TX_OUT"

# =========================================================================
# Test 7: Owner CAN still delete immutable collection
# =========================================================================
echo ""
echo "--- Test 7: Owner can delete immutable collection ---"
# Deletion should still work (owner right to remove own content)
TX_OUT=$(send_tx collect delete-collection "$IMMUT_COLL_ID" --from nonmember1)
assert_tx_success "Owner can delete immutable collection" "$TX_OUT"

# Verify gone
DEL_QUERY=$(query collect collection "$IMMUT_COLL_ID" 2>&1)
DEL_EXISTS=$(echo "$DEL_QUERY" | jq -r '.collection.id // empty' 2>/dev/null)
assert_equal "Deleted immutable collection gone" "" "$DEL_EXISTS"

echo ""
print_summary
exit $?
