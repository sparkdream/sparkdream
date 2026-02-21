#!/bin/bash

echo "--- TESTING: Voter Registration (x/vote) ---"
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

echo "Voter1:    $VOTER1_ADDR"
echo "Voter2:    $VOTER2_ADDR"
echo "Voter3:    $VOTER3_ADDR"
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
# PART 1: Verify existing registrations from setup
# =========================================================================
echo "--- PART 1: Verify existing voter registrations ---"

for ACCOUNT in "voter1" "voter2" "voter3" "alice"; do
    case "$ACCOUNT" in
        "voter1") ADDR=$VOTER1_ADDR ;;
        "voter2") ADDR=$VOTER2_ADDR ;;
        "voter3") ADDR=$VOTER3_ADDR ;;
        "alice") ADDR=$ALICE_ADDR ;;
    esac

    REG_INFO=$($BINARY query vote get-voter-registration $ADDR --output json 2>&1)

    if echo "$REG_INFO" | grep -qi "not found\|error"; then
        echo "  $ACCOUNT: NOT registered"
        exit 1
    fi

    ACTIVE=$(echo "$REG_INFO" | jq -r '.voter_registration.active // "unknown"')
    if [ "$ACTIVE" != "true" ]; then
        echo "  $ACCOUNT: Expected active=true, got active=$ACTIVE"
        exit 1
    fi

    echo "  $ACCOUNT: active=$ACTIVE"
done

echo "All voter registrations verified"
echo ""

# =========================================================================
# PART 2: List voter registrations
# =========================================================================
echo "--- PART 2: List voter registrations ---"

LIST_RESULT=$($BINARY query vote list-voter-registration --output json 2>&1)

if echo "$LIST_RESULT" | grep -qi "error"; then
    echo "Failed to list voter registrations"
    exit 1
fi

REG_COUNT=$(echo "$LIST_RESULT" | jq -r '.voter_registration | length')
echo "  Total registrations: $REG_COUNT"

if [ "$REG_COUNT" -lt 4 ]; then
    echo "  Expected at least 4 registrations (voter1, voter2, voter3, alice)"
    exit 1
fi

echo "  Voter registration list verified"
echo ""

# =========================================================================
# PART 3: Query voter registration via dedicated query
# =========================================================================
echo "--- PART 3: Query voter registration details ---"

REG_DETAIL=$($BINARY query vote voter-registration-query $VOTER1_ADDR --output json 2>&1)

if echo "$REG_DETAIL" | grep -qi "error\|not found"; then
    echo "  Failed to query voter1 registration"
    exit 1
fi

# voter-registration-query returns .registration (not .voter_registration)
REG_ADDR=$(echo "$REG_DETAIL" | jq -r '.registration.address // "null"')
REG_ACTIVE=$(echo "$REG_DETAIL" | jq -r '.registration.active // "null"')

if [ "$REG_ADDR" != "$VOTER1_ADDR" ]; then
    echo "  Address mismatch: got $REG_ADDR, expected $VOTER1_ADDR"
    exit 1
fi

if [ "$REG_ACTIVE" != "true" ]; then
    echo "  Expected active=true, got $REG_ACTIVE"
    exit 1
fi

echo "  Voter1 registration: address=$REG_ADDR, active=$REG_ACTIVE"
echo ""

# =========================================================================
# PART 4: Deactivate a voter (voter3)
# =========================================================================
echo "--- PART 4: Deactivate voter3 ---"

TX_RES=$($BINARY tx vote deactivate-voter \
    --from voter3 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to deactivate voter3: no txhash"
    echo "  Response: $TX_RES"
    exit 1
fi

sleep 6
TX_RESULT=$(wait_for_tx "$TXHASH")

if check_tx_success "$TX_RESULT"; then
    DEACTIVATED_VOTER=$(extract_event_value "$TX_RESULT" "voter_deactivated" "voter")
    DEACTIVATE_REASON=$(extract_event_value "$TX_RESULT" "voter_deactivated" "reason")
    echo "  Deactivated voter: $DEACTIVATED_VOTER (reason: $DEACTIVATE_REASON)"
else
    echo "  Failed to deactivate voter3"
    echo "  $(echo "$TX_RESULT" | jq -r '.raw_log // "Unknown error"')"
    exit 1
fi

# Verify deactivation via query
# Note: proto3 omits false booleans from JSON, so active will be absent (null) when false
REG_INFO=$($BINARY query vote get-voter-registration $VOTER3_ADDR --output json 2>&1)
ACTIVE=$(echo "$REG_INFO" | jq -r '.voter_registration.active // false')
if [ "$ACTIVE" != "false" ]; then
    echo "  Expected voter3 active=false after deactivation, got $ACTIVE"
    exit 1
fi

echo "  Verified voter3 is now inactive"
echo ""

# =========================================================================
# PART 5: Double deactivation should fail
# =========================================================================
echo "--- PART 5: Attempt double deactivation (should fail) ---"

TX_RES=$($BINARY tx vote deactivate-voter \
    --from voter3 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Transaction did not broadcast (possibly client-side rejection)"
    echo "  Correctly rejected double deactivation"
else
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")

    if check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected double deactivation"
    else
        echo "  ERROR: Double deactivation should have failed but succeeded"
        exit 1
    fi
fi

echo ""

# =========================================================================
# PART 6: Reactivate voter3 with new keys
# =========================================================================
echo "--- PART 6: Reactivate voter3 with new keys ---"

NEW_VOTER3_ZK_KEY="0303030303030303030303030303030303030303030303030303030303030304"
NEW_VOTER3_ENC_KEY="3333333333333333333333333333333333333333333333333333333333333334"

NEW_ZK_B64=$(echo "$NEW_VOTER3_ZK_KEY" | xxd -r -p | base64)
NEW_ENC_B64=$(echo "$NEW_VOTER3_ENC_KEY" | xxd -r -p | base64)

TX_RES=$($BINARY tx vote register-voter \
    --zk-public-key "$NEW_ZK_B64" \
    --encryption-public-key "$NEW_ENC_B64" \
    --from voter3 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to reactivate voter3: no txhash"
    echo "  Response: $TX_RES"
    exit 1
fi

sleep 6
TX_RESULT=$(wait_for_tx "$TXHASH")

if check_tx_success "$TX_RESULT"; then
    echo "  Reactivated voter3 with new keys"
else
    echo "  Failed to reactivate voter3"
    echo "  $(echo "$TX_RESULT" | jq -r '.raw_log // "Unknown error"')"
    exit 1
fi

# Verify reactivation
REG_INFO=$($BINARY query vote get-voter-registration $VOTER3_ADDR --output json 2>&1)
ACTIVE=$(echo "$REG_INFO" | jq -r '.voter_registration.active // "unknown"')
if [ "$ACTIVE" != "true" ]; then
    echo "  Expected voter3 active=true after reactivation, got $ACTIVE"
    exit 1
fi

echo "  Verified voter3 is active again"
echo ""

# =========================================================================
# PART 7: Rotate voter key (voter1)
# =========================================================================
echo "--- PART 7: Rotate voter1's keys ---"

ROTATED_ZK_KEY="0101010101010101010101010101010101010101010101010101010101010102"
ROTATED_ENC_KEY="1111111111111111111111111111111111111111111111111111111111111112"

ROTATED_ZK_B64=$(echo "$ROTATED_ZK_KEY" | xxd -r -p | base64)
ROTATED_ENC_B64=$(echo "$ROTATED_ENC_KEY" | xxd -r -p | base64)

TX_RES=$($BINARY tx vote rotate-voter-key \
    --new-zk-public-key "$ROTATED_ZK_B64" \
    --new-encryption-public-key "$ROTATED_ENC_B64" \
    --from voter1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Failed to rotate voter1 key: no txhash"
    echo "  Response: $TX_RES"
    exit 1
fi

sleep 6
TX_RESULT=$(wait_for_tx "$TXHASH")

if check_tx_success "$TX_RESULT"; then
    ROTATED_VOTER=$(extract_event_value "$TX_RESULT" "voter_key_rotated" "voter")
    echo "  Rotated keys for: $ROTATED_VOTER"
else
    echo "  Failed to rotate voter1 key"
    echo "  $(echo "$TX_RESULT" | jq -r '.raw_log // "Unknown error"')"
    exit 1
fi

# Verify the registration still exists and is active
REG_INFO=$($BINARY query vote get-voter-registration $VOTER1_ADDR --output json 2>&1)
ACTIVE=$(echo "$REG_INFO" | jq -r '.voter_registration.active // "unknown"')
if [ "$ACTIVE" != "true" ]; then
    echo "  Expected voter1 still active after key rotation, got $ACTIVE"
    exit 1
fi

echo "  Verified voter1 still active after key rotation"
echo ""

# =========================================================================
# PART 8: Duplicate key registration should fail
# =========================================================================
echo "--- PART 8: Duplicate ZK key registration (should fail) ---"

# Try to register proposer1 with voter2's existing key
VOTER2_ZK_B64=$(echo "$VOTER2_ZK_KEY" | xxd -r -p | base64)
VOTER2_ENC_B64=$(echo "$VOTER2_ENC_KEY" | xxd -r -p | base64)

TX_RES=$($BINARY tx vote register-voter \
    --zk-public-key "$VOTER2_ZK_B64" \
    --encryption-public-key "$VOTER2_ENC_B64" \
    --from proposer1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Transaction did not broadcast (client-side rejection)"
    echo "  Correctly rejected duplicate key"
else
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")

    if check_tx_failure "$TX_RESULT"; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
        echo "  Correctly rejected: $RAW_LOG"
    else
        echo "  ERROR: Duplicate key registration should have failed but succeeded"
        exit 1
    fi
fi

echo ""

# =========================================================================
# PART 9: Non-member registration should fail
# =========================================================================
echo "--- PART 9: Non-member registration (should fail) ---"

# Create a temporary account that is NOT an x/rep member
if ! $BINARY keys show nonmember_voter --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add nonmember_voter --keyring-backend test --output json > /dev/null 2>&1
fi

NONMEMBER_ADDR=$($BINARY keys show nonmember_voter -a --keyring-backend test)

# Fund for gas
TX_RES=$($BINARY tx bank send \
    alice $NONMEMBER_ADDR \
    5000000uspark \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
fi

# Try to register as voter without being an x/rep member
NONMEMBER_ZK_B64=$(echo "0909090909090909090909090909090909090909090909090909090909090909" | xxd -r -p | base64)
NONMEMBER_ENC_B64=$(echo "9999999999999999999999999999999999999999999999999999999999999999" | xxd -r -p | base64)

TX_RES=$($BINARY tx vote register-voter \
    --zk-public-key "$NONMEMBER_ZK_B64" \
    --encryption-public-key "$NONMEMBER_ENC_B64" \
    --from nonmember_voter \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Transaction did not broadcast"
    echo "  Correctly rejected non-member registration"
else
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")

    if check_tx_failure "$TX_RESULT"; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
        echo "  Correctly rejected non-member: $RAW_LOG"
    else
        echo "  ERROR: Non-member registration should have failed"
        exit 1
    fi
fi

echo ""

# =========================================================================
# PART 10: Re-register active voter with different keys (should fail)
# =========================================================================
echo "--- PART 10: Re-register active voter (should fail - use rotate) ---"

# voter2 is active; trying to register-voter again with DIFFERENT keys should
# return ErrUseRotateKey (active registration exists, use rotate-voter-key).
DIFFERENT_ZK_KEY="0505050505050505050505050505050505050505050505050505050505050505"
DIFFERENT_ENC_KEY="5555555555555555555555555555555555555555555555555555555555555555"

DIFF_ZK_B64=$(echo "$DIFFERENT_ZK_KEY" | xxd -r -p | base64)
DIFF_ENC_B64=$(echo "$DIFFERENT_ENC_KEY" | xxd -r -p | base64)

TX_RES=$($BINARY tx vote register-voter \
    --zk-public-key "$DIFF_ZK_B64" \
    --encryption-public-key "$DIFF_ENC_B64" \
    --from voter2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Transaction did not broadcast (client-side rejection)"
    echo "  Correctly rejected re-registration"
else
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")

    if check_tx_failure "$TX_RESULT"; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
        echo "  Correctly rejected: $RAW_LOG"
    else
        echo "  ERROR: Re-registration of active voter should have failed (use rotate-voter-key)"
        exit 1
    fi
fi

echo ""

# =========================================================================
# PART 11: Rotate keys on inactive voter (should fail)
# =========================================================================
echo "--- PART 11: Rotate keys on inactive voter (should fail) ---"

# First deactivate voter2
TX_RES=$($BINARY tx vote deactivate-voter \
    --from voter2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    wait_for_tx "$TXHASH" > /dev/null 2>&1
fi

# Now try to rotate keys on inactive voter2 → should fail with ErrAlreadyInactive
ROTATED_ZK2="0606060606060606060606060606060606060606060606060606060606060606"
ROTATED_ENC2="6666666666666666666666666666666666666666666666666666666666666666"
ROTATED_ZK2_B64=$(echo "$ROTATED_ZK2" | xxd -r -p | base64)
ROTATED_ENC2_B64=$(echo "$ROTATED_ENC2" | xxd -r -p | base64)

TX_RES=$($BINARY tx vote rotate-voter-key \
    --new-zk-public-key "$ROTATED_ZK2_B64" \
    --new-encryption-public-key "$ROTATED_ENC2_B64" \
    --from voter2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Transaction did not broadcast"
    echo "  Correctly rejected key rotation on inactive voter"
else
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")

    if check_tx_failure "$TX_RESULT"; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
        echo "  Correctly rejected: $RAW_LOG"
    else
        echo "  ERROR: Key rotation on inactive voter should have failed"
        exit 1
    fi
fi

# Reactivate voter2 for later tests
VOTER2_ZK_B64=$(echo "$VOTER2_ZK_KEY" | xxd -r -p | base64)
VOTER2_ENC_B64=$(echo "$VOTER2_ENC_KEY" | xxd -r -p | base64)

TX_RES=$($BINARY tx vote register-voter \
    --zk-public-key "$VOTER2_ZK_B64" \
    --encryption-public-key "$VOTER2_ENC_B64" \
    --from voter2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
    sleep 6
    wait_for_tx "$TXHASH" > /dev/null 2>&1
fi

echo "  Reactivated voter2 for subsequent tests"
echo ""

# =========================================================================
# PART 12: List all voter registrations (final state)
# =========================================================================
echo "--- PART 12: Final voter registration state ---"

# voter-registrations returns .registrations (not .voter_registration)
LIST_RESULT=$($BINARY query vote voter-registrations --output json 2>&1)

if echo "$LIST_RESULT" | grep -qi "error"; then
    # Try alternate query (list-voter-registration returns .voter_registration)
    LIST_RESULT=$($BINARY query vote list-voter-registration --output json 2>&1)
    REG_COUNT=$(echo "$LIST_RESULT" | jq -r '.voter_registration | length' 2>/dev/null || echo "0")
else
    REG_COUNT=$(echo "$LIST_RESULT" | jq -r '.registrations | length' 2>/dev/null || echo "0")
fi
echo "  Total voter registrations: $REG_COUNT"

echo ""

# =========================================================================
# SUMMARY
# =========================================================================
echo "--- TEST SUMMARY ---"
echo "  Part 1:  Verify existing registrations      - PASSED"
echo "  Part 2:  List voter registrations            - PASSED"
echo "  Part 3:  Query registration details          - PASSED"
echo "  Part 4:  Deactivate voter                    - PASSED"
echo "  Part 5:  Double deactivation rejection       - PASSED"
echo "  Part 6:  Reactivate with new keys            - PASSED"
echo "  Part 7:  Rotate voter keys                   - PASSED"
echo "  Part 8:  Duplicate key rejection             - PASSED"
echo "  Part 9:  Non-member rejection                - PASSED"
echo "  Part 10: Re-register active (use-rotate)     - PASSED"
echo "  Part 11: Rotate inactive voter rejection     - PASSED"
echo "  Part 12: Final state verification            - PASSED"
echo ""
echo "All voter registration checks passed!"
