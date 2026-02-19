#!/bin/bash
# Advanced voting/flagging tests for x/collect
# Tests: item-level reactions, flag with OTHER reason

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/test_helpers.sh"
source "$SCRIPT_DIR/.test_env"

echo "========================================================================="
echo "  X/COLLECT - ADVANCED VOTING & FLAGGING TESTS"
echo "========================================================================="
echo ""

# =========================================================================
# Test 1: Item-level upvote
# =========================================================================
echo "--- Test 1: Item-level upvote ---"
TX_OUT=$(send_tx collect upvote-content "$ITEM1_ID" item --from collector2)
assert_tx_success "Member upvotes item" "$TX_OUT"

ITEM_QUERY=$(query collect item "$ITEM1_ID")
ITEM_UP=$(echo "$ITEM_QUERY" | jq -r '.item.upvote_count // "0"' 2>/dev/null)
assert_equal "Item upvote count is 1" "1" "$ITEM_UP"

# =========================================================================
# Test 2: Cannot vote same item twice
# =========================================================================
echo ""
echo "--- Test 2: Cannot vote same item twice ---"
TX_OUT=$(send_tx collect upvote-content "$ITEM1_ID" item --from collector2)
assert_tx_failure "Cannot vote same item twice" "$TX_OUT"

# =========================================================================
# Test 3: Cannot vote own item
# =========================================================================
echo ""
echo "--- Test 3: Cannot vote own item ---"
TX_OUT=$(send_tx collect upvote-content "$ITEM1_ID" item --from collector1)
assert_tx_failure "Cannot vote own item" "$TX_OUT"

# =========================================================================
# Test 4: Item-level flag
# =========================================================================
echo ""
echo "--- Test 4: Item-level flag ---"
# Flagging is separate from voting dedup; collector2 can still flag after upvoting
TX_OUT=$(send_tx collect flag-content "$ITEM1_ID" item spam "" --from collector2)
assert_tx_success "Member flags item" "$TX_OUT"

# =========================================================================
# Test 5: Item-level downvote
# =========================================================================
echo ""
echo "--- Test 5: Item-level downvote ---"
# Need an item NOT owned by collector1 for collector1 to downvote
# Find a collector2 collection and add a target item
C2_COLLS=$(query collect collections-by-owner "$COLLECTOR2_ADDR")
C2_COLL_ID=$(echo "$C2_COLLS" | jq -r '.collections[0].id // empty' 2>/dev/null)

if [ -n "$C2_COLL_ID" ]; then
    TX_OUT=$(send_tx collect add-item "$C2_COLL_ID" 0 "DVTarget" "For downvote" "" unspecified --from collector2)
    assert_tx_success "Add item for downvote" "$TX_OUT"

    DV_ITEM_ID=$(extract_event_attr "$TX_RESULT_OUT" "item_added" "id")
    if [ -z "$DV_ITEM_ID" ]; then
        ITEMS_DATA=$(query collect items "$C2_COLL_ID")
        DV_ITEM_ID=$(echo "$ITEMS_DATA" | jq -r '.items[-1].id // empty' 2>/dev/null)
    fi

    TX_OUT=$(send_tx collect downvote-content "$DV_ITEM_ID" item --from collector1)
    assert_tx_success "Member downvotes item" "$TX_OUT"

    ITEM_QUERY=$(query collect item "$DV_ITEM_ID")
    ITEM_DV=$(echo "$ITEM_QUERY" | jq -r '.item.downvote_count // "0"' 2>/dev/null)
    assert_equal "Item downvote count is 1" "1" "$ITEM_DV"
else
    skip_test "Item downvote" "No collector2 collection found"
    skip_test "Item downvote count" "Skipped"
fi

# =========================================================================
# Test 6: Flag with OTHER reason + reason_text
# =========================================================================
echo ""
echo "--- Test 6: Flag with OTHER reason ---"
if [ -n "$C2_COLL_ID" ]; then
    TX_OUT=$(send_tx collect flag-content "$C2_COLL_ID" collection other "Custom reason for testing" --from collector1)
    assert_tx_success "Flag with OTHER reason" "$TX_OUT"
else
    skip_test "Flag with OTHER" "No collector2 collection found"
fi

echo ""
print_summary
exit $?
