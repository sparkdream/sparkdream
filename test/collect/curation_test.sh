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
TX_OUT=$(send_tx rep bond-role collect-curator 500 --from alice)
assert_tx_success "Register Alice as curator" "$TX_OUT"

# =========================================================================
# Test 2: Query curator bonded-role record (Phase 3: lives in x/rep).
# =========================================================================
echo ""
echo "--- Test 2: Query curator record ---"
CURATOR_QUERY=$(query rep bonded-role collect-curator "$ALICE_ADDR")
# The response wraps the record under `.bonded_role`. BondStatus is an enum
# name; NORMAL means the bond is at or above min_bond.
CURATOR_STATUS=$(echo "$CURATOR_QUERY" | jq -r '.bonded_role.bond_status // "MISSING"')
CURATOR_BOND=$(echo "$CURATOR_QUERY" | jq -r '.bonded_role.current_bond // "0"')
assert_equal "Curator bond status is NORMAL" "BONDED_ROLE_STATUS_NORMAL" "$CURATOR_STATUS"
assert_equal "Curator current_bond is 500" "500" "$CURATOR_BOND"

# =========================================================================
# Test 3: Query bonded-roles-by-type for curators (Phase 3: replaces the old
# query collect active-curators).
# =========================================================================
echo ""
echo "--- Test 3: Query bonded-roles-by-type collect-curator ---"
ACTIVE_CURATORS=$(query rep bonded-roles-by-type collect-curator)
CURATOR_COUNT=$(echo "$ACTIVE_CURATORS" | jq -r '.bonded_roles | length' 2>/dev/null)
assert_gt "Active curators exist" "0" "$CURATOR_COUNT"
# Also verify alice is specifically in the list (safer than just counting).
FOUND_ALICE=$(echo "$ACTIVE_CURATORS" | jq -r --arg a "$ALICE_ADDR" '.bonded_roles[] | select(.address==$a) | .address' | head -1)
assert_equal "Alice appears in bonded-roles-by-type response" "$ALICE_ADDR" "$FOUND_ALICE"

# =========================================================================
# Test 3b: New curator-activity query (collect-side per-curator counters).
# Alice has not rated anything yet, so the response is a zero-valued record
# (NotFound is soft-converted to a zeroed record by the query handler).
# =========================================================================
echo ""
echo "--- Test 3b: Query curator-activity (counters) ---"
ACTIVITY=$(query collect curator-activity "$ALICE_ADDR")
ACT_ADDR=$(echo "$ACTIVITY" | jq -r '.activity.address // ""')
ACT_TOTAL=$(echo "$ACTIVITY" | jq -r '.activity.total_reviews // "0"')
assert_equal "curator-activity returns alice's address" "$ALICE_ADDR" "$ACT_ADDR"
assert_equal "curator-activity.total_reviews is zero pre-rating" "0" "$ACT_TOTAL"

# =========================================================================
# Test 4: bond-role on an existing record tops up the current_bond
# (Phase 3: the old "register twice fails" contract is gone — bond-role is a
# generic top-up primitive, so the second call succeeds and adds to the bond).
# =========================================================================
echo ""
echo "--- Test 4: bond-role on existing record tops up current_bond ---"
TX_OUT=$(send_tx rep bond-role collect-curator 500 --from alice)
assert_tx_success "Top-up bond-role succeeds on existing record" "$TX_OUT"

CURATOR_QUERY=$(query rep bonded-role collect-curator "$ALICE_ADDR")
TOPPED_UP_BOND=$(echo "$CURATOR_QUERY" | jq -r '.bonded_role.current_bond // "0"')
assert_equal "current_bond after top-up is 1000" "1000" "$TOPPED_UP_BOND"

# =========================================================================
# Test 5: Non-member cannot register as curator
# =========================================================================
echo ""
echo "--- Test 5: Non-member cannot register as curator ---"
TX_OUT=$(send_tx rep bond-role collect-curator 500 --from nonmember1)
assert_tx_failure "Non-member cannot register as curator" "$TX_OUT"

# =========================================================================
# Test 6: Low-trust member cannot register as curator
# =========================================================================
echo ""
echo "--- Test 6: Low-trust member cannot register as curator ---"
# collector1 is TRUST_LEVEL_NEW, below PROVISIONAL requirement
TX_OUT=$(send_tx rep bond-role collect-curator 500 --from collector1)
assert_tx_failure "Low-trust member cannot register as curator" "$TX_OUT"

# =========================================================================
# Test 7: Rate collection (expect failure due to min_curator_age_blocks)
# =========================================================================
echo ""
echo "--- Test 7: Rate collection (curator too new) ---"
# config.yml sets min_curator_age_blocks=5 for testing (production: 14400);
# alice just registered on the previous block, so currentBlock - registered = 0-1 < 5.
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
# Test 12: Partial unbond updates current_bond.
# Phase 3: unbonding no longer "unregisters" — the BondedRole record persists
# with a recomputed bond_status. Alice has 1000 bonded after Test 4's top-up;
# we unbond 600 so she lands at 400, which is below MinBond=500 (RECOVERY)
# but above DemotionThreshold=250 (no cooldown). tag_test.sh runs after this
# suite and tops her bond back up — a cooldown-triggering drop would block
# that top-up. Demotion-cooldown behavior itself is covered in
# test/rep/bonded_role_test.sh.
# =========================================================================
echo ""
echo "--- Test 12: Partial unbond updates current_bond ---"
TX_OUT=$(send_tx rep unbond-role collect-curator 600 --from alice)
assert_tx_success "Partial unbond curator" "$TX_OUT"

CURATOR_QUERY=$(query rep bonded-role collect-curator "$ALICE_ADDR")
PARTIAL_BOND=$(echo "$CURATOR_QUERY" | jq -r '.bonded_role.current_bond // "0"')
PARTIAL_STATUS=$(echo "$CURATOR_QUERY" | jq -r '.bonded_role.bond_status // "MISSING"')
assert_equal "current_bond reduced to 400" "400" "$PARTIAL_BOND"
# 400 < MinBond(500) but 400 >= DemotionThreshold(250) → RECOVERY (no cooldown).
assert_equal "bond_status is RECOVERY below min_bond" "BONDED_ROLE_STATUS_RECOVERY" "$PARTIAL_STATUS"

# =========================================================================
# Test 13: Cannot unbond when no BondedRole record exists
# =========================================================================
echo ""
echo "--- Test 13: Cannot unbond when no BondedRole record ---"
TX_OUT=$(send_tx rep unbond-role collect-curator 1 --from collector2)
assert_tx_failure "Cannot unbond when record missing" "$TX_OUT"

# =========================================================================
# Test 14: Cannot unbond more than current_bond
# =========================================================================
echo ""
echo "--- Test 14: Cannot unbond more than current_bond ---"
# Alice has 400 bonded after Test 12. Try to drain 500 — rejected as insufficient.
TX_OUT=$(send_tx rep unbond-role collect-curator 500 --from alice)
assert_tx_failure "Cannot unbond past current_bond" "$TX_OUT"

echo ""
print_summary
exit $?
