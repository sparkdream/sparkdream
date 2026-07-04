#!/bin/bash

echo "--- TESTING: UNHIDE POST (SENTINEL SELF-CORRECT, COUNCIL OVERRIDE, NEGATIVE) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Load test environment (POSTER1/2_ADDR, SENTINEL1/2_ADDR, ALICE_ADDR, TEST_CATEGORY_ID).
if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    echo "   Run: bash setup_test_accounts.sh"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

# Sanity check: the CLI command must be registered by autocli — otherwise
# every tx call below returns YAML help text, jq fails to parse it,
# expect_tx_failure misreads it as "rejected at submission", and the
# negative cases all "pass" while happy paths fail confusingly. (Lesson
# learned from the category_delete_test.sh rollout.)
if ! $BINARY tx forum --help 2>&1 | grep -q '^[[:space:]]*unhide-post'; then
    echo "FATAL: 'sparkdreamd tx forum unhide-post' CLI command not found." >&2
    echo "       Rebuild the binary with: ignite chain build -y --build.tags testparams" >&2
    exit 1
fi

echo "Poster 1:   $POSTER1_ADDR"
echo "Poster 2:   $POSTER2_ADDR"
echo "Sentinel 1: $SENTINEL1_ADDR"
echo "Sentinel 2: $SENTINEL2_ADDR"
echo "Alice:      $ALICE_ADDR (council authority)"
echo "Category:   $TEST_CATEGORY_ID"
echo ""

# ========================================================================
# Helper Functions (mirror the pattern used by appeals_test.sh / category_delete_test.sh)
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
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        echo "Transaction failed with code: $CODE"
        echo "$TX_RESULT" | jq -r '.raw_log'
        return 1
    fi
    return 0
}

extract_event_value() {
    local TX_RESULT=$1
    local EVENT_TYPE=$2
    local ATTR_KEY=$3
    echo "$TX_RESULT" | jq -r ".events[] | select(.type==\"$EVENT_TYPE\") | .attributes[] | select(.key==\"$ATTR_KEY\") | .value" | tr -d '"'
}

submit_tx_and_wait() {
    local TX_RES="$1"
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        TX_RESULT=""
        return 1
    fi
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    return 0
}

expect_tx_failure() {
    local TX_RES="$1"
    local RESULT_VAR="$2"
    local DESC="$3"

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Transaction rejected at submission (expected)"
        eval "$RESULT_VAR=PASS"
        return 0
    fi
    echo "  Transaction submitted: $TXHASH"
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        echo "  Transaction failed as expected (code: $CODE)"
        echo "  Error: $RAW_LOG"
        eval "$RESULT_VAR=PASS"
        return 0
    fi
    echo "  ERROR: Transaction succeeded — $DESC"
    eval "$RESULT_VAR=FAIL"
    return 1
}

# Build COUNT EPIC interims for $ACCOUNT — each grants 100 reputation, so 3
# EPICs comfortably exceed the tier-3 (200+) floor required to bond as a
# sentinel under default params. Mirrors bootstrap_reputation in
# appeals_test.sh but uses a smaller COUNT (this test does not exercise
# thread-locking, which is the only reason appeals needs tier 4 / 500+).
bootstrap_reputation() {
    local ACCOUNT=$1
    local COUNT=$2
    echo "  Bootstrapping $COUNT EPIC interims for $ACCOUNT..."

    for i in $(seq 1 $COUNT); do
        TX_RES=$($BINARY tx rep create-interim other 0 "unhide-test-$i" epic 999999999 \
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

        TX_RES=$($BINARY tx rep complete-interim $INTERIM_ID "unhide-test setup" \
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

# ========================================================================
# PART 0: BOOTSTRAP SENTINEL1 (rep + bonded role)
# ========================================================================
# MsgHidePost requires the caller to hold a non-DEMOTED ROLE_TYPE_CONTENT_SENTINEL
# bond in x/rep. The setup script funds sentinel1 with enough DREAM to bond
# but does NOT register the role — that's a per-test concern because not
# every test needs a sentinel. Two-step setup: (1) bootstrap reputation past
# the MinRepTierSentinel gate, (2) bond. Both steps idempotent: if sentinel1
# is already bonded (e.g. appeals_test.sh ran first in the suite), skip them.
echo "--- PART 0: BOOTSTRAP SENTINEL1 ---"

SENTINEL_STATUS=$($BINARY query rep bonded-role content-sentinel "$SENTINEL1_ADDR" --output json 2>&1)
if echo "$SENTINEL_STATUS" | grep -q "error\|not found"; then
    # 3 EPIC interims → 300 rep gross, well above tier-3 (200+) floor.
    bootstrap_reputation sentinel1 3 || {
        echo "FATAL: failed to bootstrap sentinel1 reputation"
        exit 1
    }

    echo "Bonding sentinel1..."
    BOND_AMOUNT="500000000"  # 500 DREAM matches DefaultMinSentinelBond
    TX_RES=$($BINARY tx rep bond-role content-sentinel \
        "$BOND_AMOUNT" \
        --from sentinel1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000${BOND_DENOM} \
        -y \
        --output json 2>&1)
    if ! submit_tx_and_wait "$TX_RES" || ! check_tx_success "$TX_RESULT"; then
        echo "FATAL: failed to bond sentinel1"
        exit 1
    fi
    echo "  Sentinel1 bonded"
else
    echo "  Sentinel1 already bonded (skipping bootstrap)"
fi
echo ""

# Convenience: poster1 creates a post, returns the post id via POST_ID.
create_post_as_poster1() {
    local BODY="$1"
    TX_RES=$($BINARY tx forum create-post \
        "$TEST_CATEGORY_ID" \
        "0" \
        "$BODY" \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000${BOND_DENOM} \
        -y \
        --output json 2>&1)
    if ! submit_tx_and_wait "$TX_RES" || ! check_tx_success "$TX_RESULT"; then
        POST_ID=""
        return 1
    fi
    POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
    if [ -z "$POST_ID" ] || [ "$POST_ID" == "null" ]; then
        POSTS=$($BINARY query forum list-post --output json 2>&1)
        POST_ID=$(echo "$POSTS" | jq -r '.post[-1].id // empty')
    fi
    return 0
}

# Hide POST_ID using SENTINEL_KEY (key name). Returns 0 on success.
hide_post() {
    local POST_ID="$1"
    local SENTINEL_KEY="$2"
    local REASON_TEXT="${3:-test-hide}"
    TX_RES=$($BINARY tx forum hide-post \
        "$POST_ID" \
        "1" \
        "$REASON_TEXT" \
        --from "$SENTINEL_KEY" \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000${BOND_DENOM} \
        -y \
        --output json 2>&1)
    submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"
}

# Returns the post's status string via `query forum get-post`.
post_status() {
    local POST_ID="$1"
    $BINARY query forum get-post "$POST_ID" --output json 2>&1 \
        | jq -r '.post.status // .Post.status // "UNKNOWN"'
}

# ========================================================================
# PART 1: HAPPY PATH — SENTINEL SELF-CORRECT WITHIN WINDOW
# ========================================================================
echo "--- PART 1: SENTINEL SELF-CORRECT (UNHIDE WITHIN WINDOW) ---"

create_post_as_poster1 "Self-correct test post $(date +%s)"
if [ -z "$POST_ID" ]; then
    echo "  Setup failed: could not create post"
    SELF_CORRECT_RESULT="FAIL"
else
    echo "  Setup: created post $POST_ID"

    if hide_post "$POST_ID" "sentinel1"; then
        STATUS_AFTER_HIDE=$(post_status "$POST_ID")
        echo "  After hide: status=$STATUS_AFTER_HIDE"

        # Now sentinel1 self-corrects.
        TX_RES=$($BINARY tx forum unhide-post \
            "$POST_ID" \
            --from sentinel1 \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000${BOND_DENOM} \
            -y \
            --output json 2>&1)

        if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
            STATUS_AFTER_UNHIDE=$(post_status "$POST_ID")
            EVT_POST_ID=$(extract_event_value "$TX_RESULT" "post_unhidden" "post_id")
            IS_SELF_CORRECT=$(extract_event_value "$TX_RESULT" "post_unhidden" "is_self_correct")
            IS_COUNCIL=$(extract_event_value "$TX_RESULT" "post_unhidden" "is_council")

            echo "  After unhide: status=$STATUS_AFTER_UNHIDE; event post_id=$EVT_POST_ID is_self_correct=$IS_SELF_CORRECT is_council=$IS_COUNCIL"

            if [ "$STATUS_AFTER_UNHIDE" = "POST_STATUS_ACTIVE" ] \
               && [ "$EVT_POST_ID" = "$POST_ID" ] \
               && [ "$IS_SELF_CORRECT" = "true" ] \
               && [ "$IS_COUNCIL" = "false" ]; then
                SELF_CORRECT_RESULT="PASS"
            else
                echo "  ERROR: unexpected post status or event attributes after self-correct"
                SELF_CORRECT_RESULT="FAIL"
            fi
        else
            echo "  ERROR: sentinel self-correct tx failed"
            SELF_CORRECT_RESULT="FAIL"
        fi
    else
        echo "  Setup: hide failed; cannot exercise self-correct"
        SELF_CORRECT_RESULT="FAIL"
    fi
fi

echo ""

# ========================================================================
# PART 2: HAPPY PATH — COUNCIL UNHIDE (ALICE)
# ========================================================================
echo "--- PART 2: COUNCIL UNHIDE (ALICE OVERRIDE) ---"

create_post_as_poster1 "Council unhide test post $(date +%s)"
if [ -z "$POST_ID" ]; then
    echo "  Setup failed: could not create post"
    COUNCIL_UNHIDE_RESULT="FAIL"
else
    echo "  Setup: created post $POST_ID"
    if hide_post "$POST_ID" "sentinel1"; then
        TX_RES=$($BINARY tx forum unhide-post \
            "$POST_ID" \
            --from alice \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000${BOND_DENOM} \
            -y \
            --output json 2>&1)
        if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
            STATUS=$(post_status "$POST_ID")
            IS_COUNCIL=$(extract_event_value "$TX_RESULT" "post_unhidden" "is_council")
            IS_SELF_CORRECT=$(extract_event_value "$TX_RESULT" "post_unhidden" "is_self_correct")
            echo "  After alice unhide: status=$STATUS is_council=$IS_COUNCIL is_self_correct=$IS_SELF_CORRECT"
            if [ "$STATUS" = "POST_STATUS_ACTIVE" ] \
               && [ "$IS_COUNCIL" = "true" ] \
               && [ "$IS_SELF_CORRECT" = "false" ]; then
                COUNCIL_UNHIDE_RESULT="PASS"
            else
                echo "  ERROR: unexpected status or event attributes after council unhide"
                COUNCIL_UNHIDE_RESULT="FAIL"
            fi
        else
            echo "  ERROR: council unhide tx failed"
            COUNCIL_UNHIDE_RESULT="FAIL"
        fi
    else
        COUNCIL_UNHIDE_RESULT="FAIL"
    fi
fi

echo ""

# ========================================================================
# PART 3: HIDE RECORD CLEANED UP AFTER UNHIDE
# ========================================================================
echo "--- PART 3: HIDE RECORD REMOVED AFTER UNHIDE ---"

create_post_as_poster1 "HideRecord cleanup test $(date +%s)"
if [ -z "$POST_ID" ]; then
    HIDE_RECORD_CLEANUP_RESULT="FAIL"
else
    if hide_post "$POST_ID" "sentinel1"; then
        # Verify HideRecord exists.
        REC_BEFORE=$($BINARY query forum get-hide-record "$POST_ID" --output json 2>&1)
        if echo "$REC_BEFORE" | grep -qiE "not found|does not exist"; then
            echo "  ERROR: HideRecord missing after hide; cannot verify cleanup"
            HIDE_RECORD_CLEANUP_RESULT="FAIL"
        else
            # Unhide via sentinel.
            TX_RES=$($BINARY tx forum unhide-post "$POST_ID" \
                --from sentinel1 --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} -y --output json 2>&1)
            if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
                REC_AFTER=$($BINARY query forum get-hide-record "$POST_ID" --output json 2>&1)
                if echo "$REC_AFTER" | grep -qiE "not found|does not exist"; then
                    echo "  HideRecord removed after unhide (correct)"
                    HIDE_RECORD_CLEANUP_RESULT="PASS"
                else
                    echo "  ERROR: HideRecord still present after unhide"
                    HIDE_RECORD_CLEANUP_RESULT="FAIL"
                fi
            else
                HIDE_RECORD_CLEANUP_RESULT="FAIL"
            fi
        fi
    else
        HIDE_RECORD_CLEANUP_RESULT="FAIL"
    fi
fi

echo ""

# ========================================================================
# PART 4: RE-HIDE AFTER UNHIDE STILL WORKS (round-trip)
# ========================================================================
echo "--- PART 4: RE-HIDE AFTER UNHIDE WORKS ---"

create_post_as_poster1 "Re-hide round-trip test $(date +%s)"
if [ -z "$POST_ID" ]; then
    REHIDE_RESULT="FAIL"
else
    if hide_post "$POST_ID" "sentinel1"; then
        TX_RES=$($BINARY tx forum unhide-post "$POST_ID" \
            --from sentinel1 --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} -y --output json 2>&1)
        if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
            # Now re-hide; should succeed cleanly.
            if hide_post "$POST_ID" "sentinel1" "second hide"; then
                STATUS=$(post_status "$POST_ID")
                if [ "$STATUS" = "POST_STATUS_HIDDEN" ]; then
                    echo "  Re-hide succeeded (correct)"
                    REHIDE_RESULT="PASS"

                    # Cleanup: leave the post unhidden so the suite is idempotent.
                    TX_RES=$($BINARY tx forum unhide-post "$POST_ID" \
                        --from sentinel1 --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} -y --output json 2>&1)
                    submit_tx_and_wait "$TX_RES" > /dev/null
                else
                    echo "  ERROR: re-hide did not move post back to HIDDEN (status=$STATUS)"
                    REHIDE_RESULT="FAIL"
                fi
            else
                echo "  ERROR: re-hide tx failed"
                REHIDE_RESULT="FAIL"
            fi
        else
            REHIDE_RESULT="FAIL"
        fi
    else
        REHIDE_RESULT="FAIL"
    fi
fi

echo ""

# ########################################################################
#
#   NEGATIVE PATH TESTS
#
# ########################################################################

echo "========================================================================"
echo "  NEGATIVE PATH TESTS"
echo "========================================================================"
echo ""

# Common shared target for the negative cases — must survive every attempt
# so the final cleanup can unhide it.
create_post_as_poster1 "Negative target $(date +%s)"
if [ -z "$POST_ID" ]; then
    echo "FATAL: could not provision negative-target post."
    exit 1
fi
NEG_TARGET=$POST_ID
echo "Negative-test target post id: $NEG_TARGET"
hide_post "$NEG_TARGET" "sentinel1" "negative-test target" > /dev/null
STATUS_NEG=$(post_status "$NEG_TARGET")
if [ "$STATUS_NEG" != "POST_STATUS_HIDDEN" ]; then
    echo "FATAL: negative-target failed to enter HIDDEN state (got $STATUS_NEG)"
    exit 1
fi
echo "  negative-target now HIDDEN"
echo ""

# ========================================================================
# NEG 1: WRONG SENTINEL (sentinel2 did not hide this post)
# ========================================================================
echo "--- NEG 1: WRONG SENTINEL (sentinel2) ---"
TX_RES=$($BINARY tx forum unhide-post \
    "$NEG_TARGET" \
    --from sentinel2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)
expect_tx_failure "$TX_RES" "NEG_WRONG_SENTINEL_RESULT" "non-hiding sentinel unhid a post!"
echo ""

# ========================================================================
# NEG 2: AUTHOR CANNOT SELF-UNHIDE
# ========================================================================
echo "--- NEG 2: POST AUTHOR (poster1) ---"
TX_RES=$($BINARY tx forum unhide-post \
    "$NEG_TARGET" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)
expect_tx_failure "$TX_RES" "NEG_AUTHOR_SELF_RESULT" "post author self-unhid their own post!"
echo ""

# ========================================================================
# NEG 3: RANDOM NON-MEMBER NON-SENTINEL
# ========================================================================
echo "--- NEG 3: RANDOM ACCOUNT (poster2) ---"
TX_RES=$($BINARY tx forum unhide-post \
    "$NEG_TARGET" \
    --from poster2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)
expect_tx_failure "$TX_RES" "NEG_RANDOM_RESULT" "random member unhid a sentinel hide!"
echo ""

# ========================================================================
# NEG 4: UNHIDE AN ACTIVE (NOT-HIDDEN) POST
# ========================================================================
echo "--- NEG 4: UNHIDE A POST THAT ISN'T HIDDEN ---"
create_post_as_poster1 "Active never hidden $(date +%s)"
if [ -n "$POST_ID" ]; then
    TX_RES=$($BINARY tx forum unhide-post \
        "$POST_ID" \
        --from sentinel1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000${BOND_DENOM} \
        -y \
        --output json 2>&1)
    expect_tx_failure "$TX_RES" "NEG_NOT_HIDDEN_RESULT" "unhide of never-hidden post succeeded!"
else
    NEG_NOT_HIDDEN_RESULT="FAIL"
fi
echo ""

# ========================================================================
# NEG 5: UNHIDE A NON-EXISTENT POST ID
# ========================================================================
echo "--- NEG 5: UNHIDE NON-EXISTENT POST ID ---"
TX_RES=$($BINARY tx forum unhide-post \
    "999999999" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)
expect_tx_failure "$TX_RES" "NEG_MISSING_RESULT" "unhide of non-existent post succeeded!"
echo ""

# ========================================================================
# NEG 6: NEGATIVE TARGET SURVIVED EVERY FAILED ATTEMPT
# ========================================================================
echo "--- NEG 6: NEGATIVE TARGET NEVER UNHIDDEN ---"
# The invariant under test is that none of the failed unhide attempts ever
# flipped the target back to ACTIVE. Under the testparams build the hidden
# expiration is only 15s (vs 7 days in prod), so the cumulative ~6s/tx pacing
# of NEG 1-5 can let ExpireHiddenPosts soft-delete the target mid-run. That is
# fine for this assertion: a post only reaches DELETED via expiry of a still-
# HIDDEN post — a successful unhide would have set it ACTIVE (and ACTIVE posts
# are never touched by ExpireHiddenPosts). So HIDDEN or DELETED both prove no
# unauthorized unhide succeeded; only ACTIVE is a real failure.
STATUS_FINAL=$(post_status "$NEG_TARGET")
if [ "$STATUS_FINAL" = "POST_STATUS_HIDDEN" ] || [ "$STATUS_FINAL" = "POST_STATUS_DELETED" ]; then
    echo "  Negative target $NEG_TARGET never unhidden (status=$STATUS_FINAL) after all failed attempts (correct)"
    NEG_TARGET_SURVIVED_RESULT="PASS"
else
    echo "  ERROR: negative target moved to $STATUS_FINAL — a failed unhide attempt promoted it to ACTIVE"
    NEG_TARGET_SURVIVED_RESULT="FAIL"
fi

# Cleanup — leave the suite idempotent across reruns.
TX_RES=$($BINARY tx forum unhide-post "$NEG_TARGET" \
    --from sentinel1 --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} -y --output json 2>&1)
submit_tx_and_wait "$TX_RES" > /dev/null

echo ""

# ========================================================================
# SUMMARY
# ========================================================================
echo "========================================================================"
echo "  UNHIDE POST TEST SUMMARY"
echo "========================================================================"
echo ""
echo "  --- Happy / Edge Path ---"
echo "  Sentinel self-correct within window:  $SELF_CORRECT_RESULT"
echo "  Council unhide (alice):                $COUNCIL_UNHIDE_RESULT"
echo "  HideRecord cleaned up after unhide:    $HIDE_RECORD_CLEANUP_RESULT"
echo "  Re-hide after unhide works:            $REHIDE_RESULT"
echo ""
echo "  --- Negative Path ---"
echo "  Wrong sentinel can't unhide:           $NEG_WRONG_SENTINEL_RESULT"
echo "  Author can't self-unhide:              $NEG_AUTHOR_SELF_RESULT"
echo "  Random account can't unhide:           $NEG_RANDOM_RESULT"
echo "  Unhide of never-hidden post rejected:  $NEG_NOT_HIDDEN_RESULT"
echo "  Unhide of missing post rejected:       $NEG_MISSING_RESULT"
echo "  Negative target survived:              $NEG_TARGET_SURVIVED_RESULT"
echo ""

FAIL_COUNT=0
TOTAL_COUNT=0
for RESULT in \
    "$SELF_CORRECT_RESULT" "$COUNCIL_UNHIDE_RESULT" "$HIDE_RECORD_CLEANUP_RESULT" "$REHIDE_RESULT" \
    "$NEG_WRONG_SENTINEL_RESULT" "$NEG_AUTHOR_SELF_RESULT" "$NEG_RANDOM_RESULT" \
    "$NEG_NOT_HIDDEN_RESULT" "$NEG_MISSING_RESULT" "$NEG_TARGET_SURVIVED_RESULT"; do
    TOTAL_COUNT=$((TOTAL_COUNT + 1))
    if [ "$RESULT" = "FAIL" ]; then
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
done
PASS_COUNT=$((TOTAL_COUNT - FAIL_COUNT))

echo "  Total: $TOTAL_COUNT | Passed: $PASS_COUNT | Failed: $FAIL_COUNT"
echo ""

if [ "$FAIL_COUNT" -gt 0 ]; then
    echo "  FAILURES: $FAIL_COUNT test(s) failed"
    exit 1
fi

echo "  ALL TESTS PASSED"
echo ""
echo "UNHIDE POST TEST COMPLETED"
echo ""
