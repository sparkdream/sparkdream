#!/bin/bash

echo "--- TESTING: FORUM POST TAGS (CREATE+EDIT USAGE DIFF) ---"

# This suite pins the create+edit tag bookkeeping contract for x/forum:
# (1) IncrementTagUsage fires for each tag on create;
# (2) on EditPost the diff between old and new tag sets drives both
#     IncrementTagUsage (added) and DecrementTagUsage (removed), without
#     touching the kept side. Pre-fix, the diff was gated on
#     `len(msg.Tags) > 0` and only handled the add direction — repeated
#     edits or partial drops silently leaked UsageCount in the rep registry
#     and steered ExpireTags wrong (audit BLOG-S2-4 / FORUM-S2-3 cousin).

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    echo "   Run: bash setup_test_accounts.sh"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

# Genesis-registered tags. quantum-computing/cryptography/advanced-physics are
# all seeded in config.yml and not consumed by earlier forum suites — picking
# them here avoids cross-suite usage_count contention. (commons-council family
# is used heavily by blog/collect tag tests, so we steer clear of those.)
TAG_A="quantum-computing"
TAG_B="advanced-physics"
TAG_C="budget"
TAG_D="cryptography"

CREATOR="poster1"
CREATOR_ADDR="$POSTER1_ADDR"

echo "Creator:      $CREATOR ($CREATOR_ADDR)"
echo "Tags pool:    $TAG_A $TAG_B $TAG_C $TAG_D"
echo ""

# ========================================================================
# Helpers
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

check_tx_success() {
    [ "$(echo "$1" | jq -r '.code')" == "0" ]
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

post_tags_sorted() {
    local POST_ID=$1
    local DATA=$($BINARY query forum get-post "$POST_ID" --output json 2>&1)
    echo "$DATA" | jq -r '(.post.tags // []) | sort | join(",")'
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

# ========================================================================
# PART 0: Resolve category (same lazy-create idiom as post_test.sh)
# ========================================================================
echo "--- PART 0: ENSURE CATEGORY EXISTS ---"
CATEGORIES=$($BINARY query commons list-category --output json 2>&1)
CATEGORY_COUNT=$(echo "$CATEGORIES" | jq -r '.category | length' 2>/dev/null || echo "0")

if [ "$CATEGORY_COUNT" -gt 0 ]; then
    TEST_CATEGORY_ID=$(echo "$CATEGORIES" | jq -r '.category[0].category_id // "0"')
else
    echo "  No categories found; aborting (post_test.sh creates one — run it first)"
    exit 1
fi
echo "  Using category ID: $TEST_CATEGORY_ID"
echo ""

# ========================================================================
# TEST 1: Create tagged post — IncrementTagUsage fires for each tag
# ========================================================================
echo "--- TEST 1: Create tagged post bumps usage_count for each tag ---"

U_A_PRE=$(tag_usage_count "$TAG_A")
U_B_PRE=$(tag_usage_count "$TAG_B")
echo "  Pre-create: $TAG_A=$U_A_PRE $TAG_B=$U_B_PRE"

TX_RES=$($BINARY tx forum create-post \
    "$TEST_CATEGORY_ID" \
    "0" \
    "Forum tag test post body" \
    --tags "$TAG_A,$TAG_B" \
    --from "$CREATOR" \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    TAGGED_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
    [ -z "$TAGGED_POST_ID" ] && TAGGED_POST_ID=$(extract_event_value "$TX_RESULT" "forum.post.created" "post_id")
    echo "  Post created with ID: $TAGGED_POST_ID"

    U_A_POST=$(tag_usage_count "$TAG_A")
    U_B_POST=$(tag_usage_count "$TAG_B")
    DIFF_A=$((U_A_POST - U_A_PRE))
    DIFF_B=$((U_B_POST - U_B_PRE))
    echo "  Post-create: $TAG_A=$U_A_POST (+$DIFF_A) $TAG_B=$U_B_POST (+$DIFF_B)"

    POST_TAGS=$(post_tags_sorted "$TAGGED_POST_ID")
    EXPECTED_TAGS=$(echo -e "$TAG_A\n$TAG_B" | sort | paste -sd, -)

    if [ "$DIFF_A" -ge 1 ] && [ "$DIFF_B" -ge 1 ] && [ "$POST_TAGS" == "$EXPECTED_TAGS" ]; then
        record_result "Create tagged post — usage and post.tags both set" "PASS"
    else
        echo "  post.tags expected=$EXPECTED_TAGS got=$POST_TAGS"
        record_result "Create tagged post — usage and post.tags both set" "FAIL"
    fi
else
    echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
    TAGGED_POST_ID=""
    record_result "Create tagged post — usage and post.tags both set" "FAIL"
fi

# ========================================================================
# TEST 2: Edit post tags — partial swap (keep A, drop B, add C)
#   Asserts: A unchanged, B -1, C +1; post.tags reflects new set.
# ========================================================================
echo "--- TEST 2: edit-post partial swap diffs usage_count correctly ---"

if [ -n "$TAGGED_POST_ID" ]; then
    U_A_PRE=$(tag_usage_count "$TAG_A")
    U_B_PRE=$(tag_usage_count "$TAG_B")
    U_C_PRE=$(tag_usage_count "$TAG_C")
    echo "  Pre-edit: $TAG_A=$U_A_PRE $TAG_B=$U_B_PRE $TAG_C=$U_C_PRE"

    TX_RES=$($BINARY tx forum edit-post \
        "$TAGGED_POST_ID" \
        "edited body v1" \
        --tags "$TAG_A,$TAG_C" \
        --from "$CREATOR" \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        U_A_POST=$(tag_usage_count "$TAG_A")
        U_B_POST=$(tag_usage_count "$TAG_B")
        U_C_POST=$(tag_usage_count "$TAG_C")
        echo "  Post-edit: $TAG_A=$U_A_POST $TAG_B=$U_B_POST $TAG_C=$U_C_POST"

        EXPECTED_B=$((U_B_PRE - 1)); [ "$EXPECTED_B" -lt 0 ] && EXPECTED_B=0
        EXPECTED_C=$((U_C_PRE + 1))

        OK=true
        if [ "$U_A_POST" != "$U_A_PRE" ]; then
            echo "  $TAG_A (kept) should be unchanged: was $U_A_PRE, now $U_A_POST"
            OK=false
        fi
        if [ "$U_B_POST" != "$EXPECTED_B" ]; then
            echo "  $TAG_B (dropped) should be $EXPECTED_B, got $U_B_POST"
            OK=false
        fi
        if [ "$U_C_POST" != "$EXPECTED_C" ]; then
            echo "  $TAG_C (added) should be $EXPECTED_C, got $U_C_POST"
            OK=false
        fi

        POST_TAGS=$(post_tags_sorted "$TAGGED_POST_ID")
        EXPECTED_TAGS=$(echo -e "$TAG_A\n$TAG_C" | sort | paste -sd, -)
        if [ "$POST_TAGS" != "$EXPECTED_TAGS" ]; then
            echo "  post.tags expected=$EXPECTED_TAGS got=$POST_TAGS"
            OK=false
        fi

        if $OK; then
            record_result "edit-post partial swap diffs usage_count" "PASS"
        else
            record_result "edit-post partial swap diffs usage_count" "FAIL"
        fi
    else
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "edit-post partial swap diffs usage_count" "FAIL"
    fi
else
    record_result "edit-post partial swap diffs usage_count" "FAIL"
fi

# ========================================================================
# TEST 3: Edit post tags — re-submit the same set is a no-op for usage
#   Every tag is "kept" — neither increment nor decrement should fire.
#   Regression hook: repeated edits within the edit window must not
#   double-count UsageCount for already-attached tags.
# ========================================================================
echo "--- TEST 3: edit-post with identical tag set is a usage no-op ---"

if [ -n "$TAGGED_POST_ID" ]; then
    U_A_PRE=$(tag_usage_count "$TAG_A")
    U_C_PRE=$(tag_usage_count "$TAG_C")

    TX_RES=$($BINARY tx forum edit-post \
        "$TAGGED_POST_ID" \
        "edited body v2" \
        --tags "$TAG_A,$TAG_C" \
        --from "$CREATOR" \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        U_A_POST=$(tag_usage_count "$TAG_A")
        U_C_POST=$(tag_usage_count "$TAG_C")
        if [ "$U_A_POST" == "$U_A_PRE" ] && [ "$U_C_POST" == "$U_C_PRE" ]; then
            record_result "edit-post identical tag set is a usage no-op" "PASS"
        else
            echo "  $TAG_A: was $U_A_PRE, now $U_A_POST (expected unchanged)"
            echo "  $TAG_C: was $U_C_PRE, now $U_C_POST (expected unchanged)"
            record_result "edit-post identical tag set is a usage no-op" "FAIL"
        fi
    else
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "edit-post identical tag set is a usage no-op" "FAIL"
    fi
else
    record_result "edit-post identical tag set is a usage no-op" "FAIL"
fi

# ========================================================================
# TEST 4: Edit post tags — full multi-drop swap (drop A+C, add D)
#   Covers the "multiple dropped tags in one edit" path. Pre-fix this case
#   leaked usage on every dropped tag.
# ========================================================================
echo "--- TEST 4: edit-post full multi-drop swap decrements every removed tag ---"

if [ -n "$TAGGED_POST_ID" ]; then
    U_A_PRE=$(tag_usage_count "$TAG_A")
    U_C_PRE=$(tag_usage_count "$TAG_C")
    U_D_PRE=$(tag_usage_count "$TAG_D")
    echo "  Pre-edit: $TAG_A=$U_A_PRE $TAG_C=$U_C_PRE $TAG_D=$U_D_PRE"

    TX_RES=$($BINARY tx forum edit-post \
        "$TAGGED_POST_ID" \
        "edited body v3" \
        --tags "$TAG_D" \
        --from "$CREATOR" \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        U_A_POST=$(tag_usage_count "$TAG_A")
        U_C_POST=$(tag_usage_count "$TAG_C")
        U_D_POST=$(tag_usage_count "$TAG_D")
        echo "  Post-edit: $TAG_A=$U_A_POST $TAG_C=$U_C_POST $TAG_D=$U_D_POST"

        EXPECTED_A=$((U_A_PRE - 1)); [ "$EXPECTED_A" -lt 0 ] && EXPECTED_A=0
        EXPECTED_C=$((U_C_PRE - 1)); [ "$EXPECTED_C" -lt 0 ] && EXPECTED_C=0
        EXPECTED_D=$((U_D_PRE + 1))

        if [ "$U_A_POST" == "$EXPECTED_A" ] \
            && [ "$U_C_POST" == "$EXPECTED_C" ] \
            && [ "$U_D_POST" == "$EXPECTED_D" ]; then
            record_result "edit-post multi-drop swap diffs usage_count" "PASS"
        else
            echo "  A expected=$EXPECTED_A got=$U_A_POST"
            echo "  C expected=$EXPECTED_C got=$U_C_POST"
            echo "  D expected=$EXPECTED_D got=$U_D_POST"
            record_result "edit-post multi-drop swap diffs usage_count" "FAIL"
        fi
    else
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "edit-post multi-drop swap diffs usage_count" "FAIL"
    fi
else
    record_result "edit-post multi-drop swap diffs usage_count" "FAIL"
fi

# ========================================================================
# TEST 5: delete-post decrements usage_count for every tag the post carried
#   AND clears post.Tags. Cousin of edit-side decrement coverage — pre-fix,
#   delete-post left UsageCount inflated so create/delete churn drove it up
#   monotonically and ExpireTags lost its grip on idle tags.
# ========================================================================
echo "--- TEST 5: delete-post decrements usage_count for every tag ---"

if [ -n "$TAGGED_POST_ID" ]; then
    # Post currently carries [TAG_D] after TEST 4.
    U_D_PRE=$(tag_usage_count "$TAG_D")
    echo "  Pre-delete: $TAG_D=$U_D_PRE"

    TX_RES=$($BINARY tx forum delete-post \
        "$TAGGED_POST_ID" \
        --from "$CREATOR" \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        U_D_POST=$(tag_usage_count "$TAG_D")
        echo "  Post-delete: $TAG_D=$U_D_POST"

        EXPECTED_D=$((U_D_PRE - 1)); [ "$EXPECTED_D" -lt 0 ] && EXPECTED_D=0

        # post.Tags is severed by the soft-delete handler so a future
        # gov-reverse / appeal-restore path can't re-diff against stale tags.
        TAGS_AFTER=$(post_tags_sorted "$TAGGED_POST_ID")

        if [ "$U_D_POST" == "$EXPECTED_D" ] && [ -z "$TAGS_AFTER" ]; then
            record_result "delete-post decrements usage_count and clears post.tags" "PASS"
        else
            echo "  $TAG_D: expected $EXPECTED_D, got $U_D_POST"
            echo "  post.tags after delete (expected empty): $TAGS_AFTER"
            record_result "delete-post decrements usage_count and clears post.tags" "FAIL"
        fi
    else
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "delete-post decrements usage_count and clears post.tags" "FAIL"
    fi
else
    record_result "delete-post decrements usage_count and clears post.tags" "FAIL"
fi

# ========================================================================
# SUMMARY
# ========================================================================
echo "============================================"
echo "FORUM TAG TEST RESULTS"
echo "============================================"
for i in "${!TEST_NAMES[@]}"; do
    printf "  %-65s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done
echo ""
echo "  Passed: $PASS_COUNT / $((PASS_COUNT + FAIL_COUNT))"
echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME FORUM TAG TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL FORUM TAG TESTS PASSED <<<"
    exit 0
fi
