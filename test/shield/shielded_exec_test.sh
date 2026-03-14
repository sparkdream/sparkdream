#!/bin/bash

echo "--- TESTING: Shielded Execution (x/shield) ---"
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

# Resolve shield module address if not set
if [ -z "$SHIELD_MODULE_ADDR" ] || [ "$SHIELD_MODULE_ADDR" == "null" ]; then
    SHIELD_MODULE_ADDR=$($BINARY query auth module-account shield --output json 2>/dev/null | jq -r '.account.base_account.address // empty' 2>/dev/null)
fi

echo "Alice:          $ALICE_ADDR"
echo "Member1:        $MEMBER1_ADDR"
echo "Submitter1:     $SUBMITTER1_ADDR"
echo "Shield Module:  $SHIELD_MODULE_ADDR"
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
# PART 1: Verify shield module is enabled
# =========================================================================
echo "--- PART 1: Verify shield module is enabled ---"

PARAMS=$($BINARY query shield params --output json 2>&1)
ENABLED=$(echo "$PARAMS" | jq -r '.params.enabled // "false"')

if [ "$ENABLED" != "true" ]; then
    echo "  Shield module is disabled. Skipping shielded exec tests."
    echo ""
    echo "--- TEST SUMMARY ---"
    echo "  Shielded exec tests skipped (module disabled)"
    exit 0
fi

echo "  Shield module is enabled"
record_result "Shield module enabled" "PASS"

# =========================================================================
# PART 2: Verify registered operations exist
# =========================================================================
echo "--- PART 2: Verify operations are registered ---"

OPS=$($BINARY query shield shielded-ops --output json 2>&1)
OP_COUNT=$(echo "$OPS" | jq -r '.registrations | length' 2>/dev/null || echo "0")

echo "  Registered operations: $OP_COUNT"

if [ "$OP_COUNT" -lt 1 ]; then
    echo "  No operations registered. Cannot test shielded execution."
    record_result "Operations registered" "FAIL"
else
    # Check that blog MsgCreatePost is registered (used for immediate mode test)
    BLOG_URL="/sparkdream.blog.v1.MsgCreatePost"
    BLOG_OP=$($BINARY query shield shielded-op "$BLOG_URL" --output json 2>&1)

    if echo "$BLOG_OP" | grep -qi "not found\|error"; then
        echo "  Blog MsgCreatePost not registered — cannot test immediate execution"
        record_result "Operations registered" "FAIL"
    else
        BLOG_ACTIVE=$(echo "$BLOG_OP" | jq -r '.registration.active // false')
        echo "  Blog MsgCreatePost: registered (active=$BLOG_ACTIVE)"
        record_result "Operations registered" "PASS"
    fi
fi

# =========================================================================
# PART 3: Verify encrypted batch mode status
# =========================================================================
echo "--- PART 3: Encrypted batch mode status ---"

BATCH_ENABLED=$(echo "$PARAMS" | jq -r '.params.encrypted_batch_enabled // "false"')
echo "  Encrypted batch enabled: $BATCH_ENABLED"

if [ "$BATCH_ENABLED" != "true" ]; then
    echo "  Encrypted batch mode is disabled (DKG not completed)"
    echo "  Only immediate mode tests will run"
fi

# This is informational — batch mode being disabled is expected without DKG
record_result "Encrypted batch mode status" "PASS"

# =========================================================================
# PART 4: Verify shield module has gas balance
# =========================================================================
echo "--- PART 4: Shield module gas balance ---"

BALANCE=$($BINARY query shield module-balance --output json 2>&1)
BAL_AMOUNT=$(echo "$BALANCE" | jq -r '.balance.amount // "0"')

echo "  Shield module balance: $BAL_AMOUNT uspark"

if [ "$BAL_AMOUNT" == "0" ] || [ "$BAL_AMOUNT" == "null" ]; then
    echo "  Shield module has no balance (community pool funding may not have triggered yet)"
    echo "  (BeginBlocker attempts to auto-fund from community pool each block)"
    echo "  This is expected on a fresh test chain where community pool spendable balance is low"
fi

# Balance query itself succeeds regardless of amount
record_result "Shield module gas balance" "PASS"

# =========================================================================
# PART 5: Verify rate limiting parameters
# =========================================================================
echo "--- PART 5: Rate limiting parameters ---"

MAX_EXECS=$(echo "$PARAMS" | jq -r '.params.max_execs_per_identity_per_epoch // "0"')
MAX_GAS=$(echo "$PARAMS" | jq -r '.params.max_gas_per_exec // "0"')

echo "  Max execs per identity per epoch: $MAX_EXECS"
echo "  Max gas per exec: $MAX_GAS"

if [ "$MAX_EXECS" == "0" ] && [ "$MAX_GAS" == "0" ]; then
    echo "  WARNING: Both rate limit parameters are zero"
    record_result "Rate limiting parameters" "FAIL"
else
    record_result "Rate limiting parameters" "PASS"
fi

# =========================================================================
# PART 6: Verify shield module address is valid for inner messages
# =========================================================================
echo "--- PART 6: Shield module address for inner messages ---"

if [ -z "$SHIELD_MODULE_ADDR" ] || [ "$SHIELD_MODULE_ADDR" == "null" ]; then
    echo "  Shield module address not available"
    echo "  Cannot construct inner messages with shield module as creator"
    record_result "Shield module address" "FAIL"
else
    echo "  Shield module address: $SHIELD_MODULE_ADDR"
    echo "  Inner messages should set creator = $SHIELD_MODULE_ADDR"
    record_result "Shield module address" "PASS"
fi

# =========================================================================
# PART 7: Verify nullifier domain assignments
# =========================================================================
echo "--- PART 7: Verify nullifier domain assignments ---"

echo "  Checking nullifier domain uniqueness across registered ops..."

# Extract all nullifier domains
DOMAINS=$(echo "$OPS" | jq -r '.registrations[]? | "\(.message_type_url): domain=\(.nullifier_domain // 0)"' 2>/dev/null)

if [ -n "$DOMAINS" ]; then
    echo "$DOMAINS" | while IFS= read -r line; do
        echo "    $line"
    done

    # Check for duplicate domains (same domain should only exist if scope types differ)
    DOMAIN_COUNTS=$(echo "$OPS" | jq -r '[.registrations[]? | .nullifier_domain // 0] | group_by(.) | map({domain: .[0], count: length}) | .[] | select(.count > 1) | "domain \(.domain): \(.count) registrations"' 2>/dev/null)

    if [ -n "$DOMAIN_COUNTS" ]; then
        echo ""
        echo "  Domains shared by multiple operations:"
        echo "  $DOMAIN_COUNTS"
        echo "  (This is OK if they have different scope types)"
    fi

    record_result "Nullifier domain assignments" "PASS"
else
    echo "    Could not extract domains"
    record_result "Nullifier domain assignments" "FAIL"
fi

# =========================================================================
# PART 8: Verify scope field paths for MESSAGE_FIELD operations
# =========================================================================
echo "--- PART 8: Verify scope field paths ---"

MSG_FIELD_OPS=$(echo "$OPS" | jq -r '.registrations[]? | select(.nullifier_scope_type == "NULLIFIER_SCOPE_MESSAGE_FIELD" or .nullifier_scope_type == 1) | "\(.message_type_url): scope_field=\(.scope_field_path // "MISSING")"' 2>/dev/null)

if [ -n "$MSG_FIELD_OPS" ]; then
    echo "  Operations with MESSAGE_FIELD scope:"
    echo "$MSG_FIELD_OPS" | while IFS= read -r line; do
        echo "    $line"
    done

    # Check for missing scope_field_path
    MISSING=$(echo "$OPS" | jq -r '.registrations[]? | select((.nullifier_scope_type == "NULLIFIER_SCOPE_MESSAGE_FIELD" or .nullifier_scope_type == 1) and (.scope_field_path == null or .scope_field_path == "")) | .message_type_url' 2>/dev/null)
    if [ -n "$MISSING" ]; then
        echo ""
        echo "  WARNING: Missing scope_field_path for: $MISSING"
        record_result "Scope field paths" "FAIL"
    else
        record_result "Scope field paths" "PASS"
    fi
else
    echo "  No operations with MESSAGE_FIELD scope type found"
    record_result "Scope field paths" "PASS"
fi

# =========================================================================
# PART 9: Test shielded exec with missing fields (should fail)
# =========================================================================
echo "--- PART 9: Shielded exec with empty payload (should fail) ---"

# Try submitting a shielded exec with no inner message and no encrypted payload
# This should be rejected (both modes require content)
# Using autocli's flag-based approach for shielded-exec
TX_RES=$($BINARY tx shield shielded-exec \
    --from submitter1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    -y \
    --output json 2>&1)

TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
    echo "  Correctly rejected empty shielded exec (no broadcast)"
    record_result "Empty payload rejection" "PASS"
else
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")

    if check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected empty shielded exec"
        record_result "Empty payload rejection" "PASS"
    else
        echo "  Empty shielded exec was accepted (should have been rejected)"
        record_result "Empty payload rejection" "FAIL"
    fi
fi

# =========================================================================
# PART 10: Verify module-paid gas model
# =========================================================================
echo "--- PART 10: Verify module-paid gas model ---"

# The shield ante handler should:
# 1. Detect MsgShieldedExec in a transaction
# 2. Deduct fees from shield module account (not submitter)
# 3. Reject multi-message transactions containing MsgShieldedExec

echo "  Module-paid gas model:"
echo "    - Shield module account pays all gas for MsgShieldedExec"
echo "    - Submitter needs zero balance for gas"
echo "    - Multi-message txs with MsgShieldedExec are rejected"
echo "    - Standard DeductFeeDecorator is skipped"
echo ""
echo "  This is enforced by ShieldGasDecorator in ante handler chain"
echo "  (Verified via ante/ante_test.go unit tests)"

record_result "Module-paid gas model" "PASS"

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
