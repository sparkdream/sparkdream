#!/bin/bash

echo "--- TESTING: BLOG MEMBERSHIP-DRIVEN PROMOTION QUEUE ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

echo "Alice (CORE, inviter): $ALICE_ADDR"
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

# ========================================================================
# Phase 1: Create a brand-new non-member account, fund it, have it create
# several ephemeral posts and one ephemeral reply BEFORE membership.
# ========================================================================
echo "=== PHASE 1: pre-admission ephemeral content ==="
echo ""

PROMOTEE_ACCOUNT="promoq_pre_admission"
if ! $BINARY keys show $PROMOTEE_ACCOUNT --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add $PROMOTEE_ACCOUNT --keyring-backend test --output json > /dev/null 2>&1
fi
PROMOTEE_ADDR=$($BINARY keys show $PROMOTEE_ACCOUNT -a --keyring-backend test)
echo "  Promotee account: $PROMOTEE_ADDR"

# Verify the promotee is NOT a member before we start (otherwise their content
# would already be permanent and the test would be meaningless).
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

# Create three ephemeral posts.
EPHEMERAL_POST_IDS=()
for i in 1 2 3; do
    TX_RES=$($BINARY tx blog create-post \
        "Pre-admission Post $i" \
        "Ephemeral content authored before becoming a member." \
        --min-reply-trust-level=-1 \
        --from $PROMOTEE_ACCOUNT \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        PID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
        EPHEMERAL_POST_IDS+=("$PID")
        echo "  Created ephemeral post #$i: ID=$PID"
    else
        echo "  Failed to create ephemeral post #$i"
    fi
done

# Create one ephemeral reply on the first post.
EPHEMERAL_REPLY_ID=""
if [ ${#EPHEMERAL_POST_IDS[@]} -gt 0 ]; then
    TX_RES=$($BINARY tx blog create-reply \
        ${EPHEMERAL_POST_IDS[0]} \
        "Pre-admission reply." \
        --from $PROMOTEE_ACCOUNT \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        EPHEMERAL_REPLY_ID=$(extract_event_value "$TX_RESULT" "blog.reply.created" "reply_id")
        echo "  Created ephemeral reply: ID=$EPHEMERAL_REPLY_ID"
    fi
fi

# Verify all three posts are ephemeral (expires_at > 0) right now.
ALL_EPHEMERAL=true
for PID in "${EPHEMERAL_POST_IDS[@]}"; do
    POST_Q=$($BINARY query blog show-post $PID --output json 2>&1)
    EXPIRES_AT=$(echo "$POST_Q" | jq -r '(.post.expires_at // 0)')
    if [ "$EXPIRES_AT" == "0" ]; then
        echo "  WARNING: post $PID was created with expires_at=0 — promotee may already be a member"
        ALL_EPHEMERAL=false
    fi
done

if [ "$ALL_EPHEMERAL" != "true" ] || [ ${#EPHEMERAL_POST_IDS[@]} -ne 3 ]; then
    echo "  Pre-admission fixture state is bad; aborting."
    exit 1
fi
echo "  All ${#EPHEMERAL_POST_IDS[@]} posts confirmed ephemeral (expires_at > 0)."
echo ""

# ========================================================================
# Phase 2: Promotee accepts invitation → AfterMemberAdmitted hook fires →
# author is enqueued → EndBlocker drains the queue → ephemerals become
# permanent.
# ========================================================================
echo "=== PHASE 2: invite + accept + drain ==="
echo ""

# Alice invites the promotee.
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

# Promotee accepts.
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

# Allow a couple of blocks for the EndBlocker drain pass to process all
# enqueued promotions (default MaxPromotionsPerBlock=50 should drain four
# items in one block, but we wait an extra block for finality).
echo "  Waiting two blocks for the EndBlocker drain to run..."
sleep 12

# ========================================================================
# TEST 1: All three pre-admission posts are now permanent (expires_at=0).
# ========================================================================
echo "--- TEST 1: All pre-admission posts auto-promoted ---"

ALL_PROMOTED=true
for PID in "${EPHEMERAL_POST_IDS[@]}"; do
    POST_Q=$($BINARY query blog show-post $PID --output json 2>&1)
    EXPIRES_AT=$(echo "$POST_Q" | jq -r '(.post.expires_at // 0)')
    if [ "$EXPIRES_AT" != "0" ]; then
        echo "  Post $PID NOT promoted (expires_at=$EXPIRES_AT)"
        ALL_PROMOTED=false
    else
        echo "  Post $PID promoted (expires_at=0)"
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
    REPLY_Q=$($BINARY query blog show-reply $EPHEMERAL_REPLY_ID --output json 2>&1)
    EXPIRES_AT=$(echo "$REPLY_Q" | jq -r '(.reply.expires_at // 0)')
    if [ "$EXPIRES_AT" == "0" ]; then
        echo "  Reply $EPHEMERAL_REPLY_ID promoted"
        record_result "Pre-admission reply auto-promoted" "PASS"
    else
        echo "  Reply $EPHEMERAL_REPLY_ID NOT promoted (expires_at=$EXPIRES_AT)"
        record_result "Pre-admission reply auto-promoted" "FAIL"
    fi
else
    record_result "Pre-admission reply auto-promoted" "FAIL"
fi

# ========================================================================
# TEST 3: Pin markers were NOT set on promoted content (strict separation —
# AfterMemberAdmitted only promotes; it never touches the pin marker).
# ========================================================================
echo "--- TEST 3: Auto-promotion does NOT set pin markers ---"

NO_PINS_SET=true
for PID in "${EPHEMERAL_POST_IDS[@]}"; do
    POST_Q=$($BINARY query blog show-post $PID --output json 2>&1)
    PINNED_BY=$(echo "$POST_Q" | jq -r '.post.pinned_by // ""')
    if [ -n "$PINNED_BY" ] && [ "$PINNED_BY" != "null" ]; then
        echo "  Post $PID unexpectedly has pinned_by=$PINNED_BY"
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

TX_RES=$($BINARY tx blog create-post \
    "Post-admission Post" \
    "Posts authored by an active member must be permanent on creation." \
    --from $PROMOTEE_ACCOUNT \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    POST_AFTER_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
    POST_Q=$($BINARY query blog show-post $POST_AFTER_ID --output json 2>&1)
    EXPIRES_AT=$(echo "$POST_Q" | jq -r '(.post.expires_at // 0)')
    if [ "$EXPIRES_AT" == "0" ]; then
        echo "  Post-admission post $POST_AFTER_ID created permanent"
        record_result "Post-admission content permanent on creation" "PASS"
    else
        echo "  Post $POST_AFTER_ID unexpectedly ephemeral (expires_at=$EXPIRES_AT)"
        record_result "Post-admission content permanent on creation" "FAIL"
    fi
else
    record_result "Post-admission content permanent on creation" "FAIL"
fi

# ========================================================================
# SUMMARY
# ========================================================================
echo "============================================"
echo "PROMOTION QUEUE TEST RESULTS"
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
