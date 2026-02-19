#!/bin/bash
# Advanced collaborator tests for x/collect
# Tests: self-removal, ADMIN abilities

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/test_helpers.sh"
source "$SCRIPT_DIR/.test_env"

echo "========================================================================="
echo "  X/COLLECT - ADVANCED COLLABORATOR TESTS"
echo "========================================================================="
echo ""

# =========================================================================
# Setup: Ensure collector2 is a collaborator on COLL1_ID
# =========================================================================
echo "--- Setup: Add collector2 as EDITOR on COLL1_ID ---"

# Remove if already present (from previous test runs)
COLLABS=$(query collect collaborators "$COLL1_ID")
COLLAB_EXISTS=$(echo "$COLLABS" | jq -r --arg addr "$COLLECTOR2_ADDR" \
    '.collaborators[] | select(.address==$addr) | .address // empty' 2>/dev/null)
if [ -n "$COLLAB_EXISTS" ]; then
    TX_OUT=$(send_tx collect remove-collaborator "$COLL1_ID" "$COLLECTOR2_ADDR" --from collector1)
    sleep $TX_WAIT
fi

TX_OUT=$(send_tx collect add-collaborator "$COLL1_ID" "$COLLECTOR2_ADDR" editor --from collector1)
assert_tx_success "Add collector2 as EDITOR" "$TX_OUT"

# =========================================================================
# Test 1: Collaborator self-removal
# =========================================================================
echo ""
echo "--- Test 1: Collaborator self-removal ---"
TX_OUT=$(send_tx collect remove-collaborator "$COLL1_ID" "$COLLECTOR2_ADDR" --from collector2)
assert_tx_success "Collaborator self-removes" "$TX_OUT"

COLLABS=$(query collect collaborators "$COLL1_ID")
COLLAB_COUNT=$(echo "$COLLABS" | jq -r '.collaborators | length' 2>/dev/null)
assert_equal "No collaborators after self-removal" "0" "$COLLAB_COUNT"

# =========================================================================
# Test 2: Self-removed cannot add items
# =========================================================================
echo ""
echo "--- Test 2: Self-removed cannot add items ---"
TX_OUT=$(send_tx collect add-item "$COLL1_ID" 0 "Unauthorized" "Should fail" "" unspecified --from collector2)
assert_tx_failure "Self-removed cannot add items" "$TX_OUT"

# =========================================================================
# Test 3: Re-add as ADMIN, verify abilities
# =========================================================================
echo ""
echo "--- Test 3: ADMIN can add items ---"
TX_OUT=$(send_tx collect add-collaborator "$COLL1_ID" "$COLLECTOR2_ADDR" admin --from collector1)
assert_tx_success "Re-add as ADMIN" "$TX_OUT"

COLLABS=$(query collect collaborators "$COLL1_ID")
ROLE=$(echo "$COLLABS" | jq -r '.collaborators[0].role // empty' 2>/dev/null)
assert_equal "Role is ADMIN" "COLLABORATOR_ROLE_ADMIN" "$ROLE"

TX_OUT=$(send_tx collect add-item "$COLL1_ID" 0 "AdminItem" "Added by ADMIN" "" unspecified --from collector2)
assert_tx_success "ADMIN can add items" "$TX_OUT"

# Clean up: remove the item
ADMIN_ITEM_ID=$(extract_event_attr "$TX_RESULT_OUT" "item_added" "id")
if [ -n "$ADMIN_ITEM_ID" ]; then
    send_tx collect remove-item "$ADMIN_ITEM_ID" --from collector2 > /dev/null 2>&1
    sleep $TX_WAIT
fi

# =========================================================================
# Test 4: ADMIN cannot add non-member as collaborator
# =========================================================================
echo ""
echo "--- Test 4: ADMIN cannot add non-member ---"
TX_OUT=$(send_tx collect add-collaborator "$COLL1_ID" "$NONMEMBER1_ADDR" editor --from collector2)
assert_tx_failure "ADMIN cannot add non-member" "$TX_OUT"

# Clean up: remove collector2
send_tx collect remove-collaborator "$COLL1_ID" "$COLLECTOR2_ADDR" --from collector1 > /dev/null 2>&1
sleep $TX_WAIT

echo ""
print_summary
exit $?
