#!/bin/bash

echo "--- TESTING: ACCEPTANCE CRITERIA & JURY SEAT LIFECYCLE ---"

# Covers the chain surface added alongside the jury-participation rework, none
# of which any other script exercises over the wire:
#
#   * acceptance_criteria declared at initiative creation (--acceptance-criteria)
#   * criteria_id cited on a challenge (--criteria-id), valid and bogus
#   * a non-empty deliverable_uri being required at submission
#   * jury-reviews-by-juror, the query a summoned juror uses to find their seat
#   * accept-jury-duty / decline-jury-duty, including the unauthorized case
#
# Deliberately builds its own initiative and challenge rather than reusing
# challenge_test.sh's: declining a seat vacates it and triggers a redraw, which
# would change the juror roster that script's voting steps depend on.

# ========================================================================
# 0. SETUP
# ========================================================================
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
source "$SCRIPT_DIR/../lib/denoms.sh"

TEST_1_RESULT="FAIL"   # criteria declared and stored
TEST_2_RESULT="FAIL"   # empty deliverable rejected
TEST_3_RESULT="FAIL"   # challenge citing an unknown criterion is rejected
TEST_4_RESULT="FAIL"   # challenge cites a valid criterion, jury seated
TEST_5_RESULT="FAIL"   # jury-reviews-by-juror finds the seat
TEST_6_RESULT="FAIL"   # accept-jury-duty
TEST_7_RESULT="FAIL"   # decline-jury-duty
TEST_8_RESULT="FAIL"   # accept by a non-juror is rejected

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
    local CODE=$(echo "$1" | jq -r '.code')
    [ "$CODE" == "0" ]
}

extract_event_value() {
    echo "$1" | jq -r ".events[] | select(.type==\"$2\") | .attributes[] | select(.key==\"$3\") | .value" | tr -d '"'
}

# Broadcast, wait, and echo the delivered tx result.
send_tx() {
    local TX_RES TXHASH
    TX_RES=$("$@" --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json 2>&1)
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        # Rejected at CheckTx — surface it in the same shape as a delivered tx
        # so callers have one thing to parse.
        echo "{\"code\": 998, \"raw_log\": $(echo "$TX_RES" | jq -Rs '.')}"
        return 0
    fi
    sleep 6
    wait_for_tx "$TXHASH"
}

# Broadcast without waiting, echoing just the txhash. Needed where several
# transactions must land inside a short deadline window: waiting on each in turn
# costs more blocks than the window has.
broadcast_tx() {
    "$@" --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json 2>&1 | jq -r '.txhash // empty'
}

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "[FAIL] Test environment not initialized. Run: bash test/rep/setup_test_accounts.sh"
    exit 1
fi
source "$SCRIPT_DIR/.test_env"

echo ""
echo "=== TEST ACTORS ==="
echo "Challenger: $CHALLENGER_ADDR"
echo "Assignee:   $ASSIGNEE_ADDR"
echo "Project:    #$TEST_PROJECT_ID"
echo ""

PROJECT_ID=$TEST_PROJECT_ID
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)

# assign_to_free_member assigns $1 to the first candidate that still has room
# under max_active_initiatives_per_member (10), setting ASSIGNEE_KEY and
# ASSIGNEE_ADDR. The suite shares one "assignee" account across every script and
# runs alphabetically, so by the time this one runs that account is routinely at
# the cap — which failed here as an assign error three tests before the
# assertion it broke.
#
# Candidates deliberately exclude anyone holding reputation on $JURY_TAG: the
# assignee is barred from their own jury, and spending a juror on the assignee
# seat shrinks an already-thin eligible pool.
assign_to_free_member() {
    local INIT=$1 NAME ADDR RES
    for NAME in assignee poster2 bounty_creator expert anonymous_challenger juror3; do
        ADDR=$($BINARY keys show "$NAME" -a --keyring-backend test 2>/dev/null) || continue
        [ -z "$ADDR" ] && continue
        RES=$(send_tx $BINARY tx rep assign-initiative "$INIT" "$ADDR" --from alice)
        if check_tx_success "$RES"; then
            ASSIGNEE_KEY="$NAME"; ASSIGNEE_ADDR="$ADDR"
            echo "  assigned to $NAME"
            return 0
        fi
        if ! echo "$RES" | jq -r '.raw_log // ""' | grep -qi "max active initiatives"; then
            echo "  assign-initiative failed: $(echo "$RES" | jq -r '.raw_log')"
            return 1
        fi
    done
    echo "  no candidate has capacity under the active-initiative cap"
    return 1
}

# ------------------------------------------------------------------------
# Pick a tag whose eligible-juror pool is actually populated.
#
# Jurors are drawn by lot from members holding at least min_juror_reputation
# (20) on a tag of the disputed initiative. setup_test_accounts.sh builds that
# reputation two ways: initiative completions on "jury"/"challenge", which is
# slow and routinely does not finish, and EPIC interims on "interim-work" for
# the sentinel/poster/moderator accounts, which reliably does. Use the latter,
# registering the tag first if the registry does not have it — reputation can be
# earned on a tag that was never registered, but an initiative cannot be created
# with one.
# ------------------------------------------------------------------------
# Address -> keyring name, resolved once. `keys show` is a process spawn each
# time, and looking these up inside the seat-lifecycle section below burned more
# of the jury deadline than the transactions did.
declare -A KEY_FOR_ADDR
for NAME in juror1 juror2 juror3 alice challenger assignee expert \
            sentinel1 sentinel2 poster1 poster2 moderator bounty_creator; do
    ADDR=$($BINARY keys show $NAME -a --keyring-backend test 2>/dev/null) || continue
    [ -n "$ADDR" ] && KEY_FOR_ADDR["$ADDR"]="$NAME"
done

JURY_TAG="interim-work"
if ! $BINARY query rep list-tag --output json 2>&1 | jq -r '.tag[]?.name' | grep -qx "$JURY_TAG"; then
    echo "Registering tag '$JURY_TAG' (needed to create an initiative with it)..."
    $BINARY tx rep create-tag "$JURY_TAG" --from alice --chain-id $CHAIN_ID \
        --keyring-backend test --fees 5000${BOND_DENOM} -y --output json > /dev/null 2>&1
    sleep 6
fi

ELIGIBLE=$($BINARY query rep list-member --output json 2>&1 \
    | jq -r --arg t "$JURY_TAG" '[.member[]? | select((.reputation_scores[$t] // "0" | tonumber) >= 20)] | length')
echo "Members with >=20 reputation on '$JURY_TAG': ${ELIGIBLE:-0}"
echo ""

# ========================================================================
# TEST 1: acceptance_criteria are declared at creation and stored
# ========================================================================
echo "--- TEST 1: Acceptance criteria declared at creation ---"

# A definition of done, fixed before any work starts. Two criteria so the
# unknown-id case in TEST 4 is unambiguously about resolution, not emptiness.
#
# --acceptance-criteria is a repeated message flag: pass it once per criterion
# with a single JSON object. A JSON array in one flag is a parse error.
CRITERION_1='{"id":"builds","question":"Does the change build from a clean tree?","type":"CRITERIA_TYPE_BINARY","required":true,"how_to_verify":"run make build"}'
CRITERION_2='{"id":"tested","question":"Is the new behaviour covered by a test?","type":"CRITERIA_TYPE_BINARY","required":true,"how_to_verify":"run make test"}'

TX_RESULT=$(send_tx $BINARY tx rep create-initiative \
    $PROJECT_ID "Criteria initiative" "Declares a definition of done" "0" "0" "5000" \
    --tags "$JURY_TAG" \
    --acceptance-criteria "$CRITERION_1" \
    --acceptance-criteria "$CRITERION_2" \
    --from alice)

if check_tx_success "$TX_RESULT"; then
    INIT_ID=$(extract_event_value "$TX_RESULT" "initiative_created" "initiative_id")
    if [ -z "$INIT_ID" ] || [ "$INIT_ID" == "null" ]; then
        INIT_ID=$($BINARY query rep list-initiative --output json 2>&1 | jq -r '.initiative[-1].id')
    fi
    echo "  Initiative: #$INIT_ID"

    STORED=$($BINARY query rep get-initiative $INIT_ID --output json 2>&1)
    CRIT_COUNT=$(echo "$STORED" | jq -r '.initiative.acceptance_criteria | length // 0')
    FIRST_ID=$(echo "$STORED" | jq -r '.initiative.acceptance_criteria[0].id // ""')
    echo "  Criteria stored: $CRIT_COUNT (first id: $FIRST_ID)"
    if [ "$CRIT_COUNT" == "2" ] && [ "$FIRST_ID" == "builds" ]; then
        TEST_1_RESULT="PASS"
    else
        echo "  Expected 2 criteria with first id 'builds'"
    fi
else
    echo "  create-initiative failed: $(echo "$TX_RESULT" | jq -r '.raw_log')"
fi
echo "  Result: $TEST_1_RESULT"
echo ""

# ========================================================================
# TEST 2: an empty deliverable_uri is rejected at submission
# ========================================================================
echo "--- TEST 2: Empty deliverable rejected ---"

if [ "$TEST_1_RESULT" != "PASS" ]; then
    echo "  Skipped: no initiative from TEST 1"
    TEST_2_RESULT="SKIP"
else
    if ! assign_to_free_member "$INIT_ID"; then
        :
    else
        # Nothing on the happy path reads the deliverable, so an empty URI would
        # otherwise ride through review and challenge windows into a payout.
        TX_RESULT=$(send_tx $BINARY tx rep submit-initiative-work $INIT_ID "" "no deliverable" --from $ASSIGNEE_KEY)
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
        if check_tx_success "$TX_RESULT"; then
            echo "  Expected rejection but submission succeeded"
        elif echo "$RAW_LOG" | grep -qi "deliverable URI is empty"; then
            echo "  Correctly rejected: deliverable URI is empty"
            TEST_2_RESULT="PASS"
        else
            echo "  Rejected but with an unexpected error: $RAW_LOG"
        fi

        # A real deliverable must still be accepted, or TEST 3 has nothing to
        # challenge.
        TX_RESULT=$(send_tx $BINARY tx rep submit-initiative-work $INIT_ID \
            "https://example.test/deliverable-$INIT_ID" "Deliverable" --from $ASSIGNEE_KEY)
        if ! check_tx_success "$TX_RESULT"; then
            echo "  Follow-up submission with a real URI failed: $(echo "$TX_RESULT" | jq -r '.raw_log')"
            TEST_2_RESULT="FAIL"
        fi
    fi
fi
echo "  Result: $TEST_2_RESULT"
echo ""

# ========================================================================
# TEST 3: citing a criterion the initiative never declared is an error
# ========================================================================
echo "--- TEST 3: Unknown criterion rejected ---"

# Runs BEFORE the valid challenge: citing an unknown criterion is rejected
# before the stake is locked and before the initiative changes status, so the
# initiative is still SUBMITTED afterwards and TEST 4 can challenge it. The
# reverse order does not work — a challenged initiative refuses a second
# challenge, which would mask what this is testing.
if [ "$TEST_2_RESULT" != "PASS" ]; then
    echo "  Skipped: no initiative to challenge"
    TEST_3_RESULT="SKIP"
else
    TX_RESULT=$(send_tx $BINARY tx rep create-challenge \
        $INIT_ID "Cites a criterion that does not exist" "50000000" \
        --criteria-id "no-such-criterion" \
        --from challenger)
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
    if check_tx_success "$TX_RESULT"; then
        echo "  Expected rejection but the challenge was created"
    elif echo "$RAW_LOG" | grep -qi "unknown acceptance criterion"; then
        echo "  Correctly rejected: unknown acceptance criterion"
        TEST_3_RESULT="PASS"
    else
        echo "  Rejected but with an unexpected error: $RAW_LOG"
    fi
fi
echo "  Result: $TEST_3_RESULT"
echo ""

# ========================================================================
# TEST 4: a challenge may cite a declared criterion, and seats a jury
# ========================================================================
echo "--- TEST 4: Challenge cites a declared criterion ---"

# challenge_response_deadline_epochs is 1 (x epoch_blocks 10 = ~10 blocks under
# test params), and an unanswered challenge is auto-upheld by the EndBlocker.
# So the response has to follow the challenge back-to-back, with no other
# transaction in between — hence TEST 3 running first and the jury query
# (TEST 5) reading state rather than sending anything.
JURY_REVIEW_ID=""
CHALLENGE_ID=""
if [ "$TEST_2_RESULT" != "PASS" ]; then
    echo "  Skipped: initiative is not in a challengeable state"
    TEST_4_RESULT="SKIP"
else
    TX_RESULT=$(send_tx $BINARY tx rep create-challenge \
        $INIT_ID "The change does not build from a clean tree" "50000000" \
        --criteria-id "builds" \
        --from challenger)

    if check_tx_success "$TX_RESULT"; then
        CHALLENGE_ID=$(extract_event_value "$TX_RESULT" "challenge_created" "challenge_id")
        if [ -z "$CHALLENGE_ID" ] || [ "$CHALLENGE_ID" == "null" ]; then
            CHALLENGE_ID=$($BINARY query rep list-challenge --output json 2>&1 \
                | jq -r ".challenge[] | select(.initiative_id == \"$INIT_ID\") | .id" | tail -1)
        fi

        # Respond immediately — the jury is seated by the response, and the
        # window to send it is short.
        RESPOND_RESULT=$(send_tx $BINARY tx rep respond-to-challenge $CHALLENGE_ID \
            "The tree builds; see the linked CI run" --from $ASSIGNEE_KEY)

        STORED_CRIT=$($BINARY query rep get-challenge $CHALLENGE_ID --output json 2>&1 \
            | jq -r '.challenge.criteria_id // ""')
        echo "  Challenge #$CHALLENGE_ID cites criteria_id: '$STORED_CRIT'"
        if [ "$STORED_CRIT" == "builds" ]; then
            TEST_4_RESULT="PASS"
        else
            echo "  Expected the cited criterion to be stored on the challenge"
        fi

        if check_tx_success "$RESPOND_RESULT"; then
            JURY_REVIEW_ID=$(extract_event_value "$RESPOND_RESULT" "jury_review_created" "jury_review_id")
            if [ -z "$JURY_REVIEW_ID" ] || [ "$JURY_REVIEW_ID" == "null" ]; then
                JURY_REVIEW_ID=$($BINARY query rep list-jury-review --output json 2>&1 \
                    | jq -r ".jury_review[] | select(.challenge_id == \"$CHALLENGE_ID\") | .id" | head -1)
            fi
        else
            echo "  respond-to-challenge failed: $(echo "$RESPOND_RESULT" | jq -r '.raw_log')"
        fi
    else
        echo "  create-challenge failed: $(echo "$TX_RESULT" | jq -r '.raw_log')"
    fi
fi
echo "  Result: $TEST_4_RESULT"
echo ""

# ========================================================================
# TEST 5: a summoned juror can find their own seat
# ========================================================================
echo "--- TEST 5: jury-reviews-by-juror ---"

SEATED=""
if [ -z "$JURY_REVIEW_ID" ] || [ "$JURY_REVIEW_ID" == "null" ]; then
    echo "  Skipped: no jury was seated (the eligible pool on $JURY_TAG may be too small)"
    TEST_5_RESULT="SKIP"
else
    echo "  Jury review: #$JURY_REVIEW_ID"
    SEATED=$($BINARY query rep get-jury-review $JURY_REVIEW_ID --output json 2>&1 \
        | jq -r '.jury_review.jurors[]')
    SEAT_COUNT=$(echo "$SEATED" | grep -c . )
    echo "  Seated jurors: $SEAT_COUNT"

    FIRST_JUROR=$(echo "$SEATED" | head -1)
    # Jury duty pays, but is drawn by lot and arrives without warning. Before
    # this query a juror had no way to ask "am I seated" short of paging every
    # review on the chain.
    BY_JUROR=$($BINARY query rep jury-reviews-by-juror "$FIRST_JUROR" --output json 2>&1)
    FOUND=$(echo "$BY_JUROR" | jq -r "[.jury_review[]? // empty | select(.id == \"$JURY_REVIEW_ID\")] | length")
    PENDING=$($BINARY query rep jury-reviews-by-juror "$FIRST_JUROR" --pending-only --output json 2>&1)
    PENDING_FOUND=$(echo "$PENDING" | jq -r "[.jury_review[]? // empty | select(.id == \"$JURY_REVIEW_ID\")] | length")
    echo "  Found in juror index: $FOUND (pending-only: $PENDING_FOUND)"

    # An address that was never seated must not see it.
    NON_JUROR=$($BINARY query rep jury-reviews-by-juror "$ALICE_ADDR" --output json 2>&1)
    NON_JUROR_FOUND=$(echo "$NON_JUROR" | jq -r "[.jury_review[]? // empty | select(.id == \"$JURY_REVIEW_ID\")] | length")

    if [ "$FOUND" == "1" ] && [ "$PENDING_FOUND" == "1" ] && [ "$NON_JUROR_FOUND" == "0" ]; then
        TEST_5_RESULT="PASS"
    else
        echo "  Expected the seated juror to find the review and a non-juror not to"
        echo "  (non-juror saw: $NON_JUROR_FOUND)"
    fi
fi
echo "  Result: $TEST_5_RESULT"
echo ""

# ========================================================================
# TEST 6 / 7 / 8: accept, decline, and reject a seat that is not yours
# ========================================================================
echo "--- TESTS 6-8: Jury seat lifecycle ---"

# The vote deadline is default_review_period_epochs (1) x epoch_blocks (10) =
# ~10 blocks, after which the review resolves and every seat message is refused
# with "jury review already resolved". Three sequential send_tx calls do not fit
# in that. Broadcast all three first — they have different signers, so there is
# no sequence contention — then collect the results.
if [ -z "$SEATED" ]; then
    echo "  Skipped: no seated jury"
    TEST_6_RESULT="SKIP"
    TEST_7_RESULT="SKIP"
    TEST_8_RESULT="SKIP"
else
    # Re-read the roster immediately before acting. jury_size is now above
    # MinSeatedJurors, so the acceptance sweep genuinely vacates and replaces
    # seats, and a decline refills on the spot — a roster read a few blocks ago
    # can already be stale.
    SEATED=$($BINARY query rep get-jury-review $JURY_REVIEW_ID --output json 2>&1 \
        | jq -r '.jury_review.jurors[]?')
    if [ -z "$SEATED" ]; then
        echo "  Skipped: roster is empty on re-read"
        TEST_6_RESULT="SKIP"; TEST_7_RESULT="SKIP"; TEST_8_RESULT="SKIP"
        SEATED=""
    fi

    ACCEPT_KEY=""
    DECLINE_KEY=""
    for ADDR in $SEATED; do
        NAME="${KEY_FOR_ADDR[$ADDR]}"
        [ -z "$NAME" ] && continue
        if [ -z "$ACCEPT_KEY" ]; then
            ACCEPT_KEY="$NAME"
        elif [ -z "$DECLINE_KEY" ]; then
            DECLINE_KEY="$NAME"
            break
        fi
    done

    # Someone the lot did not draw.
    NON_JUROR_KEY=""
    for NAME in poster2 bounty_creator expert challenger; do
        ADDR=$($BINARY keys show $NAME -a --keyring-backend test 2>/dev/null) || continue
        if ! echo "$SEATED" | grep -q "$ADDR"; then
            NON_JUROR_KEY="$NAME"
            break
        fi
    done

    DECLINE_ADDR=""
    [ -n "$DECLINE_KEY" ] && DECLINE_ADDR=$($BINARY keys show $DECLINE_KEY -a --keyring-backend test)

    echo "  accept as: ${ACCEPT_KEY:-<none>}, decline as: ${DECLINE_KEY:-<none>}, non-juror: ${NON_JUROR_KEY:-<none>}"

    ACCEPT_HASH=""
    DECLINE_HASH=""
    NON_JUROR_HASH=""
    [ -n "$ACCEPT_KEY" ] && ACCEPT_HASH=$(broadcast_tx $BINARY tx rep accept-jury-duty $JURY_REVIEW_ID --from $ACCEPT_KEY)
    [ -n "$DECLINE_KEY" ] && DECLINE_HASH=$(broadcast_tx $BINARY tx rep decline-jury-duty $JURY_REVIEW_ID --from $DECLINE_KEY)
    [ -n "$NON_JUROR_KEY" ] && NON_JUROR_HASH=$(broadcast_tx $BINARY tx rep accept-jury-duty $JURY_REVIEW_ID --from $NON_JUROR_KEY)

    sleep 6

    # --- TEST 6: accepting a seat you hold ---
    if [ -z "$ACCEPT_HASH" ]; then
        echo "  TEST 6 skipped: no seated juror is in the test keyring"
        TEST_6_RESULT="SKIP"
    else
        TX_RESULT=$(wait_for_tx "$ACCEPT_HASH")
        if check_tx_success "$TX_RESULT"; then
            echo "  TEST 6: seat accepted by $ACCEPT_KEY"
            TEST_6_RESULT="PASS"
        else
            echo "  TEST 6: accept-jury-duty failed: $(echo "$TX_RESULT" | jq -r '.raw_log')"
        fi
    fi

    # --- TEST 7: declining is free and releases the seat ---
    # Jurors are conscripted by sortition, so the seat-vacating consequences are
    # only fair if handing the seat back costs nothing. A decline is never
    # recorded as a no-show, and is subtracted from the participation
    # denominator via total_declined.
    if [ -z "$DECLINE_HASH" ]; then
        echo "  TEST 7 skipped: fewer than two seated jurors are in the test keyring"
        TEST_7_RESULT="SKIP"
    else
        TX_RESULT=$(wait_for_tx "$DECLINE_HASH")
        if check_tx_success "$TX_RESULT"; then
            STILL_SEATED=$($BINARY query rep get-jury-review $JURY_REVIEW_ID --output json 2>&1 \
                | jq -r "[.jury_review.jurors[]? | select(. == \"$DECLINE_ADDR\")] | length")
            DECLINED_COUNT=$($BINARY query rep list-jury-participation --output json 2>&1 \
                | jq -r "[.jury_participation[]? | select(.juror == \"$DECLINE_ADDR\")] | .[0].total_declined // \"0\"")
            echo "  TEST 7: $DECLINE_KEY declined; still seated: $STILL_SEATED, total_declined: $DECLINED_COUNT"
            if [ "$STILL_SEATED" == "0" ] && [ "$DECLINED_COUNT" != "0" ]; then
                TEST_7_RESULT="PASS"
            else
                echo "  Expected the seat released and the decline recorded"
            fi
        else
            echo "  TEST 7: decline-jury-duty failed: $(echo "$TX_RESULT" | jq -r '.raw_log')"
        fi
    fi

    # --- TEST 8: a seat you were never drawn for ---
    if [ -z "$NON_JUROR_HASH" ]; then
        echo "  TEST 8 skipped: every candidate is seated on this jury"
        TEST_8_RESULT="SKIP"
    else
        TX_RESULT=$(wait_for_tx "$NON_JUROR_HASH")
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
        if check_tx_success "$TX_RESULT"; then
            echo "  TEST 8: expected rejection but $NON_JUROR_KEY accepted a seat they do not hold"
        elif echo "$RAW_LOG" | grep -qi "not seated on this jury"; then
            echo "  TEST 8: correctly rejected — $NON_JUROR_KEY is not seated"
            TEST_8_RESULT="PASS"
        else
            echo "  TEST 8: rejected but with an unexpected error: $RAW_LOG"
        fi
    fi
fi
echo ""

# ========================================================================
# SUMMARY
# ========================================================================
echo "================================================================================"
echo "ACCEPTANCE CRITERIA & JURY SEAT LIFECYCLE TEST COMPLETED"
echo "================================================================================"
echo ""
print_result() {
    case "$2" in
        PASS) echo "[ OK ] $1" ;;
        SKIP) echo "[SKIP] $1" ;;
        FAIL) echo "[FAIL] $1" ;;
        *)    echo "[WARN] $1 (rc=$2)" ;;
    esac
}
print_result "TEST 1: Acceptance criteria stored at creation" "$TEST_1_RESULT"
print_result "TEST 2: Empty deliverable rejected"             "$TEST_2_RESULT"
print_result "TEST 3: Unknown criterion rejected"             "$TEST_3_RESULT"
print_result "TEST 4: Challenge cites a declared criterion"   "$TEST_4_RESULT"
print_result "TEST 5: jury-reviews-by-juror finds the seat"   "$TEST_5_RESULT"
print_result "TEST 6: accept-jury-duty"                       "$TEST_6_RESULT"
print_result "TEST 7: decline-jury-duty releases the seat"    "$TEST_7_RESULT"
print_result "TEST 8: non-juror cannot accept"                "$TEST_8_RESULT"
echo ""
echo "================================================================================"

FAIL_COUNT=0
for R in "$TEST_1_RESULT" "$TEST_2_RESULT" "$TEST_3_RESULT" "$TEST_4_RESULT" \
         "$TEST_5_RESULT" "$TEST_6_RESULT" "$TEST_7_RESULT" "$TEST_8_RESULT"; do
    [ "$R" == "FAIL" ] && FAIL_COUNT=$((FAIL_COUNT + 1))
done

if [ "$FAIL_COUNT" -gt 0 ]; then
    echo "FAILURES: $FAIL_COUNT test(s) failed"
    exit 1
fi
exit 0
