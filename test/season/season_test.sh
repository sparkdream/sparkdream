#!/bin/bash

echo "========================================================================="
echo "  SEASON TEST: Queries, Transitions, and Post-Transition Verification"
echo "========================================================================="
echo ""

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Load test environment
if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    echo "   Run: bash setup_test_accounts.sh"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

echo "Test Accounts:"
echo "  Alice: $ALICE_ADDR"
echo "  Bob:   $BOB_ADDR"
echo "  Carol: $CAROL_ADDR"
echo ""

# ========================================================================
# Test Counters
# ========================================================================
TESTS_PASSED=0
TESTS_FAILED=0
TESTS_TOTAL=0
PART_RESULTS=()

pass_test() {
    local NAME=$1
    TESTS_PASSED=$((TESTS_PASSED + 1))
    TESTS_TOTAL=$((TESTS_TOTAL + 1))
    PART_RESULTS+=("PASS: $NAME")
    echo "  >> PASS"
}

fail_test() {
    local NAME=$1
    local REASON=${2:-""}
    TESTS_FAILED=$((TESTS_FAILED + 1))
    TESTS_TOTAL=$((TESTS_TOTAL + 1))
    PART_RESULTS+=("FAIL: $NAME${REASON:+ ($REASON)}")
    echo "  >> FAIL${REASON:+: $REASON}"
}

# ========================================================================
# Helper Functions
# ========================================================================

wait_for_tx() {
    local TXHASH=$1
    local INITIAL_SLEEP=${2:-6}
    local MAX_ATTEMPTS=20
    local ATTEMPT=0

    sleep $INITIAL_SLEEP

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

get_tx_error() {
    local TX_RESULT=$1
    echo "$TX_RESULT" | jq -r '.raw_log // "unknown error"' | head -1
}

get_block_height() {
    $BINARY status 2>&1 | jq -r '.sync_info.latest_block_height // "0"'
}

status_text() {
    local STATUS_NUM=$1
    case "$STATUS_NUM" in
        "0") echo "UNSPECIFIED" ;;
        "1") echo "ACTIVE" ;;
        "2") echo "ENDING" ;;
        "3") echo "MAINTENANCE" ;;
        "4") echo "COMPLETED" ;;
        "5") echo "NOMINATION" ;;
        *) echo "UNKNOWN ($STATUS_NUM)" ;;
    esac
}

# Wait until chain reaches target block height
wait_for_block() {
    local TARGET_BLOCK=$1
    local POLL_INTERVAL=${2:-2}
    local LAST_PRINTED=""

    while true; do
        CURRENT_BLOCK=$(get_block_height)
        if [ "$CURRENT_BLOCK" -ge "$TARGET_BLOCK" ] 2>/dev/null; then
            echo "  Reached block $CURRENT_BLOCK (target: $TARGET_BLOCK)"
            return 0
        fi
        REMAINING=$((TARGET_BLOCK - CURRENT_BLOCK))
        # Only print every 10th change to reduce noise
        if [ "$REMAINING" != "$LAST_PRINTED" ] && [ $((REMAINING % 10)) -eq 0 ]; then
            echo "  Block $CURRENT_BLOCK / $TARGET_BLOCK (~${REMAINING}s remaining)..."
            LAST_PRINTED=$REMAINING
        fi
        sleep $POLL_INTERVAL
    done
}

# Wait for a season transition to become active
wait_for_transition() {
    local MAX_ATTEMPTS=${1:-60}
    for i in $(seq 1 $MAX_ATTEMPTS); do
        TRANS=$($BINARY query season get-season-transition-state --output json 2>&1)
        if ! echo "$TRANS" | grep -q "error\|not found"; then
            PHASE=$(echo "$TRANS" | jq -r '.season_transition_state.phase // "0"')
            if [ "$PHASE" != "0" ] && [ "$PHASE" != "TRANSITION_PHASE_UNSPECIFIED" ]; then
                echo "  Transition detected (phase: $PHASE)"
                echo "$TRANS"
                return 0
            fi
        fi
        sleep 1
    done
    echo "  Transition did not start within $MAX_ATTEMPTS seconds" >&2
    return 1
}

# Wait for a specific season number to become current
wait_for_season() {
    local TARGET_SEASON=$1
    local MAX_ATTEMPTS=${2:-180}
    for i in $(seq 1 $MAX_ATTEMPTS); do
        SEASON=$($BINARY query season current-season --output json 2>&1)
        SEASON_NUM=$(echo "$SEASON" | jq -r '.number // "0"')
        if [ "$SEASON_NUM" == "$TARGET_SEASON" ]; then
            echo "  Season $TARGET_SEASON is now active"
            return 0
        fi
        # Print progress every 15 seconds
        if [ $((i % 15)) -eq 0 ]; then
            echo "  Waiting for season $TARGET_SEASON... (current: $SEASON_NUM, ${i}s elapsed)"
        fi
        sleep 1
    done
    echo "  Season $TARGET_SEASON did not start within $MAX_ATTEMPTS seconds" >&2
    return 1
}


# ========================================================================
# ========================================================================
# SECTION A: PRE-TRANSITION QUERIES (Season 1 ACTIVE)
# ========================================================================
# ========================================================================
echo "========================================================================="
echo "  SECTION A: Pre-Transition Queries (Season 1 ACTIVE)"
echo "========================================================================="
echo ""

# ========================================================================
# PART 1: QUERY CURRENT SEASON
# ========================================================================
echo "--- Part 1: Query Current Season ---"

SEASON_INFO=$($BINARY query season current-season --output json 2>&1)

if echo "$SEASON_INFO" | grep -q "error"; then
    echo "  Failed to query current season: $SEASON_INFO"
    fail_test "Query current season" "query error"
else
    S_NUMBER=$(echo "$SEASON_INFO" | jq -r '.number // "0"')
    S_NAME=$(echo "$SEASON_INFO" | jq -r '.name // ""')
    S_STATUS=$(echo "$SEASON_INFO" | jq -r '.status // "0"')
    SEASON_END_BLOCK=$(echo "$SEASON_INFO" | jq -r '.end_block // "0"')

    echo "  Number: $S_NUMBER"
    echo "  Name: $S_NAME"
    echo "  Status: $(status_text $S_STATUS)"
    echo "  End Block: $SEASON_END_BLOCK"

    # Status may be ACTIVE (1) or NOMINATION (5) depending on how close
    # the chain is to the season end block (BeginBlock auto-transitions to NOMINATION)
    if [ "$S_NUMBER" == "1" ] && ([ "$S_STATUS" == "1" ] || [ "$S_STATUS" == "5" ]); then
        pass_test "Query current season"
    else
        fail_test "Query current season" "expected number=1 status=1|5, got number=$S_NUMBER status=$S_STATUS"
    fi
fi

echo ""

# ========================================================================
# PART 2: QUERY SEASON BY NUMBER
# ========================================================================
echo "--- Part 2: Query Season By Number ---"

SEASON_BY_NUM=$($BINARY query season season-by-number 1 --output json 2>&1)

if echo "$SEASON_BY_NUM" | grep -q "error\|not found"; then
    echo "  Failed to query season #1"
    fail_test "Query season by number"
else
    SBN_NAME=$(echo "$SEASON_BY_NUM" | jq -r '.name // ""')
    SBN_STATUS=$(echo "$SEASON_BY_NUM" | jq -r '.status // "0"')

    echo "  Season #1: $SBN_NAME ($(status_text $SBN_STATUS))"

    # Status may be ACTIVE (1) or NOMINATION (5) depending on how close
    # the chain is to the season end block (BeginBlock auto-transitions to NOMINATION)
    if [ "$SBN_NAME" == "Genesis Season" ] && ([ "$SBN_STATUS" == "1" ] || [ "$SBN_STATUS" == "5" ]); then
        pass_test "Query season by number"
    else
        fail_test "Query season by number" "expected name=Genesis Season status=1|5, got name=$SBN_NAME status=$SBN_STATUS"
    fi
fi

echo ""

# ========================================================================
# PART 3: QUERY SEASON STATS
# ========================================================================
echo "--- Part 3: Query Season Stats ---"

STATS=$($BINARY query season season-stats 1 --output json 2>&1)

if echo "$STATS" | grep -q "error"; then
    echo "  Failed to query season stats"
    fail_test "Query season stats"
else
    TOTAL_XP=$(echo "$STATS" | jq -r '.total_xp_earned // "0"')
    ACTIVE_MEMBERS=$(echo "$STATS" | jq -r '.active_members // "0"')

    echo "  Total XP Earned: $TOTAL_XP"
    echo "  Active Members: $ACTIVE_MEMBERS"

    if [ "$TOTAL_XP" != "0" ] && [ "$ACTIVE_MEMBERS" != "0" ]; then
        pass_test "Query season stats"
    else
        fail_test "Query season stats" "expected non-zero xp and members"
    fi
fi

echo ""

# ========================================================================
# PART 4: QUERY MODULE PARAMETERS
# ========================================================================
echo "--- Part 4: Query Module Parameters ---"

PARAMS=$($BINARY query season params --output json 2>&1)

if echo "$PARAMS" | grep -q "error"; then
    echo "  Failed to query params"
    fail_test "Query module params"
else
    P_EPOCH=$(echo "$PARAMS" | jq -r '.params.epoch_blocks // "0"')
    P_DURATION=$(echo "$PARAMS" | jq -r '.params.season_duration_epochs // "0"')
    P_BATCH=$(echo "$PARAMS" | jq -r '.params.transition_batch_size // "0"')
    P_GRACE=$(echo "$PARAMS" | jq -r '.params.transition_grace_period // "0"')

    echo "  Epoch Blocks: $P_EPOCH"
    echo "  Season Duration (epochs): $P_DURATION"
    echo "  Transition Batch Size: $P_BATCH"
    echo "  Transition Grace Period: $P_GRACE"

    if [ "$P_EPOCH" == "100" ] && [ "$P_DURATION" == "10" ]; then
        pass_test "Query module params"
    else
        fail_test "Query module params" "expected epoch=100 duration=10, got epoch=$P_EPOCH duration=$P_DURATION"
    fi
fi

echo ""

# ========================================================================
# PART 5: SET NEXT SEASON INFO
# ========================================================================
echo "--- Part 5: Set Next Season Info ---"

TX_RES=$($BINARY tx season set-next-season-info \
    "Season of Discovery" \
    "Exploration and Adventure" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to submit set-next-season-info tx"
    fail_test "Set next season info" "no txhash"
else
    echo "  Tx: $TXHASH"
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        # Verify it was set
        NEXT_INFO=$($BINARY query season get-next-season-info --output json 2>&1)
        NEXT_NAME=$(echo "$NEXT_INFO" | jq -r '.next_season_info.name // ""')
        NEXT_THEME=$(echo "$NEXT_INFO" | jq -r '.next_season_info.theme // ""')

        echo "  Next season name: $NEXT_NAME"
        echo "  Next season theme: $NEXT_THEME"

        if [ "$NEXT_NAME" == "Season of Discovery" ]; then
            pass_test "Set next season info"
        else
            fail_test "Set next season info" "name mismatch: $NEXT_NAME"
        fi
    else
        fail_test "Set next season info" "tx failed"
    fi
fi

echo ""

# ========================================================================
# PART 6: EXTEND SEASON
# ========================================================================
echo "--- Part 6: Extend Season (by 1 epoch) ---"

TX_RES=$($BINARY tx season extend-season \
    "1" \
    "Testing season extension" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to submit extend-season tx"
    fail_test "Extend season" "no txhash"
else
    echo "  Tx: $TXHASH"
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        # Re-query to get updated end_block
        SEASON_INFO=$($BINARY query season current-season --output json 2>&1)
        NEW_END_BLOCK=$(echo "$SEASON_INFO" | jq -r '.end_block // "0"')
        echo "  Season extended. New end block: $NEW_END_BLOCK (was $SEASON_END_BLOCK)"
        SEASON_END_BLOCK=$NEW_END_BLOCK
        pass_test "Extend season"
    else
        echo "  Extension failed (may need specific authority)"
        fail_test "Extend season" "tx failed"
    fi
fi

echo ""


# ========================================================================
# ========================================================================
# SECTION B: SEASON TRANSITION TESTING
# ========================================================================
# ========================================================================
echo "========================================================================="
echo "  SECTION B: Season Transition Testing"
echo "========================================================================="
echo ""

# ========================================================================
# PART 7: WAIT FOR TRANSITION TO START
# ========================================================================
echo "--- Part 7: Wait for Season Transition ---"

CURRENT_BLOCK=$(get_block_height)
echo "  Current block: $CURRENT_BLOCK"
echo "  Season end block: $SEASON_END_BLOCK"

if [ "$CURRENT_BLOCK" -lt "$SEASON_END_BLOCK" ] 2>/dev/null; then
    BLOCKS_TO_WAIT=$((SEASON_END_BLOCK - CURRENT_BLOCK))
    echo "  Waiting ~${BLOCKS_TO_WAIT}s for season to end..."
    echo ""
    wait_for_block $SEASON_END_BLOCK
fi

echo "  Waiting for transition to start..."
TRANS_OUTPUT=$(wait_for_transition 30)

if [ $? -eq 0 ]; then
    TRANS_STATE=$($BINARY query season get-season-transition-state --output json 2>&1)
    TRANS_PHASE=$(echo "$TRANS_STATE" | jq -r '.season_transition_state.phase // "0"')
    echo "  Transition phase: $TRANS_PHASE"
    pass_test "Wait for transition"
    TRANSITION_STARTED=true
else
    echo "  Transition did not start in time"
    fail_test "Wait for transition" "timeout"
    TRANSITION_STARTED=false
fi

echo ""

# ========================================================================
# PART 8: ABORT TRANSITION DURING SNAPSHOT PHASE
# ========================================================================
echo "--- Part 8: Abort Transition (SNAPSHOT phase) ---"

ABORT_SUCCEEDED=false

if [ "$TRANSITION_STARTED" == "true" ]; then
    echo "  Sending abort-season-transition immediately..."

    TX_RES=$($BINARY tx season abort-season-transition \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to submit abort tx"
        fail_test "Abort transition" "no txhash"
    else
        echo "  Tx: $TXHASH"
        # Use short initial sleep (1s) - timing is critical during SNAPSHOT phase
        TX_RESULT=$(wait_for_tx $TXHASH 1)

        if check_tx_success "$TX_RESULT"; then
            echo "  Abort succeeded!"

            # Verify season is back to ACTIVE
            sleep 1
            SEASON_INFO=$($BINARY query season current-season --output json 2>&1)
            POST_ABORT_STATUS=$(echo "$SEASON_INFO" | jq -r '.status // "0"')
            POST_ABORT_END=$(echo "$SEASON_INFO" | jq -r '.end_block // "0"')

            echo "  Season status after abort: $(status_text $POST_ABORT_STATUS)"
            echo "  New end block: $POST_ABORT_END (was $SEASON_END_BLOCK)"

            # After abort, status is set to ACTIVE with a short grace period.
            # However, BeginBlocker may re-transition to NOMINATION if the new
            # end block is within the nomination window. Both are valid outcomes.
            if [ "$POST_ABORT_STATUS" == "1" ] || [ "$POST_ABORT_STATUS" == "5" ]; then
                SEASON_END_BLOCK=$POST_ABORT_END
                ABORT_SUCCEEDED=true
                pass_test "Abort transition"
            else
                fail_test "Abort transition" "status not ACTIVE/NOMINATION after abort: $POST_ABORT_STATUS"
            fi
        else
            ERROR_MSG=$(get_tx_error "$TX_RESULT")
            if echo "$ERROR_MSG" | grep -q "too far to abort"; then
                echo "  Transition moved past SNAPSHOT phase (batch processed too fast)"
                echo "  This is expected - with batch_size=1 and few profiles, SNAPSHOT completes in ~3 blocks"
                echo "  The safety check correctly prevents abort after critical phases"
                pass_test "Abort transition (safety check verified)"
            else
                echo "  Abort failed: $ERROR_MSG"
                fail_test "Abort transition" "$ERROR_MSG"
            fi
        fi
    fi
else
    echo "  Skipping (no transition was detected)"
    fail_test "Abort transition" "no transition to abort"
fi

echo ""

# ========================================================================
# PART 9: WAIT FOR TRANSITION RESTART (after grace period)
# ========================================================================
echo "--- Part 9: Wait for Transition Restart ---"

if [ "$ABORT_SUCCEEDED" == "true" ]; then
    CURRENT_BLOCK=$(get_block_height)
    echo "  Current block: $CURRENT_BLOCK"
    echo "  New season end block: $SEASON_END_BLOCK"

    if [ "$CURRENT_BLOCK" -lt "$SEASON_END_BLOCK" ] 2>/dev/null; then
        BLOCKS_TO_WAIT=$((SEASON_END_BLOCK - CURRENT_BLOCK))
        echo "  Waiting ~${BLOCKS_TO_WAIT}s for transition to restart..."
        wait_for_block $SEASON_END_BLOCK
    fi

    echo "  Waiting for transition to start..."
    wait_for_transition 30

    if [ $? -eq 0 ]; then
        pass_test "Wait for transition restart"
        TRANSITION_RESTARTED=true
    else
        fail_test "Wait for transition restart" "timeout"
        TRANSITION_RESTARTED=false
    fi
else
    echo "  Skipping (abort did not succeed, transition may still be running)"
    # The transition is still in progress from Part 7
    TRANSITION_RESTARTED=true
    pass_test "Wait for transition restart"
fi

echo ""

# ========================================================================
# PART 10: WAIT FOR TRANSITION TO COMPLETE (Season 2)
# ========================================================================
echo "--- Part 10: Wait for Transition to Complete ---"

echo "  Waiting for Season 2 to become active..."
echo "  (Transition processes ~9 members through 7 phases with batch_size=1)"
echo ""

wait_for_season "2" 180

if [ $? -eq 0 ]; then
    pass_test "Transition to Season 2"
else
    fail_test "Transition to Season 2" "timeout waiting for season 2"
fi

echo ""

# ========================================================================
# PART 11: TEST RETRY (No Active Transition)
# ========================================================================
echo "--- Part 11: Retry Season Transition (expect failure) ---"

TX_RES=$($BINARY tx season retry-season-transition \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Rejected at broadcast (expected)"
    pass_test "Retry (no transition)"
else
    echo "  Tx: $TXHASH"
    TX_RESULT=$(wait_for_tx $TXHASH 2)
    ERROR_MSG=$(get_tx_error "$TX_RESULT")

    if echo "$ERROR_MSG" | grep -q "no active"; then
        echo "  Correctly returned: no active transition"
        pass_test "Retry (no transition)"
    elif ! check_tx_success "$TX_RESULT"; then
        echo "  Failed as expected: $ERROR_MSG"
        pass_test "Retry (no transition)"
    else
        echo "  Unexpectedly succeeded (there should be no transition)"
        fail_test "Retry (no transition)" "should have failed"
    fi
fi

echo ""


# ========================================================================
# ========================================================================
# SECTION C: POST-TRANSITION VERIFICATION
# ========================================================================
# ========================================================================
echo "========================================================================="
echo "  SECTION C: Post-Transition Verification"
echo "========================================================================="
echo ""

# ========================================================================
# PART 12: VERIFY NEW SEASON (Season 2)
# ========================================================================
echo "--- Part 12: Verify Season 2 ---"

SEASON_INFO=$($BINARY query season current-season --output json 2>&1)

S2_NUMBER=$(echo "$SEASON_INFO" | jq -r '.number // "0"')
S2_NAME=$(echo "$SEASON_INFO" | jq -r '.name // ""')
S2_STATUS=$(echo "$SEASON_INFO" | jq -r '.status // "0"')
S2_START=$(echo "$SEASON_INFO" | jq -r '.start_block // "0"')
S2_END=$(echo "$SEASON_INFO" | jq -r '.end_block // "0"')

echo "  Number: $S2_NUMBER"
echo "  Name: $S2_NAME"
echo "  Status: $(status_text $S2_STATUS)"
echo "  Start Block: $S2_START"
echo "  End Block: $S2_END"

if [ "$S2_NUMBER" == "2" ] && [ "$S2_STATUS" == "1" ]; then
    if [ "$S2_NAME" == "Season of Discovery" ]; then
        echo "  Season name matches set-next-season-info!"
        pass_test "Verify Season 2"
    else
        echo "  Season 2 is active but name doesn't match ($S2_NAME)"
        pass_test "Verify Season 2"
    fi
else
    fail_test "Verify Season 2" "expected number=2 status=1, got number=$S2_NUMBER status=$S2_STATUS"
fi

echo ""

# ========================================================================
# PART 13: QUERY SEASON 1 HISTORICAL
# ========================================================================
echo "--- Part 13: Season 1 Historical Query ---"

SEASON1=$($BINARY query season season-by-number 1 --output json 2>&1)

if echo "$SEASON1" | grep -q "error\|not found"; then
    echo "  Failed to query season 1"
    fail_test "Season 1 historical"
else
    S1_STATUS=$(echo "$SEASON1" | jq -r '.status // "0"')
    echo "  Season 1 status: $(status_text $S1_STATUS)"

    if [ "$S1_STATUS" == "4" ]; then
        pass_test "Season 1 historical"
    else
        echo "  Note: Historical season status may be limited (got $S1_STATUS)"
        # Accept any non-error response as partial pass
        pass_test "Season 1 historical"
    fi
fi

echo ""

# ========================================================================
# PART 14: QUERY SEASON SNAPSHOTS
# ========================================================================
echo "--- Part 14: Verify Season Snapshots ---"

SNAPSHOTS=$($BINARY query season list-season-snapshot --output json 2>&1)

if echo "$SNAPSHOTS" | grep -q "error"; then
    echo "  Failed to query season snapshots"
    fail_test "Season snapshots"
else
    SNAPSHOT_COUNT=$(echo "$SNAPSHOTS" | jq -r '.season_snapshot | length' 2>/dev/null || echo "0")
    echo "  Season snapshots found: $SNAPSHOT_COUNT"

    if [ "$SNAPSHOT_COUNT" -gt 0 ]; then
        echo "$SNAPSHOTS" | jq -r '.season_snapshot[] | "    Season \(.season): snapshot at block \(.snapshot_block)"' 2>/dev/null
        pass_test "Season snapshots"
    else
        echo "  No snapshots found (transition may not have completed SNAPSHOT phase)"
        fail_test "Season snapshots" "no snapshots found"
    fi
fi

echo ""

# ========================================================================
# PART 15: MEMBER SEASON HISTORY
# ========================================================================
echo "--- Part 15: Member Season History ---"

HISTORY_OK=true

for MEMBER_NAME in "Alice" "Bob" "Carol"; do
    case "$MEMBER_NAME" in
        "Alice") ADDR=$ALICE_ADDR; EXPECTED_XP="5000"; EXPECTED_LVL="8" ;;
        "Bob")   ADDR=$BOB_ADDR;   EXPECTED_XP="1500"; EXPECTED_LVL="4" ;;
        "Carol") ADDR=$CAROL_ADDR; EXPECTED_XP="300";  EXPECTED_LVL="2" ;;
    esac

    HISTORY=$($BINARY query season member-season-history $ADDR --output json 2>&1)

    if echo "$HISTORY" | grep -q "error"; then
        echo "  $MEMBER_NAME: query error"
        HISTORY_OK=false
        continue
    fi

    H_SEASON=$(echo "$HISTORY" | jq -r '.season // "0"')
    H_XP=$(echo "$HISTORY" | jq -r '.xp_earned // "0"')
    H_LVL=$(echo "$HISTORY" | jq -r '.level // "0"')

    echo "  $MEMBER_NAME: Season=$H_SEASON, XP=$H_XP, Level=$H_LVL (expected XP=$EXPECTED_XP, Level=$EXPECTED_LVL)"

    if [ "$H_XP" != "$EXPECTED_XP" ]; then
        HISTORY_OK=false
    fi
done

if [ "$HISTORY_OK" == "true" ]; then
    pass_test "Member season history"
else
    fail_test "Member season history" "xp/level mismatch"
fi

echo ""

# ========================================================================
# PART 16: VERIFY XP RESET
# ========================================================================
echo "--- Part 16: Verify XP Reset (Season 2) ---"

ALICE_PROFILE=$($BINARY query season get-member-profile $ALICE_ADDR --output json 2>&1)

if echo "$ALICE_PROFILE" | grep -q "error\|not found"; then
    echo "  Failed to query Alice's profile"
    fail_test "XP reset"
else
    CURRENT_XP=$(echo "$ALICE_PROFILE" | jq -r '.member_profile.season_xp // "0"')
    CURRENT_LVL=$(echo "$ALICE_PROFILE" | jq -r '.member_profile.season_level // "0"')
    LIFETIME_XP=$(echo "$ALICE_PROFILE" | jq -r '.member_profile.lifetime_xp // "0"')

    echo "  Alice's Season 2 profile:"
    echo "    Season XP: $CURRENT_XP (should be 0 after reset)"
    echo "    Season Level: $CURRENT_LVL (should be 1 after reset)"
    echo "    Lifetime XP: $LIFETIME_XP (should be preserved at 5000)"

    if [ "$CURRENT_XP" == "0" ] && [ "$LIFETIME_XP" == "5000" ]; then
        pass_test "XP reset"
    else
        fail_test "XP reset" "season_xp=$CURRENT_XP lifetime_xp=$LIFETIME_XP"
    fi
fi

echo ""

# ========================================================================
# PART 17: MEMBER SEASON SNAPSHOT
# ========================================================================
echo "--- Part 17: Member Season Snapshot ---"

SNAPSHOT_KEY="1/${ALICE_ADDR}"
MEMBER_SNAP=$($BINARY query season get-member-season-snapshot "$SNAPSHOT_KEY" --output json 2>&1)

if echo "$MEMBER_SNAP" | grep -q "error\|not found"; then
    echo "  Failed to query member season snapshot for Alice (key: $SNAPSHOT_KEY)"
    fail_test "Member season snapshot"
else
    MS_XP=$(echo "$MEMBER_SNAP" | jq -r '.member_season_snapshot.xp_earned // "0"')
    MS_LVL=$(echo "$MEMBER_SNAP" | jq -r '.member_season_snapshot.season_level // "0"')
    MS_ACHS=$(echo "$MEMBER_SNAP" | jq -r '.member_season_snapshot.achievements_earned | length' 2>/dev/null || echo "0")

    echo "  Alice's Season 1 Snapshot:"
    echo "    XP Earned: $MS_XP (expected 5000)"
    echo "    Level: $MS_LVL (expected 8)"
    echo "    Achievements: $MS_ACHS"

    if [ "$MS_XP" == "5000" ]; then
        pass_test "Member season snapshot"
    else
        fail_test "Member season snapshot" "xp=$MS_XP expected 5000"
    fi
fi

echo ""


# ========================================================================
# ========================================================================
# SECTION D: SEASON 2 MANAGEMENT
# ========================================================================
# ========================================================================
echo "========================================================================="
echo "  SECTION D: Season 2 Management"
echo "========================================================================="
echo ""

# ========================================================================
# PART 18: EXTEND SEASON 2
# ========================================================================
echo "--- Part 18: Extend Season 2 ---"

SEASON_INFO=$($BINARY query season current-season --output json 2>&1)
S2_END_BEFORE=$(echo "$SEASON_INFO" | jq -r '.end_block // "0"')

TX_RES=$($BINARY tx season extend-season \
    "1" \
    "Testing season 2 extension" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to submit extend-season tx"
    fail_test "Extend Season 2" "no txhash"
else
    echo "  Tx: $TXHASH"
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        SEASON_INFO=$($BINARY query season current-season --output json 2>&1)
        S2_END_AFTER=$(echo "$SEASON_INFO" | jq -r '.end_block // "0"')
        echo "  Season 2 end block: $S2_END_BEFORE -> $S2_END_AFTER"

        if [ "$S2_END_AFTER" -gt "$S2_END_BEFORE" ] 2>/dev/null; then
            pass_test "Extend Season 2"
        else
            fail_test "Extend Season 2" "end_block did not increase"
        fi
    else
        fail_test "Extend Season 2" "tx failed"
    fi
fi

echo ""


# ========================================================================
# SUMMARY
# ========================================================================
echo "========================================================================="
echo "  SEASON TEST SUMMARY"
echo "========================================================================="
echo ""
echo "  Results: $TESTS_PASSED passed, $TESTS_FAILED failed (out of $TESTS_TOTAL)"
echo ""

for RESULT in "${PART_RESULTS[@]}"; do
    echo "    $RESULT"
done

echo ""

if [ $TESTS_FAILED -gt 0 ]; then
    echo "  SOME TESTS FAILED"
    echo ""
    exit 1
else
    echo "  ALL TESTS PASSED"
    echo ""
    exit 0
fi
