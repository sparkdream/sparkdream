#!/bin/bash

echo "--- TESTING: FORUM MAKE-PERMANENT (lifecycle promotion) ---"

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/../lib/denoms.sh"
source "$SCRIPT_DIR/_lib_params.sh"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    echo "   Run: bash setup_test_accounts.sh"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

# The default test ephemeral_ttl is 15s (post_test.sh part 19 exercises the
# fast prune). MakePostPermanent needs the ephemeral fixture to survive ~60s
# of `submit_tx_and_wait` cycles, so bump it before creating any fixtures.
bump_ephemeral_ttl 600 || {
    echo "Failed to bump ephemeral_ttl; aborting."
    exit 1
}

echo "Alice (gov authority via Commons Council): $ALICE_ADDR"
echo "Category for test posts:                   ${TEST_CATEGORY_ID:-1}"
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

CATEGORY_ID="${TEST_CATEGORY_ID:-1}"

# ========================================================================
# PREREQUISITE: a non-member account that produces ephemeral posts.
# Forum's create-post returns POST_STATUS_ACTIVE for everyone; ephemeral
# vs permanent is decided by whether the author is a rep member at create-
# time. We mint a fresh keyring entry that is funded with SPARK but never
# invited, so its posts pick up params.EphemeralTtl > 0.
# ========================================================================
echo "=== PREREQUISITE: Create ephemeral fixtures for promotion tests ==="
echo ""

NONMEMBER_ACCOUNT="forum_makeperm_nonmember"
if ! $BINARY keys show $NONMEMBER_ACCOUNT --keyring-backend test > /dev/null 2>&1; then
    $BINARY keys add $NONMEMBER_ACCOUNT --keyring-backend test --output json > /dev/null 2>&1
fi
NONMEMBER_ADDR=$($BINARY keys show $NONMEMBER_ACCOUNT -a --keyring-backend test)
echo "  Non-member account: $NONMEMBER_ADDR"

echo "  Funding non-member..."
TX_RES=$($BINARY tx bank send \
    alice $NONMEMBER_ADDR \
    20000000${BOND_DENOM} \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000${BOND_DENOM} \
    -y \
    --output json 2>&1)
submit_tx_and_wait "$TX_RES" > /dev/null

# Ephemeral root post — happy-path promotion target.
EPHEMERAL_POST_ID=""
TX_RES=$($BINARY tx forum create-post \
    $CATEGORY_ID 0 "Ephemeral forum post awaiting promotion." \
    --from $NONMEMBER_ACCOUNT \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    EPHEMERAL_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
    POST_Q=$($BINARY query forum get-post $EPHEMERAL_POST_ID --output json 2>&1)
    EXPIRATION_TIME=$(echo "$POST_Q" | jq -r '(.post.expiration_time // 0)')
    echo "  Ephemeral post: ID=$EPHEMERAL_POST_ID expiration_time=$EXPIRATION_TIME"
fi

# Ephemeral reply — happy-path promotion target for replies.
EPHEMERAL_REPLY_ID=""
if [ -n "$EPHEMERAL_POST_ID" ]; then
    TX_RES=$($BINARY tx forum create-post \
        $CATEGORY_ID $EPHEMERAL_POST_ID "Ephemeral reply awaiting promotion." \
        --from $NONMEMBER_ACCOUNT \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)
    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        EPHEMERAL_REPLY_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
        echo "  Ephemeral reply: ID=$EPHEMERAL_REPLY_ID"
    fi
fi

# Permanent post — used to assert MakePermanent is idempotent.
PERMANENT_POST_ID=""
TX_RES=$($BINARY tx forum create-post \
    $CATEGORY_ID 0 "Already-permanent post for idempotency test." \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    PERMANENT_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
    echo "  Permanent post: ID=$PERMANENT_POST_ID"
fi

# A second ephemeral post used by TEST 3 (pre-promotion pin must be rejected).
PIN_REJECT_POST_ID=""
TX_RES=$($BINARY tx forum create-post \
    $CATEGORY_ID 0 "Ephemeral post for pin-rejection test." \
    --from $NONMEMBER_ACCOUNT \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000${BOND_DENOM} \
    -y \
    --output json 2>&1)
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    PIN_REJECT_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
    echo "  Pin-reject ephemeral post: ID=$PIN_REJECT_POST_ID"
fi

echo ""
echo "=== MAKE-PERMANENT TESTS ==="
echo ""

# ========================================================================
# TEST 1: MakePostPermanent happy path. Alice (CORE) is well above the
# default PROVISIONAL gate.
# ========================================================================
echo "--- TEST 1: MakePostPermanent happy path ---"

if [ -n "$EPHEMERAL_POST_ID" ]; then
    TX_RES=$($BINARY tx forum make-post-permanent \
        $EPHEMERAL_POST_ID \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        POST_Q=$($BINARY query forum get-post $EPHEMERAL_POST_ID --output json 2>&1)
        EXP_TIME=$(echo "$POST_Q" | jq -r '(.post.expiration_time // 0)')
        PINNED=$(echo "$POST_Q" | jq -r '.post.pinned // false')

        if [ "$EXP_TIME" == "0" ] && [ "$PINNED" != "true" ]; then
            echo "  Post promoted: expiration_time=0, pinned=$PINNED (strict separation)"
            record_result "MakePostPermanent happy path" "PASS"
        else
            echo "  Expected expiration_time=0 + pinned=false, got expiration_time=$EXP_TIME pinned=$PINNED"
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
    TX_RES=$($BINARY tx forum make-post-permanent \
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
# TEST 3: Pin rejects ephemeral targets (ErrCannotPinEphemeral).
# Verifies the strict-separation gate is enforced at the pin entrypoint.
# ========================================================================
echo "--- TEST 3: PinPost rejects ephemeral target ---"

if [ -n "$PIN_REJECT_POST_ID" ]; then
    TX_RES=$($BINARY tx forum pin-post \
        $PIN_REJECT_POST_ID 1 \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)
        echo "  Correctly rejected: $RAW_LOG"
        record_result "PinPost rejects ephemeral target" "PASS"

        # Sanity: post still ephemeral, NOT pinned.
        POST_Q=$($BINARY query forum get-post $PIN_REJECT_POST_ID --output json 2>&1)
        EXP_TIME=$(echo "$POST_Q" | jq -r '(.post.expiration_time // 0)')
        PINNED=$(echo "$POST_Q" | jq -r '.post.pinned // false')
        echo "  Post state: expiration_time=$EXP_TIME pinned=$PINNED"
    else
        echo "  Should have been rejected by ephemeral-block"
        record_result "PinPost rejects ephemeral target" "FAIL"
    fi
else
    record_result "PinPost rejects ephemeral target" "FAIL"
fi

# ========================================================================
# TEST 4: MakePostPermanent on non-existent post.
# ========================================================================
echo "--- TEST 4: MakePostPermanent rejected for non-existent post ---"

TX_RES=$($BINARY tx forum make-post-permanent \
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
# TEST 5: MakePostPermanent works on a reply (forum unifies posts + replies).
# ========================================================================
echo "--- TEST 5: MakePostPermanent works on reply ---"

if [ -n "$EPHEMERAL_REPLY_ID" ]; then
    TX_RES=$($BINARY tx forum make-post-permanent \
        $EPHEMERAL_REPLY_ID \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        REPLY_Q=$($BINARY query forum get-post $EPHEMERAL_REPLY_ID --output json 2>&1)
        EXP_TIME=$(echo "$REPLY_Q" | jq -r '(.post.expiration_time // 0)')

        if [ "$EXP_TIME" == "0" ]; then
            echo "  Reply promoted: expiration_time=0"
            record_result "MakePostPermanent works on reply" "PASS"
        else
            echo "  Expected expiration_time=0, got $EXP_TIME"
            record_result "MakePostPermanent works on reply" "FAIL"
        fi
    else
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "MakePostPermanent works on reply" "FAIL"
    fi
else
    record_result "MakePostPermanent works on reply" "FAIL"
fi

# ========================================================================
# TEST 6: MakePostPermanent then PinPost round-trip — the two-step flow
# that the new separation requires. EPHEMERAL_POST_ID was already promoted
# in TEST 1; now the pin should succeed.
# ========================================================================
echo "--- TEST 6: MakePermanent then PinPost round-trip ---"

if [ -n "$EPHEMERAL_POST_ID" ]; then
    TX_RES=$($BINARY tx forum pin-post \
        $EPHEMERAL_POST_ID 1 \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000${BOND_DENOM} \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        POST_Q=$($BINARY query forum get-post $EPHEMERAL_POST_ID --output json 2>&1)
        EXP_TIME=$(echo "$POST_Q" | jq -r '(.post.expiration_time // 0)')
        PINNED=$(echo "$POST_Q" | jq -r '.post.pinned // false')

        if [ "$EXP_TIME" == "0" ] && [ "$PINNED" == "true" ]; then
            echo "  Round-trip success: expiration_time=0, pinned=true"
            record_result "MakePermanent then Pin round-trip" "PASS"
        else
            echo "  Expected expiration_time=0+pinned=true, got expiration_time=$EXP_TIME pinned=$PINNED"
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
echo "FORUM MAKE-PERMANENT TEST RESULTS"
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
