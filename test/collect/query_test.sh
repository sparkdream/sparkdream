#!/bin/bash
# Query endpoint tests for x/collect

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/test_helpers.sh"
source "$SCRIPT_DIR/.test_env"

echo "========================================================================="
echo "  X/COLLECT - QUERY TESTS"
echo "========================================================================="
echo ""

# =========================================================================
# Test 1: Query params
# =========================================================================
echo "--- Test 1: Query params ---"
PARAMS=$(query collect params)
BASE_DEPOSIT=$(echo "$PARAMS" | jq -r '.params.base_collection_deposit // empty' 2>/dev/null)
assert_not_empty "Params base_collection_deposit is set" "$BASE_DEPOSIT"

MAX_ITEMS=$(echo "$PARAMS" | jq -r '.params.max_items_per_collection // "0"' 2>/dev/null)
assert_gt "Params max_items > 0" "0" "$MAX_ITEMS"

# =========================================================================
# Test 2: Query public-collections
# =========================================================================
echo ""
echo "--- Test 2: Query public-collections ---"
PUBLIC=$(query collect public-collections)
PUBLIC_COUNT=$(echo "$PUBLIC" | jq -r '.collections | length' 2>/dev/null)
assert_gt "Public collections exist" "0" "$PUBLIC_COUNT"
echo "  Found $PUBLIC_COUNT public collections"

# =========================================================================
# Test 3: Query public-collections-by-type
# =========================================================================
echo ""
echo "--- Test 3: Query public-collections-by-type ---"
# Type 1 = COLLECTION_TYPE_NFT (queries use integer enum values)
BY_TYPE=$(query collect public-collections-by-type 1)
BY_TYPE_COUNT=$(echo "$BY_TYPE" | jq -r '.collections | length' 2>/dev/null)
# May or may not have NFT-type collections, just check query works
if [ "$BY_TYPE_COUNT" -ge 0 ] 2>/dev/null; then
    echo "PASS: Query public-collections-by-type works ($BY_TYPE_COUNT found)"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    echo "FAIL: Query public-collections-by-type failed"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# =========================================================================
# Test 4: Query active-curators (should be empty)
# =========================================================================
echo ""
echo "--- Test 4: Query active-curators ---"
CURATORS=$(query collect active-curators)
CURATOR_COUNT=$(echo "$CURATORS" | jq -r '.curators | length' 2>/dev/null)
if [ "$CURATOR_COUNT" -ge 0 ] 2>/dev/null; then
    echo "PASS: Query active-curators works ($CURATOR_COUNT found)"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    echo "FAIL: Query active-curators failed"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# =========================================================================
# Test 5: Query pending-collections
# =========================================================================
echo ""
echo "--- Test 5: Query pending-collections ---"
PENDING=$(query collect pending-collections)
PENDING_COUNT=$(echo "$PENDING" | jq -r '.collections | length' 2>/dev/null)
if [ "$PENDING_COUNT" -ge 0 ] 2>/dev/null; then
    echo "PASS: Query pending-collections works ($PENDING_COUNT found)"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    echo "FAIL: Query pending-collections failed"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# =========================================================================
# Test 6: Query flagged-content
# =========================================================================
echo ""
echo "--- Test 6: Query flagged-content ---"
FLAGGED=$(query collect flagged-content)
FLAGGED_COUNT=$(echo "$FLAGGED" | jq -r '.flags | length' 2>/dev/null)
if [ "$FLAGGED_COUNT" -ge 0 ] 2>/dev/null; then
    echo "PASS: Query flagged-content works ($FLAGGED_COUNT found)"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    echo "FAIL: Query flagged-content failed"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# =========================================================================
# Test 7: Query sponsorship-requests
# =========================================================================
echo ""
echo "--- Test 7: Query sponsorship-requests ---"
REQUESTS=$(query collect sponsorship-requests)
REQ_COUNT=$(echo "$REQUESTS" | jq -r '.sponsorship_requests | length' 2>/dev/null)
if [ "$REQ_COUNT" -ge 0 ] 2>/dev/null; then
    echo "PASS: Query sponsorship-requests works ($REQ_COUNT found)"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    echo "FAIL: Query sponsorship-requests failed"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

echo ""
print_summary
exit $?
