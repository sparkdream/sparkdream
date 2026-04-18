#!/bin/bash

echo "--- TESTING: RETROACTIVE PUBLIC GOODS FUNDING (NOMINATIONS & STAKING) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

echo "Test Accounts:"
echo "  Alice:        $ALICE_ADDR"
echo "  Bob:          $BOB_ADDR"
echo "  Carol:        $CAROL_ADDR"
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
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        return 1
    fi
    return 0
}

check_tx_failure() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        return 0
    fi
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

extract_event_value() {
    local TX_RESULT=$1
    local EVENT_TYPE=$2
    local ATTR_KEY=$3
    echo "$TX_RESULT" | jq -r ".events[] | select(.type==\"$EVENT_TYPE\") | .attributes[] | select(.key==\"$ATTR_KEY\") | .value" | tr -d '"'
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

# ========================================================================
# PREREQUISITE: Check season status and transition to NOMINATION if needed
# ========================================================================
echo "=== PREREQUISITE: Check Season Status ==="
echo ""

SEASON_Q=$($BINARY query season current-season --output json 2>&1)
SEASON_STATUS=$(echo "$SEASON_Q" | jq -r '.status // "unknown"')
SEASON_NUMBER=$(echo "$SEASON_Q" | jq -r '.number // "0"')
echo "  Current season: #$SEASON_NUMBER"
echo "  Status: $SEASON_STATUS"

# Nominations require SEASON_STATUS_NOMINATION phase (status=5)
# Status 5 = NOMINATION, status 1 = ACTIVE. The nomination window opens automatically
# in BeginBlocker at (season.end_block - nomination_window_epochs * epoch_blocks).
# We can't force the transition via a msg — wait for the chain to reach that block.
if [ "$SEASON_STATUS" != "5" ] && [ "$SEASON_STATUS" != "SEASON_STATUS_NOMINATION" ]; then
    echo ""
    echo "  Season is not in NOMINATION phase (status: $SEASON_STATUS)."
    echo "  Waiting for nomination window to open via BeginBlocker..."

    # Compute nomination start block
    PARAMS_Q=$($BINARY query season params --output json 2>&1)
    NOM_WIN_EPOCHS=$(echo "$PARAMS_Q" | jq -r '.params.nomination_window_epochs // "5"')
    EPOCH_BLOCKS=$(echo "$PARAMS_Q" | jq -r '.params.epoch_blocks // "100"')
    END_BLOCK=$(echo "$SEASON_Q" | jq -r '.end_block // "1001"')
    NOM_START_BLOCK=$((END_BLOCK - NOM_WIN_EPOCHS * EPOCH_BLOCKS))
    CURRENT_BLOCK=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height // "0"')
    echo "  Nomination start block: $NOM_START_BLOCK, current: $CURRENT_BLOCK, end: $END_BLOCK"

    if [ "$CURRENT_BLOCK" -ge "$END_BLOCK" ] 2>/dev/null; then
        echo "  Current block is past end_block — season already transitioning; aborting nomination tests."
    else
        WAIT_TARGET=$((NOM_START_BLOCK + 2))
        if [ "$CURRENT_BLOCK" -lt "$WAIT_TARGET" ] 2>/dev/null; then
            WAIT_BLOCKS=$((WAIT_TARGET - CURRENT_BLOCK))
            echo "  Waiting ~${WAIT_BLOCKS} blocks (${WAIT_BLOCKS}s) for nomination window..."
            WAITED=0
            MAX_WAIT=600
            while [ $WAITED -lt $MAX_WAIT ]; do
                CURRENT_BLOCK=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height // "0"')
                if [ "$CURRENT_BLOCK" -ge "$WAIT_TARGET" ] 2>/dev/null; then
                    break
                fi
                sleep 5
                WAITED=$((WAITED + 5))
            done
        fi
        SEASON_Q=$($BINARY query season current-season --output json 2>&1)
        SEASON_STATUS=$(echo "$SEASON_Q" | jq -r '.status // "unknown"')
        echo "  Season status after wait: $SEASON_STATUS (block $CURRENT_BLOCK)"
        if [ "$SEASON_STATUS" != "5" ] && [ "$SEASON_STATUS" != "SEASON_STATUS_NOMINATION" ]; then
            echo "  Season still not in NOMINATION phase; nomination tests will fail."
        fi
    fi
fi

echo ""

# We need a blog post to reference in nominations
echo "=== PREREQUISITE: Create blog posts for content references ==="
echo ""

TX_RES=$($BINARY tx blog create-post \
    "Outstanding Community Contribution" \
    "This blog post documents a significant community contribution worthy of retroactive reward." \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

BLOG_POST_ID=""
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    BLOG_POST_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
    echo "  Blog post created: ID=$BLOG_POST_ID"
else
    echo "  Failed to create blog post"
fi

TX_RES=$($BINARY tx blog create-post \
    "Second Contribution" \
    "Another contribution for nomination testing." \
    --from bob \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

BLOG_POST_2_ID=""
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    BLOG_POST_2_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
    echo "  Blog post 2 created: ID=$BLOG_POST_2_ID"
else
    echo "  Failed to create blog post 2"
fi

echo ""
echo "=== NOMINATION TESTS ==="
echo ""

# ========================================================================
# TEST 1: Create nomination with blog post reference (happy path)
# ========================================================================
echo "--- TEST 1: Create nomination (happy path) ---"

if [ -n "$BLOG_POST_ID" ]; then
    TX_RES=$($BINARY tx season nominate \
        "blog/post/$BLOG_POST_ID" \
        "This contribution significantly improved community engagement and should be retroactively rewarded." \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        NOMINATION_ID=$(extract_event_value "$TX_RESULT" "nomination_created" "nomination_id")
        echo "  Nomination created: ID=$NOMINATION_ID"
        record_result "Create nomination (happy path)" "PASS"
    else
        echo "  Failed to create nomination"
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Create nomination (happy path)" "FAIL"
        NOMINATION_ID=""
    fi
else
    echo "  Skipped (no blog post ID)"
    record_result "Create nomination (happy path)" "FAIL"
    NOMINATION_ID=""
fi

# ========================================================================
# TEST 2: Query get-nomination by ID
# ========================================================================
echo "--- TEST 2: Query get-nomination by ID ---"

if [ -n "$NOMINATION_ID" ]; then
    NOM_Q=$($BINARY query season get-nomination $NOMINATION_ID --output json 2>&1)
    NOM_NOMINATOR=$(echo "$NOM_Q" | jq -r '.nomination.nominator // ""')
    NOM_CONTENT_REF=$(echo "$NOM_Q" | jq -r '.nomination.content_ref // ""')
    NOM_SEASON=$(echo "$NOM_Q" | jq -r '.nomination.season // ""')

    if [ "$NOM_CONTENT_REF" == "blog/post/$BLOG_POST_ID" ] && [ -n "$NOM_NOMINATOR" ]; then
        echo "  Nomination found: nominator=${NOM_NOMINATOR:0:20}..., content_ref=$NOM_CONTENT_REF, season=$NOM_SEASON"
        record_result "Query get-nomination by ID" "PASS"
    else
        echo "  Unexpected nomination data"
        echo "  Response: $NOM_Q"
        record_result "Query get-nomination by ID" "FAIL"
    fi
else
    echo "  Skipped (no nomination ID)"
    record_result "Query get-nomination by ID" "FAIL"
fi

# ========================================================================
# TEST 3: Query list-nominations
# ========================================================================
echo "--- TEST 3: Query list-nominations ---"

NOM_LIST=$($BINARY query season list-nominations --output json 2>&1)
NOM_COUNT=$(echo "$NOM_LIST" | jq -r '.nominations | length' 2>/dev/null || echo "0")

if [ "$NOM_COUNT" -ge 1 ]; then
    echo "  Nominations found: $NOM_COUNT"
    record_result "Query list-nominations" "PASS"
else
    echo "  Expected >= 1 nominations, got: $NOM_COUNT"
    echo "  Response: $NOM_LIST"
    record_result "Query list-nominations" "FAIL"
fi

# ========================================================================
# TEST 4: Query list-nominations-by-creator
# ========================================================================
echo "--- TEST 4: Query list-nominations-by-creator ---"

NOM_BY_CREATOR=$($BINARY query season list-nominations-by-creator $ALICE_ADDR --output json 2>&1)
CREATOR_NOM_COUNT=$(echo "$NOM_BY_CREATOR" | jq -r '.nominations | length' 2>/dev/null || echo "0")

if [ "$CREATOR_NOM_COUNT" -ge 1 ]; then
    echo "  Nominations by Alice: $CREATOR_NOM_COUNT"
    record_result "Query list-nominations-by-creator" "PASS"
else
    echo "  Expected >= 1 nominations by Alice, got: $CREATOR_NOM_COUNT"
    record_result "Query list-nominations-by-creator" "FAIL"
fi

# ========================================================================
# TEST 5: Fail — nominate with invalid content reference
# ========================================================================
echo "--- TEST 5: Fail — invalid content reference ---"

TX_RES=$($BINARY tx season nominate \
    "invalid/reference" \
    "Bad content ref" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    echo "  Correctly rejected: invalid content reference"
    record_result "Invalid content reference rejected" "PASS"
else
    echo "  Should have been rejected"
    record_result "Invalid content reference rejected" "FAIL"
fi

# ========================================================================
# TEST 6: Fail — nominate same content twice in same season
# ========================================================================
echo "--- TEST 6: Fail — nominate same content twice ---"

if [ -n "$BLOG_POST_ID" ]; then
    TX_RES=$($BINARY tx season nominate \
        "blog/post/$BLOG_POST_ID" \
        "Duplicate nomination attempt" \
        --from bob \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: already nominated"
        record_result "Duplicate nomination rejected" "PASS"
    else
        echo "  Should have been rejected"
        record_result "Duplicate nomination rejected" "FAIL"
    fi
else
    echo "  Skipped (no blog post ID)"
    record_result "Duplicate nomination rejected" "FAIL"
fi

# ========================================================================
# TEST 7: Create second nomination with different content (for staking)
# ========================================================================
echo "--- TEST 7: Create second nomination (different content) ---"

NOMINATION_2_ID=""
if [ -n "$BLOG_POST_2_ID" ]; then
    TX_RES=$($BINARY tx season nominate \
        "blog/post/$BLOG_POST_2_ID" \
        "This second contribution also deserves retroactive recognition." \
        --from bob \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        NOMINATION_2_ID=$(extract_event_value "$TX_RESULT" "nomination_created" "nomination_id")
        echo "  Second nomination created: ID=$NOMINATION_2_ID"
        record_result "Create second nomination" "PASS"
    else
        echo "  Failed to create second nomination"
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Create second nomination" "FAIL"
    fi
else
    echo "  Skipped (no second blog post)"
    record_result "Create second nomination" "FAIL"
fi

# ========================================================================
# TEST 8: Stake on nomination (happy path)
# ========================================================================
echo "--- TEST 8: Stake on nomination (happy path) ---"

if [ -n "$NOMINATION_ID" ]; then
    # Stake 10 DREAM (10000000 udream) on the first nomination
    TX_RES=$($BINARY tx season stake-nomination \
        $NOMINATION_ID \
        "10000000" \
        --from bob \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        STAKED_AMT=$(extract_event_value "$TX_RESULT" "nomination_staked" "amount")
        TOTAL_STAKED=$(extract_event_value "$TX_RESULT" "nomination_staked" "total_staked")
        echo "  Staked $STAKED_AMT, total_staked=$TOTAL_STAKED"
        record_result "Stake on nomination (happy path)" "PASS"
    else
        echo "  Failed to stake on nomination"
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Stake on nomination (happy path)" "FAIL"
    fi
else
    echo "  Skipped (no nomination ID)"
    record_result "Stake on nomination (happy path)" "FAIL"
fi

# ========================================================================
# TEST 9: Query list-nomination-stakes
# ========================================================================
echo "--- TEST 9: Query list-nomination-stakes ---"

if [ -n "$NOMINATION_ID" ]; then
    STAKES_Q=$($BINARY query season list-nomination-stakes $NOMINATION_ID --output json 2>&1)
    STAKE_COUNT=$(echo "$STAKES_Q" | jq -r '.stakes | length' 2>/dev/null || echo "0")

    if [ "$STAKE_COUNT" -ge 1 ]; then
        echo "  Stakes on nomination #$NOMINATION_ID: $STAKE_COUNT"
        record_result "Query list-nomination-stakes" "PASS"
    else
        echo "  Expected >= 1 stakes, got: $STAKE_COUNT"
        echo "  Response: $STAKES_Q"
        record_result "Query list-nomination-stakes" "FAIL"
    fi
else
    echo "  Skipped"
    record_result "Query list-nomination-stakes" "FAIL"
fi

# ========================================================================
# TEST 10: Verify conviction calculated
# ========================================================================
echo "--- TEST 10: Verify conviction calculated ---"

if [ -n "$NOMINATION_ID" ]; then
    NOM_Q=$($BINARY query season get-nomination $NOMINATION_ID --output json 2>&1)
    CONVICTION=$(echo "$NOM_Q" | jq -r '.nomination.conviction // "0"')
    TOTAL_STAKED=$(echo "$NOM_Q" | jq -r '.nomination.total_staked // "0"')

    # total_staked should be non-zero after staking
    if [ "$TOTAL_STAKED" != "0" ] && [ "$TOTAL_STAKED" != "0.000000000000000000" ]; then
        echo "  total_staked=$TOTAL_STAKED, conviction=$CONVICTION"
        record_result "Conviction calculated" "PASS"
    else
        echo "  Expected non-zero total_staked, got: $TOTAL_STAKED"
        record_result "Conviction calculated" "FAIL"
    fi
else
    echo "  Skipped"
    record_result "Conviction calculated" "FAIL"
fi

# ========================================================================
# TEST 11: Fail — self-stake (nominator cannot stake own nomination)
# ========================================================================
echo "--- TEST 11: Fail — self-stake ---"

if [ -n "$NOMINATION_ID" ]; then
    TX_RES=$($BINARY tx season stake-nomination \
        $NOMINATION_ID \
        "5000000" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: self-stake not allowed"
        record_result "Self-stake rejected" "PASS"
    else
        echo "  Should have been rejected (self-stake)"
        record_result "Self-stake rejected" "FAIL"
    fi
else
    echo "  Skipped"
    record_result "Self-stake rejected" "FAIL"
fi

# ========================================================================
# TEST 12: Fail — double-stake same nomination (ErrNominationStakeExists)
# ========================================================================
echo "--- TEST 12: Fail — double-stake same nomination ---"

if [ -n "$NOMINATION_ID" ]; then
    TX_RES=$($BINARY tx season stake-nomination \
        $NOMINATION_ID \
        "5000000" \
        --from bob \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: already staked on this nomination"
        record_result "Double-stake rejected" "PASS"
    else
        echo "  Should have been rejected (double-stake)"
        record_result "Double-stake rejected" "FAIL"
    fi
else
    echo "  Skipped"
    record_result "Double-stake rejected" "FAIL"
fi

# ========================================================================
# TEST 13: Unstake from nomination (happy path)
# ========================================================================
echo "--- TEST 13: Unstake from nomination ---"

if [ -n "$NOMINATION_ID" ]; then
    TX_RES=$($BINARY tx season unstake-nomination \
        $NOMINATION_ID \
        --from bob \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        UNSTAKED_AMT=$(extract_event_value "$TX_RESULT" "nomination_unstaked" "amount")
        echo "  Unstaked $UNSTAKED_AMT from nomination #$NOMINATION_ID"
        record_result "Unstake from nomination" "PASS"
    else
        echo "  Failed to unstake"
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Unstake from nomination" "FAIL"
    fi
else
    echo "  Skipped"
    record_result "Unstake from nomination" "FAIL"
fi

# ========================================================================
# TEST 14: Fail — unstake when no stake exists
# ========================================================================
echo "--- TEST 14: Fail — unstake when no stake exists ---"

if [ -n "$NOMINATION_ID" ]; then
    # guild_founder already unstaked, so this should fail
    TX_RES=$($BINARY tx season unstake-nomination \
        $NOMINATION_ID \
        --from bob \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: no stake to unstake"
        record_result "Unstake without stake rejected" "PASS"
    else
        echo "  Should have been rejected"
        record_result "Unstake without stake rejected" "FAIL"
    fi
else
    echo "  Skipped"
    record_result "Unstake without stake rejected" "FAIL"
fi

# ========================================================================
# TEST 15: Verify total_staked decreased after unstake
# ========================================================================
echo "--- TEST 15: Verify total_staked decreased after unstake ---"

if [ -n "$NOMINATION_ID" ]; then
    NOM_Q=$($BINARY query season get-nomination $NOMINATION_ID --output json 2>&1)
    TOTAL_STAKED=$(echo "$NOM_Q" | jq -r '.nomination.total_staked // "unknown"')

    if [ "$TOTAL_STAKED" == "0" ] || [ "$TOTAL_STAKED" == "0.000000000000000000" ]; then
        echo "  total_staked=$TOTAL_STAKED (back to zero after unstake)"
        record_result "Total staked decreased after unstake" "PASS"
    else
        echo "  Expected total_staked=0 after unstake, got: $TOTAL_STAKED"
        record_result "Total staked decreased after unstake" "FAIL"
    fi
else
    echo "  Skipped"
    record_result "Total staked decreased after unstake" "FAIL"
fi

# ========================================================================
# SUMMARY
# ========================================================================
echo "============================================"
echo "NOMINATION TEST RESULTS"
echo "============================================"

for i in "${!TEST_NAMES[@]}"; do
    printf "  %-50s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done

echo ""
echo "  Passed: $PASS_COUNT / $((PASS_COUNT + FAIL_COUNT))"
echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME NOMINATION TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL NOMINATION TESTS PASSED <<<"
    exit 0
fi
