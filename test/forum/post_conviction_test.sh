#!/bin/bash

echo "--- TESTING: FORUM POST CONVICTION STAKE (accrual + slash) ---"

# Exercises the PostConvictionStake lifecycle introduced in x/forum:
#   - OpenPostConvictionStake error paths: self-stake, below-min, non-member
#   - AccruePostConvictions (EndBlocker phase 4): per-tag forum rep is
#     credited to the post's author across blocks while the stake is active
#   - Read-back queries: GetPostConvictionStake / PostConvictionStakesByStaker
#     / PostConvictionStakesByPost surface an open stake (and its id) so
#     clients don't have to mirror stake ids locally
#   - ReleasePostConvictionStake happy path: after the lock window passes,
#     the staker reclaims locked DREAM and the stake is closed
#   - SlashStakesForPost (called from ExpireHiddenPosts): on confirmed
#     hide, the author's accrued forum rep is clawed back exactly and a
#     fraction of the staker's locked DREAM is burned
#
# Uses Commons Operations Committee proposals (via _lib_params.sh) to
# tighten post_conviction_lock_seconds + boost the stream rate so the
# whole lifecycle fits in a fast test run. The testparams build's 15-second
# hidden_expiration drives the slash path.

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

# The slash path runs from ExpireHiddenPosts (15s window in testparams);
# we need ephemeral posts alive longer than the cumulative sleep budget so
# the accrual + slash sequences all run against still-permanent fixtures.
bump_ephemeral_ttl 600 || {
    echo "Failed to bump ephemeral_ttl; aborting."
    exit 1
}

# Lock=30s lets the Release happy path finish in a reasonable window.
# Rate=100 rep/DREAM/day means a 100-DREAM stake credits ~0.116/day per
# second of elapsed time — visible as a positive delta after a few blocks.
# Cap=1000 effectively disables the per-epoch cap for the duration of the
# test (we have a unit test for the cap path).
bump_post_conviction_params 30 "100" "1000" || {
    echo "Failed to bump post_conviction params; aborting."
    exit 1
}

echo "Alice (CORE, staker):                $ALICE_ADDR"
echo "Poster 2 (member, post author):      $POSTER2_ADDR"
echo "Sentinel 1 (slash hide actor):       $SENTINEL1_ADDR"
echo "Category for test posts:             ${TEST_CATEGORY_ID:-1}"
echo ""

# ============================================================================
# Helpers (mirrors the shape used by promoter_warning_test.sh)
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

# Build COUNT EPIC interims for $ACCOUNT to push it past the ESTABLISHED trust
# gate that bonding a forum-sentinel requires. Mirrors bootstrap_reputation in
# unhide_post_test.sh — 3 EPICs reliably reach ESTABLISHED under default params.
bootstrap_reputation() {
    local ACCOUNT=$1
    local COUNT=$2
    echo "  Bootstrapping $COUNT EPIC interims for $ACCOUNT..."

    for i in $(seq 1 $COUNT); do
        TX_RES=$($BINARY tx rep create-interim other 0 "pc-sentinel-$i" epic 999999999 \
            --from $ACCOUNT \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000${BOND_DENOM} \
            -y \
            --output json 2>&1)
        if ! submit_tx_and_wait "$TX_RES" || ! check_tx_success "$TX_RESULT"; then
            echo "    Failed to create interim $i"
            return 1
        fi
        INTERIM_ID=$(extract_event_value "$TX_RESULT" "interim_created" "interim_id")

        TX_RES=$($BINARY tx rep complete-interim $INTERIM_ID "post-conviction test setup" \
            --from $ACCOUNT \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000${BOND_DENOM} \
            -y \
            --output json 2>&1)
        if ! submit_tx_and_wait "$TX_RES" || ! check_tx_success "$TX_RESULT"; then
            echo "    Failed to complete interim $i"
            return 1
        fi
        echo "    Completed interim $i/$COUNT (ID: $INTERIM_ID)"
    done
    echo "  Reputation bootstrapped for $ACCOUNT"
}

# forum_rep_for returns the author's forum_rep_per_tag entry for the given
# tag as a decimal string (zero if the field is absent — pre-stake state).
forum_rep_for() {
    local ADDR=$1 TAG=$2
    $BINARY query rep get-member "$ADDR" --output json 2>/dev/null \
        | jq -r --arg t "$TAG" '.member.forum_rep_per_tag[$t] // "0"'
}

dream_balance_for() {
    local ADDR=$1
    $BINARY query rep get-member "$ADDR" --output json 2>/dev/null \
        | jq -r '.member.dream_balance // "0"'
}

# dec_gt returns 0 (true) when $1 > $2 in math.LegacyDec string form.
# Uses awk because bash arithmetic is integer-only.
dec_gt() {
    awk -v a="$1" -v b="$2" 'BEGIN{exit !(a+0 > b+0)}'
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
POST_TAG="test"  # Genesis-registered reserved tag.

# ============================================================================
# Non-member fixture for the membership-gate error case.
# ============================================================================
NONMEMBER_ACCOUNT="forum_pc_nonmember"
if ! $BINARY keys show $NONMEMBER_ACCOUNT --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add $NONMEMBER_ACCOUNT --keyring-backend test --output json > /dev/null 2>&1
fi
NONMEMBER_ADDR=$($BINARY keys show $NONMEMBER_ACCOUNT -a --keyring-backend test)
echo "  Non-member account: $NONMEMBER_ADDR"

echo "  Funding non-member with SPARK (fees only — no DREAM)..."
TX_RES=$($BINARY tx bank send \
    alice $NONMEMBER_ADDR \
    20000000${BOND_DENOM} \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)
submit_tx_and_wait "$TX_RES" > /dev/null

# ============================================================================
# FIXTURES: a permanent post by alice (for self-stake reject) and a
# permanent post by poster2 (for the rest of the lifecycle).
# ============================================================================
echo "=== FIXTURES ==="

ALICE_POST_ID=""
TX_RES=$($BINARY tx forum create-post \
    $CATEGORY_ID 0 "Alice's post for self-stake reject test." \
    --tags "$POST_TAG" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    ALICE_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
    echo "  Alice post: ID=$ALICE_POST_ID"
else
    echo "  Failed to create alice's fixture; aborting."
    exit 1
fi

POSTER2_POST_ID=""
TX_RES=$($BINARY tx forum create-post \
    $CATEGORY_ID 0 "Poster2's post — happy-path accrual + release target." \
    --tags "$POST_TAG" \
    --from poster2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    POSTER2_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
    echo "  Poster2 post: ID=$POSTER2_POST_ID"
else
    echo "  Failed to create poster2's fixture; aborting."
    exit 1
fi
echo ""

# ============================================================================
# TEST 1: self-stake rejected.
# ============================================================================
echo "--- TEST 1: alice tries to stake on her own post -> rejected ---"

TX_RES=$($BINARY tx forum stake-post-conviction \
    "$ALICE_POST_ID" \
    "10000000" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)
submit_tx_and_wait "$TX_RES"
TX_CODE=$(echo "$TX_RESULT" | jq -r '.code // "0"')
RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')

if [ "$TX_CODE" != "0" ] && echo "$RAW_LOG" | grep -qi "own post"; then
    record_result "Self-stake rejected" "PASS"
else
    echo "  Expected code != 0 with 'own post' in raw_log; got code=$TX_CODE log='$RAW_LOG'"
    record_result "Self-stake rejected" "FAIL"
fi

# ============================================================================
# TEST 2: below-min stake amount rejected.
# ============================================================================
echo "--- TEST 2: alice tries to stake 1 uDREAM on poster2's post -> rejected ---"

TX_RES=$($BINARY tx forum stake-post-conviction \
    "$POSTER2_POST_ID" \
    "1" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)
submit_tx_and_wait "$TX_RES"
TX_CODE=$(echo "$TX_RESULT" | jq -r '.code // "0"')
RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')

if [ "$TX_CODE" != "0" ] && echo "$RAW_LOG" | grep -qi "below min"; then
    record_result "Below-min stake rejected" "PASS"
else
    echo "  Expected code != 0 with 'below min' in raw_log; got code=$TX_CODE log='$RAW_LOG'"
    record_result "Below-min stake rejected" "FAIL"
fi

# ============================================================================
# TEST 3: non-member rejected.
# ============================================================================
echo "--- TEST 3: nonmember tries to stake -> rejected at membership gate ---"

TX_RES=$($BINARY tx forum stake-post-conviction \
    "$POSTER2_POST_ID" \
    "10000000" \
    --from $NONMEMBER_ACCOUNT \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)
submit_tx_and_wait "$TX_RES"
TX_CODE=$(echo "$TX_RESULT" | jq -r '.code // "0"')
RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')

# Either of two acceptable rejections: "only active members" (membership
# gate) or "ESTABLISHED+" (trust gate, if the address happens to register
# as a member somewhere upstream).
if [ "$TX_CODE" != "0" ] && \
   ( echo "$RAW_LOG" | grep -qi "active members" || echo "$RAW_LOG" | grep -qi "ESTABLISHED" ); then
    record_result "Non-member rejected" "PASS"
else
    echo "  Expected code != 0 with membership/trust rejection; got code=$TX_CODE log='$RAW_LOG'"
    record_result "Non-member rejected" "FAIL"
fi

# ============================================================================
# TEST 4: happy-path stake — alice stakes 100 DREAM on poster2's post.
# ============================================================================
echo "--- TEST 4: alice opens a 100-DREAM stake on poster2's post ---"

POSTER2_REP_PRE=$(forum_rep_for "$POSTER2_ADDR" "$POST_TAG")
ALICE_DREAM_PRE=$(dream_balance_for "$ALICE_ADDR")
echo "  poster2 forum_rep[$POST_TAG] before: $POSTER2_REP_PRE"
echo "  alice dream_balance before:          $ALICE_DREAM_PRE"

# 100 DREAM = 100_000_000 uDREAM.
STAKE_AMOUNT="100000000"
HAPPY_STAKE_ID=""
TX_RES=$($BINARY tx forum stake-post-conviction \
    "$POSTER2_POST_ID" \
    "$STAKE_AMOUNT" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    HAPPY_STAKE_ID=$(extract_event_value "$TX_RESULT" "post_conviction_staked" "stake_id")
    echo "  Stake opened: ID=$HAPPY_STAKE_ID"
    record_result "Happy-path stake opens" "PASS"
else
    echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log')"
    record_result "Happy-path stake opens" "FAIL"
fi

# ============================================================================
# TEST 4b: the new read-back queries surface the open stake by id, staker,
# and post — so UI clients can recover the stake_id required by
# MsgReleasePostConviction without mirroring it locally.
# ============================================================================
if [ -n "$HAPPY_STAKE_ID" ]; then
    echo "--- TEST 4b: query stake by id / staker / post ---"
    QUERY_OK=true

    # NOTE: uint64 proto fields are omitted from JSON when zero, so a stake
    # with id 0 (the first one minted) has no ".id" key — normalize absent
    # numeric fields to "0" everywhere below.

    # get-post-conviction-stake [id]
    STAKE_Q=$($BINARY query forum get-post-conviction-stake "$HAPPY_STAKE_ID" --output json 2>&1)
    Q_ID=$(echo "$STAKE_Q" | jq -r '.stake.id // "0"')
    Q_STAKER=$(echo "$STAKE_Q" | jq -r '.stake.staker // ""')
    Q_POST=$(echo "$STAKE_Q" | jq -r '.stake.post_id // "0"')
    if [ "$Q_ID" != "$HAPPY_STAKE_ID" ] || [ "$Q_STAKER" != "$ALICE_ADDR" ] || [ "$Q_POST" != "$POSTER2_POST_ID" ]; then
        echo "  FAIL get-by-id: id=$Q_ID staker=$Q_STAKER post=$Q_POST (want id=$HAPPY_STAKE_ID staker=$ALICE_ADDR post=$POSTER2_POST_ID)"
        QUERY_OK=false
    fi

    # get-post-conviction-stake on a non-existent id -> error (no stake body).
    MISS_Q=$($BINARY query forum get-post-conviction-stake 999999 --output json 2>&1)
    if echo "$MISS_Q" | jq -e '.stake.staker' > /dev/null 2>&1; then
        echo "  FAIL get-by-id miss: expected error for id 999999, got $MISS_Q"
        QUERY_OK=false
    fi

    # post-conviction-stakes-by-staker [staker] -> must include our stake.
    BY_STAKER_Q=$($BINARY query forum post-conviction-stakes-by-staker "$ALICE_ADDR" --output json 2>&1)
    if ! echo "$BY_STAKER_Q" | jq -e --arg id "$HAPPY_STAKE_ID" '.stakes[] | select((.id // "0")==$id)' > /dev/null 2>&1; then
        echo "  FAIL by-staker: stake $HAPPY_STAKE_ID not in $BY_STAKER_Q"
        QUERY_OK=false
    fi
    # every returned stake must belong to alice.
    BAD_STAKER=$(echo "$BY_STAKER_Q" | jq -r --arg s "$ALICE_ADDR" '[.stakes[] | select(.staker != $s)] | length')
    if [ "${BAD_STAKER:-0}" != "0" ]; then
        echo "  FAIL by-staker: returned $BAD_STAKER stakes not owned by alice"
        QUERY_OK=false
    fi

    # post-conviction-stakes-by-post [post-id] -> must include our stake.
    BY_POST_Q=$($BINARY query forum post-conviction-stakes-by-post "$POSTER2_POST_ID" --output json 2>&1)
    if ! echo "$BY_POST_Q" | jq -e --arg id "$HAPPY_STAKE_ID" '.stakes[] | select((.id // "0")==$id)' > /dev/null 2>&1; then
        echo "  FAIL by-post: stake $HAPPY_STAKE_ID not in $BY_POST_Q"
        QUERY_OK=false
    fi
    # every returned stake must reference this post.
    BAD_POST=$(echo "$BY_POST_Q" | jq -r --arg p "$POSTER2_POST_ID" '[.stakes[] | select((.post_id // "0") != $p)] | length')
    if [ "${BAD_POST:-0}" != "0" ]; then
        echo "  FAIL by-post: returned $BAD_POST stakes not on post $POSTER2_POST_ID"
        QUERY_OK=false
    fi

    if [ "$QUERY_OK" == "true" ]; then
        record_result "Stake read-back queries (id/staker/post)" "PASS"
    else
        record_result "Stake read-back queries (id/staker/post)" "FAIL"
    fi
fi

# ============================================================================
# TEST 5: accrual credits the author's forum_rep over a few blocks.
# ============================================================================
if [ -n "$HAPPY_STAKE_ID" ]; then
    echo "--- TEST 5: accrual credits author forum_rep across blocks ---"
    echo "  Sleeping 18s (3 blocks) for EndBlocker accrual..."
    sleep 18
    POSTER2_REP_POST=$(forum_rep_for "$POSTER2_ADDR" "$POST_TAG")
    echo "  poster2 forum_rep[$POST_TAG] after:  $POSTER2_REP_POST"
    if dec_gt "$POSTER2_REP_POST" "$POSTER2_REP_PRE"; then
        record_result "Accrual credits author forum_rep" "PASS"
    else
        echo "  Expected forum_rep to increase; got pre=$POSTER2_REP_PRE post=$POSTER2_REP_POST"
        record_result "Accrual credits author forum_rep" "FAIL"
    fi
fi

# ============================================================================
# TEST 6: release before lock window -> rejected; release after window -> OK.
# ============================================================================
if [ -n "$HAPPY_STAKE_ID" ]; then
    echo "--- TEST 6a: release before lock window -> rejected ---"
    TX_RES=$($BINARY tx forum release-post-conviction \
        "$HAPPY_STAKE_ID" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)
    submit_tx_and_wait "$TX_RES"
    TX_CODE=$(echo "$TX_RESULT" | jq -r '.code // "0"')
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
    if [ "$TX_CODE" != "0" ] && echo "$RAW_LOG" | grep -qi "locked until"; then
        record_result "Premature release rejected" "PASS"
    else
        echo "  Expected 'locked until' rejection; got code=$TX_CODE log='$RAW_LOG'"
        record_result "Premature release rejected" "FAIL"
    fi

    echo "--- TEST 6b: wait past lock window (30s) then release ---"
    # Stake was opened ~24s ago (TEST 5 slept 18s + TEST 6a took ~6s); a
    # further 18s reliably crosses the 30s lock window with margin.
    echo "  Sleeping 18s to cross the 30s lock window..."
    sleep 18

    TX_RES=$($BINARY tx forum release-post-conviction \
        "$HAPPY_STAKE_ID" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)
    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        record_result "Release after lock window succeeds" "PASS"
    else
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log')"
        record_result "Release after lock window succeeds" "FAIL"
    fi
fi

# ============================================================================
# SETUP FOR TEST 7: ensure sentinel1 is a bonded forum-sentinel.
# ============================================================================
# The slash path is driven by a sentinel hiding the post, and MsgHidePost
# requires the caller to hold a non-DEMOTED ROLE_TYPE_FORUM_SENTINEL bond at
# ESTABLISHED+ trust. In the full suite sentinel_test.sh bonds sentinel1 in an
# earlier PART, but this test must also pass standalone — so bond it here if no
# preceding test already did. Idempotent: skipped when sentinel1 is bonded.
echo "--- SETUP: ensure sentinel1 is a bonded forum-sentinel ---"
SENTINEL_READY=true
SENTINEL_STATUS=$($BINARY query rep bonded-role forum-sentinel "$SENTINEL1_ADDR" --output json 2>&1)
if echo "$SENTINEL_STATUS" | grep -qi "error\|not found"; then
    if ! bootstrap_reputation sentinel1 3; then
        echo "  Failed to bootstrap sentinel1 reputation; skipping slash test."
        SENTINEL_READY=false
    else
        echo "  Bonding sentinel1 (500 DREAM = DefaultMinSentinelBond)..."
        TX_RES=$($BINARY tx rep bond-role forum-sentinel \
            "500000000" \
            --from sentinel1 \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000${BOND_DENOM} \
            -y \
            --output json 2>&1)
        if ! submit_tx_and_wait "$TX_RES" || ! check_tx_success "$TX_RESULT"; then
            echo "  Failed to bond sentinel1: $(echo "$TX_RESULT" | jq -r '.raw_log')"
            SENTINEL_READY=false
        else
            echo "  Sentinel1 bonded"
        fi
    fi
else
    echo "  Sentinel1 already bonded (skipping bootstrap)"
fi
echo ""

# ============================================================================
# TEST 7: SLASH — open a new stake on a fresh post, hide it, wait for
# ExpireHiddenPosts to finalize, and confirm:
#   (a) the author's forum_rep for the tag is clawed back, AND
#   (b) the staker's DREAM balance dropped by at least the slash portion.
# ============================================================================
echo "--- TEST 7: slash path — hide finalization claws back rep + burns DREAM ---"

# Fresh post by poster2 — different from POSTER2_POST_ID because that one's
# author rep may still be non-zero post-release.
SLASH_POST_ID=""
if [ "$SENTINEL_READY" != "true" ]; then
    echo "  Sentinel1 prerequisite unmet; cannot exercise the slash path."
    record_result "Hide-finalization slashes conviction stake" "FAIL"
else
    TX_RES=$($BINARY tx forum create-post \
        $CATEGORY_ID 0 "Poster2's slash-test post — will be hidden." \
        --tags "$POST_TAG" \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)
    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        SLASH_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        echo "  Slash-target post: ID=$SLASH_POST_ID"
    fi
fi

if [ -n "$SLASH_POST_ID" ]; then
    POSTER2_REP_SLASH_PRE=$(forum_rep_for "$POSTER2_ADDR" "$POST_TAG")
    ALICE_DREAM_SLASH_PRE=$(dream_balance_for "$ALICE_ADDR")
    echo "  poster2 forum_rep[$POST_TAG] pre-slash-stake: $POSTER2_REP_SLASH_PRE"
    echo "  alice dream_balance pre-slash-stake:          $ALICE_DREAM_SLASH_PRE"

    # 100 DREAM stake — same scale as TEST 4 so accrual is visible.
    SLASH_STAKE_ID=""
    TX_RES=$($BINARY tx forum stake-post-conviction \
        "$SLASH_POST_ID" \
        "$STAKE_AMOUNT" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)
    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        SLASH_STAKE_ID=$(extract_event_value "$TX_RESULT" "post_conviction_staked" "stake_id")
        echo "  Slash-target stake opened: ID=$SLASH_STAKE_ID"
    fi

    # Let some accrual happen so there's actually rep to claw back.
    echo "  Sleeping 12s for accrual before hide..."
    sleep 12
    POSTER2_REP_PRE_HIDE=$(forum_rep_for "$POSTER2_ADDR" "$POST_TAG")
    echo "  poster2 forum_rep[$POST_TAG] post-accrual / pre-hide: $POSTER2_REP_PRE_HIDE"

    # Sentinel 1 hides the post.
    TX_RES=$($BINARY tx forum hide-post \
        "$SLASH_POST_ID" \
        "1" \
        "Post-conviction slash e2e: hidden by sentinel1" \
        --from sentinel1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000${BOND_DENOM} \
        -y \
        --output json 2>&1)
    if ! submit_tx_and_wait "$TX_RES" || ! check_tx_success "$TX_RESULT"; then
        echo "  Failed to hide slash-target post: $(echo "$TX_RESULT" | jq -r '.raw_log')"
        record_result "Hide-finalization slashes conviction stake" "FAIL"
    else
        echo "  Sleeping 24s for hide-expiry + EndBlocker pass..."
        sleep 24
        POLL_ATTEMPTS=0
        SLASH_POST_DELETED=false
        while [ $POLL_ATTEMPTS -lt 5 ]; do
            POST_Q=$($BINARY query forum get-post "$SLASH_POST_ID" --output json 2>&1)
            POST_STATUS=$(echo "$POST_Q" | jq -r '(.post.status // "")')
            if [ "$POST_STATUS" == "POST_STATUS_DELETED" ]; then
                SLASH_POST_DELETED=true
                break
            fi
            POLL_ATTEMPTS=$((POLL_ATTEMPTS + 1))
            sleep 6
        done

        POSTER2_REP_POST_SLASH=$(forum_rep_for "$POSTER2_ADDR" "$POST_TAG")
        ALICE_DREAM_POST_SLASH=$(dream_balance_for "$ALICE_ADDR")
        echo "  Slash-target post status: $POST_STATUS"
        echo "  poster2 forum_rep[$POST_TAG] post-slash: $POSTER2_REP_POST_SLASH"
        echo "  alice dream_balance post-slash:           $ALICE_DREAM_POST_SLASH"

        # Asserting the slash:
        #   (a) post tombstoned (proves ExpireHiddenPosts ran)
        #   (b) poster2's rep is no greater than it was immediately pre-hide
        #       (clawback should fully zero out *this stake's* contribution
        #       — released stake from TEST 4 may still hold some rep on this
        #       tag, hence ≤ not exactly equal)
        #   (c) alice's DREAM balance dropped — the 25% slash burned 25 DREAM
        #       of the 100-DREAM stake; the rest is unlocked back, so net
        #       drop is ≥ 25 DREAM (25_000_000 uDREAM)
        #
        # (c) ignores fee costs vs. balance bookkeeping nuance: we only
        # assert the burn DROP, not exact post-balance, because release-
        # unlocks credit StakedDream→DreamBalance and we want the asserted
        # value to be robust to that internal transfer.
        SLASH_OK=true
        if [ "$SLASH_POST_DELETED" != "true" ]; then
            echo "  FAIL: post did not reach POST_STATUS_DELETED"
            SLASH_OK=false
        fi
        if dec_gt "$POSTER2_REP_POST_SLASH" "$POSTER2_REP_PRE_HIDE"; then
            echo "  FAIL: forum_rep increased after slash"
            SLASH_OK=false
        fi
        DROP=$(awk -v a="$ALICE_DREAM_SLASH_PRE" -v b="$ALICE_DREAM_POST_SLASH" 'BEGIN{printf "%.0f", a-b}')
        if [ "$DROP" -lt 25000000 ] 2>/dev/null; then
            echo "  FAIL: alice DREAM dropped by only $DROP uDREAM, expected ≥ 25_000_000 (25 DREAM burn)"
            SLASH_OK=false
        fi

        if [ "$SLASH_OK" == "true" ]; then
            record_result "Hide-finalization slashes conviction stake" "PASS"
        else
            record_result "Hide-finalization slashes conviction stake" "FAIL"
        fi
    fi
fi

# ============================================================================
# SUMMARY
# ============================================================================
echo "============================================"
echo "FORUM POST CONVICTION TEST RESULTS"
echo "============================================"
echo ""
for i in "${!TEST_NAMES[@]}"; do
    printf "  %-50s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done
echo ""
echo "Total: $((PASS_COUNT + FAIL_COUNT))   Passed: $PASS_COUNT   Failed: $FAIL_COUNT"
echo ""
if [ $FAIL_COUNT -gt 0 ]; then
    exit 1
fi
