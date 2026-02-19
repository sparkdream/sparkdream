#!/bin/bash
# Advanced collection tests for x/collect
# Tests: encryption validation, TTL conversion, cascade delete, community feedback

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/test_helpers.sh"
source "$SCRIPT_DIR/.test_env"

echo "========================================================================="
echo "  X/COLLECT - ADVANCED COLLECTION TESTS"
echo "========================================================================="
echo ""

BLOCK_HEIGHT=$(get_block_height)
FUTURE_BLOCK=$((BLOCK_HEIGHT + 5000))

# =========================================================================
# Test 1: Encrypted + public rejected
# =========================================================================
echo "--- Test 1: Encrypted + public rejected ---"
TX_OUT=$(send_tx collect create-collection \
    nft public true "$FUTURE_BLOCK" "" "" "" "" \
    --from collector1)
assert_tx_failure "Encrypted+public rejected" "$TX_OUT"

# =========================================================================
# Test 2: Private requires encryption
# =========================================================================
echo ""
echo "--- Test 2: Private requires encryption ---"
TX_OUT=$(send_tx collect create-collection \
    nft private false "$FUTURE_BLOCK" "PrivNoEnc" "Priv without enc" "" "" \
    --from collector1)
assert_tx_failure "Private requires encryption" "$TX_OUT"

# =========================================================================
# Test 3: Non-member TTL cap exceeded
# =========================================================================
echo ""
echo "--- Test 3: Non-member TTL cap exceeded ---"
FAR_FUTURE=$((BLOCK_HEIGHT + 500000))
TX_OUT=$(send_tx collect create-collection \
    nft public false "$FAR_FUTURE" "TooLong" "TTL too long" "" "" \
    --from nonmember1)
assert_tx_failure "Non-member TTL cap exceeded" "$TX_OUT"

# =========================================================================
# Test 4: Delete collection with items (cascade)
# =========================================================================
echo ""
echo "--- Test 4: Delete collection with items (cascade) ---"
TX_OUT=$(send_tx collect create-collection \
    mixed public false 0 "CascadeDel" "For cascade delete" "" "" \
    --from collector2)
assert_tx_success "Create collection for cascade delete" "$TX_OUT"

CASCADE_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$CASCADE_COLL_ID" ]; then
    DATA=$(query collect collections-by-owner "$COLLECTOR2_ADDR")
    CASCADE_COLL_ID=$(echo "$DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi

# Add 2 items
TX_OUT=$(send_tx collect add-item "$CASCADE_COLL_ID" 0 "CascadeItem1" "Item 1" "" unspecified --from collector2)
assert_tx_success "Add cascade item 1" "$TX_OUT"
TX_OUT=$(send_tx collect add-item "$CASCADE_COLL_ID" 1 "CascadeItem2" "Item 2" "" unspecified --from collector2)
assert_tx_success "Add cascade item 2" "$TX_OUT"

# Verify items exist
ITEMS=$(query collect items "$CASCADE_COLL_ID")
ITEM_COUNT=$(echo "$ITEMS" | jq -r '.items | length' 2>/dev/null)
assert_equal "Collection has 2 items before delete" "2" "$ITEM_COUNT"

# Delete collection with items
TX_OUT=$(send_tx collect delete-collection "$CASCADE_COLL_ID" --from collector2)
assert_tx_success "Delete collection with items" "$TX_OUT"

# Verify collection is gone
DEL_QUERY=$(query collect collection "$CASCADE_COLL_ID" 2>&1)
DEL_EXISTS=$(echo "$DEL_QUERY" | jq -r '.collection.id // empty' 2>/dev/null)
assert_equal "Cascade-deleted collection gone" "" "$DEL_EXISTS"

# =========================================================================
# Test 5: Permanent → TTL rejected
# =========================================================================
echo ""
echo "--- Test 5: Permanent → TTL rejected ---"
TX_OUT=$(send_tx collect update-collection \
    "$COLL1_ID" nft "$FUTURE_BLOCK" "ArtGalleryUpdated" "Updated" "" "" \
    --from collector1)
assert_tx_failure "Permanent to TTL rejected" "$TX_OUT"

# =========================================================================
# Test 6: Member TTL → permanent conversion
# =========================================================================
echo ""
echo "--- Test 6: Member TTL → permanent conversion ---"
TX_OUT=$(send_tx collect create-collection \
    link public false "$FUTURE_BLOCK" "ConvertMe" "Will become permanent" "" "" \
    --from collector2)
assert_tx_success "Create TTL for conversion" "$TX_OUT"

CONVERT_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$CONVERT_COLL_ID" ]; then
    DATA=$(query collect collections-by-owner "$COLLECTOR2_ADDR")
    CONVERT_COLL_ID=$(echo "$DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi

TX_OUT=$(send_tx collect update-collection \
    "$CONVERT_COLL_ID" link 0 "ConvertMe" "Now permanent" "" "" \
    --from collector2)
assert_tx_success "TTL to permanent conversion" "$TX_OUT"

CONV_QUERY=$(query collect collection "$CONVERT_COLL_ID")
CONV_EXPIRES=$(echo "$CONV_QUERY" | jq -r '.collection.expires_at // "0"' 2>/dev/null)
CONV_BURNED=$(echo "$CONV_QUERY" | jq -r '.collection.deposit_burned // "false"' 2>/dev/null)
assert_equal "Converted expires_at is 0" "0" "$CONV_EXPIRES"
assert_equal "Converted deposits burned" "true" "$CONV_BURNED"

# =========================================================================
# Test 7: Non-member cannot convert TTL → permanent
# =========================================================================
echo ""
echo "--- Test 7: Non-member cannot convert TTL → permanent ---"
TX_OUT=$(send_tx collect update-collection \
    "$COLL3_ID" nft 0 "GuestColl" "Trying permanent" "" "" \
    --from nonmember1)
assert_tx_failure "Non-member cannot convert TTL to permanent" "$TX_OUT"

# =========================================================================
# Test 8: Community feedback toggle
# =========================================================================
echo ""
echo "--- Test 8: Community feedback toggle ---"
TX_OUT=$(send_tx collect create-collection \
    nft public false 0 "FeedbackColl" "For feedback toggle" "" "" \
    --from collector2)
assert_tx_success "Create collection for feedback toggle" "$TX_OUT"

FB_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$FB_COLL_ID" ]; then
    DATA=$(query collect collections-by-owner "$COLLECTOR2_ADDR")
    FB_COLL_ID=$(echo "$DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi

# Disable community feedback
TX_OUT=$(send_tx collect update-collection \
    "$FB_COLL_ID" nft 0 "FeedbackColl" "For feedback toggle" "" "" \
    --update-community-feedback --community-feedback-enabled=false \
    --from collector2)
assert_tx_success "Disable community feedback" "$TX_OUT"

# Verify
FB_QUERY=$(query collect collection "$FB_COLL_ID")
FB_ENABLED=$(echo "$FB_QUERY" | jq -r '.collection.community_feedback_enabled // "false"' 2>/dev/null)
assert_equal "Community feedback disabled" "false" "$FB_ENABLED"

# Upvote should be rejected
TX_OUT=$(send_tx collect upvote-content "$FB_COLL_ID" collection --from collector1)
assert_tx_failure "Upvote rejected when feedback disabled" "$TX_OUT"

# =========================================================================
# Test 9: Delete PENDING non-member collection
# =========================================================================
echo ""
echo "--- Test 9: Delete PENDING collection ---"
TX_OUT=$(send_tx collect create-collection \
    nft public false "$FUTURE_BLOCK" "PendingDel" "Will be deleted" "" "" \
    --from nonmember1)
assert_tx_success "Create PENDING for deletion" "$TX_OUT"

PENDING_DEL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$PENDING_DEL_ID" ]; then
    DATA=$(query collect collections-by-owner "$NONMEMBER1_ADDR")
    PENDING_DEL_ID=$(echo "$DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi

TX_OUT=$(send_tx collect delete-collection "$PENDING_DEL_ID" --from nonmember1)
assert_tx_success "Delete PENDING collection" "$TX_OUT"

DEL_QUERY=$(query collect collection "$PENDING_DEL_ID" 2>&1)
DEL_EXISTS=$(echo "$DEL_QUERY" | jq -r '.collection.id // empty' 2>/dev/null)
assert_equal "Deleted PENDING collection gone" "" "$DEL_EXISTS"

# Export for other tests
cat >> "$SCRIPT_DIR/.test_env" <<EOF
FB_COLL_ID=$FB_COLL_ID
EOF

echo ""
print_summary
exit $?
