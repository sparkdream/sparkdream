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
        $PROJECT_ID "$TITLE" "$DESC" "0" "0" "1" "5000" \
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
    TX_RES=$($BINARY tx rep respond-to-challenge \
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

    VOTE_TX_HASHES=()
    for JADDR in $JURY_ADDRS; do
        JNAME="${JUROR_NAME_BY_ADDR[$JADDR]:-}"
        if [ -z "$JNAME" ]; then
            echo "  [WARN]  Jury member $JADDR is not one of juror1/2/3 — skipping vote"
            continue
        fi
        VERDICT="${VERDICT_FOR_JUROR[$JNAME]}"
        REASON="${REASONING_FOR_JUROR[$JNAME]}"
        TX_RES=$($BINARY tx rep submit-juror-vote \
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

    sleep 8
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
echo ""
echo "================================================================================"

# Exit non-zero on any FAIL so the parent runner picks it up. SKIP does not
# count as failure (TEST 1 is intentionally skipped).
if [ "$TEST_2_RESULT" = "FAIL" ] || [ "$TEST_3_RESULT" = "FAIL" ]; then
    exit 1
fi
exit 0
