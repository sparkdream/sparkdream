#!/bin/bash

echo "--- TESTING: GNOVM COUNTER REALM (DEPLOY, CALL, QUERY) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

echo "Deployer: $ALICE_ADDR"
echo "Contracts: $CONTRACTS_DIR"
echo ""

PASS=0
FAIL=0

# ========================================================================
# Helper Functions
# ========================================================================

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

    echo "ERROR: Transaction $TXHASH not found after $MAX_ATTEMPTS attempts" >&2
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

    sleep 3
    TX_RESULT=$(wait_for_tx "$TXHASH")
    return $?
}

eval_gnovm() {
    local PKG_PATH=$1
    local EXPR=$2
    $BINARY query gnovm eval "$PKG_PATH" "$EXPR" 2>&1
}

# ========================================================================
# TEST 1: Deploy counter realm
# ========================================================================
echo "TEST 1: Deploy counter realm"
echo "  Deploying $CONTRACTS_DIR/counter to gno.land/r/demo/counter..."

TX_RES=$($BINARY tx gnovm add-package "$CONTRACTS_DIR/counter" \
    --send 1000uspark --max-deposit 10000uspark \
    --from alice --gas 5000000 --fees 50000uspark -y \
    --keyring-backend test --chain-id $CHAIN_ID --output json 2>&1)

submit_tx_and_wait "$TX_RES"

if check_tx_success "$TX_RESULT"; then
    GAS_USED=$(echo "$TX_RESULT" | jq -r '.gas_used')
    echo "  PASS: Counter realm deployed (gas: $GAS_USED)"
    PASS=$((PASS + 1))
else
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // "unknown error"')
    echo "  FAIL: Deploy failed: $RAW_LOG"
    FAIL=$((FAIL + 1))
    echo ""
    echo "=== RESULTS: $PASS passed, $FAIL failed ==="
    exit 1
fi

# ========================================================================
# TEST 2: Query initial counter value (should be 0)
# ========================================================================
echo ""
echo "TEST 2: Query initial counter value"

RESULT=$(eval_gnovm "gno.land/r/demo/counter" 'Render("")')
if echo "$RESULT" | grep -q '"0"'; then
    echo "  PASS: Initial counter is 0"
    PASS=$((PASS + 1))
else
    echo "  FAIL: Expected '0', got: $RESULT"
    FAIL=$((FAIL + 1))
fi

# ========================================================================
# TEST 3: Call Increment
# ========================================================================
echo ""
echo "TEST 3: Call Increment"

TX_RES=$($BINARY tx gnovm call gno.land/r/demo/counter Increment \
    --from alice --gas 5000000 --fees 50000uspark -y \
    --keyring-backend test --chain-id $CHAIN_ID --output json 2>&1)

submit_tx_and_wait "$TX_RES"

if check_tx_success "$TX_RESULT"; then
    echo "  PASS: Increment succeeded"
    PASS=$((PASS + 1))
else
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // "unknown error"')
    echo "  FAIL: Increment failed: $RAW_LOG"
    FAIL=$((FAIL + 1))
fi

# ========================================================================
# TEST 4: Verify counter is 1 after increment
# ========================================================================
echo ""
echo "TEST 4: Verify counter is 1 after increment"

RESULT=$(eval_gnovm "gno.land/r/demo/counter" 'Render("")')
if echo "$RESULT" | grep -q '"1"'; then
    echo "  PASS: Counter is 1"
    PASS=$((PASS + 1))
else
    echo "  FAIL: Expected '1', got: $RESULT"
    FAIL=$((FAIL + 1))
fi

# ========================================================================
# TEST 5: Call Increment again
# ========================================================================
echo ""
echo "TEST 5: Call Increment again"

TX_RES=$($BINARY tx gnovm call gno.land/r/demo/counter Increment \
    --from alice --gas 5000000 --fees 50000uspark -y \
    --keyring-backend test --chain-id $CHAIN_ID --output json 2>&1)

submit_tx_and_wait "$TX_RES"

if check_tx_success "$TX_RESULT"; then
    echo "  PASS: Second increment succeeded"
    PASS=$((PASS + 1))
else
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // "unknown error"')
    echo "  FAIL: Second increment failed: $RAW_LOG"
    FAIL=$((FAIL + 1))
fi

# ========================================================================
# TEST 6: Verify counter is 2
# ========================================================================
echo ""
echo "TEST 6: Verify counter is 2"

RESULT=$(eval_gnovm "gno.land/r/demo/counter" 'Render("")')
if echo "$RESULT" | grep -q '"2"'; then
    echo "  PASS: Counter is 2"
    PASS=$((PASS + 1))
else
    echo "  FAIL: Expected '2', got: $RESULT"
    FAIL=$((FAIL + 1))
fi

# ========================================================================
# TEST 7: Call Decrement
# ========================================================================
echo ""
echo "TEST 7: Call Decrement"

TX_RES=$($BINARY tx gnovm call gno.land/r/demo/counter Decrement \
    --from alice --gas 5000000 --fees 50000uspark -y \
    --keyring-backend test --chain-id $CHAIN_ID --output json 2>&1)

submit_tx_and_wait "$TX_RES"

if check_tx_success "$TX_RESULT"; then
    echo "  PASS: Decrement succeeded"
    PASS=$((PASS + 1))
else
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // "unknown error"')
    echo "  FAIL: Decrement failed: $RAW_LOG"
    FAIL=$((FAIL + 1))
fi

# ========================================================================
# TEST 8: Verify counter is 1 after decrement
# ========================================================================
echo ""
echo "TEST 8: Verify counter is 1 after decrement"

RESULT=$(eval_gnovm "gno.land/r/demo/counter" 'Render("")')
if echo "$RESULT" | grep -q '"1"'; then
    echo "  PASS: Counter is 1 after decrement"
    PASS=$((PASS + 1))
else
    echo "  FAIL: Expected '1', got: $RESULT"
    FAIL=$((FAIL + 1))
fi

# ========================================================================
# TEST 9: Call Reset
# ========================================================================
echo ""
echo "TEST 9: Call Reset"

TX_RES=$($BINARY tx gnovm call gno.land/r/demo/counter Reset \
    --from alice --gas 5000000 --fees 50000uspark -y \
    --keyring-backend test --chain-id $CHAIN_ID --output json 2>&1)

submit_tx_and_wait "$TX_RES"

if check_tx_success "$TX_RESULT"; then
    echo "  PASS: Reset succeeded"
    PASS=$((PASS + 1))
else
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // "unknown error"')
    echo "  FAIL: Reset failed: $RAW_LOG"
    FAIL=$((FAIL + 1))
fi

# ========================================================================
# TEST 10: Verify counter is 0 after reset
# ========================================================================
echo ""
echo "TEST 10: Verify counter is 0 after reset"

RESULT=$(eval_gnovm "gno.land/r/demo/counter" 'Render("")')
if echo "$RESULT" | grep -q '"0"'; then
    echo "  PASS: Counter is 0 after reset"
    PASS=$((PASS + 1))
else
    echo "  FAIL: Expected '0', got: $RESULT"
    FAIL=$((FAIL + 1))
fi

# ========================================================================
# TEST 11: Query via GetCount() expression
# ========================================================================
echo ""
echo "TEST 11: Query via GetCount() expression"

RESULT=$(eval_gnovm "gno.land/r/demo/counter" 'GetCount()')
if echo "$RESULT" | grep -q '(0 int)'; then
    echo "  PASS: GetCount() returns (0 int)"
    PASS=$((PASS + 1))
else
    echo "  FAIL: Expected '(0 int)', got: $RESULT"
    FAIL=$((FAIL + 1))
fi

# ========================================================================
# TEST 12: Deploy duplicate package should fail
# ========================================================================
echo ""
echo "TEST 12: Deploy duplicate package should fail"

TX_RES=$($BINARY tx gnovm add-package "$CONTRACTS_DIR/counter" \
    --send 1000uspark --max-deposit 10000uspark \
    --from alice --gas 5000000 --fees 50000uspark -y \
    --keyring-backend test --chain-id $CHAIN_ID --output json 2>&1)

submit_tx_and_wait "$TX_RES"

if ! check_tx_success "$TX_RESULT"; then
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // "unknown error"')
    if echo "$RAW_LOG" | grep -qi "already exists"; then
        echo "  PASS: Duplicate deploy rejected (package already exists)"
    else
        echo "  PASS: Duplicate deploy rejected: $RAW_LOG"
    fi
    PASS=$((PASS + 1))
else
    echo "  FAIL: Duplicate deploy should have been rejected"
    FAIL=$((FAIL + 1))
fi

# ========================================================================
# TEST 13: Query non-existent package
# ========================================================================
echo ""
echo "TEST 13: Query non-existent package"

RESULT=$(eval_gnovm "gno.land/r/demo/nonexistent" 'Render("")' 2>&1)
if echo "$RESULT" | grep -qi "invalid package path\|not found\|error"; then
    echo "  PASS: Non-existent package query returns error"
    PASS=$((PASS + 1))
else
    echo "  FAIL: Expected error for non-existent package, got: $RESULT"
    FAIL=$((FAIL + 1))
fi

# ========================================================================
# SUMMARY
# ========================================================================
echo ""
echo "========================================="
echo "=== RESULTS: $PASS passed, $FAIL failed ==="
echo "========================================="

if [ $FAIL -gt 0 ]; then
    exit 1
fi
exit 0
