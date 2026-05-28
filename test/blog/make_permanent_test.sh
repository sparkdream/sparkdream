#!/bin/bash

echo "--- TESTING: BLOG MAKE-PERMANENT (lifecycle promotion) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

echo "Alice (CORE):         $ALICE_ADDR"
echo "Blogger1 (NEW):       $BLOGGER1_ADDR"
echo ""

# ========================================================================
# Helper Functions (shared shape with the rest of the blog E2E suite)
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

check_tx_failure() {
    local TX_RESULT=$1
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    [ "$CODE" != "0" ]
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
# PREREQUISITE: Set up a non-member account that produces ephemeral content,
# plus pre-seed an ephemeral post + reply we can promote.
# ========================================================================
echo "=== PREREQUISITE: Create ephemeral fixtures for promotion tests ==="
echo ""

NONMEMBER_ACCOUNT="makepermtest_nonmember"
if ! $BINARY keys show $NONMEMBER_ACCOUNT --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add $NONMEMBER_ACCOUNT --keyring-backend test --output json > /dev/null 2>&1
fi
NONMEMBER_ADDR=$($BINARY keys show $NONMEMBER_ACCOUNT -a --keyring-backend test)
echo "  Non-member account: $NONMEMBER_ADDR"

echo "  Funding non-member..."
TX_RES=$($BINARY tx bank send \
    alice $NONMEMBER_ADDR \
    10000000${BOND_DENOM} \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)
sleep 6

# EPHEMERAL_POST_ID — happy-path promotion target.
EPHEMERAL_POST_ID=""
TX_RES=$($BINARY tx blog create-post \
    "Ephemeral Post for MakePermanent" \
    "Non-member content waiting for promotion." \
    --min-reply-trust-level=-1 \
    --from $NONMEMBER_ACCOUNT \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    EPHEMERAL_POST_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
    POST_Q=$($BINARY query blog show-post $EPHEMERAL_POST_ID --output json 2>&1)
    EXPIRES_AT=$(echo "$POST_Q" | jq -r '(.post.expires_at // 0)')
    echo "  Ephemeral post: ID=$EPHEMERAL_POST_ID expires_at=$EXPIRES_AT"
fi

# EPHEMERAL_REPLY_ID — happy-path promotion target for replies.
EPHEMERAL_REPLY_ID=""
if [ -n "$EPHEMERAL_POST_ID" ]; then
    TX_RES=$($BINARY tx blog create-reply \
        $EPHEMERAL_POST_ID \
        "Ephemeral reply waiting for promotion." \
        --from $NONMEMBER_ACCOUNT \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        EPHEMERAL_REPLY_ID=$(extract_event_value "$TX_RESULT" "blog.reply.created" "reply_id")
        echo "  Ephemeral reply: ID=$EPHEMERAL_REPLY_ID"
    fi
fi

# Second ephemeral post for the gate-rejection test (we don't want to consume
# our happy-path target since rejection still consumes a rate-limit slot).
EPHEMERAL_POST_GATE_ID=""
TX_RES=$($BINARY tx blog create-post \
    "Ephemeral Post for Gate Test" \
    "Promotion must be rejected for sub-PROVISIONAL callers." \
    --min-reply-trust-level=-1 \
    --from $NONMEMBER_ACCOUNT \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    EPHEMERAL_POST_GATE_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
    echo "  Ephemeral post (gate-test): ID=$EPHEMERAL_POST_GATE_ID"
fi

# Permanent post — used to assert MakePermanent is idempotent on already-permanent.
PERMANENT_POST_ID=""
TX_RES=$($BINARY tx blog create-post \
    "Already Permanent" \
    "Promoting this must be an idempotent no-op." \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    PERMANENT_POST_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
    echo "  Permanent post: ID=$PERMANENT_POST_ID"
fi

# Hidden ephemeral post for the rejection test.
HIDDEN_EPHEMERAL_POST_ID=""
TX_RES=$($BINARY tx blog create-post \
    "Ephemeral to Hide" \
    "MakePostPermanent must reject hidden content." \
    --min-reply-trust-level=-1 \
    --from $NONMEMBER_ACCOUNT \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    HIDDEN_EPHEMERAL_POST_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
    # The author hides their own post.
    TX_RES=$($BINARY tx blog hide-post \
        $HIDDEN_EPHEMERAL_POST_ID \
        --from $NONMEMBER_ACCOUNT \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)
    submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT" \
        && echo "  Hidden ephemeral post: ID=$HIDDEN_EPHEMERAL_POST_ID" \
        || echo "  Failed to hide post (continuing)"
fi

echo ""
echo "=== MAKE-PERMANENT TESTS ==="
echo ""

# ========================================================================
# TEST 1: MakePostPermanent happy path (alice = CORE, well above PROVISIONAL).
# Post must flip to expires_at=0 with NO pin marker change.
# ========================================================================
echo "--- TEST 1: MakePostPermanent happy path ---"

if [ -n "$EPHEMERAL_POST_ID" ]; then
    TX_RES=$($BINARY tx blog make-post-permanent \
        $EPHEMERAL_POST_ID \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        POST_Q=$($BINARY query blog show-post $EPHEMERAL_POST_ID --output json 2>&1)
        EXPIRES_AT=$(echo "$POST_Q" | jq -r '(.post.expires_at // 0)')
        PINNED_BY=$(echo "$POST_Q" | jq -r '.post.pinned_by // ""')

        if [ "$EXPIRES_AT" == "0" ] && { [ -z "$PINNED_BY" ] || [ "$PINNED_BY" == "null" ]; }; then
            echo "  Post promoted: expires_at=0, pinned_by NOT set (strict separation)"
            record_result "MakePostPermanent happy path" "PASS"
        else
            echo "  Expected expires_at=0 + empty pinned_by, got expires_at=$EXPIRES_AT pinned_by=$PINNED_BY"
            record_result "MakePostPermanent happy path" "FAIL"
        fi
    else
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "MakePostPermanent happy path" "FAIL"
    fi
else
    record_result "MakePostPermanent happy path" "FAIL"
fi

# ========================================================================
# TEST 2: MakePostPermanent is idempotent on an already-permanent post.
# ========================================================================
echo "--- TEST 2: MakePostPermanent idempotent on permanent post ---"

if [ -n "$PERMANENT_POST_ID" ]; then
    TX_RES=$($BINARY tx blog make-post-permanent \
        $PERMANENT_POST_ID \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        record_result "MakePostPermanent idempotent on permanent" "PASS"
    else
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "MakePostPermanent idempotent on permanent" "FAIL"
    fi
else
    record_result "MakePostPermanent idempotent on permanent" "FAIL"
fi

# ========================================================================
# TEST 3: Insufficient trust level (blogger1 starts at TRUST_LEVEL_NEW, below
# the PROVISIONAL floor for MakePermanentMinTrustLevel).
# ========================================================================
echo "--- TEST 3: MakePostPermanent rejected below PROVISIONAL ---"

if [ -n "$EPHEMERAL_POST_GATE_ID" ]; then
    TX_RES=$($BINARY tx blog make-post-permanent \
        $EPHEMERAL_POST_GATE_ID \
        --from blogger1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)
        echo "  Correctly rejected: $RAW_LOG"
        record_result "MakePostPermanent rejected below PROVISIONAL" "PASS"

        # Confirm post is still ephemeral.
        POST_Q=$($BINARY query blog show-post $EPHEMERAL_POST_GATE_ID --output json 2>&1)
        EXPIRES_AT=$(echo "$POST_Q" | jq -r '(.post.expires_at // 0)')
        echo "  Post still ephemeral (expires_at=$EXPIRES_AT)"
    else
        echo "  Should have been rejected by trust-level gate"
        record_result "MakePostPermanent rejected below PROVISIONAL" "FAIL"
    fi
else
    record_result "MakePostPermanent rejected below PROVISIONAL" "FAIL"
fi

# ========================================================================
# TEST 4: MakePostPermanent on non-existent post.
# ========================================================================
echo "--- TEST 4: MakePostPermanent rejected for non-existent post ---"

TX_RES=$($BINARY tx blog make-post-permanent \
    999999 \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    record_result "MakePostPermanent non-existent rejected" "PASS"
else
    record_result "MakePostPermanent non-existent rejected" "FAIL"
fi

# ========================================================================
# TEST 5: MakePostPermanent on hidden post.
# ========================================================================
echo "--- TEST 5: MakePostPermanent rejected for hidden post ---"

if [ -n "$HIDDEN_EPHEMERAL_POST_ID" ]; then
    TX_RES=$($BINARY tx blog make-post-permanent \
        $HIDDEN_EPHEMERAL_POST_ID \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        record_result "MakePostPermanent rejected for hidden" "PASS"
    else
        record_result "MakePostPermanent rejected for hidden" "FAIL"
    fi
else
    record_result "MakePostPermanent rejected for hidden" "FAIL"
fi

# ========================================================================
# TEST 6: MakeReplyPermanent happy path.
# ========================================================================
echo "--- TEST 6: MakeReplyPermanent happy path ---"

if [ -n "$EPHEMERAL_REPLY_ID" ]; then
    TX_RES=$($BINARY tx blog make-reply-permanent \
        $EPHEMERAL_REPLY_ID \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        REPLY_Q=$($BINARY query blog show-reply $EPHEMERAL_REPLY_ID --output json 2>&1)
        EXPIRES_AT=$(echo "$REPLY_Q" | jq -r '(.reply.expires_at // 0)')
        PINNED_BY=$(echo "$REPLY_Q" | jq -r '.reply.pinned_by // ""')

        if [ "$EXPIRES_AT" == "0" ] && { [ -z "$PINNED_BY" ] || [ "$PINNED_BY" == "null" ]; }; then
            echo "  Reply promoted: expires_at=0, pinned_by NOT set"
            record_result "MakeReplyPermanent happy path" "PASS"
        else
            echo "  Expected expires_at=0 + empty pinned_by, got expires_at=$EXPIRES_AT pinned_by=$PINNED_BY"
            record_result "MakeReplyPermanent happy path" "FAIL"
        fi
    else
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "MakeReplyPermanent happy path" "FAIL"
    fi
else
    record_result "MakeReplyPermanent happy path" "FAIL"
fi

# ========================================================================
# TEST 7: After MakePostPermanent, Pin succeeds (strict-separation round-trip).
# Demonstrates the new two-step flow: MakePostPermanent → PinPost.
# ========================================================================
echo "--- TEST 7: MakePostPermanent then PinPost round-trip ---"

if [ -n "$EPHEMERAL_POST_ID" ]; then
    # EPHEMERAL_POST_ID has already been promoted in TEST 1. Now pinning must succeed.
    TX_RES=$($BINARY tx blog pin-post \
        $EPHEMERAL_POST_ID \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        POST_Q=$($BINARY query blog show-post $EPHEMERAL_POST_ID --output json 2>&1)
        EXPIRES_AT=$(echo "$POST_Q" | jq -r '(.post.expires_at // 0)')
        PINNED_BY=$(echo "$POST_Q" | jq -r '.post.pinned_by // ""')

        if [ "$EXPIRES_AT" == "0" ] && [ -n "$PINNED_BY" ] && [ "$PINNED_BY" != "null" ]; then
            echo "  Round-trip success: expires_at=0, pinned_by=${PINNED_BY:0:20}..."
            record_result "MakePermanent then Pin round-trip" "PASS"
        else
            record_result "MakePermanent then Pin round-trip" "FAIL"
        fi
    else
        record_result "MakePermanent then Pin round-trip" "FAIL"
    fi
else
    record_result "MakePermanent then Pin round-trip" "FAIL"
fi

# ========================================================================
# SUMMARY
# ========================================================================
echo "============================================"
echo "MAKE-PERMANENT TEST RESULTS"
echo "============================================"

for i in "${!TEST_NAMES[@]}"; do
    printf "  %-50s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done

echo ""
echo "  Passed: $PASS_COUNT / $((PASS_COUNT + FAIL_COUNT))"
echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME MAKE-PERMANENT TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL MAKE-PERMANENT TESTS PASSED <<<"
    exit 0
fi
