#!/bin/bash

echo "--- TESTING: SESSION EXPIRATION & ENDBLOCK PRUNING ---"
echo ""

# ========================================================================
# Setup
# ========================================================================
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

# Initialize tracking
PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()

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

check_tx_failure() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        return 0
    fi
    return 1
}

submit_tx_and_wait() {
    local TX_RES="$1"
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')

    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        TX_RESULT="$TX_RES"
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

get_future_expiration() {
    local HOURS=${1:-1}
    date -u -d "+${HOURS} hours" +"%Y-%m-%dT%H:%M:%SZ"
}

get_future_expiration_seconds() {
    local SECS=${1:-30}
    date -u -d "+${SECS} seconds" +"%Y-%m-%dT%H:%M:%SZ"
}

exec_session_via_json() {
    local GRANTEE_KEY=$1
    local GRANTER=$2
    local GRANTEE=$3
    local INNER_MSG=$4
    local GAS=${5:-300000}
    local FEES="50000"

    local ACCT_INFO=$($BINARY query auth account "$GRANTEE" --output json 2>&1)
    local ACCT_NUM=$(echo "$ACCT_INFO" | jq -r '.account.account_number // .account.base_account.account_number // "0"')
    local SEQ=$(echo "$ACCT_INFO" | jq -r '.account.sequence // .account.base_account.sequence // "0"')

    cat > /tmp/session_exp_unsigned.json <<TXEOF
{
  "body": {
    "messages": [
      {
        "@type": "/sparkdream.session.v1.MsgExecSession",
        "grantee": "$GRANTEE",
        "granter": "$GRANTER",
        "msgs": [$INNER_MSG]
      }
    ],
    "memo": "",
    "timeout_height": "0",
    "extension_options": [],
    "non_critical_extension_options": []
  },
  "auth_info": {
    "signer_infos": [],
    "fee": {
      "amount": [{"denom": "uspark", "amount": "$FEES"}],
      "gas_limit": "$GAS",
      "payer": "",
      "granter": ""
    }
  },
  "signatures": []
}
TXEOF

    local SIGN_RESULT=$($BINARY tx sign /tmp/session_exp_unsigned.json \
        --from "$GRANTEE_KEY" \
        --chain-id "$CHAIN_ID" \
        --keyring-backend test \
        --account-number "$ACCT_NUM" \
        --sequence "$SEQ" \
        --output-document /tmp/session_exp_signed.json 2>&1)

    if [ ! -f /tmp/session_exp_signed.json ] || [ ! -s /tmp/session_exp_signed.json ]; then
        echo "  Sign failed: $SIGN_RESULT" >&2
        TX_RESULT='{"code":99,"raw_log":"sign failed"}'
        return 1
    fi

    local TX_RES=$($BINARY tx broadcast /tmp/session_exp_signed.json --output json 2>&1)
    submit_tx_and_wait "$TX_RES"
    return $?
}

# ========================================================================
# TEST 1: Session is pruned by EndBlocker after expiration
# ========================================================================
echo "--- TEST 1: Session pruned by EndBlocker after expiration ---"

if ! $BINARY keys show exp_grantee1 --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add exp_grantee1 --keyring-backend test > /dev/null 2>&1
fi
EXP_GRANTEE1_ADDR=$($BINARY keys show exp_grantee1 -a --keyring-backend test)

# Create session expiring in 20 seconds
EXPIRATION=$(get_future_expiration_seconds 20)
echo "  Creating session expiring at $EXPIRATION..."

TX_RES=$($BINARY tx session create-session \
    "$EXP_GRANTEE1_ADDR" \
    "/sparkdream.blog.v1.MsgCreatePost" \
    "50000000uspark" \
    "$EXPIRATION" \
    "0" \
    --from session_granter \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    --gas 300000 \
    -y \
    --output json 2>&1)

if ! submit_tx_and_wait "$TX_RES" || ! check_tx_success "$TX_RESULT"; then
    echo "  Failed to create short-lived session"
    echo "  Error: $(echo "$TX_RESULT" | jq -r '.raw_log // "unknown"' 2>/dev/null)"
    record_result "Session pruned after expiration" "FAIL"
else
    # Verify session exists immediately
    SESSION=$($BINARY query session session "$GRANTER_ADDR" "$EXP_GRANTEE1_ADDR" --output json 2>&1)
    SESS_GRANTEE=$(echo "$SESSION" | jq -r '.session.grantee // empty')

    if [ "$SESS_GRANTEE" != "$EXP_GRANTEE1_ADDR" ]; then
        echo "  Session not found immediately after creation!"
        record_result "Session pruned after expiration" "FAIL"
    else
        echo "  Session exists. Waiting for expiration (~30 seconds)..."

        # Wait for session to expire and EndBlocker to prune it.
        # Block time is ~5-6s. Need to wait past expiration + at least one block.
        sleep 30

        # Query session — should be gone
        SESSION_AFTER=$($BINARY query session session "$GRANTER_ADDR" "$EXP_GRANTEE1_ADDR" --output json 2>&1)
        SESS_GRANTEE_AFTER=$(echo "$SESSION_AFTER" | jq -r '.session.grantee // empty' 2>/dev/null)

        if echo "$SESSION_AFTER" | grep -qi "not found" || [ -z "$SESS_GRANTEE_AFTER" ]; then
            echo "  Session pruned by EndBlocker after expiration"
            record_result "Session pruned after expiration" "PASS"
        else
            echo "  Session still exists after waiting! (may need longer wait)"
            echo "  Response: $(echo "$SESSION_AFTER" | head -3)"
            record_result "Session pruned after expiration" "FAIL"
        fi
    fi
fi

# ========================================================================
# TEST 2: Execute on expired session fails
# ========================================================================
echo "--- TEST 2: Execute on expired session fails ---"

if ! $BINARY keys show exp_grantee2 --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add exp_grantee2 --keyring-backend test > /dev/null 2>&1
fi
EXP_GRANTEE2_ADDR=$($BINARY keys show exp_grantee2 -a --keyring-backend test)

TX_RES=$($BINARY tx bank send alice "$EXP_GRANTEE2_ADDR" 10000000uspark \
    --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y --output json 2>&1)
sleep 6

# Create session expiring in 20 seconds
EXPIRATION=$(get_future_expiration_seconds 20)
echo "  Creating session expiring at $EXPIRATION..."

TX_RES=$($BINARY tx session create-session \
    "$EXP_GRANTEE2_ADDR" \
    "/sparkdream.blog.v1.MsgCreatePost" \
    "50000000uspark" \
    "$EXPIRATION" \
    "0" \
    --from session_granter \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    --gas 300000 \
    -y \
    --output json 2>&1)

if ! submit_tx_and_wait "$TX_RES" || ! check_tx_success "$TX_RESULT"; then
    echo "  Failed to create short-lived session"
    record_result "Execute on expired session fails" "FAIL"
else
    echo "  Session created. Waiting for expiration (~30 seconds)..."
    sleep 30

    # Try to execute — should fail (either expired or pruned)
    INNER_MSG=$(cat <<MSGEOF
{
  "@type": "/sparkdream.blog.v1.MsgCreatePost",
  "creator": "$GRANTER_ADDR",
  "title": "Should Fail - Expired",
  "body": "This should not be created"
}
MSGEOF
)

    exec_session_via_json "exp_grantee2" "$GRANTER_ADDR" "$EXP_GRANTEE2_ADDR" "$INNER_MSG"

    if check_tx_failure "$TX_RESULT"; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
        if echo "$RAW_LOG" | grep -qi "expired\|not found\|no active session"; then
            echo "  Correctly rejected: session expired/pruned"
        else
            echo "  TX failed (expected): $RAW_LOG"
        fi
        record_result "Execute on expired session fails" "PASS"
    else
        echo "  Expected failure but TX succeeded!"
        record_result "Execute on expired session fails" "FAIL"
    fi
fi

# ========================================================================
# TEST 3: Long-lived session NOT pruned (control test)
# ========================================================================
echo "--- TEST 3: Long-lived session NOT pruned ---"

if ! $BINARY keys show exp_grantee3 --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add exp_grantee3 --keyring-backend test > /dev/null 2>&1
fi
EXP_GRANTEE3_ADDR=$($BINARY keys show exp_grantee3 -a --keyring-backend test)

# Create session expiring in 2 hours
EXPIRATION=$(get_future_expiration 2)

TX_RES=$($BINARY tx session create-session \
    "$EXP_GRANTEE3_ADDR" \
    "/sparkdream.blog.v1.MsgCreatePost" \
    "50000000uspark" \
    "$EXPIRATION" \
    "0" \
    --from session_granter \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    --gas 300000 \
    -y \
    --output json 2>&1)

if ! submit_tx_and_wait "$TX_RES" || ! check_tx_success "$TX_RESULT"; then
    echo "  Failed to create long-lived session"
    record_result "Long-lived session NOT pruned" "FAIL"
else
    # Session should still exist after the wait period used in previous tests
    SESSION=$($BINARY query session session "$GRANTER_ADDR" "$EXP_GRANTEE3_ADDR" --output json 2>&1)
    SESS_GRANTEE=$(echo "$SESSION" | jq -r '.session.grantee // empty')

    if [ "$SESS_GRANTEE" = "$EXP_GRANTEE3_ADDR" ]; then
        echo "  Long-lived session still exists (correct)"
        record_result "Long-lived session NOT pruned" "PASS"
    else
        echo "  Long-lived session was unexpectedly pruned!"
        record_result "Long-lived session NOT pruned" "FAIL"
    fi
fi

# ========================================================================
# TEST 4: Granter sessions-by-granter count decreases after expiry pruning
# ========================================================================
echo "--- TEST 4: sessions-by-granter count reflects pruning ---"

# Count sessions before creating short-lived one
BY_GRANTER_BEFORE=$($BINARY query session sessions-by-granter "$GRANTER_ADDR" --output json 2>&1)
COUNT_BEFORE=$(echo "$BY_GRANTER_BEFORE" | jq '.sessions | length')

if ! $BINARY keys show exp_grantee4 --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add exp_grantee4 --keyring-backend test > /dev/null 2>&1
fi
EXP_GRANTEE4_ADDR=$($BINARY keys show exp_grantee4 -a --keyring-backend test)

# Create session expiring in 20 seconds
EXPIRATION=$(get_future_expiration_seconds 20)

TX_RES=$($BINARY tx session create-session \
    "$EXP_GRANTEE4_ADDR" \
    "/sparkdream.blog.v1.MsgCreatePost" \
    "50000000uspark" \
    "$EXPIRATION" \
    "0" \
    --from session_granter \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    --gas 300000 \
    -y \
    --output json 2>&1)

if ! submit_tx_and_wait "$TX_RES" || ! check_tx_success "$TX_RESULT"; then
    echo "  Failed to create short-lived session"
    record_result "sessions-by-granter reflects pruning" "FAIL"
else
    # Count should have increased
    BY_GRANTER_DURING=$($BINARY query session sessions-by-granter "$GRANTER_ADDR" --output json 2>&1)
    COUNT_DURING=$(echo "$BY_GRANTER_DURING" | jq '.sessions | length')
    echo "  Sessions before: $COUNT_BEFORE, after create: $COUNT_DURING"

    if [ "$COUNT_DURING" -gt "$COUNT_BEFORE" ]; then
        echo "  Waiting for expiration and pruning (~30 seconds)..."
        sleep 30

        BY_GRANTER_AFTER=$($BINARY query session sessions-by-granter "$GRANTER_ADDR" --output json 2>&1)
        COUNT_AFTER=$(echo "$BY_GRANTER_AFTER" | jq '.sessions | length')
        echo "  Sessions after pruning: $COUNT_AFTER"

        if [ "$COUNT_AFTER" -lt "$COUNT_DURING" ]; then
            echo "  Session count decreased after pruning"
            record_result "sessions-by-granter reflects pruning" "PASS"
        else
            echo "  Session count did not decrease (pruning may not have run yet)"
            record_result "sessions-by-granter reflects pruning" "FAIL"
        fi
    else
        echo "  Session count did not increase after creation"
        record_result "sessions-by-granter reflects pruning" "FAIL"
    fi
fi

# ========================================================================
# TEST 5: Error - expiration in the past
# ========================================================================
echo "--- TEST 5: Error - expiration in the past ---"

if ! $BINARY keys show exp_grantee5 --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add exp_grantee5 --keyring-backend test > /dev/null 2>&1
fi
EXP_GRANTEE5_ADDR=$($BINARY keys show exp_grantee5 -a --keyring-backend test)

# Set expiration 1 hour in the past
PAST_EXPIRATION=$(date -u -d "-1 hour" +"%Y-%m-%dT%H:%M:%SZ")

TX_RES=$($BINARY tx session create-session \
    "$EXP_GRANTEE5_ADDR" \
    "/sparkdream.blog.v1.MsgCreatePost" \
    "50000000uspark" \
    "$PAST_EXPIRATION" \
    "0" \
    --from session_granter \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    --gas 300000 \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
    if echo "$RAW_LOG" | grep -qi "past\|expiration\|invalid"; then
        echo "  Correctly rejected: expiration in the past"
    else
        echo "  TX failed (expected): $RAW_LOG"
    fi
    record_result "Error: expiration in the past" "PASS"
else
    echo "  Expected failure but TX succeeded"
    record_result "Error: expiration in the past" "FAIL"
fi

# ========================================================================
# Results
# ========================================================================
echo "============================================"
echo "EXPIRATION TEST RESULTS"
echo "============================================"

for i in "${!TEST_NAMES[@]}"; do
    printf "  %-45s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done

echo ""
echo "  Passed: $PASS_COUNT / $((PASS_COUNT + FAIL_COUNT))"
echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL TESTS PASSED <<<"
    exit 0
fi
