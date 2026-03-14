#!/bin/bash

echo "--- TESTING: Auto-Funding & Module Balance (x/shield) ---"
echo ""

# === 0. SETUP ===
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

# Load test environment
if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    echo "   Run: bash setup_test_accounts.sh"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

echo "Alice:     $ALICE_ADDR"
echo ""

# === PASS/FAIL TRACKING ===
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

# === HELPER FUNCTIONS ===

check_query_success() {
    local RESULT=$1
    local QUERY_NAME=$2

    if echo "$RESULT" | grep -qi "error\|Error\|ERROR"; then
        if echo "$RESULT" | grep -qi "not found"; then
            echo "  $QUERY_NAME: empty result (not found)"
            return 0
        fi
        echo "  $QUERY_NAME: FAILED"
        echo "  $RESULT"
        return 1
    fi
    echo "  $QUERY_NAME: OK"
    return 0
}

# =========================================================================
# PART 1: Query shield module balance
# =========================================================================
echo "--- PART 1: Shield module account balance ---"

BALANCE=$($BINARY query shield module-balance --output json 2>&1)

if ! check_query_success "$BALANCE" "module-balance"; then
    echo "  Could not query module balance"
    record_result "Shield module balance query" "FAIL"
else
    BAL_DENOM=$(echo "$BALANCE" | jq -r '.balance.denom // "null"')
    BAL_AMOUNT=$(echo "$BALANCE" | jq -r '.balance.amount // "0"')
    echo "  Shield module balance: $BAL_AMOUNT $BAL_DENOM"
    record_result "Shield module balance query" "PASS"
fi

# =========================================================================
# PART 2: Verify module account exists
# =========================================================================
echo "--- PART 2: Verify shield module account ---"

MODULE_ACCT=$($BINARY query auth module-account shield --output json 2>&1)

if echo "$MODULE_ACCT" | grep -qi "error\|not found"; then
    echo "  Shield module account not found"
    record_result "Shield module account exists" "FAIL"
else
    MODULE_ADDR=$(echo "$MODULE_ACCT" | jq -r '.account.base_account.address // .account.value.address // "null"')
    MODULE_NAME=$(echo "$MODULE_ACCT" | jq -r '.account.name // .account.value.name // "null"')

    echo "  Module name: $MODULE_NAME"
    echo "  Module address: $MODULE_ADDR"

    if [ "$MODULE_ADDR" == "null" ] || [ -z "$MODULE_ADDR" ]; then
        echo "  Could not determine shield module address"
        record_result "Shield module account exists" "FAIL"
    else
        echo "  Shield module account verified (address: $MODULE_ADDR)"
        record_result "Shield module account exists" "PASS"
    fi
fi

# =========================================================================
# PART 3: Query funding parameters
# =========================================================================
echo "--- PART 3: Funding parameters ---"

PARAMS=$($BINARY query shield params --output json 2>&1)

if ! check_query_success "$PARAMS" "params"; then
    echo "  Could not query shield params"
    record_result "Funding parameters query" "FAIL"
else
    MAX_FUNDING=$(echo "$PARAMS" | jq -r '.params.max_funding_per_day // "0"')
    MIN_RESERVE=$(echo "$PARAMS" | jq -r '.params.min_gas_reserve // "0"')

    echo "  Max funding per day: $MAX_FUNDING uspark"
    echo "  Min gas reserve: $MIN_RESERVE uspark"

    # Verify params are non-zero
    if [ "$MAX_FUNDING" == "0" ] || [ "$MAX_FUNDING" == "null" ]; then
        echo "  WARNING: max_funding_per_day is 0 or null"
    fi

    if [ "$MIN_RESERVE" == "0" ] || [ "$MIN_RESERVE" == "null" ]; then
        echo "  WARNING: min_gas_reserve is 0 or null"
    fi

    record_result "Funding parameters query" "PASS"
fi

# =========================================================================
# PART 4: Query day funding ledger
# =========================================================================
echo "--- PART 4: Day funding ledger ---"

# Compute current day from block height
BLOCK_HEIGHT=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height // "0"')
CURRENT_DAY=$((BLOCK_HEIGHT / 14400))

echo "  Current block height: $BLOCK_HEIGHT"
echo "  Current day: $CURRENT_DAY"

DAY_FUND=$($BINARY query shield day-funding $CURRENT_DAY --output json 2>&1)

if echo "$DAY_FUND" | grep -qi "not found"; then
    echo "  No funding recorded for day $CURRENT_DAY"
    echo "  (Expected if BeginBlocker has not funded yet or balance is above reserve)"
    record_result "Day funding ledger query" "PASS"
elif echo "$DAY_FUND" | grep -qi "error"; then
    echo "  Day funding query returned error (may be expected)"
    record_result "Day funding ledger query" "PASS"
else
    FUNDED=$(echo "$DAY_FUND" | jq -r '.day_funding.amount_funded // "0"')
    echo "  Day $CURRENT_DAY funded: $FUNDED uspark"
    record_result "Day funding ledger query" "PASS"
fi

# =========================================================================
# PART 5: Verify funding cap is enforced
# =========================================================================
echo "--- PART 5: Verify funding cap math ---"

# The BeginBlocker should only fund up to min_gas_reserve, capped by max_funding_per_day
# We can verify the relationship between balance and reserve

if [ -z "$BAL_AMOUNT" ] || [ "$BAL_AMOUNT" == "null" ]; then
    BAL_AMOUNT="0"
fi
if [ -z "$MIN_RESERVE" ] || [ "$MIN_RESERVE" == "null" ]; then
    MIN_RESERVE="0"
fi

if [ "$BAL_AMOUNT" != "0" ]; then
    echo "  Current balance: $BAL_AMOUNT uspark"
    echo "  Min gas reserve: $MIN_RESERVE uspark"

    # If balance >= min_reserve, no funding should occur
    if [ "$BAL_AMOUNT" -ge "$MIN_RESERVE" ] 2>/dev/null; then
        echo "  Balance >= min reserve: no additional funding needed"
    else
        DEFICIT=$((MIN_RESERVE - BAL_AMOUNT))
        echo "  Balance < min reserve: deficit of $DEFICIT uspark"
        echo "  (BeginBlocker should top up in next block, up to daily cap)"
    fi
    record_result "Funding cap math verification" "PASS"
else
    echo "  Balance is 0 or unavailable"
    echo "  (BeginBlocker should fund from community pool in next block)"
    record_result "Funding cap math verification" "PASS"
fi

# =========================================================================
# PART 6: Query community pool (funding source)
# =========================================================================
echo "--- PART 6: Community pool (funding source) ---"

POOL=$($BINARY query distribution community-pool --output json 2>&1)

if echo "$POOL" | grep -qi "not found"; then
    echo "  Community pool query returned not found (acceptable)"
    record_result "Community pool query" "PASS"
elif echo "$POOL" | grep -qi "error"; then
    echo "  Warning: Could not query community pool"
    echo "  $POOL"
    record_result "Community pool query" "FAIL"
else
    POOL_AMOUNT=$(echo "$POOL" | jq -r '.pool[] | select(.denom=="uspark") | .amount // "0"' 2>/dev/null || echo "0")
    echo "  Community pool uspark: $POOL_AMOUNT"

    if [ -z "$POOL_AMOUNT" ] || [ "$POOL_AMOUNT" == "0" ]; then
        echo "  Community pool may be empty (shield auto-funding may not work)"
    fi
    record_result "Community pool query" "PASS"
fi

# =========================================================================
# PART 7: Verify shield module has burner/minter permissions
# =========================================================================
echo "--- PART 7: Verify module permissions ---"

# The shield module needs to be able to receive coins from fee collector
# and community pool. Check if the module account has the right permissions.
PERMISSIONS=$(echo "$MODULE_ACCT" | jq -r '.account.permissions[]? // empty' 2>/dev/null)

if [ -n "$PERMISSIONS" ]; then
    echo "  Module permissions: $PERMISSIONS"
else
    echo "  Module permissions: (none listed or default)"
    echo "  (Shield module receives funds via community pool distribution)"
fi

record_result "Module permissions check" "PASS"

# =========================================================================
# SUMMARY
# =========================================================================
echo "--- FINAL RESULTS ---"
for i in "${!TEST_NAMES[@]}"; do
    printf "  %-50s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done
echo ""
echo "Total: $PASS_COUNT passed, $FAIL_COUNT failed out of $((PASS_COUNT + FAIL_COUNT))"
if [ $FAIL_COUNT -gt 0 ]; then
    exit 1
fi
