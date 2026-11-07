#!/bin/bash

echo "--- TESTING: CHALLENGE & JURY RESOLUTION FLOW ---"

# ========================================================================
# 0. SETUP
# ========================================================================
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Counter for the script-level result. Each TEST_n_RESULT is set to PASS,
# FAIL, or SKIP. The summary at the bottom exits non-zero on any FAIL so
# the parent runner records this script as failing.
TEST_1_RESULT="PASS"
TEST_2_RESULT="PASS"
TEST_3_RESULT="PASS"
TEST_4_RESULT="PASS"
TEST_5_RESULT="PASS"

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

extract_event_value() {
    local TX_RESULT=$1
    local EVENT_TYPE=$2
    local ATTR_KEY=$3

    echo "$TX_RESULT" | jq -r ".events[] | select(.type==\"$EVENT_TYPE\") | .attributes[] | select(.key==\"$ATTR_KEY\") | .value" | tr -d '"'
}

check_tx_success() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        return 1
    fi
    return 0
}

# Check if test environment is set up
if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo ""
    echo "[WARN]  Test environment not initialized"
    echo "Running setup script..."
    echo ""
    bash "$SCRIPT_DIR/setup_test_accounts.sh"
    if [ $? -ne 0 ]; then
        echo "[FAIL] Setup failed. Please fix errors and try again."
        exit 1
    fi
fi

# Load test environment
source "$SCRIPT_DIR/.test_env"
source "$SCRIPT_DIR/../lib/jury.sh"

echo ""
echo "=== TEST ACTORS ==="
echo "Challenger:           $CHALLENGER_ADDR"
echo "Anonymous Challenger: $ANON_CHALLENGER_ADDR (NOTE: anon challenge flow now lives in x/shield)"
echo "Juror1:               $JUROR1_ADDR"
echo "Juror2:               $JUROR2_ADDR"
echo "Juror3:               $JUROR3_ADDR"
echo "Expert Witness:       $EXPERT_ADDR"
echo "Assignee:             $ASSIGNEE_ADDR"
echo ""

# Verify test project exists
PROJECT_INFO=$($BINARY query rep get-project $TEST_PROJECT_ID --output json 2>&1)
if echo "$PROJECT_INFO" | grep -q "not found"; then
    echo "[FAIL] Test project #$TEST_PROJECT_ID not found"
    echo "Please run setup_test_accounts.sh first"
    exit 1
fi

PROJECT_ID=$TEST_PROJECT_ID
echo "Using test project: #$PROJECT_ID"
echo ""

# ========================================================================
# Helper: bring an initiative to SUBMITTED status (assigned + work submitted)
# Echoes the new initiative_id on stdout. Returns 0 on success.
# ========================================================================
make_submitted_initiative() {
    local TITLE="$1"
    local DESC="$2"
    local TAGS="$3"   # comma-separated
    local TX_RES TXHASH TX_RESULT INIT_ID
    TX_RES=$($BINARY tx rep create-initiative \
        $PROJECT_ID "$TITLE" "$DESC" "0" "0" "5000" \
        --tags "$TAGS" \
        --from alice --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json)
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    [ -z "$TXHASH" ] || [ "$TXHASH" = "null" ] && return 1
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    check_tx_success "$TX_RESULT" || { echo "$TX_RESULT" | jq -r '.raw_log' >&2; return 1; }
    INIT_ID=$(extract_event_value "$TX_RESULT" "initiative_created" "initiative_id")
    if [ -z "$INIT_ID" ] || [ "$INIT_ID" = "null" ]; then
        INIT_ID=$($BINARY query rep list-initiative --output json 2>&1 | jq -r '.initiative[-1].id')
    fi

    $BINARY tx rep assign-initiative $INIT_ID "$ASSIGNEE_ADDR" \
        --from alice --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y > /dev/null 2>&1
    sleep 6
    $BINARY tx rep submit-initiative-work $INIT_ID \
        "https://github.com/test/deliverable-$INIT_ID" \
        "Deliverable for initiative $INIT_ID" \
        --from assignee --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y > /dev/null 2>&1
    sleep 6
    echo "$INIT_ID"
    return 0
}

# ========================================================================
# SETUP: Transfer DREAM to challenger so it can stake on challenges
# ========================================================================
echo "--- SETUP: Transferring DREAM from alice to challenger ---"
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
TX_RES=$($BINARY tx rep transfer-dream \
    $CHALLENGER_ADDR "100000000" "gift" "funding-for-challenge-tests" \
    --from alice --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000${BOND_DENOM} -y --output json)
TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ ! -z "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        echo "[ OK ] Transferred 100 DREAM to challenger"
    else
        echo "[WARN]  Transfer to challenger failed: $(echo "$TX_RESULT" | jq -r '.raw_log')"
    fi
fi
echo ""

# ========================================================================
# TEST 1: TestAnonymousChallenge — SKIPPED
#
# The anonymous-challenge flow (with ZK proof + nullifier + payout_address
# fields on MsgCreateChallenge) was REMOVED from x/rep. All
# anonymous-action infrastructure now lives in x/shield (`MsgShieldedExec`),
# which provides a unified privacy layer with module-paid gas, a centralised
# nullifier store, and per-domain scoping. The corresponding test coverage
# moves with it.
#
# Keep this stub so the TEST 1 slot is visibly accounted for in the
# summary, and so anyone hunting for "anonymous challenge" finds the
# pointer to its new home.
# ========================================================================
echo "================================================================================"
echo "TEST 1: TestAnonymousChallenge"
echo "================================================================================"
echo "[SKIP] Anonymous challenge flow moved to x/shield (MsgShieldedExec)."
echo "       See docs/x-shield-spec.md and tests under test/shield/ for the new"
echo "       ZK-proof / nullifier / payout-address coverage."
TEST_1_RESULT="SKIP"
echo ""

# ========================================================================
# TEST 2: TestJuryReviewComplete
# Challenge created -> jury selected -> jurors submit votes
# -> verdict tally -> assignee/challenger reputation updated
# ========================================================================
echo "================================================================================"
echo "TEST 2: TestJuryReviewComplete"
echo "================================================================================"
echo "Testing: Full jury review flow with votes and verdict tallying"
echo ""

echo "Step 1: Creating new SUBMITTED initiative for jury test..."
INITIATIVE2_ID=$(make_submitted_initiative \
    "Jury test initiative" \
    "This initiative will go through full jury review" \
    "jury,test,challenge")
if [ -z "$INITIATIVE2_ID" ] || [ "$INITIATIVE2_ID" = "null" ]; then
    echo "[FAIL] Could not create SUBMITTED initiative for jury test"
    TEST_2_RESULT="FAIL"
fi
[ "$TEST_2_RESULT" = "PASS" ] && echo "[ OK ] Initiative #$INITIATIVE2_ID submitted"

echo ""
echo "Step 2: Creating challenge for jury review..."
# create-challenge takes EXACTLY 3 positional args:
#   [initiative-id] [reason] [staked-dream]
# (See proto/sparkdream/rep/v1/tx.proto MsgCreateChallenge and
# x/rep/module/autocli.go.) Earlier copies of this script passed extra
# positional args (is_anonymous, payout_address) and stale --membership-proof
# / --nullifier flags from a pre-x/shield-migration draft of the message —
# those are now permanently rejected by the CLI.
CHALLENGE2_ID=""
if [ "$TEST_2_RESULT" = "PASS" ]; then
    TX_RES=$($BINARY tx rep create-challenge \
        $INITIATIVE2_ID \
        "The deliverable does not meet the stated requirements. Missing API documentation and error handling." \
        "50000000" \
        --evidence "https://github.com/repo/issues/1","https://github.com/repo/issues/2" \
        --from challenger --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json)
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        CHALLENGE2_ID=$(extract_event_value "$TX_RESULT" "challenge_created" "challenge_id")
        echo "[ OK ] Challenge #$CHALLENGE2_ID created"
    else
        echo "[FAIL] Failed to create challenge: $(echo "$TX_RESULT" | jq -r '.raw_log')"
        TEST_2_RESULT="FAIL"
    fi
fi

JURY_REVIEW_ID=""
if [ "$TEST_2_RESULT" = "PASS" ] && [ -n "$CHALLENGE2_ID" ]; then
    echo ""
    echo "Step 3: Assignee responding to challenge (creates jury review)..."
    TX_RES=$($BINARY tx rep respond-to-challenge --gas 500000 \
        $CHALLENGE2_ID \
        "We believe the deliverable meets all requirements." \
        --evidence "https://github.com/repo/README.md","https://github.com/repo/docs/api.md" \
        --from assignee --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json)
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        echo "[ OK ] Assignee responded successfully"
        JURY_REVIEW_ID=$(extract_event_value "$TX_RESULT" "jury_review_created" "jury_review_id")
        if [ -z "$JURY_REVIEW_ID" ] || [ "$JURY_REVIEW_ID" = "null" ]; then
            JURY_REVIEW_ID=$($BINARY query rep list-jury-review --output json 2>&1 \
                | jq -r ".jury_review[] | select(.challenge_id == \"$CHALLENGE2_ID\") | .id" | head -1)
        fi
        echo "   → Jury review #$JURY_REVIEW_ID created"
    else
        ERROR_MSG=$(echo "$TX_RESULT" | jq -r '.raw_log')
        echo "[FAIL] Assignee response failed: $ERROR_MSG"
        if echo "$ERROR_MSG" | grep -qi "insufficient eligible jurors"; then
            echo ""
            echo "   NOTE: Jury creation requires jurors with reputation on the initiative tags."
            echo "         setup_test_accounts.sh's juror reputation seed appears not to have"
            echo "         landed for tags=jury,test,challenge. Either the setup didn't run,"
            echo "         the chain was reinitialized, or min_juror_reputation changed."
            TEST_2_RESULT="FAIL"
        else
            TEST_2_RESULT="FAIL"
        fi
    fi
fi

if [ "$TEST_2_RESULT" = "PASS" ] && [ -n "$JURY_REVIEW_ID" ] && [ "$JURY_REVIEW_ID" != "null" ]; then
    echo ""
    echo "Step 4: Querying jury composition..."
    wait_for_jury_seated "$JURY_REVIEW_ID"
    JURY_DETAIL=$($BINARY query rep get-jury-review $JURY_REVIEW_ID --output json 2>&1)
    echo "$JURY_DETAIL" | jq '{
        id: .jury_review.id,
        challenge_id: .jury_review.challenge_id,
        jurors: .jury_review.jurors,
        required_votes: .jury_review.required_votes,
        verdict: .jury_review.verdict,
        deadline: .jury_review.deadline
    }'

    # Map juror addresses to local key names so we can sign votes.
    declare -A JUROR_NAME_BY_ADDR=(
        ["$JUROR1_ADDR"]="juror1"
        ["$JUROR2_ADDR"]="juror2"
        ["$JUROR3_ADDR"]="juror3"
    )
    JURY_ADDRS=$(echo "$JURY_DETAIL" | jq -r '.jury_review.jurors[]')

    echo ""
    echo "Step 5: Jurors submitting votes..."
    # 1× UPHOLD vote (juror1) and 2× REJECT votes (juror2/juror3) → expect
    # final verdict = VERDICT_REJECT_CHALLENGE. Submitting all three votes
    # back-to-back without sleep so we don't trip the jury deadline; chain
    # confirms them in subsequent blocks.
    #
    # NOTE: AutoCLI's enum flag binding accepts the short kebab-case form
    # (the proto's canonical names without the VERDICT_ prefix). Passing
    # the full constant name (e.g. "VERDICT_UPHOLD_CHALLENGE") fails
    # client-side with "is not a valid value for enum sparkdream.rep.v1.Verdict".
    # The JSON query response below still uses the full constant name.
    declare -A VERDICT_FOR_JUROR=(
        ["juror1"]="uphold-challenge"
        ["juror2"]="reject-challenge"
        ["juror3"]="reject-challenge"
    )
    declare -A REASONING_FOR_JUROR=(
        ["juror1"]="Documentation gaps are real and material."
        ["juror2"]="README covers the documented surface."
        ["juror3"]="Error handling looks reasonable in the linked diff."
    )

    # The deciding vote also runs the tally (juror rewards, no-show
    # accounting, alias-aware seat checks), which is why every vote carries
    # more than the 500k a plain vote would need.
    VOTE_TX_HASHES=()
    for JADDR in $JURY_ADDRS; do
        JNAME="${JUROR_NAME_BY_ADDR[$JADDR]:-}"
        if [ -z "$JNAME" ]; then
            echo "  [WARN]  Jury member $JADDR is not one of juror1/2/3 — skipping vote"
            continue
        fi
        VERDICT="${VERDICT_FOR_JUROR[$JNAME]}"
        REASON="${REASONING_FOR_JUROR[$JNAME]}"
        TX_RES=$($BINARY tx rep submit-juror-vote --gas 800000 \
            $JURY_REVIEW_ID "$VERDICT" "0.9" "$REASON" \
            --from "$JNAME" --chain-id $CHAIN_ID --keyring-backend test \
            --fees 5000${BOND_DENOM} -y --output json 2>&1)
        VTXHASH=$(echo "$TX_RES" | jq -r '.txhash' 2>/dev/null)
        if [ -n "$VTXHASH" ] && [ "$VTXHASH" != "null" ]; then
            VOTE_TX_HASHES+=("$JNAME:$VTXHASH:$VERDICT")
        else
            echo "  [WARN]  $JNAME vote submission produced no txhash"
            echo "      raw: $(echo "$TX_RES" | head -c 300)"
        fi
    done

    # No fixed sleep: wait_for_tx polls each vote, and the jury window is
    # only 10 blocks on the test chain.
    echo "   Confirming votes..."
    VOTE_PASS=0 VOTE_FAIL=0
    for ENTRY in "${VOTE_TX_HASHES[@]}"; do
        IFS=":" read -r JNAME VTXHASH VERDICT <<<"$ENTRY"
        VRES=$(wait_for_tx "$VTXHASH")
        if check_tx_success "$VRES"; then
            echo "   [ OK ] $JNAME voted $VERDICT"
            VOTE_PASS=$((VOTE_PASS + 1))
        else
            echo "   [FAIL] $JNAME vote rejected: $(echo "$VRES" | jq -r '.raw_log')"
            VOTE_FAIL=$((VOTE_FAIL + 1))
        fi
    done

    if [ "$VOTE_FAIL" -gt 0 ] || [ "$VOTE_PASS" -lt 2 ]; then
        echo "[FAIL] Insufficient successful juror votes ($VOTE_PASS pass, $VOTE_FAIL fail)"
        TEST_2_RESULT="FAIL"
    fi

    echo ""
    echo "Step 6: Checking final verdict..."
    FINAL=$($BINARY query rep get-jury-review $JURY_REVIEW_ID --output json 2>&1)
    FINAL_VERDICT=$(echo "$FINAL" | jq -r '.jury_review.verdict')
    echo "   Verdict: $FINAL_VERDICT"
    case "$FINAL_VERDICT" in
        VERDICT_REJECT_CHALLENGE)
            echo "   [ OK ] Jury voted to REJECT challenge (matches 1U/2R tally)"
            ;;
        VERDICT_PENDING|null|"")
            # Verdict is tallied at vote time once required_votes is hit;
            # if it's still pending, something broke in the tally path.
            echo "   [FAIL] Verdict still PENDING after all jurors voted"
            TEST_2_RESULT="FAIL"
            ;;
        *)
            echo "   [FAIL] Unexpected verdict: $FINAL_VERDICT (expected VERDICT_REJECT_CHALLENGE)"
            TEST_2_RESULT="FAIL"
            ;;
    esac

    # Verify the challenge itself transitioned to a terminal status.
    CSTATUS=$($BINARY query rep get-challenge $CHALLENGE2_ID --output json 2>&1 \
        | jq -r '.challenge.status')
    echo "   Challenge #$CHALLENGE2_ID status: $CSTATUS"
    case "$CSTATUS" in
        CHALLENGE_STATUS_REJECTED)
            echo "   [ OK ] Challenge correctly transitioned to REJECTED"
            ;;
        *)
            echo "   [FAIL] Challenge status is $CSTATUS (expected CHALLENGE_STATUS_REJECTED)"
            TEST_2_RESULT="FAIL"
            ;;
    esac
fi

echo ""

# ========================================================================
# TEST 3: TestChallengeAutoUphold
# Challenge created -> assignee fails to respond before deadline -> auto-upheld
# ========================================================================
echo "================================================================================"
echo "TEST 3: TestChallengeAutoUphold"
echo "================================================================================"
echo "Testing: Automatic upholding when assignee fails to respond by deadline"
echo ""

echo "Step 1: Creating new SUBMITTED initiative for auto-uphold test..."
INITIATIVE3_ID=$(make_submitted_initiative \
    "Auto-uphold test initiative" \
    "This initiative's assignee will not respond to challenge" \
    "challenge,test,jury")
if [ -z "$INITIATIVE3_ID" ] || [ "$INITIATIVE3_ID" = "null" ]; then
    echo "[FAIL] Could not create SUBMITTED initiative for auto-uphold test"
    TEST_3_RESULT="FAIL"
fi

echo ""
echo "Step 2: Creating challenge on initiative #$INITIATIVE3_ID..."
CHALLENGE3_ID=""
if [ "$TEST_3_RESULT" = "PASS" ]; then
    TX_RES=$($BINARY tx rep create-challenge \
        $INITIATIVE3_ID \
        "This deliverable is completely broken. The code does not compile." \
        "50000000" \
        --evidence "https://github.com/repo/issues/broken" \
        --from challenger --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json)
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)
    if check_tx_success "$TX_RESULT"; then
        CHALLENGE3_ID=$(extract_event_value "$TX_RESULT" "challenge_created" "challenge_id")
        echo "[ OK ] Challenge #$CHALLENGE3_ID created"
    else
        echo "[FAIL] Failed to create challenge: $(echo "$TX_RESULT" | jq -r '.raw_log')"
        TEST_3_RESULT="FAIL"
    fi
fi

if [ "$TEST_3_RESULT" = "PASS" ] && [ -n "$CHALLENGE3_ID" ]; then
    CHALLENGE3_DETAIL=$($BINARY query rep get-challenge $CHALLENGE3_ID --output json 2>&1)
    RESPONSE_DEADLINE=$(echo "$CHALLENGE3_DETAIL" | jq -r '.challenge.response_deadline')
    CURRENT_BLOCK=$($BINARY status | jq -r '.sync_info.latest_block_height')
    BLOCKS_TO_WAIT=$((RESPONSE_DEADLINE - CURRENT_BLOCK + 5))  # +5 buffer for EndBlocker

    echo ""
    echo "Step 3: Challenge deadline details:"
    echo "   → Challenge ID: $CHALLENGE3_ID"
    echo "   → Response deadline: block $RESPONSE_DEADLINE"
    echo "   → Current block: $CURRENT_BLOCK"
    echo "   → Blocks until deadline: $((RESPONSE_DEADLINE - CURRENT_BLOCK))"
    echo ""
    echo "   Assignee will NOT respond — waiting for auto-uphold..."
    if [ "$BLOCKS_TO_WAIT" -gt 0 ] && [ "$BLOCKS_TO_WAIT" -lt 600 ]; then
        echo "   → Sleeping ~${BLOCKS_TO_WAIT}s for deadline + EndBlocker tick (1s blocks)..."
        sleep "$BLOCKS_TO_WAIT"
    else
        # Deadline already past or unreasonable — still tick once.
        sleep 5
    fi

    echo ""
    echo "Step 4: Verifying auto-uphold..."
    POST_BLOCK=$($BINARY status | jq -r '.sync_info.latest_block_height')
    POST_DETAIL=$($BINARY query rep get-challenge $CHALLENGE3_ID --output json 2>&1)
    CHALLENGE_STATUS=$(echo "$POST_DETAIL" | jq -r '.challenge.status')
    INIT_STATUS=$($BINARY query rep get-initiative $INITIATIVE3_ID --output json 2>&1 \
        | jq -r '.initiative.status')
    echo "   → Current block: $POST_BLOCK"
    echo "   → Challenge status: $CHALLENGE_STATUS"
    echo "   → Initiative #$INITIATIVE3_ID status: $INIT_STATUS"

    if [ "$CHALLENGE_STATUS" = "CHALLENGE_STATUS_UPHELD" ]; then
        echo "   [ OK ] Challenge auto-upheld at deadline"
    else
        echo "   [FAIL] Expected CHALLENGE_STATUS_UPHELD, got $CHALLENGE_STATUS"
        TEST_3_RESULT="FAIL"
    fi
    if [ "$INIT_STATUS" = "INITIATIVE_STATUS_REJECTED" ]; then
        echo "   [ OK ] Initiative correctly set to REJECTED"
    else
        echo "   [FAIL] Expected INITIATIVE_STATUS_REJECTED, got $INIT_STATUS"
        TEST_3_RESULT="FAIL"
    fi
fi

echo ""

# ========================================================================
# TEST 4: THE ASSIGNEE CANNOT CHALLENGE THEIR OWN INITIATIVE
# ========================================================================
# An assignee who challenges their own work can admit fault with an empty
# response, auto-uphold the challenge, and collect the challenger reward for
# failing. The keeper rejects it with ErrSelfChallenge before the stake is
# locked, so the attempt must also leave the assignee's staked DREAM alone.
echo "--- TEST 4: Self-challenge rejected ---"

MIN_CHALLENGE_STAKE=$($BINARY query rep params --output json 2>/dev/null \
    | jq -r '.params.min_challenge_stake // "50000000"')
echo "Step 1: Creating a SUBMITTED initiative assigned to $ASSIGNEE_ADDR..."
INITIATIVE4_ID=$(make_submitted_initiative \
    "Self Challenge Guard" \
    "Work the assignee will try to challenge themselves" \
    "challenge,test")

if [ -z "$INITIATIVE4_ID" ] || [ "$INITIATIVE4_ID" = "null" ]; then
    echo "[FAIL] Could not create SUBMITTED initiative for self-challenge test"
    TEST_4_RESULT="FAIL"
else
    echo "[ OK ] Initiative #$INITIATIVE4_ID submitted"

    # proto3 omits zero-valued fields from CLI JSON, so default to "0".
    STAKED_BEFORE=$($BINARY query rep get-member "$ASSIGNEE_ADDR" --output json 2>/dev/null \
        | jq -r '.member.staked_dream // "0"')

    echo ""
    echo "Step 2: Assignee attempts to challenge their own initiative..."
    TX_RES=$($BINARY tx rep create-challenge \
        $INITIATIVE4_ID \
        "Challenging my own work to collect the reward" \
        "$MIN_CHALLENGE_STAKE" \
        --from assignee --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json 2>&1)
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')

    # The gate lives in the msg handler, so the tx is included in a block with
    # a non-zero code -- poll the DELIVERED result rather than trusting the
    # broadcast response. If the broadcast itself was refused, fall back to it.
    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
    else
        TX_RESULT="$TX_RES"
    fi
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""' 2>/dev/null)
    FULL_ERR="$TX_RES $RAW_LOG"

    if check_tx_success "$TX_RESULT"; then
        echo "[FAIL] Self-challenge was accepted; it must be rejected"
        TEST_4_RESULT="FAIL"
    elif echo "$FULL_ERR" | grep -qi "challenge their own initiative"; then
        echo "[ OK ] Correctly rejected: assignee cannot challenge their own initiative"
    else
        echo "[FAIL] Rejected, but not by the self-challenge gate: ${RAW_LOG:0:200}"
        TEST_4_RESULT="FAIL"
    fi

    echo ""
    echo "Step 3: Initiative stays SUBMITTED and no stake was locked..."
    INIT4_STATUS=$($BINARY query rep get-initiative $INITIATIVE4_ID --output json 2>/dev/null \
        | jq -r '.initiative.status')
    if [ "$INIT4_STATUS" = "INITIATIVE_STATUS_SUBMITTED" ]; then
        echo "[ OK ] Initiative still SUBMITTED (never flipped to CHALLENGED)"
    else
        echo "[FAIL] Expected INITIATIVE_STATUS_SUBMITTED, got $INIT4_STATUS"
        TEST_4_RESULT="FAIL"
    fi

    STAKED_AFTER=$($BINARY query rep get-member "$ASSIGNEE_ADDR" --output json 2>/dev/null \
        | jq -r '.member.staked_dream // "0"')
    # A challenge lock would RAISE staked_dream. It can legitimately FALL in
    # between the two reads: the per-epoch staked decay (decayStakes) shrinks
    # it when an epoch boundary lands between them. So assert "no increase",
    # not exact equality.
    if [ "$STAKED_AFTER" -le "$STAKED_BEFORE" ] 2>/dev/null; then
        echo "[ OK ] Staked DREAM not increased ($STAKED_BEFORE -> $STAKED_AFTER) - gate ran before the lock"
    else
        echo "[FAIL] Staked DREAM increased: $STAKED_BEFORE -> $STAKED_AFTER"
        TEST_4_RESULT="FAIL"
    fi

    echo ""
    echo "Step 4: Control - a different member may still challenge the same work..."
    TX_RES=$($BINARY tx rep create-challenge \
        $INITIATIVE4_ID \
        "The deliverable does not meet the stated requirements." \
        "$MIN_CHALLENGE_STAKE" \
        --from challenger --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json 2>&1)
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')
    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        if check_tx_success "$TX_RESULT"; then
            echo "[ OK ] Third-party challenge accepted - the gate is assignee-only"
        else
            echo "[WARN] Control challenge failed: $(echo "$TX_RESULT" | jq -r '.raw_log' | head -c 200)"
            echo "       (challenger DREAM is shared across suites and may be spent)"
        fi
    else
        echo "[WARN] Control challenge could not be broadcast"
    fi
fi

echo ""
# ========================================================================
# TEST 5: Expert witnesses
# A seated juror calls an expert on a pending initiative jury review, a
# second juror who accepted jury duty endorses the call (the inviter cannot,
# and the expert cannot testify before it), the expert testifies, and the
# review's verdict pays the EXPERT_TESTIMONY interim the invitation opened
# (testifying alone pays nothing). It runs
# on its own challenge so its block rounds never share TEST 2's jury window
# (one review epoch, 10 blocks on the test chain) with the votes.
# ========================================================================
echo "================================================================================"
echo "TEST 5: TestExpertWitness"
echo "================================================================================"

echo "Step 1: Funding the challenger and creating a SUBMITTED initiative..."
TX_RES=$($BINARY tx rep transfer-dream \
    $CHALLENGER_ADDR "50000000" "gift" "funding-for-expert-witness-test" \
    --from alice --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000${BOND_DENOM} -y --output json 2>&1)
TX_RESULT=$(wait_for_tx "$(echo "$TX_RES" | jq -r '.txhash')")
check_tx_success "$TX_RESULT" || echo "[WARN] Top-up to challenger failed: $(echo "$TX_RESULT" | jq -r '.raw_log' | head -c 200)"

INITIATIVE5_ID=$(make_submitted_initiative \
    "Expert witness initiative" \
    "This initiative's jury calls an expert witness" \
    "jury,test,challenge")
if [ -z "$INITIATIVE5_ID" ] || [ "$INITIATIVE5_ID" = "null" ]; then
    echo "[FAIL] Could not create SUBMITTED initiative for expert witness test"
    TEST_5_RESULT="FAIL"
else
    echo "[ OK ] Initiative #$INITIATIVE5_ID submitted"
fi

CHALLENGE5_ID=""
if [ "$TEST_5_RESULT" = "PASS" ]; then
    TX_RES=$($BINARY tx rep create-challenge \
        $INITIATIVE5_ID \
        "The deliverable needs a specialist's assessment." \
        "50000000" \
        --from challenger --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json 2>&1)
    TX_RESULT=$(wait_for_tx "$(echo "$TX_RES" | jq -r '.txhash')")
    if check_tx_success "$TX_RESULT"; then
        CHALLENGE5_ID=$(extract_event_value "$TX_RESULT" "challenge_created" "challenge_id")
        echo "[ OK ] Challenge #$CHALLENGE5_ID created"
    else
        echo "[FAIL] Failed to create challenge: $(echo "$TX_RESULT" | jq -r '.raw_log' | head -c 300) $(echo "$TX_RES" | head -c 200)"
        TEST_5_RESULT="FAIL"
    fi
fi

# Everything that does not depend on the review is read before the jury
# window opens: the expert pick and their balance.
BOB_KEY_ADDR=$($BINARY keys show bob -a --keyring-backend test)
declare -A JUROR_NAME_BY_ADDR=(
    ["$JUROR1_ADDR"]="juror1"
    ["$JUROR2_ADDR"]="juror2"
    ["$JUROR3_ADDR"]="juror3"
)
# An eligible expert is an active ESTABLISHED+ member with positive
# reputation in one of the initiative's tags who is not a party (challenger,
# assignee, project creator alice). config.yml seeds bob with "jury"
# reputation below min_juror_reputation, so bob is never seated as a juror.
EXPERT_KEY="" EXPERT_PICK_ADDR=""
for CAND in bob expert; do
    CADDR=$($BINARY keys show $CAND -a --keyring-backend test 2>/dev/null)
    [ -z "$CADDR" ] && continue
    CMEM=$($BINARY query rep get-member $CADDR --output json 2>/dev/null)
    CTRUST=$(echo "$CMEM" | jq -r '.member.trust_level // "TRUST_LEVEL_NEW"')
    # MEMBER_STATUS_ACTIVE is enum 0, omitted from JSON.
    CSTATUS=$(echo "$CMEM" | jq -r '.member.status // "MEMBER_STATUS_ACTIVE"')
    CREP=$(echo "$CMEM" | jq -r '[.member.reputation_scores // {} | to_entries[] | select(.key=="jury" or .key=="test" or .key=="challenge") | .value | tonumber] | add // 0')
    case "$CTRUST" in
        TRUST_LEVEL_ESTABLISHED|TRUST_LEVEL_TRUSTED|TRUST_LEVEL_CORE) ;;
        *) continue ;;
    esac
    if [ "$CSTATUS" = "MEMBER_STATUS_ACTIVE" ] && python3 -c "import sys; sys.exit(0 if float('$CREP') > 0 else 1)" 2>/dev/null; then
        EXPERT_KEY=$CAND EXPERT_PICK_ADDR=$CADDR
        break
    fi
done
if [ "$TEST_5_RESULT" = "PASS" ] && [ -z "$EXPERT_KEY" ]; then
    echo "[FAIL] No ESTABLISHED+ member with reputation in jury/test/challenge (bob is seeded with it in config.yml)"
    TEST_5_RESULT="FAIL"
fi
BAL_BEFORE=""
[ -n "$EXPERT_PICK_ADDR" ] && BAL_BEFORE=$($BINARY query rep get-member $EXPERT_PICK_ADDR --output json | jq -r '.member.dream_balance // "0"')

# broadcast_invite <from> <expert>: prints "<code>|<txhash>|<raw>".
broadcast_invite() {
    local RES HASH CODE
    RES=$($BINARY tx rep invite-expert-witness $JURY_REVIEW5_ID $2 --gas 600000 \
        --from $1 --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json 2>&1)
    HASH=$(echo "$RES" | jq -r '.txhash // ""' 2>/dev/null)
    CODE=$(echo "$RES" | jq -r '.code // 0' 2>/dev/null)
    echo "${CODE:-x}|${HASH}|$(echo "$RES" | head -c 200 | tr '\n|' '  ')"
}
# settle_invite "<code>|<txhash>|<raw>": the delivered code (the CheckTx code
# if the broadcast was refused).
settle_invite() {
    local CODE HASH
    CODE=${1%%|*}; HASH=$(echo "$1" | cut -d'|' -f2)
    if [ -n "$HASH" ] && [ "$CODE" = "0" ]; then
        CODE=$(wait_for_tx "$HASH" | jq -r '.code')
    fi
    echo "$CODE"
}

JURY_REVIEW5_ID=""
if [ "$TEST_5_RESULT" = "PASS" ]; then
    echo ""
    echo "Step 2: Assignee responds (opens the jury review)..."
    TX_RES=$($BINARY tx rep respond-to-challenge --gas 500000 \
        $CHALLENGE5_ID "The deliverable meets its requirements." \
        --from assignee --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json 2>&1)
    TX_RESULT=$(wait_for_tx "$(echo "$TX_RES" | jq -r '.txhash')")
    if check_tx_success "$TX_RESULT"; then
        JURY_REVIEW5_ID=$(extract_event_value "$TX_RESULT" "jury_review_created" "jury_review_id")
        echo "[ OK ] Jury review #$JURY_REVIEW5_ID opened"
    else
        echo "[FAIL] Assignee response failed: $(echo "$TX_RESULT" | jq -r '.raw_log' | head -c 300)"
        TEST_5_RESULT="FAIL"
    fi
fi

if [ "$TEST_5_RESULT" = "PASS" ] && [ -n "$JURY_REVIEW5_ID" ] && [ "$JURY_REVIEW5_ID" != "null" ]; then
    wait_for_jury_seated "$JURY_REVIEW5_ID"
    JURY_ADDRS=$($BINARY query rep get-jury-review $JURY_REVIEW5_ID --output json | jq -r '.jury_review.jurors[]')
    INVITER="" REFUSER="" REFUSER_ADDR=""
    for JADDR in $JURY_ADDRS; do
        JNAME="${JUROR_NAME_BY_ADDR[$JADDR]:-}"
        [ -z "$JNAME" ] && continue
        if [ -z "$INVITER" ]; then INVITER=$JNAME; elif [ -z "$REFUSER" ]; then REFUSER=$JNAME; REFUSER_ADDR=$JADDR; fi
    done

    echo ""
    echo "Step 3: Invitations (one block: non-juror, ineligible expert, eligible expert)..."
    # Distinct signers, so all three land in the same block.
    R_NONJUROR=$(broadcast_invite challenger $BOB_KEY_ADDR)
    R_INELIGIBLE="" R_INVITE=""
    [ -n "$REFUSER" ] && R_INELIGIBLE=$(broadcast_invite $REFUSER $EXPERT_ADDR)
    [ -n "$INVITER" ] && R_INVITE=$(broadcast_invite $INVITER $EXPERT_PICK_ADDR)

    # Only a seated juror may call an expert: ErrNotSeatedJuror (rep 1708).
    INV_CODE=$(settle_invite "$R_NONJUROR")
    if [ "$INV_CODE" = "1708" ]; then
        echo "[ OK ] Non-juror invitation refused (code 1708)"
    else
        echo "[FAIL] Non-juror invitation expected code 1708, got: $INV_CODE ($(echo "$R_NONJUROR" | cut -d'|' -f3))"
        TEST_5_RESULT="FAIL"
    fi
    # The expert account has no reputation in the initiative's tags:
    # ErrExpertIneligible (rep 2418).
    if [ -n "$R_INELIGIBLE" ]; then
        INV_CODE=$(settle_invite "$R_INELIGIBLE")
        if [ "$INV_CODE" = "2418" ]; then
            echo "[ OK ] Ineligible expert refused (code 2418)"
        else
            echo "[FAIL] Ineligible expert expected code 2418, got: $INV_CODE ($(echo "$R_INELIGIBLE" | cut -d'|' -f3))"
            TEST_5_RESULT="FAIL"
        fi
    else
        echo "[WARN] Fewer than two seated jurors with local keys -- skipping the ineligible-expert check"
    fi

    if [ -z "$INVITER" ]; then
        echo "[FAIL] No seated juror is one of juror1/2/3"
        TEST_5_RESULT="FAIL"
    else
        INV_HASH=$(echo "$R_INVITE" | cut -d'|' -f2)
        INV_TX=$(wait_for_tx "$INV_HASH")
        if [ -n "$INV_HASH" ] && check_tx_success "$INV_TX"; then
            EXPERT_INTERIM=$(extract_event_value "$INV_TX" "expert_witness_invited" "interim_id")
            echo "[ OK ] $INVITER invited $EXPERT_KEY as expert witness (interim #$EXPERT_INTERIM)"

            echo ""
            echo "Step 3b: A second juror endorses the invitation (REP-E-103)..."
            # Testimony before the endorsement is refused: ErrExpertNotEndorsed (rep 2420).
            EARLY_RES=$($BINARY tx rep submit-expert-testimony $JURY_REVIEW5_ID \
                "The deliverable is sound: it meets each stated acceptance criterion as written." \
                "Submitted before any endorsement." \
                --gas 800000 --from $EXPERT_KEY --chain-id $CHAIN_ID --keyring-backend test \
                --fees 5000${BOND_DENOM} -y --output json 2>&1)
            EARLY_CODE=$(echo "$EARLY_RES" | jq -r '.code // 0' 2>/dev/null)
            EARLY_HASH=$(echo "$EARLY_RES" | jq -r '.txhash // ""' 2>/dev/null)
            if [ -n "$EARLY_HASH" ] && [ "$EARLY_CODE" = "0" ]; then
                EARLY_CODE=$(wait_for_tx "$EARLY_HASH" | jq -r '.code')
            fi
            if [ "$EARLY_CODE" = "2420" ]; then
                echo "[ OK ] Testimony before endorsement refused (code 2420)"
            else
                echo "[FAIL] Testimony before endorsement expected code 2420, got: $EARLY_CODE ($(echo "$EARLY_RES" | head -c 200 | tr '\n' ' '))"
                TEST_5_RESULT="FAIL"
            fi
            # The inviter cannot endorse their own invitation: ErrInvalidEndorsement (rep 2421).
            SELF_RES=$($BINARY tx rep endorse-expert-witness $JURY_REVIEW5_ID $EXPERT_PICK_ADDR --gas 400000 \
                --from $INVITER --chain-id $CHAIN_ID --keyring-backend test \
                --fees 5000${BOND_DENOM} -y --output json 2>&1)
            SELF_CODE=$(echo "$SELF_RES" | jq -r '.code // 0' 2>/dev/null)
            SELF_HASH=$(echo "$SELF_RES" | jq -r '.txhash // ""' 2>/dev/null)
            if [ -n "$SELF_HASH" ] && [ "$SELF_CODE" = "0" ]; then
                SELF_CODE=$(wait_for_tx "$SELF_HASH" | jq -r '.code')
            fi
            if [ "$SELF_CODE" = "2421" ]; then
                echo "[ OK ] Inviter's self-endorsement refused (code 2421)"
            else
                echo "[FAIL] Self-endorsement expected code 2421, got: $SELF_CODE ($(echo "$SELF_RES" | head -c 200 | tr '\n' ' '))"
                TEST_5_RESULT="FAIL"
            fi
            if [ -z "$REFUSER" ]; then
                echo "[FAIL] No second seated juror with a local key to endorse the invitation"
                TEST_5_RESULT="FAIL"
            else
                # The endorser must have answered the summons: accept jury duty first.
                ACC_RES=$($BINARY tx rep accept-jury-duty $JURY_REVIEW5_ID --gas 400000 \
                    --from $REFUSER --chain-id $CHAIN_ID --keyring-backend test \
                    --fees 5000${BOND_DENOM} -y --output json 2>&1)
                ACC_TX=$(wait_for_tx "$(echo "$ACC_RES" | jq -r '.txhash // ""')")
                if ! check_tx_success "$ACC_TX"; then
                    echo "[FAIL] $REFUSER could not accept jury duty: $(echo "$ACC_TX" | jq -r '.raw_log' | head -c 200)"
                    TEST_5_RESULT="FAIL"
                fi
                END_RES=$($BINARY tx rep endorse-expert-witness $JURY_REVIEW5_ID $EXPERT_PICK_ADDR --gas 400000 \
                    --from $REFUSER --chain-id $CHAIN_ID --keyring-backend test \
                    --fees 5000${BOND_DENOM} -y --output json 2>&1)
                END_TX=$(wait_for_tx "$(echo "$END_RES" | jq -r '.txhash // ""')")
                if check_tx_success "$END_TX"; then
                    ENDORSERS=$($BINARY query rep get-jury-review $JURY_REVIEW5_ID --output json | jq -r '.jury_review.expert_endorsers // [] | join(",")')
                    if [ "$ENDORSERS" = "$REFUSER_ADDR" ]; then
                        echo "[ OK ] $REFUSER endorsed the invitation (expert_endorsers = $ENDORSERS)"
                    else
                        echo "[FAIL] Review expert_endorsers = '$ENDORSERS', expected $REFUSER_ADDR"
                        TEST_5_RESULT="FAIL"
                    fi
                else
                    echo "[FAIL] Endorsement rejected: $(echo "$END_TX" | jq -r '.raw_log' | head -c 300)"
                    TEST_5_RESULT="FAIL"
                fi
            fi

            echo ""
            echo "Step 4: Expert testifies (paid at the verdict, not now)..."
            # The opinion must be at least MinExpertOpinionLength (64) bytes.
            TEST_RES=$($BINARY tx rep submit-expert-testimony $JURY_REVIEW5_ID \
                "The deliverable is sound: it meets each stated acceptance criterion as written." \
                "The deliverable meets its stated criteria." \
                --gas 800000 --from $EXPERT_KEY --chain-id $CHAIN_ID --keyring-backend test \
                --fees 5000${BOND_DENOM} -y --output json 2>&1)
            TEST_TX=$(wait_for_tx "$(echo "$TEST_RES" | jq -r '.txhash')")
            if check_tx_success "$TEST_TX"; then
                ISTATUS=$($BINARY query rep get-interim $EXPERT_INTERIM --output json 2>/dev/null | jq -r '.interim.status // "INTERIM_STATUS_PENDING"')
                if [ "$ISTATUS" = "INTERIM_STATUS_PENDING" ]; then
                    echo "[ OK ] Testimony recorded, interim still PENDING until the verdict"
                else
                    echo "[FAIL] Testimony settled the interim early: status $ISTATUS"
                    TEST_5_RESULT="FAIL"
                fi
                WITNESSES=$($BINARY query rep get-jury-review $JURY_REVIEW5_ID --output json | jq -r '.jury_review.expert_witnesses // [] | join(",")')
                if [ "$WITNESSES" = "$EXPERT_PICK_ADDR" ]; then
                    echo "[ OK ] Review lists the expert as its only witness"
                else
                    echo "[FAIL] Review expert_witnesses = '$WITNESSES', expected $EXPERT_PICK_ADDR"
                    TEST_5_RESULT="FAIL"
                fi
            else
                echo "[FAIL] Expert testimony rejected: $(echo "$TEST_TX" | jq -r '.raw_log')"
                TEST_5_RESULT="FAIL"
            fi
        else
            echo "[FAIL] Expert invitation rejected: $(echo "$INV_TX" | jq -r '.raw_log') ($(echo "$R_INVITE" | cut -d'|' -f3))"
            TEST_5_RESULT="FAIL"
        fi
    fi

    echo ""
    echo "Step 5: Jurors close the review (best effort)..."
    # Only to spare the jurors a no-show at the deadline tally. The verdict is
    # TEST 2's subject, so a vote that misses the window is a WARN here.
    VOTE5_HASHES=()
    for JADDR in $JURY_ADDRS; do
        JNAME="${JUROR_NAME_BY_ADDR[$JADDR]:-}"
        [ -z "$JNAME" ] && continue
        TX_RES=$($BINARY tx rep submit-juror-vote --gas 800000 \
            $JURY_REVIEW5_ID "reject-challenge" "0.9" "The expert's assessment is persuasive." \
            --from "$JNAME" --chain-id $CHAIN_ID --keyring-backend test \
            --fees 5000${BOND_DENOM} -y --output json 2>&1)
        VOTE5_HASHES+=("$JNAME:$(echo "$TX_RES" | jq -r '.txhash // ""' 2>/dev/null)")
    done
    for ENTRY in "${VOTE5_HASHES[@]}"; do
        IFS=":" read -r JNAME VTXHASH <<<"$ENTRY"
        VRES=$(wait_for_tx "$VTXHASH")
        if check_tx_success "$VRES"; then
            echo "[ OK ] $JNAME voted"
        else
            echo "[WARN] $JNAME vote did not land: $(echo "$VRES" | jq -r '.raw_log' | head -c 160)"
        fi
    done

    echo ""
    echo "Step 6: The verdict settles the expert's interim..."
    VERDICT5=$($BINARY query rep get-jury-review $JURY_REVIEW5_ID --output json 2>/dev/null | jq -r '.jury_review.verdict // "VERDICT_PENDING"')
    if [ -z "$EXPERT_INTERIM" ]; then
        :
    elif [ "$VERDICT5" = "VERDICT_PENDING" ]; then
        echo "[WARN] Review #$JURY_REVIEW5_ID has no verdict yet; the expert is paid when it does"
    else
        ISTATUS=$($BINARY query rep get-interim $EXPERT_INTERIM --output json 2>/dev/null | jq -r '.interim.status')
        BAL_AFTER=$($BINARY query rep get-member $EXPERT_PICK_ADDR --output json | jq -r '.member.dream_balance // "0"')
        if [ "$ISTATUS" = "INTERIM_STATUS_COMPLETED" ] && python3 -c "import sys; sys.exit(0 if int('$BAL_AFTER') > int('$BAL_BEFORE') else 1)"; then
            echo "[ OK ] Verdict $VERDICT5 paid the expert ($BAL_BEFORE -> $BAL_AFTER)"
        elif [ "$ISTATUS" = "INTERIM_STATUS_EXPIRED" ]; then
            echo "[WARN] Verdict $VERDICT5 left the testimony unpaid (season interim cap?)"
        else
            echo "[FAIL] Verdict $VERDICT5 did not settle the expert: interim status $ISTATUS, balance $BAL_BEFORE -> $BAL_AFTER"
            TEST_5_RESULT="FAIL"
        fi
    fi
fi
echo ""


# ========================================================================
# SUMMARY
# ========================================================================
echo "================================================================================"
echo "CHALLENGE & JURY RESOLUTION FLOW TEST COMPLETED"
echo "================================================================================"
echo ""
print_result() {
    local name="$1" rc="$2"
    case "$rc" in
        PASS) echo "[ OK ] $name" ;;
        SKIP) echo "[SKIP] $name" ;;
        FAIL) echo "[FAIL] $name" ;;
        *)    echo "[WARN] $name (rc=$rc)" ;;
    esac
}
print_result "TEST 1: Anonymous Challenge (moved to x/shield)" "$TEST_1_RESULT"
print_result "TEST 2: Jury Review (votes + verdict tally)"     "$TEST_2_RESULT"
print_result "TEST 3: Auto-Uphold (deadline + EndBlocker)"     "$TEST_3_RESULT"
print_result "TEST 4: Self-Challenge Rejected (ErrSelfChallenge)" "$TEST_4_RESULT"
print_result "TEST 5: Expert Witness (invite, endorse, testify, pay)"   "$TEST_5_RESULT"
echo ""
echo "================================================================================"

# Exit non-zero on any FAIL so the parent runner picks it up. SKIP does not
# count as failure (TEST 1 is intentionally skipped).
if [ "$TEST_2_RESULT" = "FAIL" ] || [ "$TEST_3_RESULT" = "FAIL" ] || [ "$TEST_4_RESULT" = "FAIL" ] || [ "$TEST_5_RESULT" = "FAIL" ]; then
    exit 1
fi
exit 0
