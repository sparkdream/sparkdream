#!/bin/bash

echo "--- TESTING: EXEC SESSION ---"
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

# Build an unsigned MsgExecSession tx JSON and sign+broadcast it.
# Usage: exec_session_via_json <grantee_key> <granter_addr> <grantee_addr> <inner_msg_json> [gas]
# Sets TX_RESULT on success.
exec_session_via_json() {
    local GRANTEE_KEY=$1
    local GRANTER=$2
    local GRANTEE=$3
    local INNER_MSG=$4
    local GAS=${5:-300000}
    local FEES="50000"

    # Get account number and sequence for the grantee
    local ACCT_INFO=$($BINARY query auth account "$GRANTEE" --output json 2>&1)
    local ACCT_NUM=$(echo "$ACCT_INFO" | jq -r '.account.account_number // .account.base_account.account_number // "0"')
    local SEQ=$(echo "$ACCT_INFO" | jq -r '.account.sequence // .account.base_account.sequence // "0"')

    # Construct unsigned tx
    cat > /tmp/session_exec_unsigned.json <<TXEOF
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

    # Sign with grantee key
    local SIGN_RESULT=$($BINARY tx sign /tmp/session_exec_unsigned.json \
        --from "$GRANTEE_KEY" \
        --chain-id "$CHAIN_ID" \
        --keyring-backend test \
        --account-number "$ACCT_NUM" \
        --sequence "$SEQ" \
        --output-document /tmp/session_exec_signed.json 2>&1)

    if [ ! -f /tmp/session_exec_signed.json ] || [ ! -s /tmp/session_exec_signed.json ]; then
        echo "  Sign failed: $SIGN_RESULT" >&2
        TX_RESULT='{"code":99,"raw_log":"sign failed"}'
        return 1
    fi

    # Broadcast
    local TX_RES=$($BINARY tx broadcast /tmp/session_exec_signed.json --output json 2>&1)
    submit_tx_and_wait "$TX_RES"
    return $?
}

# ========================================================================
# Pre-test: Create a dedicated session for exec tests
# ========================================================================
echo "--- PRE-TEST: Creating session for exec tests ---"

# Create a new grantee for exec tests to avoid conflicts
if ! $BINARY keys show exec_grantee --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add exec_grantee --keyring-backend test > /dev/null 2>&1
fi
EXEC_GRANTEE_ADDR=$($BINARY keys show exec_grantee -a --keyring-backend test)

# Fund the exec_grantee (needs account to exist for signing)
TX_RES=$($BINARY tx bank send alice "$EXEC_GRANTEE_ADDR" 10000000uspark \
    --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y --output json 2>&1)
sleep 6

# Create session: granter delegates blog post creation to exec_grantee
EXPIRATION=$(get_future_expiration 2)
TX_RES=$($BINARY tx session create-session \
    "$EXEC_GRANTEE_ADDR" \
    "/sparkdream.blog.v1.MsgCreatePost,/sparkdream.blog.v1.MsgReact" \
    "50000000uspark" \
    "$EXPIRATION" \
    "3" \
    --from session_granter \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    --gas 300000 \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    echo "  Exec test session created: granter=$GRANTER_ADDR, grantee=$EXEC_GRANTEE_ADDR, max_exec=3"
else
    echo "  Failed to create exec test session, skipping exec tests"
    echo "  Error: $(echo "$TX_RESULT" | jq -r '.raw_log // "unknown"' 2>/dev/null)"
    exit 1
fi
echo ""

# ========================================================================
# TEST 1: Execute blog create-post via session key
# ========================================================================
echo "--- TEST 1: Execute blog create-post via session key ---"

INNER_MSG=$(cat <<MSGEOF
{
  "@type": "/sparkdream.blog.v1.MsgCreatePost",
  "creator": "$GRANTER_ADDR",
  "title": "Session Test Post",
  "body": "This post was created via a session key"
}
MSGEOF
)

exec_session_via_json "exec_grantee" "$GRANTER_ADDR" "$EXEC_GRANTEE_ADDR" "$INNER_MSG"

if check_tx_success "$TX_RESULT"; then
    echo "  Blog post created via session key"

    # Verify exec_count incremented
    SESSION=$($BINARY query session session "$GRANTER_ADDR" "$EXEC_GRANTEE_ADDR" --output json 2>&1)
    EXEC_COUNT=$(echo "$SESSION" | jq -r '.session.exec_count // "0"')
    if [ "$EXEC_COUNT" = "1" ]; then
        echo "  exec_count incremented to 1"
        record_result "Execute blog create-post via session" "PASS"
    else
        echo "  exec_count=$EXEC_COUNT (expected 1)"
        record_result "Execute blog create-post via session" "FAIL"
    fi
else
    echo "  TX failed: $(echo "$TX_RESULT" | jq -r '.raw_log // "unknown"' 2>/dev/null)"
    record_result "Execute blog create-post via session" "FAIL"
fi

# ========================================================================
# TEST 2: Execute second time and verify exec_count
# ========================================================================
echo "--- TEST 2: Execute again - verify exec_count increments ---"

INNER_MSG=$(cat <<MSGEOF
{
  "@type": "/sparkdream.blog.v1.MsgCreatePost",
  "creator": "$GRANTER_ADDR",
  "title": "Session Test Post 2",
  "body": "Second post via session key"
}
MSGEOF
)

exec_session_via_json "exec_grantee" "$GRANTER_ADDR" "$EXEC_GRANTEE_ADDR" "$INNER_MSG"

if check_tx_success "$TX_RESULT"; then
    SESSION=$($BINARY query session session "$GRANTER_ADDR" "$EXEC_GRANTEE_ADDR" --output json 2>&1)
    EXEC_COUNT=$(echo "$SESSION" | jq -r '.session.exec_count // "0"')
    if [ "$EXEC_COUNT" = "2" ]; then
        echo "  exec_count incremented to 2"
        record_result "Execute again - exec_count increments" "PASS"
    else
        echo "  exec_count=$EXEC_COUNT (expected 2)"
        record_result "Execute again - exec_count increments" "FAIL"
    fi
else
    echo "  TX failed: $(echo "$TX_RESULT" | jq -r '.raw_log // "unknown"' 2>/dev/null)"
    record_result "Execute again - exec_count increments" "FAIL"
fi

# ========================================================================
# TEST 3: Execute third time (reaching max_exec_count=3)
# ========================================================================
echo "--- TEST 3: Execute at max_exec_count limit ---"

INNER_MSG=$(cat <<MSGEOF
{
  "@type": "/sparkdream.blog.v1.MsgCreatePost",
  "creator": "$GRANTER_ADDR",
  "title": "Session Test Post 3",
  "body": "Third and final post via session key"
}
MSGEOF
)

exec_session_via_json "exec_grantee" "$GRANTER_ADDR" "$EXEC_GRANTEE_ADDR" "$INNER_MSG"

if check_tx_success "$TX_RESULT"; then
    SESSION=$($BINARY query session session "$GRANTER_ADDR" "$EXEC_GRANTEE_ADDR" --output json 2>&1)
    EXEC_COUNT=$(echo "$SESSION" | jq -r '.session.exec_count // "0"')
    if [ "$EXEC_COUNT" = "3" ]; then
        echo "  exec_count reached max (3)"
        record_result "Execute at max_exec_count" "PASS"
    else
        echo "  exec_count=$EXEC_COUNT (expected 3)"
        record_result "Execute at max_exec_count" "FAIL"
    fi
else
    echo "  TX failed: $(echo "$TX_RESULT" | jq -r '.raw_log // "unknown"' 2>/dev/null)"
    record_result "Execute at max_exec_count" "FAIL"
fi

# ========================================================================
# TEST 4: Error - execute after max_exec_count exceeded
# ========================================================================
echo "--- TEST 4: Error - exec_count exceeded ---"

INNER_MSG=$(cat <<MSGEOF
{
  "@type": "/sparkdream.blog.v1.MsgCreatePost",
  "creator": "$GRANTER_ADDR",
  "title": "Should Fail",
  "body": "Should not be created"
}
MSGEOF
)

exec_session_via_json "exec_grantee" "$GRANTER_ADDR" "$EXEC_GRANTEE_ADDR" "$INNER_MSG"

if check_tx_failure "$TX_RESULT"; then
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
    if echo "$RAW_LOG" | grep -qi "exec.*count\|execution.*cap"; then
        echo "  Correctly rejected: exec count exceeded"
    else
        echo "  TX failed (expected): $RAW_LOG"
    fi
    record_result "Error: exec_count exceeded" "PASS"
else
    echo "  Expected failure but TX succeeded"
    record_result "Error: exec_count exceeded" "FAIL"
fi

# ========================================================================
# TEST 5: Error - non-allowed message type via exec
# ========================================================================
echo "--- TEST 5: Error - non-allowed message type via exec ---"

# Create a new session with only MsgCreatePost allowed
if ! $BINARY keys show exec_grantee2 --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add exec_grantee2 --keyring-backend test > /dev/null 2>&1
fi
EXEC_GRANTEE2_ADDR=$($BINARY keys show exec_grantee2 -a --keyring-backend test)

TX_RES=$($BINARY tx bank send alice "$EXEC_GRANTEE2_ADDR" 10000000uspark \
    --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y --output json 2>&1)
sleep 6

EXPIRATION=$(get_future_expiration 2)
TX_RES=$($BINARY tx session create-session \
    "$EXEC_GRANTEE2_ADDR" \
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

submit_tx_and_wait "$TX_RES"
if ! check_tx_success "$TX_RESULT"; then
    echo "  Failed to create session for test 5, skipping"
    record_result "Error: non-allowed msg type via exec" "FAIL"
else
    # Try to execute MsgUpdatePost which is NOT in this session's allowed list
    INNER_MSG=$(cat <<MSGEOF
{
  "@type": "/sparkdream.blog.v1.MsgUpdatePost",
  "creator": "$GRANTER_ADDR",
  "title": "Updated Title",
  "body": "Updated body",
  "id": "0"
}
MSGEOF
)

    exec_session_via_json "exec_grantee2" "$GRANTER_ADDR" "$EXEC_GRANTEE2_ADDR" "$INNER_MSG"

    if check_tx_failure "$TX_RESULT"; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
        if echo "$RAW_LOG" | grep -qi "not.*allowed\|not in session"; then
            echo "  Correctly rejected: msg type not in session's allowed list"
        else
            echo "  TX failed (expected): $RAW_LOG"
        fi
        record_result "Error: non-allowed msg type via exec" "PASS"
    else
        echo "  Expected failure but TX succeeded"
        record_result "Error: non-allowed msg type via exec" "FAIL"
    fi
fi

# ========================================================================
# TEST 6: Error - exec with nonexistent session
# ========================================================================
echo "--- TEST 6: Error - exec with nonexistent session ---"

# Create a key that has no session
if ! $BINARY keys show no_session_key --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add no_session_key --keyring-backend test > /dev/null 2>&1
fi
NO_SESSION_ADDR=$($BINARY keys show no_session_key -a --keyring-backend test)

TX_RES=$($BINARY tx bank send alice "$NO_SESSION_ADDR" 5000000uspark \
    --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark -y --output json 2>&1)
sleep 6

INNER_MSG=$(cat <<MSGEOF
{
  "@type": "/sparkdream.blog.v1.MsgCreatePost",
  "creator": "$GRANTER_ADDR",
  "title": "Should Fail",
  "body": "No session exists"
}
MSGEOF
)

exec_session_via_json "no_session_key" "$GRANTER_ADDR" "$NO_SESSION_ADDR" "$INNER_MSG"

if check_tx_failure "$TX_RESULT"; then
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
    if echo "$RAW_LOG" | grep -qi "not found\|no active session"; then
        echo "  Correctly rejected: no session found"
    else
        echo "  TX failed (expected): $RAW_LOG"
    fi
    record_result "Error: exec with nonexistent session" "PASS"
else
    echo "  Expected failure but TX succeeded"
    record_result "Error: exec with nonexistent session" "FAIL"
fi

# ========================================================================
# Results
# ========================================================================
echo "============================================"
echo "EXEC SESSION TEST RESULTS"
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
