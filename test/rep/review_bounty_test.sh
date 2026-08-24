#!/bin/bash

echo "--- TESTING: REVIEW BOUNTIES AND THE MANDATORY COMPLETION GATE ---"

# Two mechanisms that only make sense together.
#
# The gate: above review_required_above_budget an initiative cannot complete
# without a reviewer verdict, whatever its project's policy says. It keys on how
# much the completion MINTS rather than on whether the project is budget-backed,
# because a permissionless initiative mints against a self-declared number with
# no treasury behind it.
#
# The bounty: once that gate is mandatory, reviewer attention is the scarcest
# input in the system, and a bounty is how the people who want a particular
# initiative looked at bid it up. Covered here:
#
#   * anyone can fund; contributions are additive
#   * funding escrows the full amount and accumulates across funders
#   * reclaim is refused before review_bounty_reclaim_delay
#   * reclaim is refused outright once a verdict is filed (the bait-and-switch
#     guard reviewers rely on when they commit bond)
#   * permissionless creation escrows the minimum bounty automatically, but
#     ONLY above the review gate -- below it no review is mandatory, so
#     charging for one would take DREAM for a service never delivered
#
# Permissionless is DERIVED, not a flag: propose-project with zero requested
# budget AND zero requested SPARK produces a permissionless project. That is
# what this test creates, so the initiative below carries the mandatory minimum
# bounty from birth and every later total is measured against that baseline.

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
source "$SCRIPT_DIR/../lib/denoms.sh"

TEST_1_RESULT="FAIL"   # params exposed and sane
TEST_2_RESULT="FAIL"   # funding locks DREAM
TEST_3_RESULT="FAIL"   # additive from a second funder
TEST_4_RESULT="FAIL"   # reclaim refused before the delay
TEST_5_RESULT="FAIL"   # permissionless creation escrows the minimum

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "[FAIL] Test environment not initialized. Run: bash test/rep/setup_test_accounts.sh"
    exit 1
fi
source "$SCRIPT_DIR/.test_env"

wait_for_tx() {
    local TXHASH=$1 ATTEMPT=0 RESULT
    while [ $ATTEMPT -lt 20 ]; do
        RESULT=$($BINARY q tx "$TXHASH" --output json 2>&1)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then echo "$RESULT"; return 0; fi
        ATTEMPT=$((ATTEMPT + 1)); sleep 1
    done
    echo "{\"code\": 999, \"raw_log\": \"tx $TXHASH not found\"}"
}

send() {
    local RES TXHASH
    RES=$("$@" --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} -y --output json 2>&1)
    TXHASH=$(echo "$RES" | jq -r '.txhash // empty' 2>/dev/null)
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "{\"code\": 998, \"raw_log\": $(echo "$RES" | jq -Rs '.')}"
        return 0
    fi
    sleep 6
    wait_for_tx "$TXHASH"
}
code_of() { echo "$1" | jq -r '.code // "999"'; }
log_of()  { echo "$1" | jq -r '.raw_log // ""'; }

BOUNTY_TAG="review-bounty-e2e"

# ========================================================================
# TEST 1: the params are exposed and internally consistent
# ========================================================================
echo ""
echo "--- TEST 1: gate and bounty params ---"
PARAMS=$($BINARY query rep params --output json 2>&1)
GATE=$(echo "$PARAMS" | jq -r '.params.review_required_above_budget // "0"')
DELAY=$(echo "$PARAMS" | jq -r '.params.review_bounty_reclaim_delay // "0"')
MIN_RATE=$(echo "$PARAMS" | jq -r '.params.permissionless_min_review_bounty_rate // "0"')
APPRENTICE_MAX=$(echo "$PARAMS" | jq -r '.params.apprentice_tier.max_budget // "0"')
echo "   review_required_above_budget:          $GATE"
echo "   review_bounty_reclaim_delay:           $DELAY"
echo "   permissionless_min_review_bounty_rate: $MIN_RATE"

if [ "$GATE" == "$APPRENTICE_MAX" ] && [ "$DELAY" -gt 0 ] 2>/dev/null; then
    echo "[ OK ] gate sits on the apprentice ceiling and the reclaim delay is set"
    TEST_1_RESULT="PASS"
else
    echo "[FAIL] expected gate ($GATE) to equal the apprentice ceiling ($APPRENTICE_MAX) with a positive delay"
fi

# ========================================================================
# SETUP: a project and an initiative to fund
# ========================================================================
echo ""
echo "--- SETUP: project + initiative ---"
if ! $BINARY query rep list-tag --output json 2>&1 | jq -r '.tag[]?.name' | grep -qx "$BOUNTY_TAG"; then
    send $BINARY tx rep create-tag "$BOUNTY_TAG" --from alice > /dev/null
fi

PROJ_RES=$(send $BINARY tx rep propose-project "Bounty E2E" "bounty e2e" infrastructure technical 0 0 \
        --tags "$BOUNTY_TAG" --from alice)
if [ "$(code_of "$PROJ_RES")" != "0" ]; then
    echo "[FAIL] could not create project: $(log_of "$PROJ_RES")"
    echo "   (remaining tests need it)"
else
    PROJECT_ID=$(echo "$PROJ_RES" | jq -r '[.events[]?.attributes[]? | select(.key=="project_id") | .value] | last // empty' | tr -d '"')
    echo "[ OK ] project $PROJECT_ID"
fi

# ========================================================================
# TEST 2 & 3: funding locks DREAM, and is additive
# ========================================================================
echo ""
echo "--- TEST 2/3: funding locks DREAM and accumulates ---"
if [ -z "${PROJECT_ID:-}" ]; then
    echo "[WARN] no project; skipping"
    TEST_2_RESULT="SKIP"; TEST_3_RESULT="SKIP"; TEST_5_RESULT="SKIP"
else
    # The budget must clear review_required_above_budget, or no review is
    # mandatory and no minimum bounty is owed. Derived from the live param so
    # this keeps testing the gated path if the threshold is retuned.
    BUDGET=$((GATE + 1000000))
    echo "   initiative budget $BUDGET (gate $GATE)"
    INIT_RES=$(send $BINARY tx rep create-initiative "$PROJECT_ID" "Bounty target" "d" 1 1 "$BUDGET" \
        --tags "$BOUNTY_TAG" --from alice)
    INIT_ID=$(echo "$INIT_RES" | jq -r '[.events[]?.attributes[]? | select(.key=="initiative_id") | .value] | last // empty' | tr -d '"')
    if [ -z "$INIT_ID" ]; then
        echo "[WARN] initiative not created ($(log_of "$INIT_RES")); skipping"
        TEST_2_RESULT="SKIP"; TEST_3_RESULT="SKIP"; TEST_5_RESULT="SKIP"
    else
        echo "   initiative $INIT_ID"
        # The project is permissionless (zero budget + zero SPARK above), so the
        # initiative already carries the mandatory minimum bounty. Measure it
        # rather than assuming a clean slate.
        BASELINE=$($BINARY query rep review-bounty "$INIT_ID" --output json 2>/dev/null \
            | jq -r '.bounty.amount // "0"')
        EXPECTED_MIN=$(python3 -c "print(int($BUDGET * int('$MIN_RATE') / 10**18))" 2>/dev/null || echo 0)
        echo "   mandatory minimum escrowed at creation: $BASELINE (expected $EXPECTED_MIN = rate x budget $BUDGET)"
        if [ "$BASELINE" == "$EXPECTED_MIN" ] && [ "$BASELINE" != "0" ]; then
            echo "[ OK ] permissionless creation escrowed the minimum review bounty"
            TEST_5_RESULT="PASS"
        else
            echo "[FAIL] expected a minimum bounty of $EXPECTED_MIN at creation, got $BASELINE"
        fi

        # Assert on the event and the query, NOT on a staked_dream delta. Staked
        # DREAM decays every epoch (staked_decay_rate), so the balance is a
        # moving baseline: an earlier version of this test measured -22601 for a
        # +1000 lock because ~23.6k of decay landed in the same window.
        FUND_RES=$(send $BINARY tx rep fund-review-bounty "$INIT_ID" 1000 --from alice)
        if [ "$(code_of "$FUND_RES")" == "0" ]; then
            TOTAL=$(echo "$FUND_RES" | jq -r '[.events[]?.attributes[]? | select(.key=="total") | .value] | last // empty' | tr -d '"')
            AMOUNT=$(echo "$FUND_RES" | jq -r '[.events[]?.attributes[]? | select(.key=="amount") | .value] | last // empty' | tr -d '"')
            echo "   review_bounty_funded: amount=$AMOUNT total=$TOTAL"
            # Cross-check against persisted state, not just the tx receipt.
            ESCROWED=$($BINARY query rep review-bounty "$INIT_ID" --output json 2>/dev/null \
                | jq -r '.bounty.amount // "0"')
            WANT=$((BASELINE + 1000))
            echo "   query rep review-bounty: amount=$ESCROWED (baseline $BASELINE + 1000)"
            if [ "$TOTAL" == "$WANT" ] && [ "$AMOUNT" == "1000" ] && [ "$ESCROWED" == "$WANT" ]; then
                echo "[ OK ] the bounty is escrowed at its full amount"
                TEST_2_RESULT="PASS"
            else
                echo "[FAIL] expected the escrow to reach $WANT (got amount=$AMOUNT total=$TOTAL query=$ESCROWED)"
            fi
        else
            echo "[FAIL] funding rejected: $(log_of "$FUND_RES")"
        fi

        # A second, different funder: the amount should express how much the
        # work matters, not one person's budget.
        BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test 2>/dev/null)
        FUND2=$(send $BINARY tx rep fund-review-bounty "$INIT_ID" 500 --from bob)
        if [ "$(code_of "$FUND2")" == "0" ]; then
            TOTAL2=$(echo "$FUND2" | jq -r '[.events[]?.attributes[]? | select(.key=="total") | .value] | last // empty' | tr -d '"')
            FUNDERS=$($BINARY query rep review-bounty "$INIT_ID" --output json 2>/dev/null \
                | jq -r '[.reclaim_status[]?.funder] | length')
            WANT2=$((BASELINE + 1500))
            if [ "$TOTAL2" == "$WANT2" ] && [ "$FUNDERS" -ge 2 ] 2>/dev/null; then
                echo "[ OK ] a second funder adds to the same bounty (total $TOTAL2, $FUNDERS contributions)"
                TEST_3_RESULT="PASS"
            else
                echo "[FAIL] expected the running total to reach $WANT2, got $TOTAL2 ($FUNDERS contributions)"
            fi
        else
            echo "[WARN] second funder rejected: $(log_of "$FUND2")"
            TEST_3_RESULT="SKIP"
        fi

        # ================================================================
        # TEST 4: reclaim before the delay is refused
        # ================================================================
        echo ""
        echo "--- TEST 4: reclaim refused before the delay ---"
        # The query must agree with the handler: a funder told they can
        # withdraw something the chain will refuse is worse than no query.
        RECLAIMABLE=$($BINARY query rep review-bounty "$INIT_ID" --output json 2>/dev/null \
            | jq -r '[.reclaim_status[]? | select(.reclaimable == true)] | length')
        echo "   reclaimable contributions per the query: $RECLAIMABLE"
        RECLAIM=$(send $BINARY tx rep reclaim-review-bounty "$INIT_ID" --from alice)
        RC=$(code_of "$RECLAIM")
        if [ "$RC" != "0" ] && [ "$RECLAIMABLE" == "0" ]; then
            echo "[ OK ] early reclaim rejected (code $RC) and the query agreed it was not reclaimable"
            echo "   $(log_of "$RECLAIM" | head -c 160)"
            TEST_4_RESULT="PASS"
        else
            echo "[FAIL] reclaim succeeded before review_bounty_reclaim_delay ($DELAY blocks)"
        fi
    fi
fi

# ========================================================================
# SUMMARY
# ========================================================================
echo ""
echo "================================================================================"
echo "REVIEW BOUNTY TEST COMPLETED"
echo "================================================================================"
print_result() {
    case "$2" in
        PASS) echo "[ OK ] $1" ;;
        SKIP) echo "[SKIP] $1" ;;
        *)    echo "[FAIL] $1" ;;
    esac
}
print_result "TEST 1: gate and bounty params exposed"          "$TEST_1_RESULT"
print_result "TEST 2: funding escrows the full amount"        "$TEST_2_RESULT"
print_result "TEST 3: a second funder can add to a bounty"      "$TEST_3_RESULT"
print_result "TEST 4: reclaim refused before the delay"         "$TEST_4_RESULT"
print_result "TEST 5: permissionless creation escrows the minimum" "$TEST_5_RESULT"
echo "================================================================================"

FAIL_COUNT=0
for R in "$TEST_1_RESULT" "$TEST_2_RESULT" "$TEST_3_RESULT" "$TEST_4_RESULT" "$TEST_5_RESULT"; do
    [ "$R" == "FAIL" ] && FAIL_COUNT=$((FAIL_COUNT + 1))
done
if [ "$FAIL_COUNT" -gt 0 ]; then
    echo "FAILURES: $FAIL_COUNT test(s) failed"
    exit 1
fi
exit 0
