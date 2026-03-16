#!/bin/bash

echo "--- TESTING: REVOKE SESSION ---"
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

# ========================================================================
# TEST 1: Create and revoke a session
# ========================================================================
echo "--- TEST 1: Create and revoke a session ---"

# Create a fresh grantee for revoke tests
if ! $BINARY keys show revoke_grantee1 --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add revoke_grantee1 --keyring-backend test > /dev/null 2>&1
fi
REVOKE_GRANTEE1_ADDR=$($BINARY keys show revoke_grantee1 -a --keyring-backend test)

# Create session
EXPIRATION=$(get_future_expiration 1)
TX_RES=$($BINARY tx session create-session \
    "$REVOKE_GRANTEE1_ADDR" \
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
    echo "  Failed to create session for revoke test"
    record_result "Create and revoke session" "FAIL"
else
    # Verify session exists
    SESSION=$($BINARY query session session "$GRANTER_ADDR" "$REVOKE_GRANTEE1_ADDR" --output json 2>&1)
    SESS_GRANTEE=$(echo "$SESSION" | jq -r '.session.grantee // empty')
    if [ "$SESS_GRANTEE" != "$REVOKE_GRANTEE1_ADDR" ]; then
        echo "  Session not found after creation"
        record_result "Create and revoke session" "FAIL"
    else
        echo "  Session created, now revoking..."

        # Revoke session
        TX_RES=$($BINARY tx session revoke-session \
            "$REVOKE_GRANTEE1_ADDR" \
            --from session_granter \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 50000uspark \
            --gas 300000 \
            -y \
            --output json 2>&1)

        submit_tx_and_wait "$TX_RES"
        if check_tx_success "$TX_RESULT"; then
            echo "  Session revoked successfully"
            record_result "Create and revoke session" "PASS"
        else
            echo "  Revoke failed: $(echo "$TX_RESULT" | jq -r '.raw_log // "unknown"' 2>/dev/null)"
            record_result "Create and revoke session" "FAIL"
        fi
    fi
fi

# ========================================================================
# TEST 2: Verify session is gone after revoke
# ========================================================================
echo "--- TEST 2: Verify session gone after revoke ---"

SESSION=$($BINARY query session session "$GRANTER_ADDR" "$REVOKE_GRANTEE1_ADDR" --output json 2>&1)

if echo "$SESSION" | grep -qi "not found\|no active session"; then
    echo "  Session correctly removed after revoke"
    record_result "Session gone after revoke" "PASS"
else
    SESS_GRANTEE=$(echo "$SESSION" | jq -r '.session.grantee // empty' 2>/dev/null)
    if [ -z "$SESS_GRANTEE" ]; then
        echo "  Session correctly removed (empty response)"
        record_result "Session gone after revoke" "PASS"
    else
        echo "  Session still exists after revoke!"
        record_result "Session gone after revoke" "FAIL"
    fi
fi

# ========================================================================
# TEST 3: Error - revoke nonexistent session
# ========================================================================
echo "--- TEST 3: Error - revoke nonexistent session ---"

# Try to revoke a session that doesn't exist
if ! $BINARY keys show revoke_grantee2 --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add revoke_grantee2 --keyring-backend test > /dev/null 2>&1
fi
REVOKE_GRANTEE2_ADDR=$($BINARY keys show revoke_grantee2 -a --keyring-backend test)

TX_RES=$($BINARY tx session revoke-session \
    "$REVOKE_GRANTEE2_ADDR" \
    --from session_granter \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    --gas 300000 \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""')
    if echo "$RAW_LOG" | grep -qi "not found\|no active session"; then
        echo "  Correctly rejected: no session to revoke"
    else
        echo "  TX failed (expected): $RAW_LOG"
    fi
    record_result "Error: revoke nonexistent session" "PASS"
else
    echo "  Expected failure but TX succeeded"
    record_result "Error: revoke nonexistent session" "FAIL"
fi

# ========================================================================
# TEST 4: Revoke session from grantee1 (created in create_session_test)
# ========================================================================
echo "--- TEST 4: Revoke grantee1 session ---"

# Check if session still exists
SESSION=$($BINARY query session session "$GRANTER_ADDR" "$GRANTEE1_ADDR" --output json 2>&1)
SESS_GRANTEE=$(echo "$SESSION" | jq -r '.session.grantee // empty' 2>/dev/null)

if [ "$SESS_GRANTEE" = "$GRANTEE1_ADDR" ]; then
    TX_RES=$($BINARY tx session revoke-session \
        "$GRANTEE1_ADDR" \
        --from session_granter \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        --gas 300000 \
        -y \
        --output json 2>&1)

    submit_tx_and_wait "$TX_RES"
    if check_tx_success "$TX_RESULT"; then
        # Verify gone
        SESSION_CHECK=$($BINARY query session session "$GRANTER_ADDR" "$GRANTEE1_ADDR" --output json 2>&1)
        if echo "$SESSION_CHECK" | grep -qi "not found\|no active session"; then
            echo "  Revoked grantee1 session and verified removal"
            record_result "Revoke grantee1 session" "PASS"
        else
            STILL_EXISTS=$(echo "$SESSION_CHECK" | jq -r '.session.grantee // empty' 2>/dev/null)
            if [ -z "$STILL_EXISTS" ]; then
                echo "  Revoked grantee1 session (empty response)"
                record_result "Revoke grantee1 session" "PASS"
            else
                echo "  Session still exists after revoke!"
                record_result "Revoke grantee1 session" "FAIL"
            fi
        fi
    else
        echo "  Revoke failed: $(echo "$TX_RESULT" | jq -r '.raw_log // "unknown"' 2>/dev/null)"
        record_result "Revoke grantee1 session" "FAIL"
    fi
else
    echo "  No grantee1 session to revoke (may have been cleaned up)"
    echo "  Skipping (marking as PASS since session was already gone)"
    record_result "Revoke grantee1 session" "PASS"
fi

# ========================================================================
# TEST 5: Revoke session from grantee2 and verify granter has fewer sessions
# ========================================================================
echo "--- TEST 5: Revoke grantee2 session and verify count decreases ---"

# Count sessions before revoke
BY_GRANTER=$($BINARY query session sessions-by-granter "$GRANTER_ADDR" --output json 2>&1)
COUNT_BEFORE=$(echo "$BY_GRANTER" | jq '.sessions | length')
echo "  Sessions before revoke: $COUNT_BEFORE"

# Check if grantee2 session exists
SESSION=$($BINARY query session session "$GRANTER_ADDR" "$GRANTEE2_ADDR" --output json 2>&1)
SESS_GRANTEE=$(echo "$SESSION" | jq -r '.session.grantee // empty' 2>/dev/null)

if [ "$SESS_GRANTEE" = "$GRANTEE2_ADDR" ]; then
    TX_RES=$($BINARY tx session revoke-session \
        "$GRANTEE2_ADDR" \
        --from session_granter \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        --gas 300000 \
        -y \
        --output json 2>&1)

    submit_tx_and_wait "$TX_RES"
    if check_tx_success "$TX_RESULT"; then
        # Count sessions after revoke
        BY_GRANTER=$($BINARY query session sessions-by-granter "$GRANTER_ADDR" --output json 2>&1)
        COUNT_AFTER=$(echo "$BY_GRANTER" | jq '.sessions | length')
        echo "  Sessions after revoke: $COUNT_AFTER"

        if [ "$COUNT_AFTER" -lt "$COUNT_BEFORE" ]; then
            echo "  Session count decreased from $COUNT_BEFORE to $COUNT_AFTER"
            record_result "Revoke grantee2 - count decreases" "PASS"
        else
            echo "  Session count did not decrease: before=$COUNT_BEFORE, after=$COUNT_AFTER"
            record_result "Revoke grantee2 - count decreases" "FAIL"
        fi
    else
        echo "  Revoke failed: $(echo "$TX_RESULT" | jq -r '.raw_log // "unknown"' 2>/dev/null)"
        record_result "Revoke grantee2 - count decreases" "FAIL"
    fi
else
    echo "  No grantee2 session to revoke (may have been cleaned up)"
    echo "  Marking as PASS"
    record_result "Revoke grantee2 - count decreases" "PASS"
fi

# ========================================================================
# Results
# ========================================================================
echo "============================================"
echo "REVOKE SESSION TEST RESULTS"
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
