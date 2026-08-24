#!/bin/bash

echo "--- TESTING SPLIT MODULE: COMMUNITY POOL SWEEP ---"

# --- 0. SETUP & CONFIG ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Caller-set BOND_DENOM wins; this only fills it in when the script is run
# standalone. Matters now that the distribution assertion can fail the run --
# an empty denom would otherwise surface as a confusing tx parse error.
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/../lib/denoms.sh"
DENOM="$BOND_DENOM"

# 10,000 SPARK, deliberately an order of magnitude above x/rep's daily
# community-pool skim. x/rep skims the community pool in BeginBlock
# and runs BEFORE x/split, so it takes its cut first. With a deposit equal to
# rep's daily cap there is no threshold that is both meaningful and non-flaky:
# rep could legitimately take 100% of it. At 10x, rep's share is bounded at
# ~10% and what reaches the councils is still overwhelmingly this deposit.
TEST_AMOUNT_RAW=10000000000
TEST_AMOUNT="${TEST_AMOUNT_RAW}${DENOM}"

# Ensure jq is installed
if ! command -v jq &> /dev/null; then
    echo "[FAIL] Error: jq is not installed."
    exit 1
fi

# Get Funder (Alice)
ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
echo "Funder: $ALICE_ADDR"

# --- 1. DISCOVER ADDRESSES ---
echo "--- STEP 1: DISCOVERING ADDRESSES ---"

# A. Source: Community Pool (Distribution Module)
DISTR_ADDR=$($BINARY query auth module-account distribution --output json | jq -r '.account.base_account.address // .account.value.address')
echo "Source (Community Pool): $DISTR_ADDR"

# B. Destinations: Council Treasuries
get_policy_addr() {
    local name="$1"
    ADDR=$($BINARY query commons get-group "$name" --output json 2>/dev/null | jq -r '.group.policy_address // empty')
    if [ -z "$ADDR" ]; then echo "null"; else echo "$ADDR"; fi
}

COMMONS_ADDR=$(get_policy_addr "Commons Council")
TECH_ADDR=$(get_policy_addr "Technical Council")
ECO_COUNCIL_ADDR=$(get_policy_addr "Ecosystem Council")

echo "Commons Treasury:  $COMMONS_ADDR"
echo "Technical Treasury:$TECH_ADDR"
echo "Ecosystem Treasury:$ECO_COUNCIL_ADDR"

if [ "$COMMONS_ADDR" == "null" ]; then
    echo "[FAIL] ERROR: Councils not found. Please run genesis bootstrap."
    exit 1
fi

# --- 2. SNAPSHOT BALANCES ---
echo "--- STEP 2: RECORDING INITIAL BALANCES ---"

get_balance() {
    local addr=$1
    if [ "$addr" == "null" ] || [ -z "$addr" ]; then echo "0"; return; fi
    local bal=$($BINARY query bank balances $addr --output json | jq -r --arg DENOM "$BOND_DENOM" '.balances[] | select(.denom==$DENOM) | .amount')
    if [ -z "$bal" ]; then echo "0"; else echo "$bal"; fi
}

START_COMMONS=$(get_balance $COMMONS_ADDR)
START_TECH=$(get_balance $TECH_ADDR)
START_ECO=$(get_balance $ECO_COUNCIL_ADDR)

echo "Start Commons: $START_COMMONS"
echo "Start Tech:    $START_TECH"
echo "Start Eco:     $START_ECO"

# --- 3. FUND COMMUNITY POOL ---
echo "--- STEP 3: FUNDING COMMUNITY POOL ---"
echo "Alice funds the community pool with $TEST_AMOUNT..."

# Block time degrades under parallel-runner load, so a fixed sleep is not a
# reliable "one block has passed". Poll instead -- and it matters more now that
# the distribution assertion below can actually fail the run.
wait_blocks() {
    local n=$1 start cur attempt=0
    start=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height // "0"')
    while [ $attempt -lt 60 ]; do
        cur=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height // "0"')
        if [ "$cur" -ge $((start + n)) ] 2>/dev/null; then return 0; fi
        attempt=$((attempt + 1)); sleep 1
    done
    return 1
}

wait_for_tx() {
    local hash=$1 attempt=0 result
    while [ $attempt -lt 30 ]; do
        result=$($BINARY query tx "$hash" --output json 2>/dev/null)
        if echo "$result" | jq -e '.code' > /dev/null 2>&1; then echo "$result"; return 0; fi
        attempt=$((attempt + 1)); sleep 1
    done
    echo '{"code": 999, "raw_log": "tx not found"}'
}

# Use the specific distribution command, NOT bank send
RES=$($BINARY tx distribution fund-community-pool $TEST_AMOUNT --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $RES | jq -r '.txhash')

TX_RESULT=$(wait_for_tx "$TX_HASH")
CODE=$(echo "$TX_RESULT" | jq -r '.code // "999"')
if [ "$CODE" != "0" ]; then
    echo "[FAIL] FAILURE: Fund Community Pool failed."
    echo "Log: $(echo "$TX_RESULT" | jq -r '.raw_log')"
    exit 1
fi
echo "[ OK ] Community Pool Funded."

# Give the BeginBlocker chain (shield -> rep -> split) a couple of blocks to run.
if ! wait_blocks 2; then
    echo "[WARN] chain did not advance 2 blocks; sweep results may be premature"
fi

# --- 4. VERIFY SWEEP ---
echo "--- STEP 4: VERIFYING AUTOMATIC SWEEP ---"

# 1. Check that the community pool was significantly drained.
# The pool is never exactly 0: between the block that sweeps and our query
# block, x/distribution accrues fresh validator-commission tax (currently
# 15% of fees+inflation per block). x/split also keeps a dust threshold
# to avoid sub-1-SPARK micro-transfers. Under parallel-runner load several
# blocks may pass, accumulating tens of millions of uspark in the pool
# again, so a strict "< 100" assert is unrealistic.
#
# NOTE: this check alone no longer proves x/split did anything. x/rep also
# drains the pool, in BeginBlock, ahead of split. "The pool is empty" is now
# consistent with split having done nothing at all -- the council check in
# part 2 is what actually attributes the money to split.
START_DISTR_AMOUNT="$TEST_AMOUNT_RAW"
END_DISTR=$(get_balance $DISTR_ADDR)
DRAIN_THRESHOLD=$((START_DISTR_AMOUNT * 5 / 100))   # accept up to 5% residual
if [ "$END_DISTR" -gt "$DRAIN_THRESHOLD" ]; then
    echo "[FAIL] FAILURE: Community Pool was NOT swept! Balance: $END_DISTR (>5% of $START_DISTR_AMOUNT)"
    echo "   Ensure x/split EndBlocker is wired up in app.go and permissions are set."
    exit 1
else
    echo "[ OK ] Community Pool drained (residual=$END_DISTR uspark, < 5%)."
fi

# 2. Check Destinations -- the real assertion.
END_COMMONS=$(get_balance $COMMONS_ADDR)
END_TECH=$(get_balance $TECH_ADDR)
END_ECO=$(get_balance $ECO_COUNCIL_ADDR)

DIFF_COMMONS=$((END_COMMONS - START_COMMONS))
DIFF_TECH=$((END_TECH - START_TECH))
DIFF_ECO=$((END_ECO - START_ECO))

echo "--- RESULTS ---"
echo "Commons Council:   +$DIFF_COMMONS"
echo "Technical Council: +$DIFF_TECH"
echo "Ecosystem Council: +$DIFF_ECO"

TOTAL_DISTRIBUTED=$((DIFF_COMMONS + DIFF_TECH + DIFF_ECO))

# Read x/rep's daily community-pool skim rather than hardcoding it, so this
# threshold follows the chain instead of going stale when it is retuned.
# daily_funding_cap is the COMPUTED allowance in uspark (a share of inflation),
# which is the number that actually bounds the skim -- reading the param would
# give a ratio, not an amount.
REP_SKIM=$($BINARY query rep role-reward-pools --output json 2>/dev/null \
           | jq -r '.daily_funding_cap // empty' 2>/dev/null)
if ! [ "$REP_SKIM" -ge 0 ] 2>/dev/null; then
    # Fall back to the 10% the deposit was sized around, rather than assuming
    # zero -- assuming zero would make this assert too strict and fail loudly
    # for the wrong reason.
    REP_SKIM=$((TEST_AMOUNT_RAW / 10))
    echo "[WARN] could not read rep daily allowance; assuming $REP_SKIM"
fi

# The three councils are NOT the only split recipients -- the three Operations
# Committees carry funding weight too (currently 950/1000 to councils, 50/1000
# to committees). Summing only the councils therefore captures ~95% of the
# pool, so derive the fraction from the registered shares rather than assuming
# the councils receive all of it.
SHARES=$($BINARY query split list-share --output json 2>/dev/null)
TOTAL_WEIGHT=$(echo "$SHARES" | jq -r '[.share[]?.weight // 0 | tonumber] | add // 0' 2>/dev/null)
COUNCIL_WEIGHT=$(echo "$SHARES" | jq -r --arg c "$COMMONS_ADDR" --arg t "$TECH_ADDR" --arg e "$ECO_COUNCIL_ADDR" \
    '[.share[]? | select(.address==$c or .address==$t or .address==$e) | .weight // 0 | tonumber] | add // 0' 2>/dev/null)

if ! [ "$TOTAL_WEIGHT" -gt 0 ] 2>/dev/null || ! [ "$COUNCIL_WEIGHT" -gt 0 ] 2>/dev/null; then
    echo "[WARN] could not read split shares; assuming councils take the whole pool"
    TOTAL_WEIGHT=1
    COUNCIL_WEIGHT=1
fi
echo "Council share of the pool: $COUNCIL_WEIGHT / $TOTAL_WEIGHT"

# Lower bound on what the councils should receive:
#   (deposit - rep's daily skim cap) * council weight fraction, less a dust margin.
# Fresh validator tax accrued since the deposit only pushes the real figure up,
# so this stays a lower bound.
REACHES_SPLIT=$((TEST_AMOUNT_RAW - REP_SKIM))
COUNCIL_EXPECTED=$((REACHES_SPLIT * COUNCIL_WEIGHT / TOTAL_WEIGHT))
DUST_MARGIN=$((COUNCIL_EXPECTED * 5 / 100))
EXPECTED_MIN=$((COUNCIL_EXPECTED - DUST_MARGIN))

echo "Total distributed: $TOTAL_DISTRIBUTED (expected at least $EXPECTED_MIN)"
echo "  = (deposit $TEST_AMOUNT_RAW - rep skim cap $REP_SKIM)"
echo "    x $COUNCIL_WEIGHT/$TOTAL_WEIGHT council weight, less a 5% dust margin"

if [ "$TOTAL_DISTRIBUTED" -ge "$EXPECTED_MIN" ]; then
    echo "[ OK ] SUCCESS: x/split distributed the Community Pool funds to the councils."
else
    echo "[FAIL] FAILURE: Funds missing. Distributed: $TOTAL_DISTRIBUTED, expected >= $EXPECTED_MIN"
    echo "   If the pool drained but the councils did not receive it, check whether"
    echo "   another BeginBlocker (x/rep, x/shield) is skimming ahead of x/split."
    exit 1
fi

exit 0
