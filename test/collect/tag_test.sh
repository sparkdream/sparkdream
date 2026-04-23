#!/bin/bash
# Tag validation, usage tracking, and ListCollectionsByTag tests for x/collect.

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/test_helpers.sh"
source "$SCRIPT_DIR/.test_env"

echo "========================================================================="
echo "  X/COLLECT - TAG VALIDATION & LIST-BY-TAG TESTS"
echo "========================================================================="
echo ""

# Genesis-registered tags we can safely attach to content.
TAG_A="commons-council"
TAG_B="technical-council"
TAG_C="ecosystem-council"

# Primary collection creator for tag tests. collector1/collector2 are
# PROVISIONAL-trust members (max_collections_base=5) and by the time tag_test.sh
# runs as the last suite in collect/run_all_tests.sh they have already exhausted
# their tier collection limit from earlier tests. bob is a genesis member at
# TRUST_LEVEL_ESTABLISHED (max = 5 + 15 = 20 collections) and owns 0 collections
# entering this suite, so he has plenty of room for the tagged collections we
# create here without affecting other tests.
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test 2>/dev/null)
TAG_CREATOR="bob"
TAG_CREATOR_ADDR="$BOB_ADDR"

# ------------------------------------------------------------------
# Local helpers specific to tag tests.
# ------------------------------------------------------------------

tag_usage_count() {
    local TAG=$1
    local DATA=$($BINARY query rep get-tag "$TAG" --output json 2>&1)
    echo "$DATA" | jq -r '(.tag.usage_count // 0) | tonumber' 2>/dev/null || echo "0"
}

list_by_tag_ids() {
    local TAG=$1
    local DATA=$($BINARY query collect list-collections-by-tag --tag "$TAG" --output json 2>&1)
    echo "$DATA" | jq -r '(.collections // []) | .[] | (.id // "0") | tostring' 2>/dev/null
}

# =========================================================================
# Test 1: Collect params expose tag-related fields
# =========================================================================
echo "--- Test 1: Collect params expose tag params ---"
PARAMS=$(query collect params)
MAX_TAGS_COLL=$(echo "$PARAMS" | jq -r '.params.max_tags_per_collection // "0"')
MAX_TAGS_REV=$(echo "$PARAMS" | jq -r '.params.max_tags_per_review // "0"')
MAX_TAG_LEN=$(echo "$PARAMS" | jq -r '.params.max_tag_length // "0"')
echo "  max_tags_per_collection: $MAX_TAGS_COLL"
echo "  max_tags_per_review:     $MAX_TAGS_REV"
echo "  max_tag_length:          $MAX_TAG_LEN"
assert_gt "max_tags_per_collection > 0" "0" "$MAX_TAGS_COLL"
assert_gt "max_tag_length > 0"          "0" "$MAX_TAG_LEN"

# =========================================================================
# Test 2: Create collection with valid tags — usage_count increments
# =========================================================================
echo ""
echo "--- Test 2: Create collection with valid tags ---"
USAGE_A_BEFORE=$(tag_usage_count "$TAG_A")
USAGE_B_BEFORE=$(tag_usage_count "$TAG_B")
echo "  Baseline: $TAG_A=$USAGE_A_BEFORE, $TAG_B=$USAGE_B_BEFORE"

TX_OUT=$(send_tx collect create-collection \
    mixed public false 0 "TaggedColl" "With tags" "" "$TAG_A,$TAG_B" \
    --from "$TAG_CREATOR")
assert_tx_success "Create collection with valid tags" "$TX_OUT"

TAGGED_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$TAGGED_COLL_ID" ]; then
    FALLBACK=$(query collect collections-by-owner "$TAG_CREATOR_ADDR")
    TAGGED_COLL_ID=$(echo "$FALLBACK" | jq -r '(.collections[-1].id // 0) | tostring' 2>/dev/null)
fi
echo "  Collection ID: $TAGGED_COLL_ID"
assert_not_empty "Captured TAGGED_COLL_ID" "$TAGGED_COLL_ID"

# Verify the collection record carries both tags.
COLL_Q=$(query collect collection "$TAGGED_COLL_ID")
COLL_TAGS=$(echo "$COLL_Q" | jq -r '.collection.tags // [] | sort | join(",")')
EXPECTED_TAGS=$(echo -e "$TAG_A\n$TAG_B" | sort | paste -sd, -)
assert_equal "Collection carries both tags" "$EXPECTED_TAGS" "$COLL_TAGS"

USAGE_A_AFTER=$(tag_usage_count "$TAG_A")
USAGE_B_AFTER=$(tag_usage_count "$TAG_B")
DIFF_A=$((USAGE_A_AFTER - USAGE_A_BEFORE))
DIFF_B=$((USAGE_B_AFTER - USAGE_B_BEFORE))
echo "  After create: $TAG_A +$DIFF_A, $TAG_B +$DIFF_B"
assert_gt "usage_count bumped for $TAG_A" "0" "$DIFF_A"
assert_gt "usage_count bumped for $TAG_B" "0" "$DIFF_B"

# =========================================================================
# Test 3: ListCollectionsByTag returns the collection for each of its tags
# =========================================================================
echo ""
echo "--- Test 3: ListCollectionsByTag returns collection ---"
A_IDS=$(list_by_tag_ids "$TAG_A")
B_IDS=$(list_by_tag_ids "$TAG_B")
if echo "$A_IDS" | grep -qx "$TAGGED_COLL_ID" && echo "$B_IDS" | grep -qx "$TAGGED_COLL_ID"; then
    echo "PASS: Collection appears under both tags"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    echo "FAIL: Missing under one tag"
    echo "  $TAG_A: $A_IDS"
    echo "  $TAG_B: $B_IDS"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# =========================================================================
# Test 4: ListCollectionsByTag with unknown tag returns empty
# =========================================================================
echo ""
echo "--- Test 4: ListCollectionsByTag unknown tag returns empty ---"
EMPTY_IDS=$(list_by_tag_ids "nonexistent-tag-xyz-no-match")
if [ -z "$EMPTY_IDS" ]; then
    echo "PASS: Empty result for unknown tag"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    echo "FAIL: Expected empty, got: $EMPTY_IDS"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# =========================================================================
# Test 5: ListCollectionsByTag with empty tag — error
# =========================================================================
echo ""
echo "--- Test 5: ListCollectionsByTag with empty tag rejected ---"
ERR_OUT=$($BINARY query collect list-collections-by-tag --tag "" --output json 2>&1)
if echo "$ERR_OUT" | grep -qi "tag cannot be empty\|invalid"; then
    echo "PASS: Empty tag rejected"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    echo "FAIL: Expected error for empty tag"
    echo "  Output: $(echo "$ERR_OUT" | head -3)"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# =========================================================================
# Test 6: Reject create-collection with unknown tag
# =========================================================================
echo ""
echo "--- Test 6: Reject create-collection with unknown tag ---"
TX_OUT=$(send_tx collect create-collection \
    mixed public false 0 "UnknownTagColl" "Should fail" "" "totally-fake-unregistered-tag" \
    --from "$TAG_CREATOR")
assert_tx_failure "Reject unknown tag on create" "$TX_OUT"

# =========================================================================
# Test 7: Reject create-collection with duplicate tag
# =========================================================================
echo ""
echo "--- Test 7: Reject create-collection with duplicate tag ---"
TX_OUT=$(send_tx collect create-collection \
    mixed public false 0 "DupTagColl" "Should fail" "" "$TAG_A,$TAG_A" \
    --from "$TAG_CREATOR")
assert_tx_failure "Reject duplicate tag on create" "$TX_OUT"

# =========================================================================
# Test 8: Reject create-collection with uppercase/malformed tag
# =========================================================================
echo ""
echo "--- Test 8: Reject create-collection with malformed tag ---"
TX_OUT=$(send_tx collect create-collection \
    mixed public false 0 "MalformedTagColl" "Should fail" "" "NotLowercase" \
    --from "$TAG_CREATOR")
assert_tx_failure "Reject malformed tag on create" "$TX_OUT"

# =========================================================================
# Test 9: Reject create-collection with overly-long tag
# =========================================================================
echo ""
echo "--- Test 9: Reject create-collection with tag > max_tag_length ---"
LONG_TAG=$(printf 'a%.0s' {1..33})
TX_OUT=$(send_tx collect create-collection \
    mixed public false 0 "LongTagColl" "Should fail" "" "$LONG_TAG" \
    --from "$TAG_CREATOR")
assert_tx_failure "Reject overly-long tag on create" "$TX_OUT"

# =========================================================================
# Test 10: Reject create-collection exceeding max_tags_per_collection
# =========================================================================
echo ""
echo "--- Test 10: Reject create-collection with too many tags ---"
# Default MaxTagsPerCollection=10, so pass 11 known tags to trip only the count cap.
OVERFLOW_TAGS="commons-council,technical-council,ecosystem-council,commons-ops-committee,commons-gov-committee,technical-ops-committee,technical-gov-committee,ecosystem-ops-committee,ecosystem-gov-committee,advanced-physics,budget"
TX_OUT=$(send_tx collect create-collection \
    mixed public false 0 "OverflowColl" "Should fail" "" "$OVERFLOW_TAGS" \
    --from "$TAG_CREATOR")
assert_tx_failure "Reject create-collection with too many tags" "$TX_OUT"

# =========================================================================
# Test 11: UpdateCollection diffs the tag set — only added tags bump usage
# =========================================================================
echo ""
echo "--- Test 11: UpdateCollection diffs tags — only added bumps usage ---"

# Re-read current usage counts post-create as the new baseline.
U_A_PRE=$(tag_usage_count "$TAG_A")
U_B_PRE=$(tag_usage_count "$TAG_B")
U_C_PRE=$(tag_usage_count "$TAG_C")
echo "  Pre-update: $TAG_A=$U_A_PRE $TAG_B=$U_B_PRE $TAG_C=$U_C_PRE"

# Drop TAG_B, add TAG_C; keep TAG_A.
TX_OUT=$(send_tx collect update-collection \
    "$TAGGED_COLL_ID" mixed 0 "TaggedColl" "With diffed tags" "" "$TAG_A,$TAG_C" \
    --from "$TAG_CREATOR")
assert_tx_success "Update collection tag set" "$TX_OUT"

U_A_POST=$(tag_usage_count "$TAG_A")
U_B_POST=$(tag_usage_count "$TAG_B")
U_C_POST=$(tag_usage_count "$TAG_C")
echo "  Post-update: $TAG_A=$U_A_POST $TAG_B=$U_B_POST $TAG_C=$U_C_POST"

# Expect: TAG_A unchanged (kept), TAG_B unchanged (removed), TAG_C +1 (added).
assert_equal "usage_count for kept tag unchanged"   "$U_A_PRE" "$U_A_POST"
assert_equal "usage_count for removed tag unchanged" "$U_B_PRE" "$U_B_POST"
DIFF_C=$((U_C_POST - U_C_PRE))
assert_gt "usage_count for added tag bumped" "0" "$DIFF_C"

# Secondary index reflects the diff.
A_IDS=$(list_by_tag_ids "$TAG_A")
B_IDS=$(list_by_tag_ids "$TAG_B")
C_IDS=$(list_by_tag_ids "$TAG_C")
if echo "$A_IDS" | grep -qx "$TAGGED_COLL_ID" \
    && ! echo "$B_IDS" | grep -qx "$TAGGED_COLL_ID" \
    && echo "$C_IDS" | grep -qx "$TAGGED_COLL_ID"; then
    echo "PASS: Tag index diffed correctly"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    echo "FAIL: Tag index not diffed correctly"
    echo "  $TAG_A: $A_IDS"
    echo "  $TAG_B (expected no $TAGGED_COLL_ID): $B_IDS"
    echo "  $TAG_C (expected $TAGGED_COLL_ID): $C_IDS"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# =========================================================================
# Test 12: Reject update-collection with unknown tag
# =========================================================================
echo ""
echo "--- Test 12: Reject update-collection with unknown tag ---"
TX_OUT=$(send_tx collect update-collection \
    "$TAGGED_COLL_ID" mixed 0 "TaggedColl" "With bad tag" "" "$TAG_A,bogus-unknown-tag-xyz" \
    --from "$TAG_CREATOR")
assert_tx_failure "Reject update-collection with unknown tag" "$TX_OUT"

# =========================================================================
# Test 13: Delete collection clears tag index entries
# =========================================================================
echo ""
echo "--- Test 13: Delete collection clears tag index entries ---"
TX_OUT=$(send_tx collect delete-collection "$TAGGED_COLL_ID" --from "$TAG_CREATOR")
assert_tx_success "Delete tagged collection" "$TX_OUT"

A_IDS_AFTER=$(list_by_tag_ids "$TAG_A")
C_IDS_AFTER=$(list_by_tag_ids "$TAG_C")
if ! echo "$A_IDS_AFTER" | grep -qx "$TAGGED_COLL_ID" \
    && ! echo "$C_IDS_AFTER" | grep -qx "$TAGGED_COLL_ID"; then
    echo "PASS: Tag index entries cleared on delete"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    echo "FAIL: Stale tag index entry after delete"
    echo "  $TAG_A: $A_IDS_AFTER"
    echo "  $TAG_C: $C_IDS_AFTER"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# =========================================================================
# Test 14: Reserve a tag via x/rep report+resolve flow, then reject on create
# =========================================================================
# Pattern mirrors test/rep/tag_budget_test.sh Part 22. We reserve a genesis
# tag ("cryptography") that no other collect test uses, so no earlier tests
# are perturbed.
echo ""
echo "--- Test 14: Reserve tag then reject create-collection with it ---"

RESERVED_TAG="cryptography"

# Step 1: alice reports the tag (locks DREAM bond from alice's founder stash).
TX_OUT=$($BINARY tx rep report-tag \
    "$RESERVED_TAG" "Reserve for collect e2e test" \
    --from alice --chain-id $CHAIN_ID --keyring-backend test \
    --fees 50000uspark -y --output json 2>&1)
assert_tx_success "Report tag for reserve test" "$TX_OUT"

# Step 2: alice resolves with action=2 (reserve). Alice is on the Commons
# Operations Committee, which is authorized to resolve tag reports.
# reserveMembersCanUse=false so validateTags.IsReservedTag rejects at CREATE.
TX_OUT=$($BINARY tx rep resolve-tag-report \
    "$RESERVED_TAG" "2" "$ALICE_ADDR" "false" \
    --from alice --chain-id $CHAIN_ID --keyring-backend test \
    --fees 50000uspark -y --output json 2>&1)
assert_tx_success "Resolve tag report as reserve (action=2)" "$TX_OUT"

# Verify the reserved-tag entry exists.
RES_INFO=$($BINARY query rep get-reserved-tag "$RESERVED_TAG" --output json 2>&1)
RES_NAME=$(echo "$RES_INFO" | jq -r '.reserved_tag.name // .reservedTag.name // empty')
assert_equal "Reserved tag entry stored" "$RESERVED_TAG" "$RES_NAME"

# Reject create-collection that references the reserved tag.
TX_OUT=$(send_tx collect create-collection \
    mixed public false 0 "ReservedTagColl" "Should fail" "" "$RESERVED_TAG" \
    --from "$TAG_CREATOR")
assert_tx_failure "Reject create-collection with reserved tag" "$TX_OUT"

# =========================================================================
# Test 15: rate-collection populates review tags and bumps usage_count
# =========================================================================
# Requires min_curator_age_blocks to be low enough that a short sleep ages
# the curator past the gate. config.yml overrides this to 5 blocks (production
# default is 14400) so this test can actually reach the validateTags path.
# curation_test.sh's "curator too new" assertion still fires because its
# rate attempt happens on the same block as register (0 < 5).
echo ""
echo "--- Test 15: rate-collection with tags populates review + bumps usage ---"

# curation_test.sh unregistered alice (test 12), so re-register her as curator.
TX_OUT=$(send_tx collect register-curator 500 --from alice)
assert_tx_success "Re-register alice as curator" "$TX_OUT"

# Create a fresh collection owned by bob (alice is neither owner nor
# collaborator — rate-collection requires both).
TX_OUT=$(send_tx collect create-collection \
    mixed public false 0 "RatableColl" "For curator rating" "" "" \
    --from "$TAG_CREATOR")
assert_tx_success "Create collection to rate" "$TX_OUT"
RATABLE_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$RATABLE_ID" ]; then
    FALLBACK=$(query collect collections-by-owner "$TAG_CREATOR_ADDR")
    RATABLE_ID=$(echo "$FALLBACK" | jq -r '(.collections[-1].id // 0) | tostring')
fi
echo "  Ratable collection ID: $RATABLE_ID"

# Age the curator past min_curator_age_blocks=5. With typical 2-3s block time
# this is well under 30s; we wait longer to stay safe across CI variance.
echo "  Waiting ~30s for curator to age past min_curator_age_blocks..."
sleep 30

# Baseline review-tag usage_count before rate-collection.
REV_USAGE_A_BEFORE=$(tag_usage_count "$TAG_A")
REV_USAGE_B_BEFORE=$(tag_usage_count "$TAG_B")

# Rate the collection with two tags. MaxTagsPerReview default is 5.
TX_OUT=$(send_tx collect rate-collection \
    "$RATABLE_ID" up "$TAG_A,$TAG_B" "Solid curation test collection" \
    --from alice)
assert_tx_success "Rate collection with tags" "$TX_OUT"

# Review carries the tags we supplied. The collect module exposes
# curation-reviews (by collection) and curation-reviews-by-curator; there is
# no single-review query, so we query by collection and pick the one matching
# the emitted review_id.
REVIEW_ID=$(extract_event_attr "$TX_RESULT_OUT" "collection_rated" "review_id")
if [ -n "$REVIEW_ID" ]; then
    REVIEWS_Q=$(query collect curation-reviews "$RATABLE_ID")
    REVIEW_TAGS=$(echo "$REVIEWS_Q" | jq -r --arg rid "$REVIEW_ID" \
        '(.reviews // .curation_reviews // [])[] | select((.id // "0") | tostring == $rid) | .tags // [] | sort | join(",")')
    EXPECTED_REV_TAGS=$(echo -e "$TAG_A\n$TAG_B" | sort | paste -sd, -)
    assert_equal "Review record carries both tags" "$EXPECTED_REV_TAGS" "$REVIEW_TAGS"
else
    echo "  (review_id missing from events — skipping review-content assertion)"
fi

# IncrementTagUsage was called once per review tag.
REV_USAGE_A_AFTER=$(tag_usage_count "$TAG_A")
REV_USAGE_B_AFTER=$(tag_usage_count "$TAG_B")
REV_DIFF_A=$((REV_USAGE_A_AFTER - REV_USAGE_A_BEFORE))
REV_DIFF_B=$((REV_USAGE_B_AFTER - REV_USAGE_B_BEFORE))
assert_gt "usage_count bumped for review tag $TAG_A" "0" "$REV_DIFF_A"
assert_gt "usage_count bumped for review tag $TAG_B" "0" "$REV_DIFF_B"

# =========================================================================
# Test 16: rate-collection rejects unknown review tag
# =========================================================================
# Curator-age gate is already satisfied from Test 15, so validateTags is
# actually reached here.
echo ""
echo "--- Test 16: Reject rate-collection with unknown review tag ---"

# Create another fresh collection so alice isn't hitting ErrAlreadyReviewed.
TX_OUT=$(send_tx collect create-collection \
    mixed public false 0 "RatableColl2" "For curator rating #2" "" "" \
    --from "$TAG_CREATOR")
assert_tx_success "Create second collection to rate" "$TX_OUT"
RATABLE_ID2=$(extract_event_attr "$TX_RESULT_OUT" "collection_created" "id")
if [ -z "$RATABLE_ID2" ]; then
    FALLBACK=$(query collect collections-by-owner "$TAG_CREATOR_ADDR")
    RATABLE_ID2=$(echo "$FALLBACK" | jq -r '(.collections[-1].id // 0) | tostring')
fi

TX_OUT=$(send_tx collect rate-collection \
    "$RATABLE_ID2" up "fake-nonexistent-tag" "Review with unknown tag" \
    --from alice)
assert_tx_failure "Reject rate-collection with unknown tag" "$TX_OUT"

# =========================================================================
# Test 17: rate-collection rejects reserved tag on review
# =========================================================================
# Reuses the reserved tag from Test 14.
echo ""
echo "--- Test 17: Reject rate-collection with reserved review tag ---"

TX_OUT=$(send_tx collect rate-collection \
    "$RATABLE_ID2" up "$RESERVED_TAG" "Review with reserved tag" \
    --from alice)
assert_tx_failure "Reject rate-collection with reserved tag" "$TX_OUT"

# =========================================================================
# SUMMARY
# =========================================================================
print_summary
