#!/bin/bash

echo "--- TESTING: FORUM MEMBERSHIP-DRIVEN PROMOTION QUEUE ---"

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/../lib/denoms.sh"
source "$SCRIPT_DIR/_lib_params.sh"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

# Pre-admission post creation + invite/accept + EndBlocker drain easily
# exceeds the default 15s ephemeral_ttl, which would prune the fixtures
# before they reach the promotion queue. Bump first.
bump_ephemeral_ttl 600 || {
    echo "Failed to bump ephemeral_ttl; aborting."
    exit 1
}

echo "Alice (CORE, inviter):     $ALICE_ADDR"
echo "Test category id:          ${TEST_CATEGORY_ID:-1}"
echo ""

# ========================================================================
# Helpers
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

extract_event_value() {
    local TX_RESULT=$1
    local EVENT_TYPE=$2
    local ATTR_KEY=$3
    echo "$TX_RESULT" | jq -r ".events[] | select(.type==\"$EVENT_TYPE\") | .attributes[] | select(.key==\"$ATTR_KEY\") | .value" | tr -d '"'
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

CATEGORY_ID="${TEST_CATEGORY_ID:-1}"

# ========================================================================
# Phase 1: brand-new non-member account, fund it, create three ephemeral
# posts and one ephemeral reply BEFORE membership.
# ========================================================================
echo "=== PHASE 1: pre-admission ephemeral content ==="
echo ""

PROMOTEE_ACCOUNT="forum_promoq_pre_admission"
if ! $BINARY keys show $PROMOTEE_ACCOUNT --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add $PROMOTEE_ACCOUNT --keyring-backend test --output json > /dev/null 2>&1
fi
PROMOTEE_ADDR=$($BINARY keys show $PROMOTEE_ACCOUNT -a --keyring-backend test)
echo "  Promotee account: $PROMOTEE_ADDR"

# Promotee must NOT already be a member or the test is meaningless.
MEMBER_INFO=$($BINARY query rep get-member $PROMOTEE_ADDR --output json 2>&1)
if ! echo "$MEMBER_INFO" | grep -q "not found"; then
    echo "  PROMOTEE is unexpectedly already a member; aborting."
    exit 1
fi

echo "  Funding promotee with SPARK for gas..."
TX_RES=$($BINARY tx bank send \
    alice $PROMOTEE_ADDR \
    50000000${BOND_DENOM} \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)
sleep 6

# Three ephemeral posts.
EPHEMERAL_POST_IDS=()
for i in 1 2 3; do
    TX_RES=$($BINARY tx forum create-post \
        $CATEGORY_ID 0 "Pre-admission forum post $i" \
        --from $PROMOTEE_ACCOUNT \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        PID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        EPHEMERAL_POST_IDS+=("$PID")
        echo "  Created ephemeral post #$i: ID=$PID"
    else
        echo "  Failed to create ephemeral post #$i: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
    fi
done

# One ephemeral reply on the first post.
EPHEMERAL_REPLY_ID=""
if [ ${#EPHEMERAL_POST_IDS[@]} -gt 0 ]; then
    TX_RES=$($BINARY tx forum create-post \
        $CATEGORY_ID ${EPHEMERAL_POST_IDS[0]} "Pre-admission ephemeral reply." \
        --from $PROMOTEE_ACCOUNT \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        EPHEMERAL_REPLY_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        echo "  Created ephemeral reply: ID=$EPHEMERAL_REPLY_ID"
    fi
fi

# Verify all three posts are ephemeral right now.
ALL_EPHEMERAL=true
for PID in "${EPHEMERAL_POST_IDS[@]}"; do
    POST_Q=$($BINARY query forum get-post $PID --output json 2>&1)
    EXP_TIME=$(echo "$POST_Q" | jq -r '(.post.expiration_time // 0)')
    if [ "$EXP_TIME" == "0" ]; then
        echo "  WARNING: post $PID was created with expiration_time=0 — promotee may already be a member"
        ALL_EPHEMERAL=false
    fi
done

if [ "$ALL_EPHEMERAL" != "true" ] || [ ${#EPHEMERAL_POST_IDS[@]} -ne 3 ]; then
    echo "  Pre-admission fixture state is bad; aborting."
    exit 1
fi
echo "  All ${#EPHEMERAL_POST_IDS[@]} posts confirmed ephemeral (expiration_time > 0)."
echo ""

# ========================================================================
# Phase 2: Promotee accepts invitation → AfterMemberAdmitted hook fires →
# author is enqueued → EndBlocker drains the queue → ephemerals become
# permanent.
# ========================================================================
echo "=== PHASE 2: invite + accept + drain ==="
echo ""

REQUIRED_STAKE=$($BINARY query rep required-invitation-stake "$ALICE_ADDR" --output json 2>/dev/null \
    | jq -r '.required_stake // "100000000"')
echo "  Required invitation stake: $REQUIRED_STAKE"

TX_RES=$($BINARY tx rep invite-member \
    $PROMOTEE_ADDR \
    "$REQUIRED_STAKE" \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    INVITATION_ID=$(extract_event_value "$TX_RESULT" "create_invitation" "invitation_id")
    echo "  Invitation created: ID=$INVITATION_ID"
else
    echo "  Failed to create invitation: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
    exit 1
fi

TX_RES=$($BINARY tx rep accept-invitation \
    $INVITATION_ID \
    --from $PROMOTEE_ACCOUNT \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    echo "  Promotee accepted invitation; AfterMemberAdmitted hook should have fired."
else
    echo "  Failed to accept invitation: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
    exit 1
fi

# Wait two blocks for the EndBlocker drain pass to process the four enqueued
# promotions (default MaxPromotionsPerBlock=50 → one block suffices, but we
# wait one extra for finality).
echo "  Waiting two blocks for the EndBlocker drain to run..."
sleep 12

# ========================================================================
# TEST 1: All three pre-admission posts are now permanent.
# ========================================================================
echo "--- TEST 1: All pre-admission posts auto-promoted ---"

ALL_PROMOTED=true
for PID in "${EPHEMERAL_POST_IDS[@]}"; do
    POST_Q=$($BINARY query forum get-post $PID --output json 2>&1)
    EXP_TIME=$(echo "$POST_Q" | jq -r '(.post.expiration_time // 0)')
    if [ "$EXP_TIME" != "0" ]; then
        echo "  Post $PID NOT promoted (expiration_time=$EXP_TIME)"
        ALL_PROMOTED=false
    else
        echo "  Post $PID promoted (expiration_time=0)"
    fi
done

if [ "$ALL_PROMOTED" == "true" ]; then
    record_result "All pre-admission posts auto-promoted" "PASS"
else
    record_result "All pre-admission posts auto-promoted" "FAIL"
fi

# ========================================================================
# TEST 2: Pre-admission reply is also promoted.
# ========================================================================
echo "--- TEST 2: Pre-admission reply auto-promoted ---"

if [ -n "$EPHEMERAL_REPLY_ID" ]; then
    REPLY_Q=$($BINARY query forum get-post $EPHEMERAL_REPLY_ID --output json 2>&1)
    EXP_TIME=$(echo "$REPLY_Q" | jq -r '(.post.expiration_time // 0)')
    if [ "$EXP_TIME" == "0" ]; then
        echo "  Reply $EPHEMERAL_REPLY_ID promoted"
        record_result "Pre-admission reply auto-promoted" "PASS"
    else
        echo "  Reply $EPHEMERAL_REPLY_ID NOT promoted (expiration_time=$EXP_TIME)"
        record_result "Pre-admission reply auto-promoted" "FAIL"
    fi
else
    record_result "Pre-admission reply auto-promoted" "FAIL"
fi

# ========================================================================
# TEST 3: Pin markers were NOT set on promoted content. AfterMemberAdmitted
# only promotes; it must never touch the pin marker (strict separation).
# ========================================================================
echo "--- TEST 3: Auto-promotion does NOT set pin markers ---"

NO_PINS_SET=true
for PID in "${EPHEMERAL_POST_IDS[@]}"; do
    POST_Q=$($BINARY query forum get-post $PID --output json 2>&1)
    PINNED=$(echo "$POST_Q" | jq -r '.post.pinned // false')
    if [ "$PINNED" == "true" ]; then
        echo "  Post $PID unexpectedly has pinned=true"
        NO_PINS_SET=false
    fi
done

if [ "$NO_PINS_SET" == "true" ]; then
    record_result "Auto-promotion does NOT set pin markers" "PASS"
else
    record_result "Auto-promotion does NOT set pin markers" "FAIL"
fi

# ========================================================================
# TEST 4: Post-admission content from the promotee is created permanent
# directly (the standard active-member behavior remains unchanged).
# ========================================================================
echo "--- TEST 4: Post-admission content is permanent on creation ---"

TX_RES=$($BINARY tx forum create-post \
    $CATEGORY_ID 0 "Post-admission forum post; should be permanent on creation." \
    --from $PROMOTEE_ACCOUNT \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    POST_AFTER_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
    POST_Q=$($BINARY query forum get-post $POST_AFTER_ID --output json 2>&1)
    EXP_TIME=$(echo "$POST_Q" | jq -r '(.post.expiration_time // 0)')
    if [ "$EXP_TIME" == "0" ]; then
        echo "  Post-admission post $POST_AFTER_ID created permanent"
        record_result "Post-admission content permanent on creation" "PASS"
    else
        echo "  Post $POST_AFTER_ID unexpectedly ephemeral (expiration_time=$EXP_TIME)"
        record_result "Post-admission content permanent on creation" "FAIL"
    fi
else
    record_result "Post-admission content permanent on creation" "FAIL"
fi

# ========================================================================
# SUMMARY
# ========================================================================
echo "============================================"
echo "FORUM PROMOTION QUEUE TEST RESULTS"
echo "============================================"

for i in "${!TEST_NAMES[@]}"; do
    printf "  %-55s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done

echo ""
echo "  Passed: $PASS_COUNT / $((PASS_COUNT + FAIL_COUNT))"
echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME PROMOTION QUEUE TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL PROMOTION QUEUE TESTS PASSED <<<"
    exit 0
fi
