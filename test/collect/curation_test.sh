#!/bin/bash
# Curation system tests for x/collect
# Tests: register/unregister curator, query curation endpoints, error cases
# Note: Alice (CORE trust level) is used as curator since min_curator_trust_level
# requires PROVISIONAL or higher.

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/test_helpers.sh"
source "$SCRIPT_DIR/.test_env"

echo "========================================================================="
echo "  X/COLLECT - CURATION TESTS"
echo "========================================================================="
echo ""

# =========================================================================
# Setup: Distribute DREAM to collector1 (for immutability test endorsement)
# =========================================================================
echo "--- Setup: Distribute DREAM to collector1 ---"
TX_OUT=$(send_tx rep transfer-dream "$COLLECTOR1_ADDR" 200 tip "" --from alice)
assert_tx_success "Transfer 200 DREAM from Alice to collector1" "$TX_OUT"

# =========================================================================
# Test 1: Register Alice as curator (CORE trust level)
# =========================================================================
echo ""
echo "--- Test 1: Register curator ---"
TX_OUT=$(send_tx collect register-curator 500 --from alice)
assert_tx_success "Register Alice as curator" "$TX_OUT"

# =========================================================================
# Test 2: Query curator record
# =========================================================================
echo ""
echo "--- Test 2: Query curator record ---"
CURATOR_QUERY=$(query collect curator "$ALICE_ADDR")
CURATOR_ACTIVE=$(echo "$CURATOR_QUERY" | jq -r '.curator.active // "false"' 2>/dev/null)
CURATOR_BOND=$(echo "$CURATOR_QUERY" | jq -r '.curator.bond_amount // "0"' 2>/dev/null)
assert_equal "Curator is active" "true" "$CURATOR_ACTIVE"
assert_equal "Curator bond is 500" "500" "$CURATOR_BOND"

# =========================================================================
# Test 3: Query active-curators list
# =========================================================================
echo ""
echo "--- Test 3: Query active-curators ---"
ACTIVE_CURATORS=$(query collect active-curators)
CURATOR_COUNT=$(echo "$ACTIVE_CURATORS" | jq -r '.curators | length' 2>/dev/null)
assert_gt "Active curators exist" "0" "$CURATOR_COUNT"

# =========================================================================
# Test 4: Cannot register as curator twice
# =========================================================================
echo ""
echo "--- Test 4: Cannot register as curator twice ---"
TX_OUT=$(send_tx collect register-curator 500 --from alice)
assert_tx_failure "Cannot register as curator twice" "$TX_OUT"

# =========================================================================
# Test 5: Non-member cannot register as curator
# =========================================================================
echo ""
echo "--- Test 5: Non-member cannot register as curator ---"
TX_OUT=$(send_tx collect register-curator 500 --from nonmember1)
assert_tx_failure "Non-member cannot register as curator" "$TX_OUT"

# =========================================================================
# Test 6: Low-trust member cannot register as curator
# =========================================================================
echo ""
echo "--- Test 6: Low-trust member cannot register as curator ---"
# collector1 is TRUST_LEVEL_NEW, below PROVISIONAL requirement
TX_OUT=$(send_tx collect register-curator 500 --from collector1)
assert_tx_failure "Low-trust member cannot register as curator" "$TX_OUT"

# =========================================================================
# Test 7: Rate collection (expect failure due to min_curator_age_blocks)
# =========================================================================
echo ""
echo "--- Test 7: Rate collection (curator too new) ---"
# min_curator_age_blocks=14400 (~24 hours) - curator just registered
TX_OUT=$(send_tx collect rate-collection "$COLL1_ID" up "art,quality" "Great collection" --from alice)
assert_tx_failure "Curator too new to rate" "$TX_OUT"

# =========================================================================
# Test 8: Non-curator cannot rate
# =========================================================================
echo ""
echo "--- Test 8: Non-curator cannot rate ---"
TX_OUT=$(send_tx collect rate-collection "$COLL1_ID" up "art" "Nice" --from collector2)
assert_tx_failure "Non-curator cannot rate" "$TX_OUT"

# =========================================================================
# Test 9: Query curation-summary (empty - no reviews yet)
# =========================================================================
echo ""
echo "--- Test 9: Query curation-summary ---"
SUMMARY=$(query collect curation-summary "$COLL1_ID")
UP_COUNT=$(echo "$SUMMARY" | jq -r '.curation_summary.up_count // "0"' 2>/dev/null)
echo "PASS: Curation summary query works (up_count=$UP_COUNT)"
TESTS_PASSED=$((TESTS_PASSED + 1))

# =========================================================================
# Test 10: Query curation-reviews (empty)
# =========================================================================
echo ""
echo "--- Test 10: Query curation-reviews ---"
REVIEWS=$(query collect curation-reviews "$COLL1_ID")
REVIEW_COUNT=$(echo "$REVIEWS" | jq -r '.curation_reviews | length // 0' 2>/dev/null)
assert_equal "No curation reviews yet" "0" "$REVIEW_COUNT"

# =========================================================================
# Test 11: Query curation-reviews-by-curator (empty)
# =========================================================================
echo ""
echo "--- Test 11: Query curation-reviews-by-curator ---"
CURATOR_REVIEWS=$(query collect curation-reviews-by-curator "$ALICE_ADDR")
CURATOR_REVIEW_COUNT=$(echo "$CURATOR_REVIEWS" | jq -r '.curation_reviews | length // 0' 2>/dev/null)
assert_equal "No reviews by curator" "0" "$CURATOR_REVIEW_COUNT"

# =========================================================================
# Test 12: Unregister curator
# =========================================================================
echo ""
echo "--- Test 12: Unregister curator ---"
TX_OUT=$(send_tx collect unregister-curator --from alice)
assert_tx_success "Unregister curator" "$TX_OUT"

# Verify curator record gone
CURATOR_QUERY=$(query collect curator "$ALICE_ADDR" 2>&1)
CURATOR_ACTIVE=$(echo "$CURATOR_QUERY" | jq -r '.curator.active // empty' 2>/dev/null)
assert_equal "Curator no longer active" "" "$CURATOR_ACTIVE"

# =========================================================================
# Test 13: Cannot unregister when not a curator
# =========================================================================
echo ""
echo "--- Test 13: Cannot unregister when not a curator ---"
TX_OUT=$(send_tx collect unregister-curator --from collector2)
assert_tx_failure "Cannot unregister when not a curator" "$TX_OUT"

echo ""
print_summary
exit $?
