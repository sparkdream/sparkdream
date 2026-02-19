#!/bin/bash
# Full sponsorship flow tests for x/collect
# Tests: items locked during sponsorship, sponsor-collection, post-sponsor state

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/test_helpers.sh"
source "$SCRIPT_DIR/.test_env"

echo "========================================================================="
echo "  X/COLLECT - SPONSORSHIP FLOW TESTS"
echo "========================================================================="
echo ""

BLOCK_HEIGHT=$(get_block_height)
FUTURE_BLOCK=$((BLOCK_HEIGHT + 5000))

# =========================================================================
# Setup: Create TTL collection with items for nonmember1
# =========================================================================
echo "--- Setup: Create TTL collection with items ---"
TX_OUT=$(send_tx collect create-collection \
    link public false "$FUTURE_BLOCK" "SponsorFlowColl" "For sponsor flow" "" "" \
    --from nonmember1)
assert_tx_success "Create TTL collection for sponsorship flow" "$TX_OUT"

SF_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$SF_COLL_ID" ]; then
    DATA=$(query collect collections-by-owner "$NONMEMBER1_ADDR")
    SF_COLL_ID=$(echo "$DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi
echo "  Collection ID: $SF_COLL_ID"

# Add 2 items
TX_OUT=$(send_tx collect add-item "$SF_COLL_ID" 0 "SFItem1" "First item" "" unspecified --from nonmember1)
assert_tx_success "Add item 1" "$TX_OUT"
SF_ITEM1_ID=$(extract_event_attr "$TX_RESULT_OUT" "item_added" "id")
if [ -z "$SF_ITEM1_ID" ]; then
    ITEMS_DATA=$(query collect items "$SF_COLL_ID")
    SF_ITEM1_ID=$(echo "$ITEMS_DATA" | jq -r '.items[0].id // empty' 2>/dev/null)
fi

TX_OUT=$(send_tx collect add-item "$SF_COLL_ID" 1 "SFItem2" "Second item" "" unspecified --from nonmember1)
assert_tx_success "Add item 2" "$TX_OUT"

# =========================================================================
# Test 1: Request sponsorship
# =========================================================================
echo ""
echo "--- Test 1: Request sponsorship ---"
TX_OUT=$(send_tx collect request-sponsorship "$SF_COLL_ID" --from nonmember1)
assert_tx_success "Request sponsorship" "$TX_OUT"

# Verify sponsorship request exists
SPONSOR_QUERY=$(query collect sponsorship-request "$SF_COLL_ID")
REQUESTER=$(echo "$SPONSOR_QUERY" | jq -r '.sponsorship_request.requester // empty' 2>/dev/null)
assert_equal "Sponsorship requester is nonmember1" "$NONMEMBER1_ADDR" "$REQUESTER"

# =========================================================================
# Test 2: Items locked - cannot add item during sponsorship
# =========================================================================
echo ""
echo "--- Test 2: Cannot add item during sponsorship ---"
TX_OUT=$(send_tx collect add-item "$SF_COLL_ID" 2 "Blocked" "Should fail" "" unspecified --from nonmember1)
assert_tx_failure "Cannot add item during sponsorship" "$TX_OUT"

# =========================================================================
# Test 3: Items locked - cannot remove item during sponsorship
# =========================================================================
echo ""
echo "--- Test 3: Cannot remove item during sponsorship ---"
TX_OUT=$(send_tx collect remove-item "$SF_ITEM1_ID" --from nonmember1)
assert_tx_failure "Cannot remove item during sponsorship" "$TX_OUT"

# =========================================================================
# Test 4: Non-owner cannot sponsor
# =========================================================================
echo ""
echo "--- Test 4: Non-owner cannot sponsor own collection ---"
TX_OUT=$(send_tx collect sponsor-collection "$SF_COLL_ID" --from nonmember1)
assert_tx_failure "Owner cannot sponsor own collection" "$TX_OUT"

# =========================================================================
# Test 5: Low-trust member cannot sponsor
# =========================================================================
echo ""
echo "--- Test 5: Low-trust member cannot sponsor ---"
# collector2 is NEW trust level (below ESTABLISHED)
TX_OUT=$(send_tx collect sponsor-collection "$SF_COLL_ID" --from collector2)
assert_tx_failure "Low-trust member cannot sponsor" "$TX_OUT"

# =========================================================================
# Test 6: Alice sponsors the collection (CORE trust level)
# =========================================================================
echo ""
echo "--- Test 6: Alice sponsors collection ---"
TX_OUT=$(send_tx collect sponsor-collection "$SF_COLL_ID" --from alice)
assert_tx_success "Alice sponsors collection" "$TX_OUT"

# =========================================================================
# Test 7: Verify post-sponsor state
# =========================================================================
echo ""
echo "--- Test 7: Verify post-sponsor state ---"
COLL_QUERY=$(query collect collection "$SF_COLL_ID")
EXPIRES=$(echo "$COLL_QUERY" | jq -r '.collection.expires_at // "0"' 2>/dev/null)
SPONSORED_BY=$(echo "$COLL_QUERY" | jq -r '.collection.sponsored_by // empty' 2>/dev/null)
DEPOSIT_BURNED=$(echo "$COLL_QUERY" | jq -r '.collection.deposit_burned // "false"' 2>/dev/null)

assert_equal "Collection is now permanent (expires_at=0)" "0" "$EXPIRES"
assert_equal "Sponsored by Alice" "$ALICE_ADDR" "$SPONSORED_BY"
assert_equal "Deposit burned" "true" "$DEPOSIT_BURNED"

# =========================================================================
# Test 8: Sponsorship request removed after sponsoring
# =========================================================================
echo ""
echo "--- Test 8: Sponsorship request removed ---"
SPONSOR_QUERY=$(query collect sponsorship-request "$SF_COLL_ID" 2>&1)
REQUESTER=$(echo "$SPONSOR_QUERY" | jq -r '.sponsorship_request.requester // empty' 2>/dev/null)
assert_equal "Sponsorship request removed" "" "$REQUESTER"

# =========================================================================
# Test 9: Items unlocked after sponsorship - can add items again
# =========================================================================
echo ""
echo "--- Test 9: Items unlocked after sponsorship ---"
TX_OUT=$(send_tx collect add-item "$SF_COLL_ID" 2 "PostSponsor" "Item after sponsor" "" unspecified --from nonmember1)
assert_tx_success "Can add item after sponsorship completes" "$TX_OUT"

# =========================================================================
# Test 10: Cannot request sponsorship on permanent collection
# =========================================================================
echo ""
echo "--- Test 10: Cannot request sponsorship on permanent collection ---"
TX_OUT=$(send_tx collect request-sponsorship "$SF_COLL_ID" --from nonmember1)
assert_tx_failure "Cannot request sponsorship on permanent collection" "$TX_OUT"

echo ""
print_summary
exit $?
