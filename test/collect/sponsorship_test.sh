#!/bin/bash
# Sponsorship flow tests for x/collect

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/test_helpers.sh"
source "$SCRIPT_DIR/.test_env"

echo "========================================================================="
echo "  X/COLLECT - SPONSORSHIP TESTS"
echo "========================================================================="
echo ""

BLOCK_HEIGHT=$(get_block_height)
FUTURE_BLOCK=$((BLOCK_HEIGHT + 5000))

# =========================================================================
# Test 1: Non-member creates TTL collection for sponsorship
# =========================================================================
echo "--- Test 1: Non-member creates TTL collection ---"
TX_OUT=$(send_tx collect create-collection \
    link public false "$FUTURE_BLOCK" "SponsorColl" "For sponsorship test" "" "" \
    --from nonmember1)
assert_tx_success "Create TTL collection for sponsorship" "$TX_OUT"

SPONSOR_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$SPONSOR_COLL_ID" ]; then
    DATA=$(query collect collections-by-owner "$NONMEMBER1_ADDR")
    SPONSOR_COLL_ID=$(echo "$DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi
echo "  Collection ID: $SPONSOR_COLL_ID"

# =========================================================================
# Test 2: Non-member requests sponsorship
# =========================================================================
echo ""
echo "--- Test 2: Request sponsorship ---"
TX_OUT=$(send_tx collect request-sponsorship "$SPONSOR_COLL_ID" --from nonmember1)
assert_tx_success "Non-member requests sponsorship" "$TX_OUT"

# =========================================================================
# Test 3: Query sponsorship-request
# =========================================================================
echo ""
echo "--- Test 3: Query sponsorship-request ---"
SPONSOR_QUERY=$(query collect sponsorship-request "$SPONSOR_COLL_ID")
REQUESTER=$(echo "$SPONSOR_QUERY" | jq -r '.sponsorship_request.requester // empty' 2>/dev/null)
assert_equal "Sponsorship requester is nonmember1" "$NONMEMBER1_ADDR" "$REQUESTER"

# =========================================================================
# Test 4: Query sponsorship-requests (list all)
# =========================================================================
echo ""
echo "--- Test 4: Query all sponsorship-requests ---"
ALL_REQUESTS=$(query collect sponsorship-requests)
REQ_COUNT=$(echo "$ALL_REQUESTS" | jq -r '.sponsorship_requests | length' 2>/dev/null)
assert_gt "Sponsorship requests exist" "0" "$REQ_COUNT"

# =========================================================================
# Test 5: Member cannot request sponsorship (FAIL expected)
# =========================================================================
echo ""
echo "--- Test 5: Member cannot request sponsorship ---"
# Create a TTL collection as member (use collector2 to avoid tier limit on collector1)
TX_OUT=$(send_tx collect create-collection \
    link public false "$FUTURE_BLOCK" "MemberTTL" "Member TTL" "" "" \
    --from collector2)
assert_tx_success "Create member TTL collection" "$TX_OUT"

MEMBER_TTL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$MEMBER_TTL_ID" ]; then
    DATA=$(query collect collections-by-owner "$COLLECTOR2_ADDR")
    MEMBER_TTL_ID=$(echo "$DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi

TX_OUT=$(send_tx collect request-sponsorship "$MEMBER_TTL_ID" --from collector2)
assert_tx_failure "Member cannot request sponsorship" "$TX_OUT"

# =========================================================================
# Test 6: Cannot request sponsorship twice
# =========================================================================
echo ""
echo "--- Test 6: Cannot request sponsorship twice ---"
TX_OUT=$(send_tx collect request-sponsorship "$SPONSOR_COLL_ID" --from nonmember1)
assert_tx_failure "Cannot request sponsorship twice" "$TX_OUT"

# =========================================================================
# Test 7: Cancel sponsorship request
# =========================================================================
echo ""
echo "--- Test 7: Cancel sponsorship request ---"
TX_OUT=$(send_tx collect cancel-sponsorship-request "$SPONSOR_COLL_ID" --from nonmember1)
assert_tx_success "Cancel sponsorship request" "$TX_OUT"

# Verify request is gone
SPONSOR_QUERY=$(query collect sponsorship-request "$SPONSOR_COLL_ID" 2>&1)
REQUESTER=$(echo "$SPONSOR_QUERY" | jq -r '.sponsorship_request.requester // empty' 2>/dev/null)
assert_equal "Sponsorship request removed after cancel" "" "$REQUESTER"

echo ""
print_summary
exit $?
