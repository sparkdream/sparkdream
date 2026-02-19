#!/bin/bash
# Collection CRUD tests for x/collect

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/test_helpers.sh"
source "$SCRIPT_DIR/.test_env"

echo "========================================================================="
echo "  X/COLLECT - COLLECTION CRUD TESTS"
echo "========================================================================="
echo ""

BLOCK_HEIGHT=$(get_block_height)
FUTURE_BLOCK=$((BLOCK_HEIGHT + 5000))

# =========================================================================
# Test 1: Member creates permanent PUBLIC collection (ACTIVE)
# =========================================================================
echo "--- Test 1: Member creates permanent PUBLIC collection ---"
TX_OUT=$(send_tx collect create-collection \
    nft public false 0 "ArtGallery" "A test art gallery" "" "" \
    --from collector1)
assert_tx_success "Member creates permanent PUBLIC collection" "$TX_OUT"

# Extract collection ID from events
COLL1_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$COLL1_ID" ]; then
    # Fallback: query collections-by-owner and get latest
    COLL1_DATA=$(query collect collections-by-owner "$COLLECTOR1_ADDR")
    COLL1_ID=$(echo "$COLL1_DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi
echo "  Collection ID: $COLL1_ID"

# Verify via query
COLL1_QUERY=$(query collect collection "$COLL1_ID")
COLL1_NAME=$(echo "$COLL1_QUERY" | jq -r '.collection.name // empty' 2>/dev/null)
COLL1_STATUS=$(echo "$COLL1_QUERY" | jq -r '.collection.status // empty' 2>/dev/null)
COLL1_OWNER=$(echo "$COLL1_QUERY" | jq -r '.collection.owner // empty' 2>/dev/null)
assert_equal "Collection name is ArtGallery" "ArtGallery" "$COLL1_NAME"
assert_equal "Collection status is ACTIVE" "COLLECTION_STATUS_ACTIVE" "$COLL1_STATUS"
assert_equal "Collection owner is collector1" "$COLLECTOR1_ADDR" "$COLL1_OWNER"

# =========================================================================
# Test 2: Member creates TTL PUBLIC collection
# =========================================================================
echo ""
echo "--- Test 2: Member creates TTL PUBLIC collection ---"
TX_OUT=$(send_tx collect create-collection \
    link public false "$FUTURE_BLOCK" "MyLinks" "Link bookmarks" "" "" \
    --from collector1)
assert_tx_success "Member creates TTL PUBLIC collection" "$TX_OUT"

COLL2_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$COLL2_ID" ]; then
    COLL2_DATA=$(query collect collections-by-owner "$COLLECTOR1_ADDR")
    COLL2_ID=$(echo "$COLL2_DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi
echo "  Collection ID: $COLL2_ID"

COLL2_QUERY=$(query collect collection "$COLL2_ID")
COLL2_EXPIRES=$(echo "$COLL2_QUERY" | jq -r '.collection.expires_at // "0"' 2>/dev/null)
assert_equal "TTL collection has correct expiry" "$FUTURE_BLOCK" "$COLL2_EXPIRES"

# =========================================================================
# Test 3: Non-member creates TTL PUBLIC collection (PENDING)
# =========================================================================
echo ""
echo "--- Test 3: Non-member creates TTL PUBLIC collection (PENDING) ---"
# Non-members need BaseCollectionDeposit (1 SPARK) + EndorsementCreationFee (10 SPARK) + gas
TX_OUT=$(send_tx collect create-collection \
    nft public false "$FUTURE_BLOCK" "GuestColl" "Guest collection" "" "" \
    --from nonmember1)
assert_tx_success "Non-member creates TTL PUBLIC collection" "$TX_OUT"

COLL3_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$COLL3_ID" ]; then
    COLL3_DATA=$(query collect collections-by-owner "$NONMEMBER1_ADDR")
    COLL3_ID=$(echo "$COLL3_DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi
echo "  Collection ID: $COLL3_ID"

COLL3_QUERY=$(query collect collection "$COLL3_ID")
COLL3_STATUS=$(echo "$COLL3_QUERY" | jq -r '.collection.status // empty' 2>/dev/null)
assert_equal "Non-member collection status is PENDING" "COLLECTION_STATUS_PENDING" "$COLL3_STATUS"

# =========================================================================
# Test 4: Non-member cannot create permanent collection (FAIL expected)
# =========================================================================
echo ""
echo "--- Test 4: Non-member cannot create permanent collection ---"
TX_OUT=$(send_tx collect create-collection \
    nft public false 0 "Permanent" "Should fail" "" "" \
    --from nonmember1)
assert_tx_failure "Non-member cannot create permanent collection" "$TX_OUT"

# =========================================================================
# Test 5: Update collection name and description
# =========================================================================
echo ""
echo "--- Test 5: Update collection ---"
TX_OUT=$(send_tx collect update-collection \
    "$COLL1_ID" nft 0 "ArtGalleryUpdated" "Updated description" "" "" \
    --from collector1)
assert_tx_success "Owner updates collection" "$TX_OUT"

COLL1_QUERY=$(query collect collection "$COLL1_ID")
COLL1_NAME_NEW=$(echo "$COLL1_QUERY" | jq -r '.collection.name // empty' 2>/dev/null)
assert_equal "Collection name updated" "ArtGalleryUpdated" "$COLL1_NAME_NEW"

# =========================================================================
# Test 6: Non-owner cannot update collection (FAIL expected)
# =========================================================================
echo ""
echo "--- Test 6: Non-owner cannot update collection ---"
TX_OUT=$(send_tx collect update-collection \
    "$COLL1_ID" nft 0 "Hacked" "Should fail" "" "" \
    --from collector2)
assert_tx_failure "Non-owner cannot update collection" "$TX_OUT"

# =========================================================================
# Test 7: Create and delete a collection
# =========================================================================
echo ""
echo "--- Test 7: Create and delete collection ---"
TX_OUT=$(send_tx collect create-collection \
    mixed public false 0 "ToDelete" "Will be deleted" "" "" \
    --from collector1)
assert_tx_success "Create collection for deletion" "$TX_OUT"

DEL_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$DEL_COLL_ID" ]; then
    DEL_DATA=$(query collect collections-by-owner "$COLLECTOR1_ADDR")
    DEL_COLL_ID=$(echo "$DEL_DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi

TX_OUT=$(send_tx collect delete-collection "$DEL_COLL_ID" --from collector1)
assert_tx_success "Owner deletes collection" "$TX_OUT"

# Verify deleted
DEL_QUERY=$(query collect collection "$DEL_COLL_ID" 2>&1)
DEL_EXISTS=$(echo "$DEL_QUERY" | jq -r '.collection.id // empty' 2>/dev/null)
assert_equal "Deleted collection not found" "" "$DEL_EXISTS"

# =========================================================================
# Test 8: Query collections-by-owner
# =========================================================================
echo ""
echo "--- Test 8: Query collections-by-owner ---"
OWNER_COLLS=$(query collect collections-by-owner "$COLLECTOR1_ADDR")
OWNER_COUNT=$(echo "$OWNER_COLLS" | jq -r '.collections | length' 2>/dev/null)
assert_gt "Collector1 has collections" "0" "$OWNER_COUNT"

# =========================================================================
# Test 9: Query public-collections
# =========================================================================
echo ""
echo "--- Test 9: Query public-collections ---"
PUBLIC_COLLS=$(query collect public-collections)
PUBLIC_COUNT=$(echo "$PUBLIC_COLLS" | jq -r '.collections | length' 2>/dev/null)
assert_gt "Public collections exist" "0" "$PUBLIC_COUNT"

# =========================================================================
# Export collection IDs for other tests
# =========================================================================
cat >> "$SCRIPT_DIR/.test_env" <<EOF
COLL1_ID=$COLL1_ID
COLL2_ID=$COLL2_ID
COLL3_ID=$COLL3_ID
EOF

echo ""
print_summary
exit $?
