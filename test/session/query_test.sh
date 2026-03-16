#!/bin/bash

echo "--- TESTING: SESSION QUERIES ---"
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

# ========================================================================
# TEST 1: Query params
# ========================================================================
echo "--- TEST 1: Query params ---"

PARAMS=$($BINARY query session params --output json 2>&1)
MAX_SESSIONS=$(echo "$PARAMS" | jq -r '.params.max_sessions_per_granter // "0"')
MAX_MSG_TYPES=$(echo "$PARAMS" | jq -r '.params.max_msg_types_per_session // "0"')
CEILING_COUNT=$(echo "$PARAMS" | jq '.params.max_allowed_msg_types | length')
ACTIVE_COUNT=$(echo "$PARAMS" | jq '.params.allowed_msg_types | length')

if [ "$MAX_SESSIONS" = "10" ] && [ "$MAX_MSG_TYPES" = "20" ] && [ "$CEILING_COUNT" -gt "0" ] && [ "$ACTIVE_COUNT" -gt "0" ]; then
    echo "  Params: max_sessions=$MAX_SESSIONS, max_msg_types=$MAX_MSG_TYPES, ceiling=$CEILING_COUNT types, active=$ACTIVE_COUNT types"
    record_result "Query params" "PASS"
else
    echo "  Unexpected params: max_sessions=$MAX_SESSIONS, max_msg_types=$MAX_MSG_TYPES, ceiling=$CEILING_COUNT, active=$ACTIVE_COUNT"
    record_result "Query params" "FAIL"
fi

# ========================================================================
# TEST 2: Query allowed msg types
# ========================================================================
echo "--- TEST 2: Query allowed msg types ---"

ALLOWED=$($BINARY query session allowed-msg-types --output json 2>&1)
MAX_ALLOWED_COUNT=$(echo "$ALLOWED" | jq '.max_allowed_msg_types | length')
ALLOWED_COUNT=$(echo "$ALLOWED" | jq '.allowed_msg_types | length')

# Both should have 18 default types
if [ "$MAX_ALLOWED_COUNT" -ge "18" ] && [ "$ALLOWED_COUNT" -ge "18" ]; then
    echo "  Allowed msg types: ceiling=$MAX_ALLOWED_COUNT, active=$ALLOWED_COUNT"

    # Verify a known type is present
    HAS_BLOG=$(echo "$ALLOWED" | jq -r '.allowed_msg_types[] | select(. == "/sparkdream.blog.v1.MsgCreatePost")' | head -1)
    if [ -n "$HAS_BLOG" ]; then
        echo "  Contains /sparkdream.blog.v1.MsgCreatePost: yes"
        record_result "Query allowed msg types" "PASS"
    else
        echo "  Missing expected blog msg type"
        record_result "Query allowed msg types" "FAIL"
    fi
else
    echo "  Unexpected counts: ceiling=$MAX_ALLOWED_COUNT, active=$ALLOWED_COUNT"
    record_result "Query allowed msg types" "FAIL"
fi

# ========================================================================
# TEST 3: Query session by granter+grantee
# ========================================================================
echo "--- TEST 3: Query session by granter+grantee ---"

# Session from create_session_test.sh test 1 should exist
SESSION=$($BINARY query session session "$GRANTER_ADDR" "$GRANTEE1_ADDR" --output json 2>&1)
SESS_GRANTER=$(echo "$SESSION" | jq -r '.session.granter // empty')
SESS_GRANTEE=$(echo "$SESSION" | jq -r '.session.grantee // empty')
EXEC_COUNT=$(echo "$SESSION" | jq -r '.session.exec_count // "0"')

if [ "$SESS_GRANTER" = "$GRANTER_ADDR" ] && [ "$SESS_GRANTEE" = "$GRANTEE1_ADDR" ]; then
    echo "  Session found: granter=$SESS_GRANTER, grantee=$SESS_GRANTEE, exec_count=$EXEC_COUNT"
    record_result "Query session by granter+grantee" "PASS"
else
    echo "  Session not found or unexpected data"
    echo "  Raw: $(echo "$SESSION" | head -5)"
    record_result "Query session by granter+grantee" "FAIL"
fi

# ========================================================================
# TEST 4: Query session - verify spend_limit and allowed_msg_types
# ========================================================================
echo "--- TEST 4: Query session - verify fields ---"

SPEND_LIMIT_DENOM=$(echo "$SESSION" | jq -r '.session.spend_limit.denom // empty')
SPEND_LIMIT_AMT=$(echo "$SESSION" | jq -r '.session.spend_limit.amount // "0"')
MSG_TYPES=$(echo "$SESSION" | jq -r '.session.allowed_msg_types // []')
MSG_COUNT=$(echo "$SESSION" | jq '.session.allowed_msg_types | length')
EXPIRATION=$(echo "$SESSION" | jq -r '.session.expiration // empty')

if [ "$SPEND_LIMIT_DENOM" = "uspark" ] && [ "$SPEND_LIMIT_AMT" = "50000000" ] && [ "$MSG_COUNT" = "1" ] && [ -n "$EXPIRATION" ]; then
    echo "  spend_limit=50000000uspark, msg_types=$MSG_COUNT, expiration=$EXPIRATION"
    record_result "Query session - verify fields" "PASS"
else
    echo "  Unexpected: denom=$SPEND_LIMIT_DENOM, amount=$SPEND_LIMIT_AMT, msg_count=$MSG_COUNT, expiration=$EXPIRATION"
    record_result "Query session - verify fields" "FAIL"
fi

# ========================================================================
# TEST 5: Query sessions by granter
# ========================================================================
echo "--- TEST 5: Query sessions by granter ---"

BY_GRANTER=$($BINARY query session sessions-by-granter "$GRANTER_ADDR" --output json 2>&1)
SESSION_COUNT=$(echo "$BY_GRANTER" | jq '.sessions | length')

# Should have at least 2 sessions (from create_session_test.sh tests 1 & 2)
if [ "$SESSION_COUNT" -ge "2" ]; then
    echo "  Found $SESSION_COUNT sessions for granter"
    record_result "Query sessions by granter" "PASS"
else
    echo "  Expected at least 2 sessions, got $SESSION_COUNT"
    echo "  Raw: $(echo "$BY_GRANTER" | head -5)"
    record_result "Query sessions by granter" "FAIL"
fi

# ========================================================================
# TEST 6: Query sessions by grantee
# ========================================================================
echo "--- TEST 6: Query sessions by grantee ---"

BY_GRANTEE=$($BINARY query session sessions-by-grantee "$GRANTEE1_ADDR" --output json 2>&1)
SESSION_COUNT=$(echo "$BY_GRANTEE" | jq '.sessions | length')

# Should have exactly 1 session for grantee1
if [ "$SESSION_COUNT" -ge "1" ]; then
    FIRST_GRANTER=$(echo "$BY_GRANTEE" | jq -r '.sessions[0].granter // empty')
    echo "  Found $SESSION_COUNT session(s) for grantee1, granter=$FIRST_GRANTER"
    record_result "Query sessions by grantee" "PASS"
else
    echo "  Expected at least 1 session, got $SESSION_COUNT"
    record_result "Query sessions by grantee" "FAIL"
fi

# ========================================================================
# TEST 7: Query nonexistent session (error)
# ========================================================================
echo "--- TEST 7: Query nonexistent session ---"

FAKE_ADDR="sprkdrm1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqn2ccpe"
NONEXIST=$($BINARY query session session "$FAKE_ADDR" "$GRANTEE1_ADDR" --output json 2>&1)

if echo "$NONEXIST" | grep -qi "not found\|no active session"; then
    echo "  Correctly returned error for nonexistent session"
    record_result "Query nonexistent session" "PASS"
else
    # Check if it returned an empty/error response
    SESS_GRANTER=$(echo "$NONEXIST" | jq -r '.session.granter // empty' 2>/dev/null)
    if [ -z "$SESS_GRANTER" ]; then
        echo "  Correctly returned empty/error for nonexistent session"
        record_result "Query nonexistent session" "PASS"
    else
        echo "  Unexpected: got a session for nonexistent granter"
        record_result "Query nonexistent session" "FAIL"
    fi
fi

# ========================================================================
# TEST 8: Query sessions by granter with no sessions
# ========================================================================
echo "--- TEST 8: Query sessions by granter with no sessions ---"

FAKE_ADDR="sprkdrm1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqn2ccpe"
EMPTY=$($BINARY query session sessions-by-granter "$FAKE_ADDR" --output json 2>&1)
SESSION_COUNT=$(echo "$EMPTY" | jq '.sessions | length' 2>/dev/null)

if [ "$SESSION_COUNT" = "0" ] || [ -z "$SESSION_COUNT" ]; then
    echo "  Correctly returned 0 sessions for unknown granter"
    record_result "Query sessions by granter (empty)" "PASS"
else
    echo "  Expected 0 sessions, got $SESSION_COUNT"
    record_result "Query sessions by granter (empty)" "FAIL"
fi

# ========================================================================
# Results
# ========================================================================
echo "============================================"
echo "QUERY TEST RESULTS"
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
