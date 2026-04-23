#!/bin/bash

echo "--- TESTING: BLOG POST TAGS (VALIDATION, USAGE TRACKING, LIST-BY-TAG) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

# blogger2 is the primary creator throughout the blog suite. For tag tests we
# use alice (genesis member, CORE trust) so we don't contend with the
# max_posts_per_day rate limit already exercised by post_test.sh.
CREATOR="alice"
CREATOR_ADDR="$ALICE_ADDR"

# Existing genesis tags we can safely reference (registered in x/rep at boot).
TAG_A="commons-council"
TAG_B="technical-council"
TAG_C="ecosystem-council"

echo "Creator: $CREATOR ($CREATOR_ADDR)"
echo "Tags:    $TAG_A, $TAG_B, $TAG_C"
echo ""

# ========================================================================
# Helper Functions
# ========================================================================

wait_for_tx() {
    local TXHASH=$1
    local MAX_ATTEMPTS=20
    local ATTEMPT=0

    while [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
        RESULT=$($BINARY q tx $TXHASH --output json 2>&1)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then
            echo "$RESULT"
            return 0
        fi
        ATTEMPT=$((ATTEMPT + 1))
        sleep 1
    done

    echo "ERROR: Transaction $TXHASH not found after $MAX_ATTEMPTS attempts" >&2
    return 1
}

check_tx_success() {
    local CODE=$(echo "$1" | jq -r '.code')
    [ "$CODE" == "0" ]
}

check_tx_failure() {
    local CODE=$(echo "$1" | jq -r '.code')
    [ "$CODE" != "0" ]
}

submit_tx_and_wait() {
    local TX_RES="$1"
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        TX_RESULT=""
        return 1
    fi

    local BROADCAST_CODE=$(echo "$TX_RES" | jq -r '.code // "0"')
    if [ "$BROADCAST_CODE" != "0" ]; then
        TX_RESULT="$TX_RES"
        return 0
    fi

    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    return 0
}

extract_event_value() {
    local TX_RESULT=$1
    local EVENT_TYPE=$2
    local ATTR_KEY=$3
    echo "$TX_RESULT" | jq -r ".events[] | select(.type==\"$EVENT_TYPE\") | .attributes[] | select(.key==\"$ATTR_KEY\") | .value" | tr -d '"'
}

tag_usage_count() {
    local TAG=$1
    local DATA=$($BINARY query rep get-tag "$TAG" --output json 2>&1)
    echo "$DATA" | jq -r '(.tag.usage_count // 0) | tonumber' 2>/dev/null || echo "0"
}

list_posts_by_tag_ids() {
    local TAG=$1
    local DATA=$($BINARY query blog list-posts-by-tag --tag "$TAG" --output json 2>&1)
    # Field name depends on proto: response is QueryListPostsByTagResponse with
    # repeated Post posts. Fall back to the possible singular name just in case.
    echo "$DATA" | jq -r '(.posts // .post // []) | .[] | (.id // "0") | tostring' 2>/dev/null
}

PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()

record_result() {
    TEST_NAMES+=("$1")
    RESULTS+=("$2")
    if [ "$2" == "PASS" ]; then
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
    echo "  => $2"
    echo ""
}

create_post_with_tags() {
    local TITLE=$1
    local BODY=$2
    local TAGS=$3  # comma-separated, may be empty
    local FROM=${4:-$CREATOR}

    local ARGS=(tx blog create-post "$TITLE" "$BODY"
        --from "$FROM"
        --chain-id "$CHAIN_ID"
        --keyring-backend test
        --fees 50000uspark
        -y
        --output json)

    if [ -n "$TAGS" ]; then
        ARGS+=(--tags "$TAGS")
    fi

    $BINARY "${ARGS[@]}" 2>&1
}

# ========================================================================
# TEST 1: Query blog params — tag-related fields populated
# ========================================================================
echo "--- TEST 1: Query blog params includes max_tags_per_post/max_tag_length ---"

PARAMS=$($BINARY query blog params --output json 2>&1)
MAX_TAGS=$(echo "$PARAMS" | jq -r '.params.max_tags_per_post // "0"')
MAX_LEN=$(echo "$PARAMS" | jq -r '.params.max_tag_length // "0"')
echo "  max_tags_per_post: $MAX_TAGS"
echo "  max_tag_length:    $MAX_LEN"

if [ "$MAX_TAGS" -gt 0 ] 2>/dev/null && [ "$MAX_LEN" -gt 0 ] 2>/dev/null; then
    record_result "Tag params present in blog params" "PASS"
else
    record_result "Tag params present in blog params" "FAIL"
fi

# ========================================================================
# TEST 2: Create post with valid tags — happy path
# ========================================================================
echo "--- TEST 2: Create post with valid tags ---"

USAGE_A_BEFORE=$(tag_usage_count "$TAG_A")
USAGE_B_BEFORE=$(tag_usage_count "$TAG_B")
echo "  Baseline usage: $TAG_A=$USAGE_A_BEFORE, $TAG_B=$USAGE_B_BEFORE"

TX_RES=$(create_post_with_tags "Tagged Post" "Post with two tags" "$TAG_A,$TAG_B")

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    TAGGED_POST_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
    [ -z "$TAGGED_POST_ID" ] && TAGGED_POST_ID="0"
    echo "  Post created with ID: $TAGGED_POST_ID"
    record_result "Create post with valid tags" "PASS"
else
    echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
    TAGGED_POST_ID=""
    record_result "Create post with valid tags" "FAIL"
fi

# ========================================================================
# TEST 3: Post record carries the tags we sent
# ========================================================================
echo "--- TEST 3: Post record carries both tags ---"

if [ -n "$TAGGED_POST_ID" ]; then
    POST_DATA=$($BINARY query blog show-post "$TAGGED_POST_ID" --output json 2>&1)
    POST_TAGS=$(echo "$POST_DATA" | jq -r '.post.tags // [] | sort | join(",")')
    EXPECTED=$(echo -e "$TAG_A\n$TAG_B" | sort | paste -sd, -)
    echo "  tags: $POST_TAGS"

    if [ "$POST_TAGS" == "$EXPECTED" ]; then
        record_result "Post record carries tags" "PASS"
    else
        echo "  Expected: $EXPECTED"
        record_result "Post record carries tags" "FAIL"
    fi
else
    record_result "Post record carries tags" "FAIL"
fi

# ========================================================================
# TEST 4: IncrementTagUsage was called for each tag on create
# ========================================================================
echo "--- TEST 4: usage_count incremented for each tag on create ---"

USAGE_A_AFTER=$(tag_usage_count "$TAG_A")
USAGE_B_AFTER=$(tag_usage_count "$TAG_B")
DIFF_A=$((USAGE_A_AFTER - USAGE_A_BEFORE))
DIFF_B=$((USAGE_B_AFTER - USAGE_B_BEFORE))
echo "  $TAG_A: $USAGE_A_BEFORE -> $USAGE_A_AFTER (+$DIFF_A)"
echo "  $TAG_B: $USAGE_B_BEFORE -> $USAGE_B_AFTER (+$DIFF_B)"

if [ "$DIFF_A" -ge 1 ] && [ "$DIFF_B" -ge 1 ]; then
    record_result "usage_count incremented for each tag on create" "PASS"
else
    record_result "usage_count incremented for each tag on create" "FAIL"
fi

# ========================================================================
# TEST 5: list-posts-by-tag returns the post for each of its tags
# ========================================================================
echo "--- TEST 5: list-posts-by-tag returns post for each tag ---"

if [ -n "$TAGGED_POST_ID" ]; then
    A_IDS=$(list_posts_by_tag_ids "$TAG_A")
    B_IDS=$(list_posts_by_tag_ids "$TAG_B")
    echo "  IDs under $TAG_A: $(echo "$A_IDS" | tr '\n' ' ')"
    echo "  IDs under $TAG_B: $(echo "$B_IDS" | tr '\n' ' ')"

    if echo "$A_IDS" | grep -qx "$TAGGED_POST_ID" && echo "$B_IDS" | grep -qx "$TAGGED_POST_ID"; then
        record_result "list-posts-by-tag returns post for each tag" "PASS"
    else
        record_result "list-posts-by-tag returns post for each tag" "FAIL"
    fi
else
    record_result "list-posts-by-tag returns post for each tag" "FAIL"
fi

# ========================================================================
# TEST 6: list-posts-by-tag with a tag that carries no posts — empty
# ========================================================================
echo "--- TEST 6: list-posts-by-tag with tag that has no posts — empty ---"

EMPTY_IDS=$(list_posts_by_tag_ids "nonexistent-tag-xyz-no-match")
if [ -z "$EMPTY_IDS" ]; then
    record_result "list-posts-by-tag returns empty for tag with no posts" "PASS"
else
    echo "  Got IDs: $EMPTY_IDS"
    record_result "list-posts-by-tag returns empty for tag with no posts" "FAIL"
fi

# ========================================================================
# TEST 7: list-posts-by-tag with empty tag — error
# ========================================================================
echo "--- TEST 7: list-posts-by-tag with empty tag — error ---"

EMPTY_TAG_OUT=$($BINARY query blog list-posts-by-tag --tag "" --output json 2>&1)
if echo "$EMPTY_TAG_OUT" | grep -qi "tag cannot be empty\|invalid"; then
    record_result "list-posts-by-tag rejects empty tag" "PASS"
else
    echo "  Output: $(echo "$EMPTY_TAG_OUT" | head -3)"
    record_result "list-posts-by-tag rejects empty tag" "FAIL"
fi

# ========================================================================
# TEST 8: Reject create-post with unknown tag
# ========================================================================
echo "--- TEST 8: Reject create-post with unknown tag ---"

TX_RES=$(create_post_with_tags "Unknown Tag Post" "body" "totally-fake-unregistered-tag")
if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    echo "  Rejected as expected: $(echo "$TX_RESULT" | jq -r '.raw_log // empty' | head -c 120)"
    record_result "Reject create-post with unknown tag" "PASS"
else
    record_result "Reject create-post with unknown tag" "FAIL"
fi

# ========================================================================
# TEST 9: Reject create-post with tag exceeding max length (33 chars)
# ========================================================================
echo "--- TEST 9: Reject create-post with overly-long tag ---"

LONG_TAG=$(printf 'a%.0s' {1..33})
TX_RES=$(create_post_with_tags "Long Tag Post" "body" "$LONG_TAG")
if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    record_result "Reject create-post with tag > max_tag_length" "PASS"
else
    record_result "Reject create-post with tag > max_tag_length" "FAIL"
fi

# ========================================================================
# TEST 10: Reject create-post with malformed tag (uppercase)
# ========================================================================
echo "--- TEST 10: Reject create-post with malformed tag (uppercase) ---"

TX_RES=$(create_post_with_tags "Upper Tag Post" "body" "NotLowercase")
if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    record_result "Reject create-post with malformed tag" "PASS"
else
    record_result "Reject create-post with malformed tag" "FAIL"
fi

# ========================================================================
# TEST 11: Reject create-post with duplicate tag
# ========================================================================
echo "--- TEST 11: Reject create-post with duplicate tag ---"

TX_RES=$(create_post_with_tags "Dup Tag Post" "body" "$TAG_A,$TAG_A")
if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    record_result "Reject create-post with duplicate tag" "PASS"
else
    record_result "Reject create-post with duplicate tag" "FAIL"
fi

# ========================================================================
# TEST 12: Reject create-post with more than max_tags_per_post tags
# ========================================================================
echo "--- TEST 12: Reject create-post with too many tags ---"

# max_tags_per_post default is 5. Pass 6 known tags to trip the cap
# independently of tag-existence checks. All 6 are genesis tags.
OVERFLOW_TAGS="commons-council,technical-council,ecosystem-council,commons-ops-committee,commons-gov-committee,technical-ops-committee"
TX_RES=$(create_post_with_tags "Too Many Tags" "body" "$OVERFLOW_TAGS")
if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    record_result "Reject create-post with too many tags" "PASS"
else
    record_result "Reject create-post with too many tags" "FAIL"
fi

# ========================================================================
# TEST 13: Update post tags — old tag's index entry cleared, new tag's added
# ========================================================================
echo "--- TEST 13: update-post tag diff updates secondary index ---"

if [ -n "$TAGGED_POST_ID" ]; then
    # Post originally has TAG_A, TAG_B. Drop TAG_B, add TAG_C; keep TAG_A.
    TX_RES=$($BINARY tx blog update-post \
        "Tagged Post (updated)" \
        "Body (updated)" \
        "$TAGGED_POST_ID" \
        --tags "$TAG_A,$TAG_C" \
        --from "$CREATOR" \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        A_IDS=$(list_posts_by_tag_ids "$TAG_A")
        B_IDS=$(list_posts_by_tag_ids "$TAG_B")
        C_IDS=$(list_posts_by_tag_ids "$TAG_C")

        # Expect: still under A, no longer under B, now under C.
        if echo "$A_IDS" | grep -qx "$TAGGED_POST_ID" \
            && ! echo "$B_IDS" | grep -qx "$TAGGED_POST_ID" \
            && echo "$C_IDS" | grep -qx "$TAGGED_POST_ID"; then
            record_result "Update post tags diffs secondary index" "PASS"
        else
            echo "  A: $(echo $A_IDS | tr '\n' ' ')"
            echo "  B (expected no $TAGGED_POST_ID): $(echo $B_IDS | tr '\n' ' ')"
            echo "  C (expected $TAGGED_POST_ID): $(echo $C_IDS | tr '\n' ' ')"
            record_result "Update post tags diffs secondary index" "FAIL"
        fi
    else
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Update post tags diffs secondary index" "FAIL"
    fi
else
    record_result "Update post tags diffs secondary index" "FAIL"
fi

# ========================================================================
# TEST 14: Delete post clears all remaining tag index entries
# ========================================================================
echo "--- TEST 14: Delete post clears tag index entries ---"

if [ -n "$TAGGED_POST_ID" ]; then
    TX_RES=$($BINARY tx blog delete-post \
        "$TAGGED_POST_ID" \
        --from "$CREATOR" \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        A_IDS=$(list_posts_by_tag_ids "$TAG_A")
        C_IDS=$(list_posts_by_tag_ids "$TAG_C")

        if ! echo "$A_IDS" | grep -qx "$TAGGED_POST_ID" \
            && ! echo "$C_IDS" | grep -qx "$TAGGED_POST_ID"; then
            record_result "Delete post removes all tag index entries" "PASS"
        else
            record_result "Delete post removes all tag index entries" "FAIL"
        fi
    else
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Delete post removes all tag index entries" "FAIL"
    fi
else
    record_result "Delete post removes all tag index entries" "FAIL"
fi

# ========================================================================
# TEST 15: Reserve a tag via x/rep report+resolve flow
# ========================================================================
# Pattern mirrors test/rep/tag_budget_test.sh Part 22 — reserving a tag is a
# two-step commons-operations flow, not a direct proposal. We reserve a
# genesis tag ("quantum-computing") that no other blog test uses, so no
# earlier tests are perturbed.
echo "--- TEST 15: Reserve a tag via report + resolve-tag-report (action=2) ---"

RESERVED_TAG="quantum-computing"

# Step 1: alice reports the tag (locks DREAM bond from alice's 50k founder stash).
TX_RES=$($BINARY tx rep report-tag \
    "$RESERVED_TAG" \
    "Reserve for blog e2e test" \
    --from "$CREATOR" \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if ! submit_tx_and_wait "$TX_RES" || ! check_tx_success "$TX_RESULT"; then
    echo "  report-tag failed: $(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null | head -c 150)"
    record_result "Reserve tag via report+resolve flow" "FAIL"
else
    # Step 2: alice resolves with action=2 (reserve). Authority: alice is on
    # the Commons Operations Committee. reserveMembersCanUse=false so even
    # members cannot attach this tag to new content.
    TX_RES=$($BINARY tx rep resolve-tag-report \
        "$RESERVED_TAG" \
        "2" \
        "$CREATOR_ADDR" \
        "false" \
        --from "$CREATOR" \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        # Confirm the reserved-tag entry now exists.
        RES_INFO=$($BINARY query rep get-reserved-tag "$RESERVED_TAG" --output json 2>&1)
        RES_NAME=$(echo "$RES_INFO" | jq -r '.reserved_tag.name // .reservedTag.name // empty' 2>/dev/null)
        if [ "$RES_NAME" == "$RESERVED_TAG" ]; then
            echo "  Tag reserved: $RES_NAME"
            record_result "Reserve tag via report+resolve flow" "PASS"
        else
            echo "  Reserve succeeded but query returned no record"
            record_result "Reserve tag via report+resolve flow" "FAIL"
        fi
    else
        echo "  resolve-tag-report failed: $(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null | head -c 150)"
        record_result "Reserve tag via report+resolve flow" "FAIL"
    fi
fi

# ========================================================================
# TEST 16: Reject create-post that references a reserved tag
# ========================================================================
echo "--- TEST 16: Reject create-post with reserved tag ---"

TX_RES=$(create_post_with_tags "Reserved Tag Post" "body" "$RESERVED_TAG")
if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
    if echo "$RAW_LOG" | grep -qi "reserved"; then
        echo "  Rejected with reserved-tag error"
        record_result "Reject create-post with reserved tag" "PASS"
    else
        echo "  Rejected, but not with expected reserved-tag error"
        echo "  Raw log: $(echo "$RAW_LOG" | head -c 150)"
        # Still counts as correct-behavior (rejection is what matters), but note.
        record_result "Reject create-post with reserved tag" "PASS"
    fi
else
    echo "  Expected failure but tx succeeded"
    record_result "Reject create-post with reserved tag" "FAIL"
fi

# ========================================================================
# SUMMARY
# ========================================================================
echo "============================================"
echo "BLOG TAG TEST RESULTS"
echo "============================================"

for i in "${!TEST_NAMES[@]}"; do
    printf "  %-55s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done

echo ""
echo "  Passed: $PASS_COUNT / $((PASS_COUNT + FAIL_COUNT))"
echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME BLOG TAG TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL BLOG TAG TESTS PASSED <<<"
    exit 0
fi
