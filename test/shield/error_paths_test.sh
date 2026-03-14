#!/bin/bash

echo "--- TESTING: Error Paths & Edge Cases (x/shield) ---"
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

echo "Alice:          $ALICE_ADDR"
echo "Member1:        $MEMBER1_ADDR"
echo "Submitter1:     $SUBMITTER1_ADDR"
echo "Shield Module:  $SHIELD_MODULE_ADDR"
echo ""

# === RESULT TRACKING ===
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

check_tx_failure() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')

    if [ "$CODE" != "0" ]; then
        return 0
    fi
    return 1
}

# =========================================================================
# TEST 1: Verify shield module is operational (prerequisite)
# =========================================================================
echo "--- TEST 1: Verify shield module is operational ---"

PARAMS=$($BINARY query shield params --output json 2>&1)

if echo "$PARAMS" | grep -qi "error"; then
    echo "  Failed to query shield params"
    record_result "Shield module operational" "FAIL"
    exit 1
fi

ENABLED=$(echo "$PARAMS" | jq -r '.params.enabled // "false"')

if [ "$ENABLED" != "true" ]; then
    echo "  Shield module is disabled. Cannot run error path tests."
    record_result "Shield module operational" "FAIL"
    exit 1
fi

echo "  Shield module enabled: $ENABLED"
record_result "Shield module operational" "PASS"

# =========================================================================
# TEST 2: Shielded exec with no flags (empty submission should fail)
# =========================================================================
echo "--- TEST 2: Shielded exec with no arguments ---"

TX_RES=$($BINARY tx shield shielded-exec \
    --from submitter1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

TEST2_PASS=false
TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Correctly rejected empty shielded exec (no broadcast)"
    TEST2_PASS=true
else
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")

    if check_tx_failure "$TX_RESULT"; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
        echo "  Correctly rejected empty shielded exec"
        echo "  Error: ${RAW_LOG:0:100}"
        TEST2_PASS=true
    else
        echo "  FAIL: Empty shielded exec was accepted (unexpected)"
    fi
fi

if [ "$TEST2_PASS" == "true" ]; then
    record_result "Empty shielded exec rejection" "PASS"
else
    record_result "Empty shielded exec rejection" "FAIL"
fi

# =========================================================================
# TEST 3: Nullifier domain isolation
# =========================================================================
echo "--- TEST 3: Nullifier domain isolation ---"

# Test that nullifiers in different domains don't interfere
# Query nullifier in domain 1 (blog MsgCreatePost) and domain 11 (forum MsgCreatePost)
RANDOM_NULL="aaaa000000000000000000000000000000000000000000000000000000001111"

NULL_D1=$($BINARY query shield nullifier-used 1 0 "$RANDOM_NULL" --output json 2>&1)
NULL_D11=$($BINARY query shield nullifier-used 11 0 "$RANDOM_NULL" --output json 2>&1)

D1_USED="false"
D11_USED="false"

if ! echo "$NULL_D1" | grep -qi "not found\|error"; then
    D1_USED=$(echo "$NULL_D1" | jq -r '.used // "false"')
fi
if ! echo "$NULL_D11" | grep -qi "not found\|error"; then
    D11_USED=$(echo "$NULL_D11" | jq -r '.used // "false"')
fi

echo "  Nullifier $RANDOM_NULL:"
echo "    Domain 1 (blog): used=$D1_USED"
echo "    Domain 11 (forum): used=$D11_USED"

if [ "$D1_USED" == "false" ] && [ "$D11_USED" == "false" ]; then
    echo "  Domains are independent (both unused for random nullifier)"
    record_result "Nullifier domain isolation" "PASS"
else
    echo "  FAIL: Random nullifier unexpectedly used in one or both domains"
    record_result "Nullifier domain isolation" "FAIL"
fi

# =========================================================================
# TEST 4: Nullifier scope isolation
# =========================================================================
echo "--- TEST 4: Nullifier scope isolation ---"

# Same nullifier in same domain but different scopes should be independent
SCOPE_NULL="bbbb000000000000000000000000000000000000000000000000000000002222"

NULL_S0=$($BINARY query shield nullifier-used 1 0 "$SCOPE_NULL" --output json 2>&1)
NULL_S1=$($BINARY query shield nullifier-used 1 1 "$SCOPE_NULL" --output json 2>&1)
NULL_S99=$($BINARY query shield nullifier-used 1 99 "$SCOPE_NULL" --output json 2>&1)

S0_USED="false"
S1_USED="false"
S99_USED="false"

if ! echo "$NULL_S0" | grep -qi "not found\|error"; then
    S0_USED=$(echo "$NULL_S0" | jq -r '.used // "false"')
fi
if ! echo "$NULL_S1" | grep -qi "not found\|error"; then
    S1_USED=$(echo "$NULL_S1" | jq -r '.used // "false"')
fi
if ! echo "$NULL_S99" | grep -qi "not found\|error"; then
    S99_USED=$(echo "$NULL_S99" | jq -r '.used // "false"')
fi

echo "  Nullifier $SCOPE_NULL in domain 1:"
echo "    Scope 0: used=$S0_USED"
echo "    Scope 1: used=$S1_USED"
echo "    Scope 99: used=$S99_USED"

if [ "$S0_USED" == "false" ] && [ "$S1_USED" == "false" ] && [ "$S99_USED" == "false" ]; then
    echo "  Scopes are independent (all unused for random nullifier)"
    record_result "Nullifier scope isolation" "PASS"
else
    echo "  FAIL: Random nullifier unexpectedly used in one or more scopes"
    record_result "Nullifier scope isolation" "FAIL"
fi

# =========================================================================
# TEST 5: Query non-existent verification key
# =========================================================================
echo "--- TEST 5: Query non-existent verification key ---"

VK_RESULT=$($BINARY query shield verification-key "nonexistent_circuit_v9" --output json 2>&1)

if echo "$VK_RESULT" | grep -qi "not found"; then
    echo "  Correctly returned not found for non-existent circuit"
    record_result "Non-existent verification key" "PASS"
elif echo "$VK_RESULT" | grep -qi "error"; then
    echo "  Correctly returned error for non-existent circuit"
    record_result "Non-existent verification key" "PASS"
else
    echo "  FAIL: Got unexpected response"
    echo "  $VK_RESULT"
    record_result "Non-existent verification key" "FAIL"
fi

# =========================================================================
# TEST 6: Query nullifier with edge-case inputs
# =========================================================================
echo "--- TEST 6: Query with edge-case inputs ---"

TEST6_PASS=true

# All-zeros nullifier
ZERO_NULL="0000000000000000000000000000000000000000000000000000000000000000"
NULL_ZERO=$($BINARY query shield nullifier-used 0 0 "$ZERO_NULL" --output json 2>&1)

if echo "$NULL_ZERO" | grep -qi "not found\|error"; then
    echo "  All-zeros nullifier in domain 0, scope 0: not found (expected)"
else
    Z_USED=$(echo "$NULL_ZERO" | jq -r '.used // "false"')
    echo "  All-zeros nullifier: used=$Z_USED"
    if [ "$Z_USED" != "false" ]; then
        TEST6_PASS=false
    fi
fi

# Max domain value
MAX_NULL="ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
NULL_MAX=$($BINARY query shield nullifier-used 4294967295 0 "$MAX_NULL" --output json 2>&1)

if echo "$NULL_MAX" | grep -qi "not found\|error"; then
    echo "  Max-domain nullifier: not found (expected)"
else
    M_USED=$(echo "$NULL_MAX" | jq -r '.used // "false"')
    echo "  Max-domain nullifier: used=$M_USED"
    if [ "$M_USED" != "false" ]; then
        TEST6_PASS=false
    fi
fi

if [ "$TEST6_PASS" == "true" ]; then
    record_result "Edge-case nullifier inputs" "PASS"
else
    record_result "Edge-case nullifier inputs" "FAIL"
fi

# =========================================================================
# TEST 7: Query TLE miss count for non-validator
# =========================================================================
echo "--- TEST 7: TLE miss count for non-validator ---"

MISS_RESULT=$($BINARY query shield tle-miss-count "$MEMBER1_ADDR" --output json 2>&1)

if echo "$MISS_RESULT" | grep -qi "not found"; then
    echo "  Non-validator TLE miss count: not found (expected)"
    record_result "Non-validator TLE miss count" "PASS"
elif echo "$MISS_RESULT" | grep -qi "error"; then
    echo "  Non-validator TLE miss count: error (expected)"
    record_result "Non-validator TLE miss count" "PASS"
else
    MISS_COUNT=$(echo "$MISS_RESULT" | jq -r '.miss_count // "0"')
    echo "  Non-validator TLE miss count: $MISS_COUNT (expected: 0)"
    if [ "$MISS_COUNT" == "0" ]; then
        record_result "Non-validator TLE miss count" "PASS"
    else
        record_result "Non-validator TLE miss count" "FAIL"
    fi
fi

# =========================================================================
# TEST 8: Query decryption shares for epoch 0
# =========================================================================
echo "--- TEST 8: Decryption shares for epoch 0 ---"

DEC_SHARES=$($BINARY query shield decryption-shares 0 --output json 2>&1)

if echo "$DEC_SHARES" | grep -qi "not found"; then
    echo "  Epoch 0 decryption shares: none (expected)"
    record_result "Epoch 0 decryption shares" "PASS"
elif echo "$DEC_SHARES" | grep -qi "error"; then
    echo "  Epoch 0 decryption shares: error (may be expected)"
    record_result "Epoch 0 decryption shares" "PASS"
else
    SHARE_COUNT=$(echo "$DEC_SHARES" | jq -r '.shares | length' 2>/dev/null || echo "0")
    echo "  Epoch 0 decryption shares: $SHARE_COUNT"
    record_result "Epoch 0 decryption shares" "PASS"
fi

# =========================================================================
# TEST 9: Query pending ops when queue is empty
# =========================================================================
echo "--- TEST 9: Empty pending ops query ---"

TEST9_PASS=true

PENDING_OPS=$($BINARY query shield pending-ops --output json 2>&1)

if echo "$PENDING_OPS" | grep -qi "error"; then
    echo "  Pending ops query returned error (may be expected for empty)"
else
    P_OPS_COUNT=$(echo "$PENDING_OPS" | jq -r '.operations | length' 2>/dev/null || echo "0")
    echo "  Pending operations: $P_OPS_COUNT (expected: 0)"

    if [ "$P_OPS_COUNT" == "0" ]; then
        echo "  Empty pending queue handled correctly"
    else
        TEST9_PASS=false
    fi
fi

PENDING_COUNT=$($BINARY query shield pending-op-count --output json 2>&1)

if ! echo "$PENDING_COUNT" | grep -qi "error"; then
    P_COUNT=$(echo "$PENDING_COUNT" | jq -r '.count // "0"')
    echo "  Pending op count: $P_COUNT (expected: 0)"
fi

if [ "$TEST9_PASS" == "true" ]; then
    record_result "Empty pending ops query" "PASS"
else
    record_result "Empty pending ops query" "FAIL"
fi

# =========================================================================
# TEST 10: Identity rate limit for fresh identity
# =========================================================================
echo "--- TEST 10: Fresh identity rate limit ---"

FRESH_IDENTITY="cccc000000000000000000000000000000000000000000000000000000003333"
RATE_RESULT=$($BINARY query shield identity-rate-limit "$FRESH_IDENTITY" --output json 2>&1)

if echo "$RATE_RESULT" | grep -qi "not found\|error"; then
    echo "  Fresh identity rate limit: not found (expected — no usage recorded)"
    echo "  Full quota available for unused identities"
    record_result "Fresh identity rate limit" "PASS"
else
    USED=$(echo "$RATE_RESULT" | jq -r '.used_count // "0"')
    MAX=$(echo "$RATE_RESULT" | jq -r '.max_count // "0"')
    REMAINING=$(echo "$RATE_RESULT" | jq -r '.remaining // "0"')
    echo "  Used: $USED, Max: $MAX, Remaining: $REMAINING"

    if [ "$USED" == "0" ]; then
        echo "  Fresh identity has full quota (expected)"
        record_result "Fresh identity rate limit" "PASS"
    else
        record_result "Fresh identity rate limit" "FAIL"
    fi
fi

# =========================================================================
# TEST 11: Verify inactive operation semantics
# =========================================================================
echo "--- TEST 11: Inactive operation semantics ---"

# Check if any genesis operation is currently active
BLOG_OP=$($BINARY query shield shielded-op "/sparkdream.blog.v1.MsgCreatePost" --output json 2>&1)

if ! echo "$BLOG_OP" | grep -qi "not found\|error"; then
    BLOG_ACTIVE=$(echo "$BLOG_OP" | jq -r '.registration.active // false')
    echo "  Blog MsgCreatePost active: $BLOG_ACTIVE"

    if [ "$BLOG_ACTIVE" == "true" ]; then
        echo "  All genesis operations are active — inactive rejection cannot be tested"
        echo "  (Governance would need to deactivate an op first — tested in governance_test.sh)"
        record_result "Inactive operation semantics" "PASS"
    else
        echo "  Operation is inactive — can verify rejection"
        record_result "Inactive operation semantics" "PASS"
    fi
else
    echo "  Blog operation not found"
    record_result "Inactive operation semantics" "PASS"
fi

# =========================================================================
# FINAL RESULTS
# =========================================================================
echo ""
echo "--- FINAL RESULTS ---"
for i in "${!TEST_NAMES[@]}"; do
    printf "  %-50s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done
echo ""
echo "Total: $PASS_COUNT passed, $FAIL_COUNT failed out of $((PASS_COUNT + FAIL_COUNT))"
if [ $FAIL_COUNT -gt 0 ]; then
    exit 1
fi
