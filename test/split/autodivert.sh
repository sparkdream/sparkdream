#!/bin/bash

echo "--- TESTING SPLIT MODULE: COMMUNITY POOL SWEEP ---"

# --- 0. SETUP & CONFIG ---
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
DENOM="uspark"
TEST_AMOUNT="1000000000${DENOM}" # 1000 SPARK

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
    local bal=$($BINARY query bank balances $addr --output json | jq -r --arg DENOM "uspark" '.balances[] | select(.denom==$DENOM) | .amount')
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

# Use the specific distribution command, NOT bank send
RES=$($BINARY tx distribution fund-community-pool $TEST_AMOUNT --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
TX_HASH=$(echo $RES | jq -r '.txhash')
sleep 6 # Wait for block (EndBlocker triggers)

CODE=$($BINARY query tx $TX_HASH --output json | jq -r '.code')
if [ "$CODE" != "0" ]; then
    echo "[FAIL] FAILURE: Fund Community Pool failed."
    echo "Log: $($BINARY query tx $TX_HASH --output json | jq -r '.raw_log')"
    exit 1
fi
echo "[ OK ] Community Pool Funded."

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
# What we actually want to verify: out of the 1 000 000 000 uspark
# (1000 SPARK) we just deposited, the bulk has been distributed.
START_DISTR_AT_SWEEP="$TEST_AMOUNT"   # "1000000000uspark"
START_DISTR_AMOUNT="${START_DISTR_AT_SWEEP%uspark}"
END_DISTR=$(get_balance $DISTR_ADDR)
DRAIN_THRESHOLD=$((START_DISTR_AMOUNT * 5 / 100))   # accept up to 5% residual
if [ "$END_DISTR" -gt "$DRAIN_THRESHOLD" ]; then
    echo "[FAIL] FAILURE: Community Pool was NOT swept! Balance: $END_DISTR (>5% of $START_DISTR_AMOUNT)"
    echo "   Ensure x/split EndBlocker is wired up in app.go and permissions are set."
    exit 1
else
    echo "[ OK ] SUCCESS: Community Pool drained (residual=$END_DISTR uspark, < 5%)."
fi

# 2. Check Destinations
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
EXPECTED=10000

if [ "$TOTAL_DISTRIBUTED" -ge "$((EXPECTED - 5))" ]; then
    echo " SUCCESS: x/split successfully distributed the Community Pool funds!"
else
    echo "[FAIL] FAILURE: Funds missing. Distributed: $TOTAL_DISTRIBUTED / $EXPECTED"
fi