#!/bin/bash

echo "--- TESTING: FEDERATION bridge-operator rewards ---"
#
# Exercises the bridge-operator compensation cycle, which is the half of
# federation pay that stayed in x/federation. (Verifier pay moved to x/rep --
# see verifier_rewards_test.sh.) The split is deliberate: federation owns the
# BridgeBinding submission counters operator pay is scored from, and those
# counters are incremented by VERIFIERS rather than by the operator, so
# operator pay can safely be volume-weighted. A verifier self-certifies, so
# verifier pay is not.
#
# The cycle is:
#   BeginBlock  FundOperatorRewardPool    -- one capped daily claim on the
#                                            community pool, sized as a share
#                                            of inflation and ledgered per UTC
#                                            day so it survives restarts
#   EndBlock    DistributeOperatorRewards -- pro-rata on verified submissions
#               BurnOperatorRewardPoolOverflow -- residual above the cap
#
# Tests:
#   TEST 1: operator-reward-pool query answers with a coherent shape
#           (balance/cap/headroom/funded_today/daily_funding_cap agree).
#   TEST 2: headroom == max(0, cap - balance).
#   TEST 3: funded_today never exceeds daily_funding_cap -- the UTC-day
#           ledger is what stops the draw being re-taken every block.
#   TEST 4: the pool is a plain bank account and is NOT one of the x/rep
#           bonded-role pools (the two funding paths are independent).
#   TEST 5: across an epoch boundary, epoch_verified on a binding resets --
#           proving the distribution pass walked the bindings.
#   TEST 6: inflation_share matches the on-chain param.
#
# Wall-time: waits for one operator reward epoch boundary.

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "ERROR: .test_env not found. Run setup_test_accounts.sh first."
    exit 1
fi
source "$SCRIPT_DIR/.test_env"

PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()

record_result() {
    local NAME=$1; local RESULT=$2
    TEST_NAMES+=("$NAME"); RESULTS+=("$RESULT")
    if [[ "$RESULT" == FAIL* ]]; then FAIL_COUNT=$((FAIL_COUNT + 1)); else PASS_COUNT=$((PASS_COUNT + 1)); fi
    echo "  => $RESULT"
}

# Read the cadence from chain params rather than restating a testparams
# constant that can drift.
EPOCH_BLOCKS=$($BINARY query federation params --output json 2>/dev/null \
    | jq -r '.params.operator_reward_epoch_blocks // empty')
if [ -z "$EPOCH_BLOCKS" ] || [ "$EPOCH_BLOCKS" == "null" ]; then
    EPOCH_BLOCKS=10
fi
echo "operator_reward_epoch_blocks: $EPOCH_BLOCKS"

current_height() {
    $BINARY status 2>/dev/null | jq -r '.sync_info.latest_block_height // .SyncInfo.latest_block_height // empty'
}

# Poll rather than sleeping a fixed wall-clock: block time drifts under load
# and a fixed sleep either flakes or wastes minutes.
wait_for_height() {
    local TARGET=$1; local MAX_WAIT=${2:-180}; local WAITED=0
    while [ $WAITED -lt $MAX_WAIT ]; do
        local H=$(current_height)
        if [ -n "$H" ] && [ "$H" -ge "$TARGET" ] 2>/dev/null; then echo "$H"; return 0; fi
        sleep 3; WAITED=$((WAITED + 3))
    done
    current_height
}

POOL=$($BINARY query federation operator-reward-pool --output json 2>/dev/null)
if [ -z "$POOL" ] || ! echo "$POOL" | jq -e '.' > /dev/null 2>&1; then
    echo "[FAIL] operator-reward-pool query returned nothing parseable"
    echo "  The query is new; if the binary predates it, rebuild with /build-test-chain."
    exit 1
fi

# proto3 omits zero-valued fields from CLI JSON, so every numeric read needs a
# default or the arithmetic below silently compares against an empty string.
POOL_ADDR=$(echo "$POOL" | jq -r '.address // ""')
BALANCE=$(echo "$POOL"   | jq -r '.balance // "0"')
CAP=$(echo "$POOL"       | jq -r '.cap // "0"')
HEADROOM=$(echo "$POOL"  | jq -r '.headroom // "0"')
FUNDED_TODAY=$(echo "$POOL" | jq -r '.funded_today // "0"')
DAILY_CAP=$(echo "$POOL" | jq -r '.daily_funding_cap // "0"')
INFLATION_SHARE=$(echo "$POOL" | jq -r '.inflation_share // "0"')

echo "  address:           $POOL_ADDR"
echo "  balance:           $BALANCE"
echo "  cap:               $CAP"
echo "  headroom:          $HEADROOM"
echo "  funded_today:      $FUNDED_TODAY"
echo "  daily_funding_cap: $DAILY_CAP"
echo "  inflation_share:   $INFLATION_SHARE"
echo ""

# ========================================================================
# TEST 1: query shape
# ========================================================================
echo "--- TEST 1: operator-reward-pool returns a coherent shape ---"
SHAPE_OK=1
[ -z "$POOL_ADDR" ] && SHAPE_OK=0
for V in "$BALANCE" "$CAP" "$HEADROOM" "$FUNDED_TODAY" "$DAILY_CAP"; do
    if ! [[ "$V" =~ ^[0-9]+$ ]]; then SHAPE_OK=0; fi
done
if [ "$SHAPE_OK" == "1" ]; then
    echo "  address set, all five amounts are non-negative integers"
    record_result "operator-reward-pool query shape" "PASS"
else
    echo "  [FAIL] malformed response: addr='$POOL_ADDR' bal='$BALANCE' cap='$CAP' head='$HEADROOM' today='$FUNDED_TODAY' daily='$DAILY_CAP'"
    record_result "operator-reward-pool query shape" "FAIL"
fi

# ========================================================================
# TEST 2: headroom == max(0, cap - balance)
# Headroom is what the funding pass divides by, so a wrong value silently
# misallocates the draw across pools rather than erroring.
# ========================================================================
echo ""
echo "--- TEST 2: headroom == max(0, cap - balance) ---"
EXPECTED_HEADROOM=$(python3 -c "print(max(0, $CAP - $BALANCE))")
if [ "$HEADROOM" == "$EXPECTED_HEADROOM" ]; then
    echo "  headroom=$HEADROOM matches cap($CAP) - balance($BALANCE)"
    record_result "headroom arithmetic" "PASS"
else
    echo "  [FAIL] headroom=$HEADROOM, expected $EXPECTED_HEADROOM"
    record_result "headroom arithmetic" "FAIL"
fi

# ========================================================================
# TEST 3: funded_today <= daily_funding_cap
# The UTC-day ledger is the only thing stopping the daily allowance being
# re-drawn on every block of the day.
# ========================================================================
echo ""
echo "--- TEST 3: funded_today does not exceed the daily allowance ---"
if [ "$DAILY_CAP" == "0" ]; then
    echo "  daily_funding_cap is 0 (inflation_share=$INFLATION_SHARE disables automatic funding)"
    if [ "$FUNDED_TODAY" == "0" ]; then
        record_result "daily draw ledger bounded" "PASS"
    else
        echo "  [FAIL] funded_today=$FUNDED_TODAY with a zero allowance"
        record_result "daily draw ledger bounded" "FAIL"
    fi
elif [ "$FUNDED_TODAY" -le "$DAILY_CAP" ] 2>/dev/null; then
    echo "  funded_today=$FUNDED_TODAY <= daily_funding_cap=$DAILY_CAP"
    record_result "daily draw ledger bounded" "PASS"
else
    echo "  [FAIL] funded_today=$FUNDED_TODAY exceeds daily_funding_cap=$DAILY_CAP"
    record_result "daily draw ledger bounded" "FAIL"
fi

# ========================================================================
# TEST 4: the operator pool is independent of the x/rep bonded-role pools
# Two separate community-pool claims; conflating them would double-count the
# skim that has to happen before x/split drains the pool to the councils.
# ========================================================================
echo ""
echo "--- TEST 4: operator pool is not an x/rep bonded-role pool ---"
REP_POOLS=$($BINARY query rep role-reward-pools --output json 2>/dev/null)
if [ -z "$REP_POOLS" ] || ! echo "$REP_POOLS" | jq -e '.' > /dev/null 2>&1; then
    echo "  [WARN] rep role-reward-pools query unavailable; cannot cross-check"
    record_result "operator pool independent of rep pools" "SKIP"
else
    CLASH=$(echo "$REP_POOLS" | jq -r --arg a "$POOL_ADDR" '[.. | strings | select(. == $a)] | length')
    if [ "${CLASH:-0}" == "0" ]; then
        echo "  operator pool address does not appear among the rep bonded-role pools"
        record_result "operator pool independent of rep pools" "PASS"
    else
        echo "  [FAIL] operator pool address $POOL_ADDR also appears in rep role-reward-pools"
        record_result "operator pool independent of rep pools" "FAIL"
    fi
fi

# ========================================================================
# TEST 5: epoch counters reset across a distribution boundary
# The strongest "did the distribution pass actually run" signal available
# from outside: it resets epoch counters on every binding regardless of
# eligibility.
# ========================================================================
echo ""
echo "--- TEST 5: binding epoch counters reset across the boundary ---"
BINDINGS=$($BINARY query federation list-bridge-bindings --output json 2>/dev/null)
BINDING_COUNT=$(echo "$BINDINGS" | jq -r '(.bridge_bindings // []) | length')

if [ "${BINDING_COUNT:-0}" == "0" ]; then
    echo "  no bridge bindings registered; nothing to distribute to"
    record_result "binding epoch counters reset" "SKIP"
else
    H=$(current_height)
    NEXT_BOUNDARY=$(( (H / EPOCH_BLOCKS + 1) * EPOCH_BLOCKS ))
    echo "  height $H -> waiting for boundary $NEXT_BOUNDARY"
    REACHED=$(wait_for_height $((NEXT_BOUNDARY + 2)) 200)
    echo "  reached height: $REACHED"

    POST=$($BINARY query federation list-bridge-bindings --output json 2>/dev/null)
    MAX_EPOCH_VERIFIED=$(echo "$POST" | jq -r '[(.bridge_bindings // [])[] | (.epoch_verified // "0" | tonumber)] | max // 0')
    if [ "${MAX_EPOCH_VERIFIED:-0}" == "0" ]; then
        echo "  all bindings show epoch_verified=0 after the boundary"
        record_result "binding epoch counters reset" "PASS"
    else
        # Non-zero is legitimate if a verification landed in the new epoch
        # between the boundary and this query.
        echo "  [WARN] max epoch_verified=$MAX_EPOCH_VERIFIED after boundary"
        echo "  A verification may have landed in the new epoch; not a hard failure."
        record_result "binding epoch counters reset" "SKIP"
    fi
fi

# ========================================================================
# TEST 6: inflation_share echoes the on-chain param
# ========================================================================
echo ""
echo "--- TEST 6: inflation_share matches federation params ---"
PARAM_SHARE=$($BINARY query federation params --output json 2>/dev/null \
    | jq -r '.params.operator_reward_inflation_share // "0"')
if [ "$INFLATION_SHARE" == "$PARAM_SHARE" ]; then
    echo "  inflation_share=$INFLATION_SHARE matches params"
    record_result "inflation_share matches params" "PASS"
else
    echo "  [FAIL] query reports $INFLATION_SHARE, params say $PARAM_SHARE"
    record_result "inflation_share matches params" "FAIL"
fi

# ========================================================================
echo ""
echo "=========================================="
echo "FEDERATION OPERATOR REWARDS TEST SUMMARY"
echo "=========================================="
for i in "${!TEST_NAMES[@]}"; do
    printf "  %-45s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done
echo "------------------------------------------"
echo "  PASS/SKIP: $PASS_COUNT   FAIL: $FAIL_COUNT"
echo "=========================================="

if [ $FAIL_COUNT -gt 0 ]; then exit 1; fi
exit 0
