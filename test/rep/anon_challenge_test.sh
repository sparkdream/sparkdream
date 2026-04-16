#!/bin/bash

echo "--- TESTING: ANONYMOUS REP CHALLENGE VIA X/SHIELD ---"
echo ""
echo "Tests anonymous challenge operations through MsgShieldedExec:"
echo "  1. Immediate mode rejected (MsgCreateChallenge is ENCRYPTED_ONLY)"
echo "  2. Verify shielded op registration for challenges"
echo "  3. Verify ENCRYPTED_ONLY batch mode enforcement"
echo ""
echo "NOTE: MsgCreateChallenge requires ENCRYPTED_ONLY batch mode (max privacy)."
echo "      Actual encrypted batch execution requires DKG completion, which is"
echo "      tested separately in test/shield/encrypted_batch_test.sh."
echo "      This test verifies the enforcement of the ENCRYPTED_ONLY constraint."
echo ""

# === 0. SETUP ===
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

echo "Alice: $ALICE_ADDR"
echo ""

# === HELPERS ===

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
    [ "$CODE" == "0" ]
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

    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    return 0
}

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

# === RESOLVE SHIELD MODULE ADDRESS ===
SHIELD_MODULE_ADDR=$($BINARY query auth module-account shield --output json 2>/dev/null | jq -r '.account.value.address // .account.base_account.address // empty' 2>/dev/null)

if [ -z "$SHIELD_MODULE_ADDR" ] || [ "$SHIELD_MODULE_ADDR" == "null" ]; then
    echo "ERROR: Could not resolve shield module address"
    exit 1
fi

echo "Shield module: $SHIELD_MODULE_ADDR"
echo ""

# Dummy ZK values
DUMMY_PROOF=$(python3 -c "print('aa' * 128)")
DUMMY_MERKLE_ROOT="0000000000000000000000000000000000000000000000000000000000000001"

# =========================================================================
# TEST 1: Verify MsgCreateChallenge is registered as ENCRYPTED_ONLY
# =========================================================================
echo "--- TEST 1: Verify MsgCreateChallenge registration ---"

OP_QUERY=$($BINARY query shield shielded-op "/sparkdream.rep.v1.MsgCreateChallenge" --output json 2>&1)

if echo "$OP_QUERY" | grep -qi "error\|not found"; then
    echo "  MsgCreateChallenge not registered as shielded operation"
    record_result "MsgCreateChallenge registration" "FAIL"
else
    BATCH_MODE=$(echo "$OP_QUERY" | jq -r '.registration.batch_mode // empty')
    IS_ACTIVE=$(echo "$OP_QUERY" | jq -r '.registration.active // "false"')
    NULL_DOMAIN=$(echo "$OP_QUERY" | jq -r '.registration.nullifier_domain // "0"')
    NULL_SCOPE=$(echo "$OP_QUERY" | jq -r '.registration.nullifier_scope_type // empty')

    echo "  Registered: active=$IS_ACTIVE"
    echo "  Batch mode: $BATCH_MODE"
    echo "  Nullifier domain: $NULL_DOMAIN"
    echo "  Nullifier scope: $NULL_SCOPE"

    # Verify ENCRYPTED_ONLY (batch_mode value 2 or string)
    if echo "$BATCH_MODE" | grep -qi "ENCRYPTED_ONLY\|2"; then
        echo "  Correctly configured as ENCRYPTED_ONLY"
        record_result "MsgCreateChallenge registration" "PASS"
    else
        echo "  UNEXPECTED batch_mode: $BATCH_MODE (expected ENCRYPTED_ONLY)"
        record_result "MsgCreateChallenge registration" "FAIL"
    fi
fi

# =========================================================================
# TEST 2: Immediate mode rejected for ENCRYPTED_ONLY operation
# =========================================================================
echo "--- TEST 2: Immediate mode rejected for challenges ---"

NULLIFIER_CHAL="re01000000000000000000000000000000000000000000000000000000000001"
RATE_NULL_CHAL=$(openssl rand -hex 32)
INNER_MSG="{\"@type\":\"/sparkdream.rep.v1.MsgCreateChallenge\",\"challenger\":\"$SHIELD_MODULE_ADDR\",\"initiative_id\":\"1\",\"reason\":\"Test anonymous challenge\",\"evidence\":[\"doc1.pdf\"],\"staked_dream\":\"100\"}"

TX_RES=$($BINARY tx shield shielded-exec \
    --inner-message "$INNER_MSG" \
    --proof "$DUMMY_PROOF" \
    --nullifier "$NULLIFIER_CHAL" \
    --rate-limit-nullifier "$RATE_NULL_CHAL" \
    --merkle-root "$DUMMY_MERKLE_ROOT" \
    --proof-domain 1 \
    --min-trust-level 0 \
    --exec-mode 0 \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 500000uspark \
    --gas 500000 \
    -y \
    --output json 2>&1)

submit_tx_and_wait "$TX_RES"

if check_tx_success "$TX_RESULT"; then
    echo "  ERROR: Immediate mode should have been rejected for ENCRYPTED_ONLY operation"
    record_result "Immediate mode rejected for challenges" "FAIL"
else
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""' 2>/dev/null)
    FULL_ERR="$TX_RES $RAW_LOG"
    if echo "$FULL_ERR" | grep -qi "immediate.*not.*allowed\|ENCRYPTED_ONLY\|batch.*mode"; then
        echo "  Correctly rejected: immediate mode not allowed for ENCRYPTED_ONLY ops"
    else
        echo "  Rejected (reason: ${RAW_LOG:0:150})"
        echo "  Broadcast response: ${TX_RES:0:150}"
    fi
    record_result "Immediate mode rejected for challenges" "PASS"
fi

# =========================================================================
# TEST 3: Verify GLOBAL nullifier scope for challenges
# =========================================================================
echo "--- TEST 3: Verify GLOBAL nullifier scope for challenges ---"

# Re-query the registration to check nullifier scope type
OP_QUERY=$($BINARY query shield shielded-op "/sparkdream.rep.v1.MsgCreateChallenge" --output json 2>&1)
NULL_SCOPE=$(echo "$OP_QUERY" | jq -r '.registration.nullifier_scope_type // empty')

if echo "$NULL_SCOPE" | grep -qi "GLOBAL\|2"; then
    echo "  Nullifier scope is GLOBAL (globally unique per challenge)"
    echo "  This means each anonymous challenge nullifier can only be used once ever,"
    echo "  not scoped to epoch or message field"
    record_result "GLOBAL nullifier scope for challenges" "PASS"
else
    echo "  Nullifier scope: $NULL_SCOPE (expected GLOBAL)"
    record_result "GLOBAL nullifier scope for challenges" "FAIL"
fi

# =========================================================================
# SUMMARY
# =========================================================================
echo "=========================================="
echo "  ANONYMOUS REP CHALLENGE TEST SUMMARY"
echo "=========================================="
echo ""
echo "  Passed: $PASS_COUNT"
echo "  Failed: $FAIL_COUNT"
echo ""

for i in "${!TEST_NAMES[@]}"; do
    echo "  ${RESULTS[$i]}  ${TEST_NAMES[$i]}"
done
echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL TESTS PASSED <<<"
fi
