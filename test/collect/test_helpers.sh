#!/bin/bash
# Common helper functions for x/collect e2e tests

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
KEYRING="test"
FEES="500000uspark"
TX_WAIT=6

# Counters
TESTS_PASSED=0
TESTS_FAILED=0
TESTS_SKIPPED=0

# --- Address helpers ---

get_address() {
    $BINARY keys show "$1" -a --keyring-backend $KEYRING 2>/dev/null
}

get_block_height() {
    $BINARY status 2>&1 | jq -r '.sync_info.latest_block_height'
}

# --- TX helpers ---

# Send a transaction and return the raw output (JSON)
send_tx() {
    local module="$1"
    shift
    $BINARY tx "$module" "$@" \
        --chain-id $CHAIN_ID \
        --keyring-backend $KEYRING \
        --fees $FEES \
        -y --output json 2>&1
}

# Wait for tx to be included and return the tx result JSON
wait_for_tx() {
    local txhash="$1"
    sleep $TX_WAIT
    $BINARY query tx "$txhash" --output json 2>&1
}

# Get tx hash from broadcast output
get_txhash() {
    echo "$1" | jq -r '.txhash // empty' 2>/dev/null
}

# Get tx code from tx result
get_tx_code() {
    echo "$1" | jq -r '.code // "999"' 2>/dev/null
}

# Extract an event attribute value from tx result
# Usage: extract_event_attr "$TX_RESULT" "collection_created" "id"
extract_event_attr() {
    local tx_result="$1"
    local event_type="$2"
    local attr_key="$3"
    echo "$tx_result" | jq -r --arg et "$event_type" --arg ak "$attr_key" \
        '.events[] | select(.type==$et) | .attributes[] | select(.key==$ak) | .value // empty' 2>/dev/null | head -1
}

# --- Query helpers ---

query() {
    $BINARY query "$@" --output json 2>&1
}

# --- Assertion helpers ---

# Assert a tx succeeds (code=0). Returns the tx result JSON via $TX_RESULT_OUT.
# Usage: assert_tx_success "description" "$TX_OUTPUT"
assert_tx_success() {
    local description="$1"
    local tx_output="$2"

    local txhash
    txhash=$(get_txhash "$tx_output")
    if [ -z "$txhash" ]; then
        echo "FAIL: $description - No txhash in broadcast output"
        echo "  Output: $(echo "$tx_output" | head -3)"
        TESTS_FAILED=$((TESTS_FAILED + 1))
        TX_RESULT_OUT=""
        return 1
    fi

    local tx_result
    tx_result=$(wait_for_tx "$txhash")
    TX_RESULT_OUT="$tx_result"
    local code
    code=$(get_tx_code "$tx_result")

    if [ "$code" = "0" ]; then
        echo "PASS: $description"
        TESTS_PASSED=$((TESTS_PASSED + 1))
        return 0
    else
        local raw_log
        raw_log=$(echo "$tx_result" | jq -r '.raw_log // "unknown error"' 2>/dev/null)
        echo "FAIL: $description (code=$code)"
        echo "  Error: $raw_log"
        TESTS_FAILED=$((TESTS_FAILED + 1))
        return 1
    fi
}

# Assert a tx fails (code!=0 or broadcast rejection).
assert_tx_failure() {
    local description="$1"
    local tx_output="$2"

    local txhash
    txhash=$(get_txhash "$tx_output")
    if [ -z "$txhash" ]; then
        # Broadcast rejection - counts as expected failure
        echo "PASS: $description (broadcast rejection)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
        return 0
    fi

    local tx_result
    tx_result=$(wait_for_tx "$txhash")
    local code
    code=$(get_tx_code "$tx_result")

    if [ "$code" != "0" ]; then
        echo "PASS: $description (expected failure, code=$code)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
        return 0
    else
        echo "FAIL: $description - Expected failure but tx succeeded"
        TESTS_FAILED=$((TESTS_FAILED + 1))
        return 1
    fi
}

# Assert two values are equal
assert_equal() {
    local description="$1"
    local expected="$2"
    local actual="$3"

    if [ "$expected" = "$actual" ]; then
        echo "PASS: $description"
        TESTS_PASSED=$((TESTS_PASSED + 1))
        return 0
    else
        echo "FAIL: $description"
        echo "  Expected: $expected"
        echo "  Actual:   $actual"
        TESTS_FAILED=$((TESTS_FAILED + 1))
        return 1
    fi
}

# Assert a value is not empty/null
assert_not_empty() {
    local description="$1"
    local value="$2"

    if [ -n "$value" ] && [ "$value" != "null" ] && [ "$value" != "" ]; then
        echo "PASS: $description"
        TESTS_PASSED=$((TESTS_PASSED + 1))
        return 0
    else
        echo "FAIL: $description - Value is empty or null"
        TESTS_FAILED=$((TESTS_FAILED + 1))
        return 1
    fi
}

# Assert a numeric value is greater than expected
assert_gt() {
    local description="$1"
    local expected="$2"
    local actual="$3"

    if [ "$actual" -gt "$expected" ] 2>/dev/null; then
        echo "PASS: $description"
        TESTS_PASSED=$((TESTS_PASSED + 1))
        return 0
    else
        echo "FAIL: $description"
        echo "  Expected > $expected, got: $actual"
        TESTS_FAILED=$((TESTS_FAILED + 1))
        return 1
    fi
}

skip_test() {
    local description="$1"
    local reason="$2"
    echo "SKIP: $description - $reason"
    TESTS_SKIPPED=$((TESTS_SKIPPED + 1))
}

print_summary() {
    echo ""
    echo "========================================="
    echo "  TEST SUMMARY"
    echo "========================================="
    echo "  Passed:  $TESTS_PASSED"
    echo "  Failed:  $TESTS_FAILED"
    echo "  Skipped: $TESTS_SKIPPED"
    echo "  Total:   $((TESTS_PASSED + TESTS_FAILED + TESTS_SKIPPED))"
    echo "========================================="

    if [ $TESTS_FAILED -gt 0 ]; then
        return 1
    fi
    return 0
}
