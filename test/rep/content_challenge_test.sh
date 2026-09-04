#!/bin/bash

echo "--- TESTING: CONTENT CHALLENGES (AUTHOR BOND, CHALLENGE, RESPONSE) ---"

# ========================================================================
# 0. SETUP
# ========================================================================
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Helper functions
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

    echo "{\"code\": 999, \"raw_log\": \"Transaction $TXHASH not found after $MAX_ATTEMPTS attempts\"}"
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

# Check if test environment is set up
if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo ""
    echo "Test environment not initialized"
    echo "Running setup script..."
    echo ""
    bash "$SCRIPT_DIR/setup_test_accounts.sh"
    if [ $? -ne 0 ]; then
        echo "Setup failed. Please fix errors and try again."
        exit 1
    fi
fi

# Load test environment
source "$SCRIPT_DIR/.test_env"

echo ""
echo "=== TEST ACTORS ==="
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test 2>/dev/null)
echo "Alice (author):       $ALICE_ADDR"
echo "Challenger:           $CHALLENGER_ADDR"
echo "Assignee (other):     $ASSIGNEE_ADDR"
echo ""

# ========================================================================
# PREREQUISITE: Create a blog post with author bond
# ========================================================================
echo "=== PREREQUISITE: Create blog post with author bond ==="
echo ""

# Get params to know min author bond
PARAMS=$($BINARY query rep params --output json 2>&1)
MIN_CHALLENGE_STAKE=$(echo "$PARAMS" | jq -r '.params.min_challenge_stake // "50000000"')
echo "  MinChallengeStake: $MIN_CHALLENGE_STAKE"

# Earlier suite tests (challenge_test, staking_errors) drain & burn the
# challenger's DREAM, leaving them with too little unlocked balance to post the
# 50-DREAM content-challenge stake here. Top up via a 250-DREAM bounty (no cap)
# so this test runs deterministically regardless of upstream test order.
CHALLENGER_BAL_JSON=$($BINARY query rep get-member "$CHALLENGER_ADDR" -o json 2>/dev/null)
CHALLENGER_BAL=$(echo "$CHALLENGER_BAL_JSON" | jq -r '.member.dream_balance // "0"')
CHALLENGER_STAKED=$(echo "$CHALLENGER_BAL_JSON" | jq -r '.member.staked_dream // "0"')
CHALLENGER_AVAILABLE=$((${CHALLENGER_BAL:-0} - ${CHALLENGER_STAKED:-0}))
NEEDED=300000000  # 300 DREAM cushion (50 stake + multiple test rounds + tax)
if [ "$CHALLENGER_AVAILABLE" -lt "$NEEDED" ]; then
    echo "  Topping up challenger (available: $CHALLENGER_AVAILABLE, need: $NEEDED)..."
    $BINARY tx rep transfer-dream "$CHALLENGER_ADDR" 500000000 bounty \
        "content_challenge_test top-up" \
        --from alice --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json > /dev/null 2>&1
    sleep 6
fi

# Create a blog post with an author bond of 50 DREAM (50000000)
AUTHOR_BOND_AMOUNT="50000000"
echo "  Creating blog post with author bond of $AUTHOR_BOND_AMOUNT..."

TX_RES=$($BINARY tx blog create-post \
    "Bonded Content for Challenge Test" \
    "This blog post has an author bond, making it challengeable." \
    --author-bond "$AUTHOR_BOND_AMOUNT" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)

BONDED_POST_ID=""
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    BONDED_POST_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
    echo "  Bonded blog post created: ID=$BONDED_POST_ID"
else
    echo "  Failed to create bonded blog post"
    echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
    echo ""
    echo "  Attempting without author bond (tests will adapt)..."

    TX_RES=$($BINARY tx blog create-post \
        "Unbonded Content for Challenge Test" \
        "No author bond. Challenge should fail." \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        UNBONDED_POST_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
        echo "  Unbonded blog post created: ID=$UNBONDED_POST_ID"
    fi
fi

# Also create a regular post (no bond) for "no author bond" test
TX_RES=$($BINARY tx blog create-post \
    "Post Without Bond" \
    "This post has no author bond." \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)

NO_BOND_POST_ID=""
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    NO_BOND_POST_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
    echo "  No-bond blog post created: ID=$NO_BOND_POST_ID"
fi

echo ""
echo "=== CONTENT CHALLENGE TESTS ==="
echo ""

# ========================================================================
# TEST 1: Query author-bond for bonded content
# ========================================================================
echo "--- TEST 1: Query author-bond for bonded content ---"

if [ -n "$BONDED_POST_ID" ]; then
    # Target type 7 = STAKE_TARGET_BLOG_AUTHOR_BOND
    BOND_Q=$($BINARY query rep author-bond 7 $BONDED_POST_ID --output json 2>&1)
    BOND_AMOUNT=$(echo "$BOND_Q" | jq -r '.bond_amount // "0"')
    BOND_AUTHOR=$(echo "$BOND_Q" | jq -r '.author // ""')

    if [ "$BOND_AMOUNT" != "0" ] && [ "$BOND_AMOUNT" != "null" ] && [ -n "$BOND_AMOUNT" ] && [ -n "$BOND_AUTHOR" ] && [ "$BOND_AUTHOR" != "null" ]; then
        echo "  Author bond found: amount=$BOND_AMOUNT, author=${BOND_AUTHOR:0:20}..."
        record_result "Query author bond" "PASS"
    else
        echo "  No author bond found (amount=$BOND_AMOUNT)"
        echo "  Full response: $BOND_Q"
        record_result "Query author bond" "FAIL"
    fi
else
    echo "  Skipped (no bonded post)"
    record_result "Query author bond" "FAIL"
fi

# ========================================================================
# TEST 1b: Query author-bonds-by-type lists the bonded post
# ========================================================================
echo "--- TEST 1b: Query author-bonds-by-type ---"

if [ -n "$BONDED_POST_ID" ]; then
    # Target type 7 = STAKE_TARGET_BLOG_AUTHOR_BOND
    BONDS_Q=$($BINARY query rep author-bonds-by-type 7 --output json 2>&1)
    # CLI proto-JSON omits zero-default fields, so a bond on post id 0 has no
    # target_id key — default it when matching.
    BONDED_ENTRY=$(echo "$BONDS_Q" | jq -r ".bonds[] | select((.target_id // \"0\")==\"$BONDED_POST_ID\")" 2>/dev/null)
    ENTRY_AMOUNT=$(echo "$BONDED_ENTRY" | jq -r '.amount // "0"' 2>/dev/null)
    # Every returned entry must be a blog author bond
    NON_BOND_ENTRIES=$(echo "$BONDS_Q" | jq -r '[.bonds[] | select(.target_type != "STAKE_TARGET_BLOG_AUTHOR_BOND")] | length' 2>/dev/null)
    # The no-bond post must not appear
    NO_BOND_ENTRY=""
    if [ -n "$NO_BOND_POST_ID" ]; then
        NO_BOND_ENTRY=$(echo "$BONDS_Q" | jq -r ".bonds[] | select((.target_id // \"0\")==\"$NO_BOND_POST_ID\")" 2>/dev/null)
    fi

    if [ -n "$BONDED_ENTRY" ] && [ "$ENTRY_AMOUNT" == "$AUTHOR_BOND_AMOUNT" ] && [ "$NON_BOND_ENTRIES" == "0" ] && [ -z "$NO_BOND_ENTRY" ]; then
        echo "  Bonded post $BONDED_POST_ID listed with amount=$ENTRY_AMOUNT; no stray entries"
        record_result "Query author bonds by type" "PASS"
    else
        echo "  Unexpected listing (entry=$BONDED_ENTRY, amount=$ENTRY_AMOUNT, non_bond=$NON_BOND_ENTRIES, no_bond_entry=$NO_BOND_ENTRY)"
        echo "  Full response: $BONDS_Q"
        record_result "Query author bonds by type" "FAIL"
    fi
else
    echo "  Skipped (no bonded post)"
    record_result "Query author bonds by type" "FAIL"
fi

# Pagination: limit=1 returns at most one bond
BONDS_PAGE_Q=$($BINARY query rep author-bonds-by-type 7 --page-limit 1 --output json 2>&1)
PAGE_LEN=$(echo "$BONDS_PAGE_Q" | jq -r '.bonds | length' 2>/dev/null)
if [ "$PAGE_LEN" == "1" ]; then
    echo "  Pagination limit honored (1 bond returned)"
    record_result "Author bonds by type pagination" "PASS"
else
    echo "  Expected 1 bond with --page-limit 1, got: $PAGE_LEN"
    echo "  Full response: $BONDS_PAGE_Q"
    record_result "Author bonds by type pagination" "FAIL"
fi

# Non-bond target type (0 = INITIATIVE) must be rejected
INVALID_Q=$($BINARY query rep author-bonds-by-type 0 --output json 2>&1)
if echo "$INVALID_Q" | grep -q "author bond type"; then
    echo "  Non-bond target type rejected as expected"
    record_result "Author bonds by type invalid target" "PASS"
else
    echo "  Expected InvalidArgument for target type 0, got: $INVALID_Q"
    record_result "Author bonds by type invalid target" "FAIL"
fi

# ========================================================================
# TEST 2: Challenge bonded content (happy path)
# ========================================================================
echo "--- TEST 2: Challenge bonded content (happy path) ---"

CONTENT_CHALLENGE_ID=""
if [ -n "$BONDED_POST_ID" ]; then
    # target_type 7 = STAKE_TARGET_BLOG_AUTHOR_BOND
    # Stake at least MinChallengeStake
    CHALLENGE_STAKE="$MIN_CHALLENGE_STAKE"

    TX_RES=$($BINARY tx rep challenge-content \
        7 \
        $BONDED_POST_ID \
        "Content quality is below standards" \
        "$CHALLENGE_STAKE" \
        --evidence "https://example.com/evidence1" \
        --from challenger \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        CONTENT_CHALLENGE_ID=$(extract_event_value "$TX_RESULT" "content_challenge_created" "content_challenge_id")
        echo "  Content challenge created: ID=$CONTENT_CHALLENGE_ID"
        record_result "Challenge bonded content (happy path)" "PASS"

        # Immediately respond to challenge before response_deadline expires
        # (deadline = challenge_response_deadline_epochs * epoch_blocks = ~10 blocks)
        echo ""
        echo "--- TEST 2b: Author responds to challenge (happy path, must be immediate) ---"

        TX_RES=$($BINARY tx rep respond-to-content-challenge \
            $CONTENT_CHALLENGE_ID \
            "The content meets all quality standards. Here is my defense with supporting evidence." \
            --evidence "https://example.com/defense1" \
            --from alice \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 50000${BOND_DENOM} \
            -y \
            --output json 2>&1)

        if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
            echo "  Author responded to challenge $CONTENT_CHALLENGE_ID"
            AUTHOR_RESPONDED=true
            record_result "Author responds to challenge" "PASS"
        else
            echo "  Failed to respond"
            echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
            AUTHOR_RESPONDED=false
            record_result "Author responds to challenge" "FAIL"
        fi
    else
        echo "  Failed to challenge content"
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Challenge bonded content (happy path)" "FAIL"
        AUTHOR_RESPONDED=false
    fi
else
    echo "  Skipped (no bonded post)"
    record_result "Challenge bonded content (happy path)" "FAIL"
    AUTHOR_RESPONDED=false
fi

# ========================================================================
# TEST 3: Query get-content-challenge by ID
# ========================================================================
echo "--- TEST 3: Query get-content-challenge by ID ---"

if [ -n "$CONTENT_CHALLENGE_ID" ]; then
    CC_Q=$($BINARY query rep get-content-challenge $CONTENT_CHALLENGE_ID --output json 2>&1)
    # Proto3 omits zero-value enums: CONTENT_CHALLENGE_STATUS_ACTIVE (0) won't appear in JSON
    CC_STATUS=$(echo "$CC_Q" | jq -r '.content_challenge.status // "CONTENT_CHALLENGE_STATUS_ACTIVE"')
    CC_TARGET_TYPE=$(echo "$CC_Q" | jq -r '.content_challenge.target_type // ""')
    CC_CHALLENGER=$(echo "$CC_Q" | jq -r '.content_challenge.challenger // ""')
    CC_ID=$(echo "$CC_Q" | jq -r '.content_challenge.id // ""')

    if [ -n "$CC_ID" ] && [ "$CC_ID" != "null" ] && [ -n "$CC_CHALLENGER" ]; then
        echo "  Challenge found: id=$CC_ID, status=$CC_STATUS, target_type=$CC_TARGET_TYPE, challenger=${CC_CHALLENGER:0:20}..."
        record_result "Query get-content-challenge" "PASS"
    else
        echo "  Challenge not found or unexpected format"
        echo "  Response: $CC_Q"
        record_result "Query get-content-challenge" "FAIL"
    fi
else
    echo "  Skipped (no content challenge)"
    record_result "Query get-content-challenge" "FAIL"
fi

# ========================================================================
# TEST 4: Query list-content-challenge
# ========================================================================
echo "--- TEST 4: Query list-content-challenge ---"

CC_LIST=$($BINARY query rep list-content-challenge --output json 2>&1)
# Proto field is singular "content_challenge" (repeated), not "content_challenges"
CC_COUNT=$(echo "$CC_LIST" | jq -r '.content_challenge | length' 2>/dev/null || echo "0")

if [ "$CC_COUNT" -ge 1 ]; then
    echo "  Content challenges found: $CC_COUNT"
    record_result "Query list-content-challenge" "PASS"
else
    echo "  Expected >= 1 content challenges, got: $CC_COUNT"
    echo "  Response: $CC_LIST"
    record_result "Query list-content-challenge" "FAIL"
fi

# ========================================================================
# TEST 5: Query content-challenges-by-target
# ========================================================================
echo "--- TEST 5: Query content-challenges-by-target ---"

if [ -n "$BONDED_POST_ID" ]; then
    CC_BY_TARGET=$($BINARY query rep content-challenges-by-target 7 $BONDED_POST_ID --output json 2>&1)
    CC_BY_TARGET_ID=$(echo "$CC_BY_TARGET" | jq -r '.content_challenge_id // .content_challenge.id // ""' 2>/dev/null)

    if [ -n "$CC_BY_TARGET_ID" ] && [ "$CC_BY_TARGET_ID" != "null" ] && [ "$CC_BY_TARGET_ID" != "0" ]; then
        echo "  Challenge for target found: ID=$CC_BY_TARGET_ID"
        record_result "Query content-challenges-by-target" "PASS"
    else
        echo "  No challenge found for target"
        echo "  Response: $CC_BY_TARGET"
        record_result "Query content-challenges-by-target" "FAIL"
    fi
else
    echo "  Skipped"
    record_result "Query content-challenges-by-target" "FAIL"
fi

# ========================================================================
# TEST 6: Fail — challenge non-bonded content (ErrNoAuthorBond)
# ========================================================================
echo "--- TEST 6: Fail — challenge non-bonded content ---"

if [ -n "$NO_BOND_POST_ID" ]; then
    TX_RES=$($BINARY tx rep challenge-content \
        7 \
        $NO_BOND_POST_ID \
        "No bond to challenge" \
        "$MIN_CHALLENGE_STAKE" \
        --from challenger \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: no author bond"
        record_result "Challenge non-bonded content rejected" "PASS"
    else
        echo "  Should have been rejected"
        record_result "Challenge non-bonded content rejected" "FAIL"
    fi
else
    echo "  Skipped (no unbonded post)"
    record_result "Challenge non-bonded content rejected" "FAIL"
fi

# ========================================================================
# TEST 7: Fail — challenge own content (ErrCannotChallengeOwnContent)
# ========================================================================
echo "--- TEST 7: Fail — challenge own content ---"

if [ -n "$BONDED_POST_ID" ]; then
    # Alice is the author — she should not be able to challenge her own content
    TX_RES=$($BINARY tx rep challenge-content \
        7 \
        $BONDED_POST_ID \
        "Self-challenge" \
        "$MIN_CHALLENGE_STAKE" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: cannot challenge own content"
        record_result "Challenge own content rejected" "PASS"
    else
        # This may succeed if the active challenge check hits first, or if alice != bond.staker
        echo "  Should have been rejected (or already challenged)"
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Challenge own content rejected" "FAIL"
    fi
else
    echo "  Skipped"
    record_result "Challenge own content rejected" "FAIL"
fi

# ========================================================================
# TEST 8: Fail — duplicate challenge on same content (ErrContentChallengeExists)
# ========================================================================
echo "--- TEST 8: Fail — duplicate challenge on same content ---"

# The one-challenge-per-target slot is held only while a challenge is LIVE
# (ACTIVE or IN_JURY_REVIEW). Challenge #1 was routed to a jury by TEST 2b, and
# on this chain a jury review expires in default_review_period_epochs *
# epoch_blocks = ~10 blocks, so by now the EndBlocker's content-jury timeout
# sweep has resolved it inconclusive and released the slot. That sweep is the
# fix for a permanent deadlock (a jury that never voted used to lock the
# challenger's stake, the author's bond and this slot forever), so the slot NOT
# being held indefinitely is correct behaviour — the duplicate guard has to be
# tested against a challenge that is still live.
CURRENT_CC_STATUS=$($BINARY query rep get-content-challenge "$CONTENT_CHALLENGE_ID" --output json 2>/dev/null \
    | jq -r '.content_challenge.status // ""')
echo "  Challenge #$CONTENT_CHALLENGE_ID status now: ${CURRENT_CC_STATUS:-unknown}"

DUP_TARGET_ID="$BONDED_POST_ID"
if [ "$CURRENT_CC_STATUS" != "CONTENT_CHALLENGE_STATUS_ACTIVE" ] \
   && [ "$CURRENT_CC_STATUS" != "CONTENT_CHALLENGE_STATUS_IN_JURY_REVIEW" ]; then
    echo "  Original challenge is no longer live; opening a fresh one to hold the slot..."
    TX_RES=$($BINARY tx rep challenge-content \
        7 \
        $DUP_TARGET_ID \
        "Fresh challenge to hold the target slot" \
        "$MIN_CHALLENGE_STAKE" \
        --from challenger \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)
    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        echo "  Fresh challenge created; the slot is held again"
    else
        echo "  [WARN] Could not open a fresh challenge: $(echo "$TX_RESULT" | jq -r '.raw_log // empty' | head -c 160)"
    fi
fi

if [ -n "$BONDED_POST_ID" ] && [ -n "$CONTENT_CHALLENGE_ID" ]; then
    TX_RES=$($BINARY tx rep challenge-content \
        7 \
        $BONDED_POST_ID \
        "Duplicate challenge" \
        "$MIN_CHALLENGE_STAKE" \
        --from assignee \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: active challenge already exists"
        record_result "Duplicate challenge rejected" "PASS"
    else
        echo "  Should have been rejected"
        record_result "Duplicate challenge rejected" "FAIL"
    fi
else
    echo "  Skipped"
    record_result "Duplicate challenge rejected" "FAIL"
fi

# ========================================================================
# TEST 9: Fail — challenge with invalid target type (ErrNotAuthorBondType)
# ========================================================================
echo "--- TEST 9: Fail — invalid target type ---"

TX_RES=$($BINARY tx rep challenge-content \
    1 \
    1 \
    "Invalid target type" \
    "$MIN_CHALLENGE_STAKE" \
    --from challenger \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    echo "  Correctly rejected: invalid target type"
    record_result "Invalid target type rejected" "PASS"
else
    echo "  Should have been rejected"
    record_result "Invalid target type rejected" "FAIL"
fi

# ========================================================================
# TEST 10: (Author response moved to TEST 2b — must run immediately after
#           challenge creation before response_deadline expires)
# ========================================================================

# ========================================================================
# TEST 11: Fail — non-author responds (ErrNotContentAuthor)
# ========================================================================
echo "--- TEST 11: Fail — non-author responds ---"

if [ -n "$CONTENT_CHALLENGE_ID" ]; then
    TX_RES=$($BINARY tx rep respond-to-content-challenge \
        $CONTENT_CHALLENGE_ID \
        "I am not the author" \
        --from challenger \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: not the content author"
        record_result "Non-author response rejected" "PASS"
    else
        echo "  Should have been rejected"
        record_result "Non-author response rejected" "FAIL"
    fi
else
    echo "  Skipped (no content challenge)"
    record_result "Non-author response rejected" "FAIL"
fi

# ========================================================================
# TEST 12: Fail — respond to non-active challenge (ErrContentChallengeNotActive)
# ========================================================================
echo "--- TEST 12: Fail — respond to non-active challenge ---"

if [ -n "$CONTENT_CHALLENGE_ID" ] && [ "$AUTHOR_RESPONDED" = true ]; then
    # The challenge was already responded to (moved to IN_JURY_REVIEW),
    # so responding again should fail
    TX_RES=$($BINARY tx rep respond-to-content-challenge \
        $CONTENT_CHALLENGE_ID \
        "Second response attempt" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: challenge not in active status"
        record_result "Respond to non-active challenge rejected" "PASS"
    else
        echo "  Should have been rejected"
        record_result "Respond to non-active challenge rejected" "FAIL"
    fi
else
    echo "  Skipped"
    record_result "Respond to non-active challenge rejected" "FAIL"
fi

# ========================================================================
# TEST 13: Fail — respond to non-existent challenge
# ========================================================================
echo "--- TEST 13: Fail — respond to non-existent challenge ---"

TX_RES=$($BINARY tx rep respond-to-content-challenge \
    999999 \
    "No such challenge" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    echo "  Correctly rejected: challenge not found"
    record_result "Respond to non-existent challenge rejected" "PASS"
else
    echo "  Should have been rejected"
    record_result "Respond to non-existent challenge rejected" "FAIL"
fi

# ========================================================================
# TEST 14: Query content-conviction for bonded content
# ========================================================================
echo "--- TEST 14: Query content-conviction ---"

if [ -n "$BONDED_POST_ID" ]; then
    # Target type 4 = STAKE_TARGET_BLOG_CONTENT for content conviction
    CONV_Q=$($BINARY query rep content-conviction 4 $BONDED_POST_ID --output json 2>&1)

    # This query may return zero conviction if nobody staked on the content itself
    # The important thing is it doesn't error out
    if echo "$CONV_Q" | jq -e '.' > /dev/null 2>&1; then
        CONV_SCORE=$(echo "$CONV_Q" | jq -r '.conviction // .total_conviction // "0"')
        echo "  Content conviction query succeeded: conviction=$CONV_SCORE"
        record_result "Query content-conviction" "PASS"
    else
        echo "  Content conviction query failed"
        echo "  Response: $CONV_Q"
        record_result "Query content-conviction" "FAIL"
    fi
else
    echo "  Skipped"
    record_result "Query content-conviction" "FAIL"
fi

# ========================================================================
# SUMMARY
# ========================================================================
echo "============================================"
echo "CONTENT CHALLENGE TEST RESULTS"
echo "============================================"

for i in "${!TEST_NAMES[@]}"; do
    printf "  %-50s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done

echo ""
echo "  Passed: $PASS_COUNT / $((PASS_COUNT + FAIL_COUNT))"
echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME CONTENT CHALLENGE TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL CONTENT CHALLENGE TESTS PASSED <<<"
    exit 0
fi
