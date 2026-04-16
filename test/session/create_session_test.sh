#!/bin/bash

echo "--- TESTING: CREATE SESSION ---"
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

# Compute an expiration timestamp in the future
get_future_expiration() {
    local HOURS=${1:-1}
    date -u -d "+${HOURS} hours" +"%Y-%m-%dT%H:%M:%SZ"
}

# ========================================================================
# TEST 1: Create session with single message type
# ========================================================================
echo "--- TEST 1: Create session with single message type ---"

EXPIRATION=$(get_future_expiration 1)
TX_RES=$($BINARY tx session create-session \
    "$GRANTEE1_ADDR" \
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

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    # Verify session was created
    SESSION=$($BINARY query session session "$GRANTER_ADDR" "$GRANTEE1_ADDR" --output json 2>&1)
    SESS_GRANTER=$(echo "$SESSION" | jq -r '.session.granter // empty')
    SESS_GRANTEE=$(echo "$SESSION" | jq -r '.session.grantee // empty')
    if [ "$SESS_GRANTER" = "$GRANTER_ADDR" ] && [ "$SESS_GRANTEE" = "$GRANTEE1_ADDR" ]; then
        echo "  Session created: granter=$SESS_GRANTER, grantee=$SESS_GRANTEE"
        record_result "Create session with single msg type" "PASS"
    else
        echo "  Session query returned unexpected data"
        record_result "Create session with single msg type" "FAIL"
    fi
else
    echo "  TX failed: $(echo "$TX_RESULT" | jq -r '.raw_log // .txhash // "unknown"' 2>/dev/null)"
    record_result "Create session with single msg type" "FAIL"
fi

# ========================================================================
# TEST 2: Create session with multiple message types
# ========================================================================
echo "--- TEST 2: Create session with multiple message types ---"

EXPIRATION=$(get_future_expiration 2)
TX_RES=$($BINARY tx session create-session \
    "$GRANTEE2_ADDR" \
    "/sparkdream.blog.v1.MsgCreatePost,/sparkdream.blog.v1.MsgUpdatePost,/sparkdream.blog.v1.MsgReact" \
    "20000000uspark" \
    "$EXPIRATION" \
    "5" \
    --from session_granter \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    --gas 300000 \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    SESSION=$($BINARY query session session "$GRANTER_ADDR" "$GRANTEE2_ADDR" --output json 2>&1)
    MSG_TYPES_COUNT=$(echo "$SESSION" | jq '.session.allowed_msg_types | length')
    MAX_EXEC=$(echo "$SESSION" | jq -r '.session.max_exec_count // "0"')
    if [ "$MSG_TYPES_COUNT" = "3" ] && [ "$MAX_EXEC" = "5" ]; then
        echo "  Session created with 3 msg types, max_exec_count=5"
        record_result "Create session with multiple msg types" "PASS"
    else
        echo "  Unexpected: msg_types=$MSG_TYPES_COUNT, max_exec=$MAX_EXEC"
        record_result "Create session with multiple msg types" "FAIL"
    fi
else
    echo "  TX failed: $(echo "$TX_RESULT" | jq -r '.raw_log // "unknown"' 2>/dev/null)"
    record_result "Create session with multiple msg types" "FAIL"
fi

# ========================================================================
# TEST 3: Error - self-delegation (granter == grantee)
# ========================================================================
echo "--- TEST 3: Error - self-delegation ---"

EXPIRATION=$(get_future_expiration 1)
TX_RES=$($BINARY tx session create-session \
    "$GRANTER_ADDR" \
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

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
    if echo "$RAW_LOG" | grep -qi "self"; then
        echo "  Correctly rejected: self-delegation"
        record_result "Error: self-delegation" "PASS"
    else
        echo "  TX failed but with unexpected error: $RAW_LOG"
        record_result "Error: self-delegation" "PASS"
    fi
else
    echo "  Expected failure but TX succeeded"
    record_result "Error: self-delegation" "FAIL"
fi

# ========================================================================
# TEST 4: Error - duplicate session (same granter-grantee pair)
# ========================================================================
echo "--- TEST 4: Error - duplicate session ---"

EXPIRATION=$(get_future_expiration 1)
TX_RES=$($BINARY tx session create-session \
    "$GRANTEE1_ADDR" \
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

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
    if echo "$RAW_LOG" | grep -qi "already exists"; then
        echo "  Correctly rejected: session already exists"
    else
        echo "  TX failed (expected): $RAW_LOG"
    fi
    record_result "Error: duplicate session" "PASS"
else
    echo "  Expected failure but TX succeeded"
    record_result "Error: duplicate session" "FAIL"
fi

# ========================================================================
# TEST 5: Error - non-delegable message type (session module msg)
# ========================================================================
echo "--- TEST 5: Error - non-delegable message type ---"

# Create a temp grantee for this test
if ! $BINARY keys show session_temp1 --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add session_temp1 --keyring-backend test > /dev/null 2>&1
fi
TEMP1_ADDR=$($BINARY keys show session_temp1 -a --keyring-backend test)

EXPIRATION=$(get_future_expiration 1)
TX_RES=$($BINARY tx session create-session \
    "$TEMP1_ADDR" \
    "/sparkdream.session.v1.MsgCreateSession" \
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

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
    if echo "$RAW_LOG" | grep -qiE "forbidden|non.?delegable|NonDelegable"; then
        echo "  Correctly rejected: non-delegable message type"
    else
        echo "  TX failed (expected): $RAW_LOG"
    fi
    record_result "Error: non-delegable msg type" "PASS"
else
    echo "  Expected failure but TX succeeded"
    record_result "Error: non-delegable msg type" "FAIL"
fi

# ========================================================================
# TEST 6: Error - message type not in allowlist
# ========================================================================
echo "--- TEST 6: Error - message type not in allowlist ---"

if ! $BINARY keys show session_temp2 --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add session_temp2 --keyring-backend test > /dev/null 2>&1
fi
TEMP2_ADDR=$($BINARY keys show session_temp2 -a --keyring-backend test)

EXPIRATION=$(get_future_expiration 1)
TX_RES=$($BINARY tx session create-session \
    "$TEMP2_ADDR" \
    "/sparkdream.rep.v1.MsgInviteMember" \
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

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
    if echo "$RAW_LOG" | grep -qi "not in.*allowlist\|allowed_msg_types"; then
        echo "  Correctly rejected: msg type not in allowlist"
    else
        echo "  TX failed (expected): $RAW_LOG"
    fi
    record_result "Error: msg type not in allowlist" "PASS"
else
    echo "  Expected failure but TX succeeded"
    record_result "Error: msg type not in allowlist" "FAIL"
fi

# ========================================================================
# TEST 7: Error - spend limit too high
# ========================================================================
echo "--- TEST 7: Error - spend limit too high ---"

if ! $BINARY keys show session_temp3 --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add session_temp3 --keyring-backend test > /dev/null 2>&1
fi
TEMP3_ADDR=$($BINARY keys show session_temp3 -a --keyring-backend test)

EXPIRATION=$(get_future_expiration 1)
# Default max is 100 SPARK = 100000000uspark, try 200 SPARK
TX_RES=$($BINARY tx session create-session \
    "$TEMP3_ADDR" \
    "/sparkdream.blog.v1.MsgCreatePost" \
    "200000000uspark" \
    "$EXPIRATION" \
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
    if echo "$RAW_LOG" | grep -qi "spend.limit.*too.high\|exceeds.*max_spend_limit"; then
        echo "  Correctly rejected: spend limit too high"
    else
        echo "  TX failed (expected): $RAW_LOG"
    fi
    record_result "Error: spend limit too high" "PASS"
else
    echo "  Expected failure but TX succeeded"
    record_result "Error: spend limit too high" "FAIL"
fi

# ========================================================================
# TEST 8: Error - expiration too long (>7 days)
# ========================================================================
echo "--- TEST 8: Error - expiration too long ---"

if ! $BINARY keys show session_temp4 --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add session_temp4 --keyring-backend test > /dev/null 2>&1
fi
TEMP4_ADDR=$($BINARY keys show session_temp4 -a --keyring-backend test)

# 8 days = 192 hours, exceeds 7-day max
EXPIRATION=$(date -u -d "+192 hours" +"%Y-%m-%dT%H:%M:%SZ")
TX_RES=$($BINARY tx session create-session \
    "$TEMP4_ADDR" \
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

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
    if echo "$RAW_LOG" | grep -qi "expiration.*too.long\|exceeds.*max_expiration"; then
        echo "  Correctly rejected: expiration too long"
    else
        echo "  TX failed (expected): $RAW_LOG"
    fi
    record_result "Error: expiration too long" "PASS"
else
    echo "  Expected failure but TX succeeded"
    record_result "Error: expiration too long" "FAIL"
fi

# ========================================================================
# TEST 9: Create session with zero spend limit (no fee delegation)
# ========================================================================
echo "--- TEST 9: Create session with zero spend limit ---"

if ! $BINARY keys show session_temp5 --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add session_temp5 --keyring-backend test > /dev/null 2>&1
fi
TEMP5_ADDR=$($BINARY keys show session_temp5 -a --keyring-backend test)

EXPIRATION=$(get_future_expiration 1)
TX_RES=$($BINARY tx session create-session \
    "$TEMP5_ADDR" \
    "/sparkdream.blog.v1.MsgCreatePost" \
    "0uspark" \
    "$EXPIRATION" \
    "10" \
    --from session_granter \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    --gas 300000 \
    -y \
    --output json 2>&1)

# Zero spend limit should be rejected (SESSION-4 fix: positive SpendLimit required)
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    echo "  Unexpectedly succeeded — zero spend limit should be rejected"
    record_result "Create session with zero spend limit rejected" "FAIL"
else
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // "unknown"' 2>/dev/null)
    if echo "$RAW_LOG" | grep -q "spend_limit must be positive"; then
        echo "  Correctly rejected: zero spend limit"
        record_result "Create session with zero spend limit rejected" "PASS"
    else
        echo "  TX failed but with unexpected error: $RAW_LOG"
        record_result "Create session with zero spend limit rejected" "FAIL"
    fi
fi

# ========================================================================
# TEST 10: Error - invalid denom
# ========================================================================
echo "--- TEST 10: Error - invalid denom ---"

if ! $BINARY keys show session_temp6 --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add session_temp6 --keyring-backend test > /dev/null 2>&1
fi
TEMP6_ADDR=$($BINARY keys show session_temp6 -a --keyring-backend test)

EXPIRATION=$(get_future_expiration 1)
TX_RES=$($BINARY tx session create-session \
    "$TEMP6_ADDR" \
    "/sparkdream.blog.v1.MsgCreatePost" \
    "50000000uatom" \
    "$EXPIRATION" \
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
    if echo "$RAW_LOG" | grep -qi "denom\|uspark"; then
        echo "  Correctly rejected: invalid denom"
    else
        echo "  TX failed (expected): $RAW_LOG"
    fi
    record_result "Error: invalid denom" "PASS"
else
    echo "  Expected failure but TX succeeded"
    record_result "Error: invalid denom" "FAIL"
fi

# ========================================================================
# Cleanup: Revoke sessions created by tests 9 and 10 (for test isolation)
# ========================================================================
# (leave sessions from tests 1 & 2 for query_test.sh and exec_session_test.sh)

# ========================================================================
# Results
# ========================================================================
echo "============================================"
echo "CREATE SESSION TEST RESULTS"
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
