#!/bin/bash

echo "--- TESTING: x/service REGISTER (happy path + validation rejections) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    echo "   Run: bash setup_test_accounts.sh"
    exit 1
fi
source "$SCRIPT_DIR/.test_env"

echo "Operator 1:           $OPERATOR1_ADDR"
echo "Operator 2:           $OPERATOR2_ADDR"
echo "Commons policy:       $COMMONS_POLICY"
echo "Service type:         $TEST_SERVICE_TYPE"
echo ""

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
        echo "Transaction failed with code: $CODE"
        echo "$TX_RESULT" | jq -r '.raw_log'
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

# Submit a tx, then return the tx-result JSON. Sets global TX_RESULT.
# Returns 0 on submission acceptance (regardless of execution success).
submit_and_wait() {
    local TX_RES="$1"
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        TX_RESULT="$TX_RES"
        return 1
    fi
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
}

# MsgRegisterOperator.metadata is `bytes` in proto, so the CLI requires
# base64-encoded input and proto JSON renders the stored value as base64
# too. b64() encodes a plain string for CLI/wire format.
b64() {
    printf '%s' "$1" | base64 -w0
}

META_OP1_V1=$(b64 "operator1-metadata-v1")
META_OP1_V2=$(b64 "operator1-metadata-v2")
META_OP2_SELF=$(b64 "operator2-self-controller")
META_OP2_BAD_CTRL=$(b64 "operator2-bad-controller")
META_OP2_BAD_DENOM=$(b64 "operator2-bad-denom")
META_OP2_LOW_BOND=$(b64 "operator2-low-bond")
META_OP2_V1=$(b64 "operator2-metadata-v1")

# ========================================================================
# PART 1: HAPPY PATH — register operator1 with valid args
# ========================================================================
echo "--- PART 1: HAPPY PATH ---"

MIN_BOND_AMT=$($BINARY query service service-type "$TEST_SERVICE_TYPE" --output json | jq -r '.config.min_bond.amount')
if [ -z "$MIN_BOND_AMT" ] || [ "$MIN_BOND_AMT" == "null" ]; then
    echo "ERROR: failed to read min_bond from service-type config"
    exit 1
fi
BOND_COIN="${MIN_BOND_AMT}uspark"
echo "Reading min_bond: $BOND_COIN"

echo "Registering operator1 against $TEST_SERVICE_TYPE..."
TX_RES=$($BINARY tx service register-operator \
    "$TEST_SERVICE_TYPE" "$COMMONS_POLICY" "$BOND_COIN" "$META_OP1_V1" \
    --from operator1 --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000uspark -y --output json 2>&1)
submit_and_wait "$TX_RES"
if ! check_tx_success "$TX_RESULT"; then
    echo "FAIL: happy-path register-operator failed"
    exit 1
fi
echo "OK: register-operator accepted"

# Verify operator record + ACTIVE status
OP_INFO=$($BINARY query service operator "$OPERATOR1_ADDR" "$TEST_SERVICE_TYPE" --output json 2>&1)
STATUS=$(echo "$OP_INFO" | jq -r '.operator.status')
STORED_BOND=$(echo "$OP_INFO" | jq -r '.operator.bond.amount')
STORED_CTRL=$(echo "$OP_INFO" | jq -r '.operator.controller')
STORED_META=$(echo "$OP_INFO" | jq -r '.operator.metadata')

if [ "$STATUS" != "OPERATOR_STATUS_ACTIVE" ]; then
    echo "FAIL: expected status OPERATOR_STATUS_ACTIVE, got $STATUS"
    echo "$OP_INFO"
    exit 1
fi
if [ "$STORED_BOND" != "$MIN_BOND_AMT" ]; then
    echo "FAIL: expected bond $MIN_BOND_AMT, got $STORED_BOND"
    exit 1
fi
if [ "$STORED_CTRL" != "$COMMONS_POLICY" ]; then
    echo "FAIL: expected controller $COMMONS_POLICY, got $STORED_CTRL"
    exit 1
fi
if [ "$STORED_META" != "$META_OP1_V1" ]; then
    echo "FAIL: expected metadata $META_OP1_V1 (base64 of operator1-metadata-v1), got $STORED_META"
    exit 1
fi
echo "OK: operator record verified (status=$STATUS bond=$STORED_BOND controller=$STORED_CTRL)"

# Also verify the OperatorsByController secondary index hits.
BY_CTRL=$($BINARY query service operators-by-controller "$COMMONS_POLICY" --output json 2>&1)
MATCHES=$(echo "$BY_CTRL" | jq -r --arg op "$OPERATOR1_ADDR" '.operators | map(select(.address == $op)) | length')
if [ "$MATCHES" -lt 1 ]; then
    echo "FAIL: operators-by-controller did not return operator1"
    echo "$BY_CTRL"
    exit 1
fi
echo "OK: operators-by-controller index returns operator1"
echo ""

# ========================================================================
# PART 2: REJECTION — duplicate registration on same (op, service_type)
# ========================================================================
echo "--- PART 2: REJECTION — duplicate registration ---"

TX_RES=$($BINARY tx service register-operator \
    "$TEST_SERVICE_TYPE" "$COMMONS_POLICY" "$BOND_COIN" "$META_OP1_V2" \
    --from operator1 --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000uspark -y --output json 2>&1)
submit_and_wait "$TX_RES"
if ! check_tx_failure "$TX_RESULT"; then
    echo "FAIL: duplicate register-operator should have been rejected"
    exit 1
fi
RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log')
echo "OK: duplicate rejected (raw_log: $(echo "$RAW_LOG" | head -c 200)...)"
echo ""

# ========================================================================
# PART 3: REJECTION — self-controller (creator == controller)
# ========================================================================
echo "--- PART 3: REJECTION — self-controller ---"

TX_RES=$($BINARY tx service register-operator \
    "$TEST_SERVICE_TYPE" "$OPERATOR2_ADDR" "$BOND_COIN" "$META_OP2_SELF" \
    --from operator2 --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000uspark -y --output json 2>&1)
submit_and_wait "$TX_RES"
if ! check_tx_failure "$TX_RESULT"; then
    echo "FAIL: self-controller register-operator should have been rejected"
    exit 1
fi
echo "OK: self-controller rejected"
echo ""

# ========================================================================
# PART 4: REJECTION — non-group controller (EOA)
# ========================================================================
echo "--- PART 4: REJECTION — non-group controller (alice EOA) ---"

# Alice is a real user account, not a x/commons group policy. The
# IsGroupPolicyAddress check must reject.
TX_RES=$($BINARY tx service register-operator \
    "$TEST_SERVICE_TYPE" "$ALICE_ADDR" "$BOND_COIN" "$META_OP2_BAD_CTRL" \
    --from operator2 --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000uspark -y --output json 2>&1)
submit_and_wait "$TX_RES"
if ! check_tx_failure "$TX_RESULT"; then
    echo "FAIL: non-group controller register-operator should have been rejected"
    exit 1
fi
echo "OK: non-group controller rejected"
echo ""

# ========================================================================
# PART 5: REJECTION — bond denom mismatch (try a non-existent denom)
# ========================================================================
echo "--- PART 5: REJECTION — bond denom mismatch ---"

# Use "udream" which is not uspark; this MUST be rejected even before
# the bank send because the proto-level check matches BondDenom.
TX_RES=$($BINARY tx service register-operator \
    "$TEST_SERVICE_TYPE" "$COMMONS_POLICY" "${MIN_BOND_AMT}udream" "$META_OP2_BAD_DENOM" \
    --from operator2 --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000uspark -y --output json 2>&1)
submit_and_wait "$TX_RES"
if ! check_tx_failure "$TX_RESULT"; then
    echo "FAIL: bond denom mismatch register-operator should have been rejected"
    exit 1
fi
echo "OK: bond denom mismatch rejected"
echo ""

# ========================================================================
# PART 6: REJECTION — insufficient bond (min_bond - 1)
# ========================================================================
echo "--- PART 6: REJECTION — insufficient bond ---"

LOW_BOND_AMT=$((MIN_BOND_AMT - 1))
TX_RES=$($BINARY tx service register-operator \
    "$TEST_SERVICE_TYPE" "$COMMONS_POLICY" "${LOW_BOND_AMT}uspark" "$META_OP2_LOW_BOND" \
    --from operator2 --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000uspark -y --output json 2>&1)
submit_and_wait "$TX_RES"
if ! check_tx_failure "$TX_RESULT"; then
    echo "FAIL: insufficient-bond register-operator should have been rejected"
    exit 1
fi
echo "OK: insufficient bond rejected"
echo ""

# ========================================================================
# PART 7: HAPPY PATH — register operator2 (independent op, ACTIVE)
# ========================================================================
echo "--- PART 7: register operator2 (parallel ACTIVE record) ---"

TX_RES=$($BINARY tx service register-operator \
    "$TEST_SERVICE_TYPE" "$COMMONS_POLICY" "$BOND_COIN" "$META_OP2_V1" \
    --from operator2 --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000uspark -y --output json 2>&1)
submit_and_wait "$TX_RES"
if ! check_tx_success "$TX_RESULT"; then
    echo "FAIL: register operator2 failed"
    exit 1
fi

OP2_STATUS=$($BINARY query service operator "$OPERATOR2_ADDR" "$TEST_SERVICE_TYPE" --output json | jq -r '.operator.status')
if [ "$OP2_STATUS" != "OPERATOR_STATUS_ACTIVE" ]; then
    echo "FAIL: operator2 should be ACTIVE, got $OP2_STATUS"
    exit 1
fi
echo "OK: operator2 ACTIVE"
echo ""

# ========================================================================
# Summary check — operators-by-service-type returns both records
# ========================================================================
echo "--- VERIFY operators-by-service-type index ---"
BY_TYPE=$($BINARY query service operators-by-service-type "$TEST_SERVICE_TYPE" --output json 2>&1)
COUNT_BY_TYPE=$(echo "$BY_TYPE" | jq -r '.operators | length')
if [ "$COUNT_BY_TYPE" -lt 2 ]; then
    echo "FAIL: expected >=2 operators for $TEST_SERVICE_TYPE, got $COUNT_BY_TYPE"
    echo "$BY_TYPE"
    exit 1
fi
echo "OK: operators-by-service-type returned $COUNT_BY_TYPE operators"
echo ""

echo "=================================================="
echo ">>> REGISTER TESTS: PASSED <<<"
echo "=================================================="
exit 0
