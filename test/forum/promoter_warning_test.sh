#!/bin/bash

echo "--- TESTING: FORUM PROMOTER WARNING + AUTHOR REP SLASH ON UNAPPEALED HIDE ---"

# Exercises the new accountability hooks added to ExpireHiddenPosts:
#   - Tier 0 (author per-tag rep slash): DeductReputation fires for each
#     tag on the post when an unappealed sentinel hide finalizes.
#   - Tier 1 (promoter MemberWarning): a MemberWarning is issued against
#     the member who called MsgMakePostPermanent if they are not the
#     post's author.
#
# Both fire from the EndBlocker pass in x/forum/keeper/abci.go's
# ExpireHiddenPosts, gated on HiddenAt + DefaultHiddenExpiration. The
# testparams build shortens that window to 15 seconds — see
# x/forum/types/params_vals_testparams.go.

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/../lib/denoms.sh"
source "$SCRIPT_DIR/_lib_params.sh"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    echo "   Run: bash setup_test_accounts.sh"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

# The new accountability hooks fire when an unappealed sentinel hide
# finalizes. The testparams build sets DefaultHiddenExpiration to 15s, so
# the hide window is short, but the ephemeral post itself must survive
# long enough to be promoted+hidden before its own TTL would expire.
# 600s matches make_permanent_test.sh's bump for the same reason.
bump_ephemeral_ttl 600 || {
    echo "Failed to bump ephemeral_ttl; aborting."
    exit 1
}

echo "Alice (gov authority, CORE trust):      $ALICE_ADDR"
echo "Sentinel 1 (already bonded by sentinel_test.sh): $SENTINEL1_ADDR"
echo "Poster 2 (member, ESTABLISHED+):        $POSTER2_ADDR"
echo "Category for test posts:                ${TEST_CATEGORY_ID:-1}"
echo ""

# ============================================================================
# Helpers
# ============================================================================

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
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    [ "$CODE" == "0" ]
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

# Per-member warning count via the MemberStanding query — much cheaper
# than walking the full MemberWarning list.
warning_count_for() {
    local ADDR=$1
    $BINARY query rep member-standing "$ADDR" --output json 2>/dev/null \
        | jq -r '.warning_count // "0"'
}

PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()
record_result() {
    local NAME=$1
    local RESULT=$2
    TEST_NAMES+=("$NAME")
    RESULTS+=("$RESULT")
    if [ "$RESULT" == "PASS" ]; then
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
    echo "  => $RESULT"
    echo ""
}

CATEGORY_ID="${TEST_CATEGORY_ID:-1}"

# ============================================================================
# PREREQUISITE: fresh non-member account that produces ephemeral posts.
# ============================================================================
echo "=== PREREQUISITE: ephemeral fixture from a non-member ==="
echo ""

NONMEMBER_ACCOUNT="forum_promoter_warning_nonmember"
if ! $BINARY keys show $NONMEMBER_ACCOUNT --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add $NONMEMBER_ACCOUNT --keyring-backend test --output json > /dev/null 2>&1
fi
NONMEMBER_ADDR=$($BINARY keys show $NONMEMBER_ACCOUNT -a --keyring-backend test)
echo "  Non-member account: $NONMEMBER_ADDR"

echo "  Funding non-member..."
TX_RES=$($BINARY tx bank send \
    alice $NONMEMBER_ADDR \
    20000000${BOND_DENOM} \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)
submit_tx_and_wait "$TX_RES" > /dev/null

# Genesis-registered tag. "test" is seeded in config.yml's reserved tag_map
# and is rarely consumed by other suites — keeps cross-suite usage_count
# contention low.
POST_TAG="test"

EPHEMERAL_POST_ID=""
TX_RES=$($BINARY tx forum create-post \
    $CATEGORY_ID 0 "Ephemeral post by non-member; will be promoted then hidden." \
    --tags "$POST_TAG" \
    --from $NONMEMBER_ACCOUNT \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    EPHEMERAL_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
    POST_Q=$($BINARY query forum get-post $EPHEMERAL_POST_ID --output json 2>&1)
    EXPIRATION_TIME=$(echo "$POST_Q" | jq -r '(.post.expiration_time // 0)')
    POST_TAGS=$(echo "$POST_Q" | jq -r '(.post.tags // []) | join(",")')
    echo "  Ephemeral post: ID=$EPHEMERAL_POST_ID expiration_time=$EXPIRATION_TIME tags=[$POST_TAGS]"
else
    echo "  Failed to create ephemeral fixture; aborting."
    echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
    exit 1
fi
echo ""

# ============================================================================
# TEST 1: cross-member MakePostPermanent records PromotedBy on the post.
# ============================================================================
echo "--- TEST 1: MakePostPermanent records PromotedBy when promoter != author ---"

TX_RES=$($BINARY tx forum make-post-permanent \
    $EPHEMERAL_POST_ID \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    POST_Q=$($BINARY query forum get-post $EPHEMERAL_POST_ID --output json 2>&1)
    PROMOTED_BY=$(echo "$POST_Q" | jq -r '(.post.promoted_by // "")')
    EXPIRATION_TIME=$(echo "$POST_Q" | jq -r '(.post.expiration_time // 0)')

    if [ "$PROMOTED_BY" == "$ALICE_ADDR" ] && [ "$EXPIRATION_TIME" == "0" ]; then
        echo "  Promoted: expiration_time=0, promoted_by=alice"
        record_result "MakePostPermanent records PromotedBy" "PASS"
    else
        echo "  Expected promoted_by=$ALICE_ADDR expiration_time=0; got promoted_by=$PROMOTED_BY expiration_time=$EXPIRATION_TIME"
        record_result "MakePostPermanent records PromotedBy" "FAIL"
    fi
else
    echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
    record_result "MakePostPermanent records PromotedBy" "FAIL"
fi

# ============================================================================
# TEST 2: sentinel1 hides the now-permanent post.
# ============================================================================
echo "--- TEST 2: sentinel1 hides the promoted post ---"

# Snapshot alice's warning count before the hide expires so we can isolate
# the warning issued by THIS hide finalization (other suites may have
# triggered warnings for alice during the larger test run).
ALICE_WARNINGS_BEFORE=$(warning_count_for "$ALICE_ADDR")
echo "  alice warning_count before: $ALICE_WARNINGS_BEFORE"

TX_RES=$($BINARY tx forum hide-post \
    "$EPHEMERAL_POST_ID" \
    "1" \
    "Promoter-warning e2e: hidden by sentinel1" \
    --from sentinel1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    HIDE_RECORD=$($BINARY query forum get-hide-record $EPHEMERAL_POST_ID --output json 2>&1)
    if ! echo "$HIDE_RECORD" | grep -q "error\|not found"; then
        record_result "sentinel1 hides the promoted post" "PASS"
    else
        echo "  Hide record missing after MsgHidePost"
        record_result "sentinel1 hides the promoted post" "FAIL"
    fi
else
    echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
    record_result "sentinel1 hides the promoted post" "FAIL"
fi

# ============================================================================
# TEST 3: wait past DefaultHiddenExpiration (15s testparams) for the
# EndBlocker pass to run ExpireHiddenPosts and fire both slash hooks.
# ============================================================================
echo "--- TEST 3: post is tombstoned + promoter warning issued after hide-expiry ---"

# 15s hide window + a generous safety margin: a couple of additional
# blocks (6s each) for the EndBlocker to pick up the expired hide record
# and run the new finalization hooks. 24s = window + 1 spare block + slack.
echo "  Sleeping 24s for hide-expiry + EndBlocker pass..."
sleep 24

# Poll up to ~30s for the post status to flip to DELETED. The EndBlocker
# walks HideRecord once per block; a brief poll absorbs block-cadence
# jitter without inflating the worst-case wall-clock.
POLL_ATTEMPTS=0
POST_DELETED=false
while [ $POLL_ATTEMPTS -lt 5 ]; do
    POST_Q=$($BINARY query forum get-post $EPHEMERAL_POST_ID --output json 2>&1)
    POST_STATUS=$(echo "$POST_Q" | jq -r '(.post.status // "")')
    if [ "$POST_STATUS" == "POST_STATUS_DELETED" ]; then
        POST_DELETED=true
        break
    fi
    POLL_ATTEMPTS=$((POLL_ATTEMPTS + 1))
    sleep 6
done

POST_TAGS_AFTER=$(echo "$POST_Q" | jq -r '(.post.tags // []) | join(",")')
echo "  Post status: $POST_STATUS (tags severed? '$POST_TAGS_AFTER' should be empty)"

if [ "$POST_DELETED" != "true" ]; then
    echo "  Post did NOT reach POST_STATUS_DELETED within poll window — EndBlocker hide-expiry didn't fire"
    record_result "Hide-expiry tombstones the promoted post" "FAIL"
else
    record_result "Hide-expiry tombstones the promoted post" "PASS"
fi

ALICE_WARNINGS_AFTER=$(warning_count_for "$ALICE_ADDR")
WARNING_DELTA=$((ALICE_WARNINGS_AFTER - ALICE_WARNINGS_BEFORE))
echo "  alice warning_count after:  $ALICE_WARNINGS_AFTER (delta=$WARNING_DELTA)"

if [ "$WARNING_DELTA" -ge "1" ] 2>/dev/null; then
    # Verify at least one warning has the expected reason — guards against
    # an unrelated suite incrementing the count for a different reason.
    WARNINGS_LIST=$($BINARY query rep list-member-warning --output json 2>/dev/null)
    MATCHING=$(echo "$WARNINGS_LIST" \
        | jq -r --arg m "$ALICE_ADDR" \
            '.member_warning // [] | map(select(.member == $m and .reason == "promoted_hidden_content")) | length')
    if [ "$MATCHING" -ge "1" ] 2>/dev/null; then
        echo "  MemberWarning found: member=alice reason=promoted_hidden_content (matches=$MATCHING)"
        record_result "Promoter MemberWarning issued on unappealed hide" "PASS"
    else
        echo "  warning_count incremented but no MemberWarning with reason=promoted_hidden_content"
        record_result "Promoter MemberWarning issued on unappealed hide" "FAIL"
    fi
else
    echo "  Expected alice.warning_count to increase by >=1; got delta=$WARNING_DELTA"
    record_result "Promoter MemberWarning issued on unappealed hide" "FAIL"
fi

# ============================================================================
# TEST 4: member-authored permanent post hidden unappealed — should NOT
# issue a promoter warning (no PromotedBy was ever set because the author
# created the post directly as a permanent member).
# ============================================================================
echo "--- TEST 4: member-authored hidden post does NOT issue a promoter warning ---"

MEMBER_POST_ID=""
TX_RES=$($BINARY tx forum create-post \
    $CATEGORY_ID 0 "Member-authored permanent post; will be hidden." \
    --from poster2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    MEMBER_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
    POST_Q=$($BINARY query forum get-post $MEMBER_POST_ID --output json 2>&1)
    PROMOTED_BY=$(echo "$POST_Q" | jq -r '(.post.promoted_by // "")')
    EXPIRATION_TIME=$(echo "$POST_Q" | jq -r '(.post.expiration_time // 0)')
    echo "  Member-authored post: ID=$MEMBER_POST_ID expiration_time=$EXPIRATION_TIME promoted_by='$PROMOTED_BY'"
    if [ "$EXPIRATION_TIME" != "0" ] || [ -n "$PROMOTED_BY" ]; then
        echo "  Unexpected pre-state: members should produce permanent posts with empty promoted_by"
    fi
else
    echo "  Failed to create member-authored post"
    record_result "Member-authored hide does NOT issue promoter warning" "FAIL"
    MEMBER_POST_ID=""
fi

if [ -n "$MEMBER_POST_ID" ]; then
    # Snapshot warning counts for ALICE (previous promoter) and POSTER2
    # (the author of THIS post) so we can confirm:
    #   - poster2 gets no warning (author of own permanent post)
    #   - alice gets no NEW warning (she had nothing to do with this post)
    POSTER2_WARNINGS_BEFORE=$(warning_count_for "$POSTER2_ADDR")
    ALICE_WARNINGS_PREHIDE=$(warning_count_for "$ALICE_ADDR")

    TX_RES=$($BINARY tx forum hide-post \
        "$MEMBER_POST_ID" \
        "1" \
        "Promoter-warning e2e (negative case): hidden by sentinel1" \
        --from sentinel1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        echo "  Sleeping 24s for hide-expiry + EndBlocker pass..."
        sleep 24

        # Poll until DELETED (same shape as TEST 3).
        POLL_ATTEMPTS=0
        NEG_POST_DELETED=false
        while [ $POLL_ATTEMPTS -lt 5 ]; do
            POST_Q=$($BINARY query forum get-post $MEMBER_POST_ID --output json 2>&1)
            POST_STATUS=$(echo "$POST_Q" | jq -r '(.post.status // "")')
            if [ "$POST_STATUS" == "POST_STATUS_DELETED" ]; then
                NEG_POST_DELETED=true
                break
            fi
            POLL_ATTEMPTS=$((POLL_ATTEMPTS + 1))
            sleep 6
        done

        POSTER2_WARNINGS_AFTER=$(warning_count_for "$POSTER2_ADDR")
        ALICE_WARNINGS_POSTHIDE=$(warning_count_for "$ALICE_ADDR")
        POSTER2_DELTA=$((POSTER2_WARNINGS_AFTER - POSTER2_WARNINGS_BEFORE))
        ALICE_DELTA=$((ALICE_WARNINGS_POSTHIDE - ALICE_WARNINGS_PREHIDE))

        echo "  poster2 warning_count delta: $POSTER2_DELTA (must be 0 — author of own permanent post)"
        echo "  alice   warning_count delta: $ALICE_DELTA  (must be 0 — not involved in this post)"
        echo "  Post status: $POST_STATUS"

        if [ "$NEG_POST_DELETED" == "true" ] && [ "$POSTER2_DELTA" -eq 0 ] && [ "$ALICE_DELTA" -eq 0 ]; then
            record_result "Member-authored hide does NOT issue promoter warning" "PASS"
        else
            record_result "Member-authored hide does NOT issue promoter warning" "FAIL"
        fi
    else
        echo "  Failed to hide member-authored post"
        record_result "Member-authored hide does NOT issue promoter warning" "FAIL"
    fi
fi

# ============================================================================
# SUMMARY
# ============================================================================
echo "============================================"
echo "FORUM PROMOTER WARNING TEST RESULTS"
echo "============================================"

for i in "${!TEST_NAMES[@]}"; do
    printf "  %-60s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done

echo ""
echo "  Passed: $PASS_COUNT / $((PASS_COUNT + FAIL_COUNT))"
echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME PROMOTER WARNING TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL PROMOTER WARNING TESTS PASSED <<<"
    exit 0
fi
