#!/bin/bash
# Voting and flagging tests for x/collect

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/test_helpers.sh"
source "$SCRIPT_DIR/.test_env"

echo "========================================================================="
echo "  X/COLLECT - VOTING & FLAGGING TESTS"
echo "========================================================================="
echo ""

# =========================================================================
# Setup: Create a collection owned by collector1 for voting tests
# =========================================================================
echo "--- Setup: Create collection for voting tests ---"
TX_OUT=$(send_tx collect create-collection \
    nft public false 0 "VotableColl" "A votable collection" "" "" \
    --from collector1)
assert_tx_success "Create votable collection" "$TX_OUT"

VOTE_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$VOTE_COLL_ID" ]; then
    DATA=$(query collect collections-by-owner "$COLLECTOR1_ADDR")
    VOTE_COLL_ID=$(echo "$DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi
echo "  Collection ID: $VOTE_COLL_ID"

# =========================================================================
# Test 1: Member upvotes another member's collection
# =========================================================================
echo ""
echo "--- Test 1: Member upvotes collection ---"
# collector2 upvotes collector1's collection
# target_type: 1 = FLAG_TARGET_TYPE_COLLECTION
TX_OUT=$(send_tx collect upvote-content "$VOTE_COLL_ID" collection --from collector2)
assert_tx_success "Member upvotes collection" "$TX_OUT"

# Verify upvote count
COLL_QUERY=$(query collect collection "$VOTE_COLL_ID")
UPVOTE_COUNT=$(echo "$COLL_QUERY" | jq -r '.collection.upvote_count // "0"' 2>/dev/null)
assert_equal "Upvote count is 1" "1" "$UPVOTE_COUNT"

# =========================================================================
# Test 2: Cannot upvote same content twice
# =========================================================================
echo ""
echo "--- Test 2: Cannot upvote same content twice ---"
TX_OUT=$(send_tx collect upvote-content "$VOTE_COLL_ID" collection --from collector2)
assert_tx_failure "Cannot upvote same content twice" "$TX_OUT"

# =========================================================================
# Test 3: Owner cannot upvote own collection
# =========================================================================
echo ""
echo "--- Test 3: Owner cannot upvote own collection ---"
TX_OUT=$(send_tx collect upvote-content "$VOTE_COLL_ID" collection --from collector1)
assert_tx_failure "Owner cannot upvote own collection" "$TX_OUT"

# =========================================================================
# Test 4: Non-member cannot upvote
# =========================================================================
echo ""
echo "--- Test 4: Non-member cannot upvote ---"
TX_OUT=$(send_tx collect upvote-content "$VOTE_COLL_ID" collection --from nonmember1)
assert_tx_failure "Non-member cannot upvote" "$TX_OUT"

# =========================================================================
# Test 5: Member downvotes collection (costs 25 SPARK)
# =========================================================================
echo ""
echo "--- Test 5: Member downvotes collection ---"
# Create collection as collector2 for downvoting (collector1 at tier limit)
TX_OUT=$(send_tx collect create-collection \
    nft public false 0 "DownvoteColl" "For downvoting" "" "" \
    --from collector2)
assert_tx_success "Create collection for downvoting" "$TX_OUT"

DV_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$DV_COLL_ID" ]; then
    DATA=$(query collect collections-by-owner "$COLLECTOR2_ADDR")
    DV_COLL_ID=$(echo "$DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi

# collector1 downvotes collector2's collection
TX_OUT=$(send_tx collect downvote-content "$DV_COLL_ID" collection --from collector1)
assert_tx_success "Member downvotes collection" "$TX_OUT"

COLL_QUERY=$(query collect collection "$DV_COLL_ID")
DV_COUNT=$(echo "$COLL_QUERY" | jq -r '.collection.downvote_count // "0"' 2>/dev/null)
assert_equal "Downvote count is 1" "1" "$DV_COUNT"

# =========================================================================
# Test 6: Member flags collection
# =========================================================================
echo ""
echo "--- Test 6: Member flags collection ---"
# Create collection as collector2 for flagging
TX_OUT=$(send_tx collect create-collection \
    nft public false 0 "FlagColl" "For flagging" "" "" \
    --from collector2)
assert_tx_success "Create collection for flagging" "$TX_OUT"

FLAG_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$FLAG_COLL_ID" ]; then
    DATA=$(query collect collections-by-owner "$COLLECTOR2_ADDR")
    FLAG_COLL_ID=$(echo "$DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi

# reason: 1 = MODERATION_REASON_SPAM, reason_text must be empty for non-OTHER reasons
# collector1 flags collector2's collection
TX_OUT=$(send_tx collect flag-content "$FLAG_COLL_ID" collection spam "" --from collector1)
assert_tx_success "Member flags collection as spam" "$TX_OUT"

# =========================================================================
# Test 7: Query content-flag
# =========================================================================
echo ""
echo "--- Test 7: Query content-flag ---"
FLAG_QUERY=$(query collect content-flag "$FLAG_COLL_ID" collection)
FLAG_TOTAL=$(echo "$FLAG_QUERY" | jq -r '.flag.total_weight // "0"' 2>/dev/null)
assert_not_empty "Flag total weight is set" "$FLAG_TOTAL"

# =========================================================================
# Test 8: Non-member cannot flag
# =========================================================================
echo ""
echo "--- Test 8: Non-member cannot flag ---"
TX_OUT=$(send_tx collect flag-content "$FLAG_COLL_ID" collection spam "" --from nonmember1)
assert_tx_failure "Non-member cannot flag" "$TX_OUT"

echo ""
print_summary
exit $?
