#!/bin/bash

echo "--- TESTING: INITIATIVE BOUNTIES ---"

# An initiative bounty is DREAM a member escrows against an initiative, paid to
# its assignee if the initiative completes and refunded if it is closed or
# rejected. It replaces the old "bounty" transfer purpose, which was an
# uncapped member-to-member send. Because a completion-released bounty is still
# a route for DREAM between members, it carries the guards covered here:
#
#   * the params are exposed
#   * anyone affiliated with the work (here, the creator) cannot fund it
#   * funding locks the full amount, visible in the query
#   * the total cannot exceed the initiative's budget
#   * reclaim is refused before initiative_bounty_reclaim_delay
#   * a funder cannot then be assigned the work
#   * closing the initiative refunds the funder in full
#
# The payout on completion (taxed, clipped to the assignee's transfer receive
# limits) needs a full conviction cycle and is covered by the keeper unit tests.

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
source "$SCRIPT_DIR/../lib/denoms.sh"

TEST_1_RESULT="FAIL"   # params exposed
TEST_2_RESULT="FAIL"   # affiliated funder refused
TEST_3_RESULT="FAIL"   # funding escrows the full amount
TEST_4_RESULT="FAIL"   # total capped at the budget
TEST_5_RESULT="FAIL"   # reclaim refused before the delay
TEST_6_RESULT="FAIL"   # funder cannot be assigned
TEST_7_RESULT="FAIL"   # close refunds the funder

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

# Broadcast, wait, echo the delivered result. CheckTx rejections are reshaped so
# callers have one thing to parse.
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
event_attr() { echo "$1" | jq -r --arg t "$2" --arg k "$3" \
    '[.events[]? | select(.type==$t) | .attributes[]? | select(.key==$k) | .value] | last // empty' | tr -d '"'; }
bounty_amount() { $BINARY query rep initiative-bounty "$1" --output json 2>/dev/null | jq -r '.bounty.amount // "0"'; }
staked_of() { $BINARY query rep get-member "$1" --output json 2>/dev/null | jq -r '.member.staked_dream // "0"'; }

BOUNTY_TAG="initiative-bounty-e2e"
FUNDER_NAME="challenger"
FUNDER_ADDR="$CHALLENGER_ADDR"
if [ -z "$FUNDER_ADDR" ]; then
    echo "[FAIL] CHALLENGER_ADDR missing from .test_env; the funder must not be alice"
    exit 1
fi

# ========================================================================
# TEST 1: params
# ========================================================================
echo ""
echo "--- TEST 1: initiative bounty params ---"
PARAMS=$($BINARY query rep params --output json 2>&1)
RATIO=$(echo "$PARAMS" | jq -r '.params.initiative_bounty_max_budget_ratio // ""')
PER_FUNDER=$(echo "$PARAMS" | jq -r '.params.max_initiative_bounty_per_funder_epoch // ""')
MIN_CONTRIB=$(echo "$PARAMS" | jq -r '.params.min_initiative_bounty_contribution // ""')
DELAY=$(echo "$PARAMS" | jq -r '.params.initiative_bounty_reclaim_delay // "0"')
echo "   initiative_bounty_max_budget_ratio:     $RATIO"
echo "   max_initiative_bounty_per_funder_epoch: $PER_FUNDER"
echo "   min_initiative_bounty_contribution:     $MIN_CONTRIB"
echo "   initiative_bounty_reclaim_delay:        $DELAY"
if [ -n "$RATIO" ] && [ -n "$PER_FUNDER" ] && [ -n "$MIN_CONTRIB" ] && [ "$DELAY" -gt 0 ] 2>/dev/null; then
    echo "[ OK ] params exposed"
    TEST_1_RESULT="PASS"
else
    echo "[FAIL] initiative bounty params missing"
fi

# ========================================================================
# SETUP: a permissionless project and an initiative below the review gate
# ========================================================================
echo ""
echo "--- SETUP: project + initiative ---"
if ! $BINARY query rep list-tag --output json 2>&1 | jq -r '.tag[]?.name' | grep -qx "$BOUNTY_TAG"; then
    send $BINARY tx rep create-tag "$BOUNTY_TAG" --from alice > /dev/null
fi
PROJ_RES=$(send $BINARY tx rep propose-project "Initiative Bounty E2E" "initiative bounty e2e" infrastructure technical 0 0 \
        --tags "$BOUNTY_TAG" --from alice)
PROJECT_ID=$(echo "$PROJ_RES" | jq -r '[.events[]?.attributes[]? | select(.key=="project_id") | .value] | last // empty' | tr -d '"')
# Below review_required_above_budget, so no mandatory review bounty muddies
# the picture, and small enough that the budget cap is easy to reach.
BUDGET=50000000   # 50 DREAM
if [ -n "$PROJECT_ID" ]; then
    INIT_RES=$(send $BINARY tx rep create-initiative "$PROJECT_ID" "Bounty target" "d" 0 1 "$BUDGET" \
        --tags "$BOUNTY_TAG" --from alice)
    INIT_ID=$(echo "$INIT_RES" | jq -r '[.events[]?.attributes[]? | select(.key=="initiative_id") | .value] | last // empty' | tr -d '"')
fi
if [ -z "${INIT_ID:-}" ]; then
    echo "[FAIL] could not create project/initiative: $(log_of "${INIT_RES:-$PROJ_RES}" | head -c 200)"
    exit 1
fi
echo "[ OK ] project $PROJECT_ID, initiative $INIT_ID (budget $BUDGET)"

# ========================================================================
# TEST 2: the creator cannot fund their own initiative's bounty
# ========================================================================
echo ""
echo "--- TEST 2: affiliated funder refused ---"
R=$(send $BINARY tx rep fund-initiative-bounty "$INIT_ID" 10000000 --from alice)
if [ "$(code_of "$R")" != "0" ] && log_of "$R" | grep -q "cannot fund its bounty"; then
    echo "[ OK ] creator refused: $(log_of "$R" | head -c 160)"
    TEST_2_RESULT="PASS"
else
    echo "[FAIL] creator was not refused (code $(code_of "$R")): $(log_of "$R" | head -c 200)"
fi

# ========================================================================
# TEST 3: funding escrows the full amount
# ========================================================================
echo ""
echo "--- TEST 3: $FUNDER_NAME funds 10 DREAM ---"
FUND=10000000
R=$(send $BINARY tx rep fund-initiative-bounty "$INIT_ID" "$FUND" --from "$FUNDER_NAME")
if [ "$(code_of "$R")" == "0" ]; then
    TOTAL=$(event_attr "$R" initiative_bounty_funded total)
    ESCROWED=$(bounty_amount "$INIT_ID")
    echo "   initiative_bounty_funded total=$TOTAL, query amount=$ESCROWED"
    if [ "$TOTAL" == "$FUND" ] && [ "$ESCROWED" == "$FUND" ]; then
        echo "[ OK ] the bounty is escrowed at its full amount"
        TEST_3_RESULT="PASS"
    else
        echo "[FAIL] expected $FUND escrowed"
    fi
else
    echo "[FAIL] funding rejected: $(log_of "$R" | head -c 200)"
fi

# ========================================================================
# TEST 4: the total cannot exceed the budget
# ========================================================================
echo ""
echo "--- TEST 4: total capped at ratio x budget ---"
OVER=$((BUDGET - FUND + 1000000))   # would take the total 1 DREAM past the budget
R=$(send $BINARY tx rep fund-initiative-bounty "$INIT_ID" "$OVER" --from "$FUNDER_NAME")
if [ "$(code_of "$R")" != "0" ] && log_of "$R" | grep -q "exceeds initiative bounty limit"; then
    echo "[ OK ] over-budget contribution refused"
    TEST_4_RESULT="PASS"
else
    echo "[FAIL] over-budget contribution was not refused (code $(code_of "$R")): $(log_of "$R" | head -c 200)"
fi

# ========================================================================
# TEST 5: reclaim refused before the delay
# ========================================================================
echo ""
echo "--- TEST 5: reclaim refused before the delay ---"
RECLAIMABLE=$($BINARY query rep initiative-bounty "$INIT_ID" --output json 2>/dev/null \
    | jq -r '[.reclaim_status[]? | select(.reclaimable == true)] | length')
R=$(send $BINARY tx rep reclaim-initiative-bounty "$INIT_ID" --from "$FUNDER_NAME")
if [ "$(code_of "$R")" != "0" ] && [ "$RECLAIMABLE" == "0" ]; then
    echo "[ OK ] early reclaim refused and the query agreed"
    TEST_5_RESULT="PASS"
else
    echo "[FAIL] reclaim before the $DELAY-block delay: code $(code_of "$R"), query reclaimable=$RECLAIMABLE"
fi

# ========================================================================
# TEST 6: a funder cannot be assigned the work
# ========================================================================
echo ""
echo "--- TEST 6: funder cannot be assigned ---"
R=$(send $BINARY tx rep assign-initiative "$INIT_ID" "$FUNDER_ADDR" --from alice)
if [ "$(code_of "$R")" != "0" ] && log_of "$R" | grep -q "funder of this initiative's bounty"; then
    echo "[ OK ] assigning the funder refused"
    TEST_6_RESULT="PASS"
else
    echo "[FAIL] funder assignment was not refused (code $(code_of "$R")): $(log_of "$R" | head -c 200)"
fi

# ========================================================================
# TEST 7: closing the initiative refunds the funder in full
# ========================================================================
echo ""
echo "--- TEST 7: close refunds the bounty ---"
R=$(send $BINARY tx rep close-initiative "$INIT_ID" "no longer needed" --from alice)
if [ "$(code_of "$R")" == "0" ]; then
    REFUNDED=$(event_attr "$R" initiative_bounty_refunded amount)
    LEFT=$(bounty_amount "$INIT_ID")
    echo "   initiative_bounty_refunded amount=$REFUNDED, escrow left=$LEFT"
    if [ "$REFUNDED" == "$FUND" ] && [ "$LEFT" == "0" ]; then
        echo "[ OK ] the funder got the whole bounty back, untaxed"
        TEST_7_RESULT="PASS"
    else
        echo "[FAIL] expected a $FUND refund and an empty escrow"
    fi
else
    echo "[FAIL] close rejected: $(log_of "$R" | head -c 200)"
fi

# ========================================================================
# SUMMARY
# ========================================================================
echo ""
echo "================================================================================"
echo "INITIATIVE BOUNTY TEST COMPLETED"
echo "================================================================================"
print_result() {
    case "$2" in
        PASS) echo "[ OK ] $1" ;;
        SKIP) echo "[SKIP] $1" ;;
        *)    echo "[FAIL] $1" ;;
    esac
}
print_result "TEST 1: params exposed"                      "$TEST_1_RESULT"
print_result "TEST 2: affiliated funder refused"           "$TEST_2_RESULT"
print_result "TEST 3: funding escrows the full amount"     "$TEST_3_RESULT"
print_result "TEST 4: total capped at the budget"          "$TEST_4_RESULT"
print_result "TEST 5: reclaim refused before the delay"    "$TEST_5_RESULT"
print_result "TEST 6: funder cannot be assigned the work"  "$TEST_6_RESULT"
print_result "TEST 7: close refunds the funder in full"    "$TEST_7_RESULT"
echo "================================================================================"

FAIL_COUNT=0
for R in "$TEST_1_RESULT" "$TEST_2_RESULT" "$TEST_3_RESULT" "$TEST_4_RESULT" \
         "$TEST_5_RESULT" "$TEST_6_RESULT" "$TEST_7_RESULT"; do
    [ "$R" == "FAIL" ] && FAIL_COUNT=$((FAIL_COUNT + 1))
done
if [ "$FAIL_COUNT" -gt 0 ]; then
    echo "FAILURES: $FAIL_COUNT test(s) failed"
    exit 1
fi
exit 0
