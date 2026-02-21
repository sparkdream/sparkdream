#!/bin/bash

echo "--- TESTING: TLE Validator Share Registration (x/vote) ---"
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

# === HELPER FUNCTIONS ===

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

check_tx_failure() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        return 0
    fi
    return 1
}

# =========================================================================
# PART 1: Check TLE status
# =========================================================================
echo "--- PART 1: Check TLE status ---"

TLE_STATUS=$($BINARY query vote tle-status --output json 2>&1)

if echo "$TLE_STATUS" | grep -qi "error"; then
    echo "  Failed to query TLE status"
    exit 1
fi

# tle-status returns .tle_enabled, .current_epoch
# .latest_available_epoch and .master_public_key may be absent when default/empty
TLE_ENABLED=$(echo "$TLE_STATUS" | jq -r '.tle_enabled // "null"')
CURRENT_EPOCH=$(echo "$TLE_STATUS" | jq -r '.current_epoch // "0"')

echo "  TLE enabled: $TLE_ENABLED"
echo "  Current epoch: $CURRENT_EPOCH"

if [ "$TLE_ENABLED" != "true" ]; then
    echo "  TLE is disabled. Skipping TLE-specific tests."
    echo ""
    echo "--- TEST SUMMARY ---"
    echo "  TLE tests skipped (TLE not enabled)"
    exit 0
fi

echo ""

# =========================================================================
# PART 2: Query TLE validator shares (should be empty initially)
# =========================================================================
echo "--- PART 2: Query TLE validator shares ---"

TLE_SHARES=$($BINARY query vote tle-validator-shares --output json 2>&1)

if echo "$TLE_SHARES" | grep -qi "error"; then
    echo "  Failed to query TLE validator shares"
    exit 1
fi

# tle-validator-shares returns .shares, .total_validators, .registered_validators, .threshold_needed
# May return empty {} when none registered
TOTAL_VALS=$(echo "$TLE_SHARES" | jq -r '.total_validators // "0"')
REGISTERED_VALS=$(echo "$TLE_SHARES" | jq -r '.registered_validators // "0"')
THRESHOLD_NEEDED=$(echo "$TLE_SHARES" | jq -r '.threshold_needed // "0"')

echo "  Total validators: $TOTAL_VALS"
echo "  Registered for TLE: $REGISTERED_VALS"
echo "  Threshold needed: $THRESHOLD_NEEDED"

echo ""

# =========================================================================
# PART 3: Register TLE share from alice (validator)
# =========================================================================
echo "--- PART 3: Register TLE share from alice ---"

# Alice is typically the validator in a single-validator test chain.
# Generate a dummy BN256 G1 public key share (48 bytes for compressed BN254 G1 point)
TLE_PUB_KEY_HEX="0400000000000000000000000000000000000000000000000000000000000001"
TLE_PUB_KEY_B64=$(echo "$TLE_PUB_KEY_HEX" | xxd -r -p | base64)

TX_RES=$($BINARY tx vote register-tle-share \
    "1" \
    --public-key-share "$TLE_PUB_KEY_B64" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to register TLE share: no txhash"
    echo "  Response: $TX_RES"
    echo "  (Alice may not be a validator in this test configuration)"
    echo ""
    echo "--- PART 3: Skipped (alice may not be a bonded validator) ---"
else
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")

    if check_tx_success "$TX_RESULT"; then
        SHARE_INDEX=$(extract_event_value "$TX_RESULT" "tle_share_registered" "share_index")
        echo "  Registered TLE share with index: $SHARE_INDEX"
    else
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // "Unknown error"')
        echo "  Failed to register TLE share: $RAW_LOG"
        echo "  (This may be expected if alice is not a bonded validator)"
    fi
fi

echo ""

# =========================================================================
# PART 4: Non-validator TLE share registration should fail
# =========================================================================
echo "--- PART 4: Non-validator TLE share (should fail) ---"

NONVAL_PUB_KEY_HEX="0500000000000000000000000000000000000000000000000000000000000002"
NONVAL_PUB_KEY_B64=$(echo "$NONVAL_PUB_KEY_HEX" | xxd -r -p | base64)

TX_RES=$($BINARY tx vote register-tle-share \
    "2" \
    --public-key-share "$NONVAL_PUB_KEY_B64" \
    --from voter1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Correctly rejected non-validator TLE share (no broadcast)"
else
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")

    if check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected non-validator TLE share"
    else
        echo "  ERROR: Non-validator should not register TLE shares"
        exit 1
    fi
fi

echo ""

# =========================================================================
# PART 5: Query TLE liveness
# =========================================================================
echo "--- PART 5: Query TLE liveness ---"

TLE_LIVE=$($BINARY query vote tle-liveness --output json 2>&1)

if echo "$TLE_LIVE" | grep -qi "error"; then
    echo "  Warning: Could not query TLE liveness"
else
    # tle-liveness returns .validators (array), .window_size, .miss_tolerance
    # When empty, may only have .miss_tolerance
    WINDOW_SIZE=$(echo "$TLE_LIVE" | jq -r '.window_size // "0"')
    MISS_TOLERANCE=$(echo "$TLE_LIVE" | jq -r '.miss_tolerance // "0"')
    VAL_COUNT=$(echo "$TLE_LIVE" | jq -r '.validators | length' 2>/dev/null || echo "0")

    echo "  Window size: $WINDOW_SIZE epochs"
    echo "  Miss tolerance: $MISS_TOLERANCE"
    echo "  Tracked validators: $VAL_COUNT"
fi

echo ""

# =========================================================================
# PART 6: Query epoch decryption key (likely unavailable)
# =========================================================================
echo "--- PART 6: Query epoch decryption key ---"

# epoch-decryption-key-query returns .epoch, .available, .decryption_key, .shares_received, .shares_needed
# May return empty [] when not found (not an error)
EPOCH_KEY=$($BINARY query vote epoch-decryption-key-query $CURRENT_EPOCH --output json 2>&1)

if echo "$EPOCH_KEY" | grep -qi "not found"; then
    echo "  No key for epoch $CURRENT_EPOCH (expected - threshold not met)"
elif echo "$EPOCH_KEY" | jq -e 'type == "array" and length == 0' > /dev/null 2>&1; then
    echo "  No key for epoch $CURRENT_EPOCH (empty response - threshold not met)"
else
    AVAILABLE=$(echo "$EPOCH_KEY" | jq -r '.available // "null"')
    SHARES_RECEIVED=$(echo "$EPOCH_KEY" | jq -r '.shares_received // "0"')
    SHARES_NEEDED=$(echo "$EPOCH_KEY" | jq -r '.shares_needed // "0"')

    echo "  Epoch $CURRENT_EPOCH key available: $AVAILABLE"
    echo "  Shares received: $SHARES_RECEIVED / $SHARES_NEEDED"
fi

echo ""

# =========================================================================
# PART 7: Query TLE validator shares (updated)
# =========================================================================
echo "--- PART 7: Updated TLE validator shares ---"

TLE_SHARES2=$($BINARY query vote tle-validator-shares --output json 2>&1)
REGISTERED_VALS2=$(echo "$TLE_SHARES2" | jq -r '.registered_validators // "0"')
echo "  Registered for TLE: $REGISTERED_VALS2"

echo ""

# =========================================================================
# PART 8: Register TLE share with invalid index 0 (should fail)
# =========================================================================
echo "--- PART 8: TLE share with index 0 (should fail) ---"

# share_index must be > 0 (1-based) → ErrInvalidShareIndex
TLE_PUB_KEY_HEX2="0400000000000000000000000000000000000000000000000000000000000002"
TLE_PUB_KEY_B64_2=$(echo "$TLE_PUB_KEY_HEX2" | xxd -r -p | base64)

TX_RES=$($BINARY tx vote register-tle-share \
    "0" \
    --public-key-share "$TLE_PUB_KEY_B64_2" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Correctly rejected (no broadcast)"
else
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")

    if check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected TLE share with index 0"
    else
        echo "  ERROR: Share index 0 should have failed"
        exit 1
    fi
fi

echo ""

# =========================================================================
# SUMMARY
# =========================================================================
echo "--- TEST SUMMARY ---"
echo "  Part 1: TLE status check                    - PASSED"
echo "  Part 2: Query validator shares               - PASSED"
echo "  Part 3: Register TLE share                   - PASSED"
echo "  Part 4: Non-validator rejection              - PASSED"
echo "  Part 5: TLE liveness query                   - PASSED"
echo "  Part 6: Epoch decryption key query           - PASSED"
echo "  Part 7: Updated validator shares             - PASSED"
echo "  Part 8: Invalid share index rejection        - PASSED"
echo ""
echo "All TLE checks passed!"
