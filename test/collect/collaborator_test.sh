#!/bin/bash
# Collaborator management tests for x/collect

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/test_helpers.sh"
source "$SCRIPT_DIR/.test_env"

echo "========================================================================="
echo "  X/COLLECT - COLLABORATOR TESTS"
echo "========================================================================="
echo ""

# =========================================================================
# Setup: Create a fresh collection for collaborator tests
# =========================================================================
echo "--- Setup: Create collection for collaborator tests ---"
TX_OUT=$(send_tx collect create-collection \
    mixed public false 0 "CollabTestColl" "For collaborator tests" "" "" \
    --from collector1)
assert_tx_success "Create collection for collaborator tests" "$TX_OUT"

COLLAB_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$COLLAB_COLL_ID" ]; then
    DATA=$(query collect collections-by-owner "$COLLECTOR1_ADDR")
    COLLAB_COLL_ID=$(echo "$DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi
echo "  Collection ID: $COLLAB_COLL_ID"

# =========================================================================
# Test 1: Owner adds member as EDITOR collaborator
# =========================================================================
echo ""
echo "--- Test 1: Owner adds EDITOR collaborator ---"
TX_OUT=$(send_tx collect add-collaborator \
    "$COLLAB_COLL_ID" "$COLLECTOR2_ADDR" editor \
    --from collector1)
assert_tx_success "Add collector2 as EDITOR" "$TX_OUT"

# Verify via query
COLLABS=$(query collect collaborators "$COLLAB_COLL_ID")
COLLAB_COUNT=$(echo "$COLLABS" | jq -r '.collaborators | length' 2>/dev/null)
assert_equal "Collection has 1 collaborator" "1" "$COLLAB_COUNT"

COLLAB_ROLE=$(echo "$COLLABS" | jq -r '.collaborators[0].role // empty' 2>/dev/null)
assert_equal "Collaborator role is EDITOR" "COLLABORATOR_ROLE_EDITOR" "$COLLAB_ROLE"

# =========================================================================
# Test 2: EDITOR collaborator can add item
# =========================================================================
echo ""
echo "--- Test 2: EDITOR collaborator adds item ---"
TX_OUT=$(send_tx collect add-item \
    "$COLLAB_COLL_ID" 0 "Collab Item" "Added by collaborator" "" unspecified \
    --from collector2)
assert_tx_success "Collaborator adds item" "$TX_OUT"

# =========================================================================
# Test 3: Cannot add non-member as collaborator
# =========================================================================
echo ""
echo "--- Test 3: Cannot add non-member as collaborator ---"
TX_OUT=$(send_tx collect add-collaborator \
    "$COLLAB_COLL_ID" "$NONMEMBER1_ADDR" editor \
    --from collector1)
assert_tx_failure "Cannot add non-member as collaborator" "$TX_OUT"

# =========================================================================
# Test 4: Cannot add owner as collaborator
# =========================================================================
echo ""
echo "--- Test 4: Cannot add owner as collaborator ---"
TX_OUT=$(send_tx collect add-collaborator \
    "$COLLAB_COLL_ID" "$COLLECTOR1_ADDR" editor \
    --from collector1)
assert_tx_failure "Cannot add owner as collaborator" "$TX_OUT"

# =========================================================================
# Test 5: Update collaborator role to ADMIN
# =========================================================================
echo ""
echo "--- Test 5: Update collaborator role to ADMIN ---"
TX_OUT=$(send_tx collect update-collaborator-role \
    "$COLLAB_COLL_ID" "$COLLECTOR2_ADDR" admin \
    --from collector1)
assert_tx_success "Update collaborator to ADMIN" "$TX_OUT"

COLLABS=$(query collect collaborators "$COLLAB_COLL_ID")
COLLAB_ROLE=$(echo "$COLLABS" | jq -r '.collaborators[0].role // empty' 2>/dev/null)
assert_equal "Collaborator role is now ADMIN" "COLLABORATOR_ROLE_ADMIN" "$COLLAB_ROLE"

# =========================================================================
# Test 6: Query collections-by-collaborator
# =========================================================================
echo ""
echo "--- Test 6: Query collections-by-collaborator ---"
COLLAB_COLLS=$(query collect collections-by-collaborator "$COLLECTOR2_ADDR")
COLLAB_COLL_COUNT=$(echo "$COLLAB_COLLS" | jq -r '.collections | length' 2>/dev/null)
assert_gt "Collector2 collaborates on collections" "0" "$COLLAB_COLL_COUNT"

# =========================================================================
# Test 7: Remove collaborator
# =========================================================================
echo ""
echo "--- Test 7: Remove collaborator ---"
TX_OUT=$(send_tx collect remove-collaborator \
    "$COLLAB_COLL_ID" "$COLLECTOR2_ADDR" \
    --from collector1)
assert_tx_success "Remove collaborator" "$TX_OUT"

COLLABS=$(query collect collaborators "$COLLAB_COLL_ID")
COLLAB_COUNT=$(echo "$COLLABS" | jq -r '.collaborators | length' 2>/dev/null)
assert_equal "No collaborators after removal" "0" "$COLLAB_COUNT"

# =========================================================================
# Test 8: Removed collaborator cannot add items
# =========================================================================
echo ""
echo "--- Test 8: Removed collaborator cannot add items ---"
TX_OUT=$(send_tx collect add-item \
    "$COLLAB_COLL_ID" 0 "Unauthorized" "Should fail" "" unspecified \
    --from collector2)
assert_tx_failure "Removed collaborator cannot add items" "$TX_OUT"

echo ""
print_summary
exit $?
