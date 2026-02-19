#!/bin/bash
# Endorsement flow tests for x/collect

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/test_helpers.sh"
source "$SCRIPT_DIR/.test_env"

echo "========================================================================="
echo "  X/COLLECT - ENDORSEMENT TESTS"
echo "========================================================================="
echo ""

BLOCK_HEIGHT=$(get_block_height)
FUTURE_BLOCK=$((BLOCK_HEIGHT + 5000))

# =========================================================================
# Test 1: Non-member creates PENDING TTL collection
# =========================================================================
echo "--- Test 1: Non-member creates PENDING TTL collection ---"
TX_OUT=$(send_tx collect create-collection \
    nft public false "$FUTURE_BLOCK" "EndorseColl" "For endorsement" "" "" \
    --from nonmember1)
assert_tx_success "Non-member creates TTL collection" "$TX_OUT"

ENDORSE_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$ENDORSE_COLL_ID" ]; then
    DATA=$(query collect collections-by-owner "$NONMEMBER1_ADDR")
    ENDORSE_COLL_ID=$(echo "$DATA" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi
echo "  Collection ID: $ENDORSE_COLL_ID"

# Verify PENDING status
COLL_QUERY=$(query collect collection "$ENDORSE_COLL_ID")
STATUS=$(echo "$COLL_QUERY" | jq -r '.collection.status // empty' 2>/dev/null)
assert_equal "Collection status is PENDING" "COLLECTION_STATUS_PENDING" "$STATUS"

# =========================================================================
# Test 2: Non-member sets seeking-endorsement
# =========================================================================
echo ""
echo "--- Test 2: Set seeking endorsement ---"
TX_OUT=$(send_tx collect set-seeking-endorsement "$ENDORSE_COLL_ID" true --from nonmember1)
assert_tx_success "Set seeking-endorsement to true" "$TX_OUT"

COLL_QUERY=$(query collect collection "$ENDORSE_COLL_ID")
SEEKING=$(echo "$COLL_QUERY" | jq -r '.collection.seeking_endorsement // "false"' 2>/dev/null)
assert_equal "Collection is seeking endorsement" "true" "$SEEKING"

# =========================================================================
# Test 3: Cannot set seeking-endorsement on non-PENDING collection
# =========================================================================
echo ""
echo "--- Test 3: Cannot set seeking-endorsement on ACTIVE collection ---"
# Use an ACTIVE collection owned by collector1
TX_OUT=$(send_tx collect set-seeking-endorsement "$COLL1_ID" true --from collector1 2>/dev/null)
assert_tx_failure "Cannot set seeking-endorsement on ACTIVE collection" "$TX_OUT"

# =========================================================================
# Test 4: Query pending-collections
# =========================================================================
echo ""
echo "--- Test 4: Query pending-collections ---"
PENDING=$(query collect pending-collections)
PENDING_COUNT=$(echo "$PENDING" | jq -r '.collections | length' 2>/dev/null)
assert_gt "Pending collections exist" "0" "$PENDING_COUNT"

# =========================================================================
# Test 5: Member endorses collection (requires DREAM stake)
# =========================================================================
echo ""
echo "--- Test 5: Member endorses collection ---"
# Endorsement requires EndorsementDreamStake (100 DREAM).
# DREAM is distributed from Alice in setup_test_accounts.sh.
TX_OUT=$(send_tx collect endorse-collection "$ENDORSE_COLL_ID" --from collector1)
assert_tx_success "Member endorses collection" "$TX_OUT"

# Verify collection is now ACTIVE and immutable
COLL_QUERY=$(query collect collection "$ENDORSE_COLL_ID")
STATUS=$(echo "$COLL_QUERY" | jq -r '.collection.status // empty' 2>/dev/null)
IMMUTABLE=$(echo "$COLL_QUERY" | jq -r '.collection.immutable // "false"' 2>/dev/null)
ENDORSED_BY=$(echo "$COLL_QUERY" | jq -r '.collection.endorsed_by // empty' 2>/dev/null)

assert_equal "Endorsed collection status is ACTIVE" "COLLECTION_STATUS_ACTIVE" "$STATUS"
assert_equal "Endorsed collection is immutable" "true" "$IMMUTABLE"
assert_equal "Endorsed by collector1" "$COLLECTOR1_ADDR" "$ENDORSED_BY"

# =========================================================================
# Test 6: Query endorsement
# =========================================================================
echo ""
echo "--- Test 6: Query endorsement ---"
ENDORSE_QUERY=$(query collect endorsement "$ENDORSE_COLL_ID")
ENDORSER=$(echo "$ENDORSE_QUERY" | jq -r '.endorsement.endorser // empty' 2>/dev/null)
assert_equal "Endorsement endorser is collector1" "$COLLECTOR1_ADDR" "$ENDORSER"

# =========================================================================
# Test 7: Cannot endorse already-endorsed collection
# =========================================================================
echo ""
echo "--- Test 7: Cannot endorse already-endorsed collection ---"
TX_OUT=$(send_tx collect endorse-collection "$ENDORSE_COLL_ID" --from collector2)
assert_tx_failure "Cannot endorse already-endorsed collection" "$TX_OUT"

echo ""
print_summary
exit $?
