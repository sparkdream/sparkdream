#!/bin/bash
# Item CRUD tests for x/collect

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/test_helpers.sh"
source "$SCRIPT_DIR/.test_env"

echo "========================================================================="
echo "  X/COLLECT - ITEM CRUD TESTS"
echo "========================================================================="
echo ""

# =========================================================================
# Setup: Create a fresh collection for item tests
# =========================================================================
echo "--- Setup: Create collection for item tests ---"
TX_OUT=$(send_tx collect create-collection --gas 400000 \
    mixed public false 0 "ItemTestColl" "For item tests" "" "" \
    --from collector1)
assert_tx_success "Create collection for item tests" "$TX_OUT"

ITEM_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$ITEM_COLL_ID" ]; then
    DATA=$(query collect collections-by-owner "$COLLECTOR1_ADDR")
    ITEM_COLL_ID=$(echo "$DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi
echo "  Collection ID: $ITEM_COLL_ID"

# =========================================================================
# Test 1: Add item to own collection
# =========================================================================
echo ""
echo "--- Test 1: Add item to own collection ---"
TX_OUT=$(send_tx collect add-item --gas 400000 \
    "$ITEM_COLL_ID" 0 "First Item" "A test item" "" unspecified \
    --from collector1)

assert_tx_success "Add first item" "$TX_OUT"

ITEM1_ID=$(extract_event_attr "$TX_RESULT_OUT" "item_added" "id")
if [ -z "$ITEM1_ID" ]; then
    ITEMS_DATA=$(query collect items "$ITEM_COLL_ID")
    ITEM1_ID=$(echo "$ITEMS_DATA" | jq -r '.items[-1].id // empty' 2>/dev/null)
fi
echo "  Item ID: $ITEM1_ID"

# Verify item via query
ITEM1_QUERY=$(query collect item "$ITEM1_ID")
ITEM1_TITLE=$(echo "$ITEM1_QUERY" | jq -r '.item.title // empty' 2>/dev/null)
assert_equal "Item title is correct" "First Item" "$ITEM1_TITLE"

# =========================================================================
# Test 2: Add second item
# =========================================================================
echo ""
echo "--- Test 2: Add second item ---"
TX_OUT=$(send_tx collect add-item --gas 400000 \
    "$ITEM_COLL_ID" 1 "Second Item" "Another test item" "" unspecified \
    --from collector1)

assert_tx_success "Add second item" "$TX_OUT"

ITEM2_ID=$(extract_event_attr "$TX_RESULT_OUT" "item_added" "id")
if [ -z "$ITEM2_ID" ]; then
    ITEMS_DATA=$(query collect items "$ITEM_COLL_ID")
    ITEM2_ID=$(echo "$ITEMS_DATA" | jq -r '.items[-1].id // empty' 2>/dev/null)
fi
echo "  Item ID: $ITEM2_ID"

# =========================================================================
# Test 3: Update item
# =========================================================================
echo ""
echo "--- Test 3: Update item ---"
TX_OUT=$(send_tx collect update-item \
    "$ITEM1_ID" "Updated First" "Updated description" "" unspecified \
    --from collector1)

assert_tx_success "Update item" "$TX_OUT"

ITEM1_QUERY=$(query collect item "$ITEM1_ID")
ITEM1_TITLE_NEW=$(echo "$ITEM1_QUERY" | jq -r '.item.title // empty' 2>/dev/null)
assert_equal "Item title updated" "Updated First" "$ITEM1_TITLE_NEW"

# =========================================================================
# Test 4: Reorder items
# =========================================================================
echo ""
echo "--- Test 4: Reorder items ---"
TX_OUT=$(send_tx collect reorder-item "$ITEM2_ID" 0 --from collector1)
assert_tx_success "Reorder item to position 0" "$TX_OUT"

# =========================================================================
# Test 5: Query items by collection
# =========================================================================
echo ""
echo "--- Test 5: Query items by collection ---"
ITEMS_QUERY=$(query collect items "$ITEM_COLL_ID")
ITEM_COUNT=$(echo "$ITEMS_QUERY" | jq -r '.items | length' 2>/dev/null)
assert_equal "Collection has 2 items" "2" "$ITEM_COUNT"

# =========================================================================
# Test 6: Query items-by-owner
# =========================================================================
echo ""
echo "--- Test 6: Query items-by-owner ---"
ITEMS_BY_OWNER=$(query collect items-by-owner "$COLLECTOR1_ADDR")
OWNER_ITEM_COUNT=$(echo "$ITEMS_BY_OWNER" | jq -r '.items | length' 2>/dev/null)
assert_gt "Owner has items" "0" "$OWNER_ITEM_COUNT"

# =========================================================================
# Test 7: Cannot add item to someone else's collection
# =========================================================================
echo ""
echo "--- Test 7: Cannot add item to someone else's collection ---"
TX_OUT=$(send_tx collect add-item --gas 400000 \
    "$ITEM_COLL_ID" 0 "Unauthorized" "Should fail" "" unspecified \
    --from collector2)
assert_tx_failure "Cannot add item to another's collection" "$TX_OUT"

# =========================================================================
# Test 8: Remove item
# =========================================================================
echo ""
echo "--- Test 8: Remove item ---"
TX_OUT=$(send_tx collect remove-item "$ITEM2_ID" --from collector1)
assert_tx_success "Remove item" "$TX_OUT"

# Verify item count decreased
ITEMS_QUERY=$(query collect items "$ITEM_COLL_ID")
ITEM_COUNT=$(echo "$ITEMS_QUERY" | jq -r '.items | length' 2>/dev/null)
assert_equal "Collection has 1 item after removal" "1" "$ITEM_COUNT"

# =========================================================================
# Test 9: Non-member adds item to own TTL collection (with spam tax)
# =========================================================================
echo ""
echo "--- Test 9: Non-member adds item to own TTL collection ---"
if [ -n "$COLL3_ID" ]; then
    TX_OUT=$(send_tx collect add-item --gas 400000 \
        "$COLL3_ID" 0 "Guest Item" "Guest added" "" unspecified \
        --from nonmember1)
    assert_tx_success "Non-member adds item to own collection" "$TX_OUT"
else
    skip_test "Non-member adds item" "No PENDING collection available"
fi

# Export for other tests
cat >> "$SCRIPT_DIR/.test_env" <<EOF
ITEM_COLL_ID=$ITEM_COLL_ID
ITEM1_ID=$ITEM1_ID
EOF

# =========================================================================
# Media: data URIs rejected, URI fields labelled (docs/content-scanning.md §3.3)
# =========================================================================
echo ""
echo "--- Media: data URI in image_uri is rejected ---"
TX_OUT=$(send_tx collect add-item --gas 400000 \
    "$ITEM_COLL_ID" 0 "Inline Image" "bytes hidden in a URI" "data:image/png;base64,iVBORw0KGgo=" unspecified \
    --from collector1)
DATA_TXHASH=$(get_txhash "$TX_OUT")
if [ -z "$DATA_TXHASH" ]; then
    DATA_LOG="$TX_OUT"
else
    DATA_LOG=$(wait_for_tx "$DATA_TXHASH" | jq -r '.raw_log // empty' 2>/dev/null)
fi
if echo "$DATA_LOG" | grep -q "data URIs are not allowed"; then
    echo "PASS: data URI image_uri rejected (ErrDataURINotAllowed)"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    echo "FAIL: data URI image_uri not rejected with ErrDataURINotAllowed"
    echo "  Log: $(echo "$DATA_LOG" | head -c 300)"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

echo ""
echo "--- Media: an image_uri labels the item EXTERNAL_URI; plain items carry no flag ---"
TX_OUT=$(send_tx collect add-item --gas 400000 \
    "$ITEM_COLL_ID" 0 "Linked Image" "points elsewhere" "https://aurora.example/cover.png" unspecified \
    --from collector1)
if assert_tx_success "Add item with an https image_uri" "$TX_OUT"; then
    URI_ITEM_ID=$(extract_event_attr "$TX_RESULT_OUT" "item_added" "id")
    if [ -n "$URI_ITEM_ID" ]; then
        URI_FLAGS=$(query collect item "$URI_ITEM_ID" | jq -r '.item.media_flags // "0"' 2>/dev/null)
        assert_equal "image_uri item labelled EXTERNAL_URI (8)" "8" "$URI_FLAGS"
    else
        skip_test "image_uri item label" "item id not found in events"
    fi
fi
# An item without any URI field (earlier tests remove some, so pick a
# surviving one from the collection).
PLAIN_ITEM=$(query collect items "$ITEM_COLL_ID" | jq -c '[.items[] | select((.image_uri // "") == "")][0] // empty' 2>/dev/null)
if [ -n "$PLAIN_ITEM" ]; then
    PLAIN_FLAGS=$(echo "$PLAIN_ITEM" | jq -r '.media_flags // "0"')
    assert_equal "plain item carries no media flag" "0" "$PLAIN_FLAGS"
else
    skip_test "plain item label" "no URI-free item left in the collection"
fi

echo ""
print_summary
exit $?
