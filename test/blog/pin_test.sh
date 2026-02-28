#!/bin/bash

echo "--- TESTING: BLOG PIN POST & PIN REPLY ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

echo "Blogger 1:  $BLOGGER1_ADDR"
echo "Blogger 2:  $BLOGGER2_ADDR"
echo "Alice:      $ALICE_ADDR"
echo ""

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
        TX_RESULT=""
        return 1
    fi

    # Check if tx was rejected at broadcast time (code != 0 in broadcast response)
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
# PREREQUISITE: Create ephemeral posts and replies for pinning tests
# ========================================================================
# Anonymous posts are ephemeral (have ExpiresAt > 0), so we use them.
# Regular posts created with create-post are permanent (ExpiresAt=0) by default,
# so we need anonymous posts or to configure ephemeral_content_ttl.

echo "=== PREREQUISITE: Create ephemeral content via anonymous posting ==="
echo ""

# We need ZK proof infrastructure (trust tree root, dummy proof)
# Reuse the same pattern from anon_test.sh

# Deterministic ZK keys (must be different from anon_test.sh to avoid conflicts)
PIN_ZK_KEY1="c10c10c10c10c10c10c10c10c10c10c10c10c10c10c10c10c10c10c10c10c10c"
PIN_ENC_KEY1="c111111111111111111111111111111111111111111111111111111111111111"
PIN_ZK_KEY2="c20c20c20c20c20c20c20c20c20c20c20c20c20c20c20c20c20c20c20c20c20c"
PIN_ENC_KEY2="c222222222222222222222222222222222222222222222222222222222222222"

register_voter() {
    local ACCOUNT=$1
    local ZK_KEY_HEX=$2
    local ENC_KEY_HEX=$3

    local ZK_KEY_B64=$(echo "$ZK_KEY_HEX" | xxd -r -p | base64)
    local ENC_KEY_B64=$(echo "$ENC_KEY_HEX" | xxd -r -p | base64)

    echo "  Registering $ACCOUNT as voter..."

    TX_RES=$($BINARY tx vote register-voter \
        --zk-public-key "$ZK_KEY_B64" \
        --encryption-public-key "$ENC_KEY_B64" \
        --from $ACCOUNT \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)

    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        echo "  Failed to register $ACCOUNT: no txhash"
        return 1
    fi

    sleep 6
    TX_RESULT=$(wait_for_tx $TXHASH)

    if check_tx_success "$TX_RESULT"; then
        echo "  Registered $ACCOUNT as voter"
        return 0
    else
        RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // "Unknown error"')
        if echo "$RAW_LOG" | grep -qi "already.*regist\|use.*rotate"; then
            echo "  $ACCOUNT is already registered as voter (OK)"
            return 0
        fi
        echo "  Failed to register $ACCOUNT: $RAW_LOG"
        return 1
    fi
}

# Register voters (may already be registered from anon_test)
register_voter "blogger1" "$PIN_ZK_KEY1" "$PIN_ENC_KEY1"
register_voter "blogger2" "$PIN_ZK_KEY2" "$PIN_ENC_KEY2"

echo ""

# Get trust tree root
echo "  Querying trust tree root..."
ABCI_RESPONSE=$(curl -s "http://localhost:26657/abci_query?path=\"/store/rep/key\"&data=0x74727573745f747265652f726f6f74" 2>&1)
TRUST_ROOT_B64=$(echo "$ABCI_RESPONSE" | jq -r '.result.response.value // ""')

if [ -z "$TRUST_ROOT_B64" ] || [ "$TRUST_ROOT_B64" == "null" ]; then
    echo "  Trust tree not found. Waiting for EndBlocker rebuild..."
    sleep 12
    ABCI_RESPONSE=$(curl -s "http://localhost:26657/abci_query?path=\"/store/rep/key\"&data=0x74727573745f747265652f726f6f74" 2>&1)
    TRUST_ROOT_B64=$(echo "$ABCI_RESPONSE" | jq -r '.result.response.value // ""')
fi

if [ -z "$TRUST_ROOT_B64" ] || [ "$TRUST_ROOT_B64" == "null" ]; then
    echo "  ERROR: Trust tree root not available. Pin tests need ephemeral content."
    echo "  Attempting to use regular posts instead (will test ErrContentNotEphemeral)."
    TRUST_ROOT_B64=""
fi

DUMMY_PROOF_B64=$(echo -n "deadbeef" | xxd -r -p | base64)

# Create ephemeral post 1 (for pinning)
EPHEMERAL_POST_ID=""
if [ -n "$TRUST_ROOT_B64" ]; then
    NULLIFIER_PIN1="dd01000000000000000000000000000000000000000000000000000000000001"
    NULLIFIER_PIN1_B64=$(echo "$NULLIFIER_PIN1" | xxd -r -p | base64)

    TX_RES=$($BINARY tx blog create-anonymous-post \
        "Ephemeral Post for Pinning" \
        "This ephemeral post will be pinned to make it permanent." \
        --proof "$DUMMY_PROOF_B64" \
        --nullifier "$NULLIFIER_PIN1_B64" \
        --merkle-root "$TRUST_ROOT_B64" \
        --min-trust-level 2 \
        --from blogger1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 500000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        EPHEMERAL_POST_ID=$(extract_event_value "$TX_RESULT" "blog.anonymous_post.created" "post_id")
        echo "  Ephemeral post created: ID=$EPHEMERAL_POST_ID"
    else
        echo "  Failed to create ephemeral post"
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
    fi
fi

# Create ephemeral post 2 (for hiding + pin fail test)
EPHEMERAL_POST_2_ID=""
if [ -n "$TRUST_ROOT_B64" ]; then
    NULLIFIER_PIN2="dd02000000000000000000000000000000000000000000000000000000000002"
    NULLIFIER_PIN2_B64=$(echo "$NULLIFIER_PIN2" | xxd -r -p | base64)

    TX_RES=$($BINARY tx blog create-anonymous-post \
        "Ephemeral Post for Hide Test" \
        "This post will be hidden, then we try to pin it." \
        --proof "$DUMMY_PROOF_B64" \
        --nullifier "$NULLIFIER_PIN2_B64" \
        --merkle-root "$TRUST_ROOT_B64" \
        --min-trust-level 2 \
        --from blogger1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 500000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        EPHEMERAL_POST_2_ID=$(extract_event_value "$TX_RESULT" "blog.anonymous_post.created" "post_id")
        echo "  Ephemeral post 2 created: ID=$EPHEMERAL_POST_2_ID"
    else
        echo "  Failed to create ephemeral post 2"
    fi
fi

# Create ephemeral reply (for reply pinning)
EPHEMERAL_REPLY_ID=""
if [ -n "$EPHEMERAL_POST_ID" ] && [ -n "$TRUST_ROOT_B64" ]; then
    NULLIFIER_PIN_R1="dd03000000000000000000000000000000000000000000000000000000000003"
    NULLIFIER_PIN_R1_B64=$(echo "$NULLIFIER_PIN_R1" | xxd -r -p | base64)

    TX_RES=$($BINARY tx blog create-anonymous-reply \
        $EPHEMERAL_POST_ID \
        "Ephemeral reply to be pinned." \
        --proof "$DUMMY_PROOF_B64" \
        --nullifier "$NULLIFIER_PIN_R1_B64" \
        --merkle-root "$TRUST_ROOT_B64" \
        --min-trust-level 2 \
        --from blogger2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 500000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        EPHEMERAL_REPLY_ID=$(extract_event_value "$TX_RESULT" "blog.anonymous_reply.created" "reply_id")
        echo "  Ephemeral reply created: ID=$EPHEMERAL_REPLY_ID"
    else
        echo "  Failed to create ephemeral reply"
    fi
fi

# Create a regular (permanent) post for "not ephemeral" test.
# Use blogger2 to avoid rate limiting blogger1 (who creates many posts in prior tests).
TX_RES=$($BINARY tx blog create-post \
    "Permanent Post for Pin Fail" \
    "This permanent post cannot be pinned because it is not ephemeral." \
    --from blogger2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

PERMANENT_POST_ID=""
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    PERMANENT_POST_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
    echo "  Permanent post created: ID=$PERMANENT_POST_ID"
else
    RAW_LOG=$(echo "${TX_RESULT:-$TX_RES}" | jq -r '.raw_log // .message // "unknown error"' 2>/dev/null)
    echo "  Failed to create permanent post: $RAW_LOG"
fi

# Create a second regular post that will be hidden (for TEST 7: pin-hidden-post)
# NOTE: regular member posts are permanent (ExpiresAt=0), but PinPost checks HIDDEN status
# BEFORE checking ephemeral, so we can test ErrPostHidden using a hidden permanent post.
# Use reader1 to avoid rate limiting blogger1/blogger2 (who create many posts in prior tests).
TX_RES=$($BINARY tx blog create-post \
    "Post to Hide for Pin Test" \
    "This post will be hidden so we can test that pinning a hidden post is rejected." \
    --from reader1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

HIDDEN_POST_FOR_PIN_ID=""
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    HIDDEN_POST_FOR_PIN_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
    echo "  Post to hide created: ID=$HIDDEN_POST_FOR_PIN_ID"

    # Hide it immediately (reader1 is the creator, so they can hide it)
    TX_RES=$($BINARY tx blog hide-post \
        $HIDDEN_POST_FOR_PIN_ID \
        --from reader1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        echo "  Post $HIDDEN_POST_FOR_PIN_ID hidden (for pin-hidden test)"
    else
        echo "  Failed to hide post $HIDDEN_POST_FOR_PIN_ID"
        HIDDEN_POST_FOR_PIN_ID=""
    fi
else
    RAW_LOG=$(echo "${TX_RESULT:-$TX_RES}" | jq -r '.raw_log // .message // "unknown error"' 2>/dev/null)
    echo "  Failed to create post for hiding: $RAW_LOG"
fi

# Create a post to delete (for pin-deleted test) - use reader1 to avoid rate limits
TX_RES=$($BINARY tx blog create-post \
    "Post to Delete for Pin Test" \
    "This post will be deleted." \
    --from reader1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

DELETED_POST_ID=""
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    DELETED_POST_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
    echo "  Post to delete created: ID=$DELETED_POST_ID"

    # Delete it (reader1 is the creator)
    TX_RES=$($BINARY tx blog delete-post $DELETED_POST_ID \
        --from reader1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        echo "  Post $DELETED_POST_ID deleted"
    fi
else
    RAW_LOG=$(echo "${TX_RESULT:-$TX_RES}" | jq -r '.raw_log // .message // "unknown error"' 2>/dev/null)
    echo "  Failed to create post to delete: $RAW_LOG"
fi

echo ""
echo "=== PIN TESTS ==="
echo ""

# ========================================================================
# TEST 1: Pin an ephemeral post (happy path)
# ========================================================================
echo "--- TEST 1: Pin an ephemeral post (happy path) ---"

if [ -n "$EPHEMERAL_POST_ID" ]; then
    # Use alice (TRUST_LEVEL_CORE) since pin requires trust_level >= PinMinTrustLevel (ESTABLISHED=2)
    TX_RES=$($BINARY tx blog pin-post \
        $EPHEMERAL_POST_ID \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        PINNED_BY=$(extract_event_value "$TX_RESULT" "blog.post.pinned" "pinned_by")
        echo "  Post $EPHEMERAL_POST_ID pinned by: $PINNED_BY"
        record_result "Pin ephemeral post (happy path)" "PASS"
    else
        echo "  Failed to pin post"
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Pin ephemeral post (happy path)" "FAIL"
    fi
else
    echo "  Skipped (no ephemeral post available)"
    record_result "Pin ephemeral post (happy path)" "FAIL"
fi

# ========================================================================
# TEST 2: Query pinned post — verify ExpiresAt=0, PinnedBy set
# ========================================================================
echo "--- TEST 2: Query pinned post — verify permanence ---"

if [ -n "$EPHEMERAL_POST_ID" ]; then
    POST_Q=$($BINARY query blog show-post $EPHEMERAL_POST_ID --output json 2>&1)
    # expires_at=0 means permanent; proto3 omits zero-value fields from JSON,
    # so use // 0 to substitute the default when the field is absent.
    EXPIRES_AT=$(echo "$POST_Q" | jq -r '(.post.expires_at // 0)')
    PINNED_BY=$(echo "$POST_Q" | jq -r '.post.pinned_by // ""')

    if [ "$EXPIRES_AT" == "0" ] && [ -n "$PINNED_BY" ] && [ "$PINNED_BY" != "null" ]; then
        echo "  ExpiresAt=$EXPIRES_AT (permanent), PinnedBy=${PINNED_BY:0:20}..."
        record_result "Query pinned post permanence" "PASS"
    else
        echo "  Expected ExpiresAt=0 and PinnedBy set, got ExpiresAt=$EXPIRES_AT, PinnedBy=$PINNED_BY"
        record_result "Query pinned post permanence" "FAIL"
    fi
else
    echo "  Skipped (no ephemeral post)"
    record_result "Query pinned post permanence" "FAIL"
fi

# ========================================================================
# TEST 3: Pin an ephemeral reply (happy path)
# ========================================================================
echo "--- TEST 3: Pin an ephemeral reply (happy path) ---"

if [ -n "$EPHEMERAL_REPLY_ID" ]; then
    # Use alice (TRUST_LEVEL_CORE) since pin requires trust_level >= PinMinTrustLevel (ESTABLISHED=2)
    TX_RES=$($BINARY tx blog pin-reply \
        $EPHEMERAL_REPLY_ID \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        PINNED_BY=$(extract_event_value "$TX_RESULT" "blog.reply.pinned" "pinned_by")
        echo "  Reply $EPHEMERAL_REPLY_ID pinned by: $PINNED_BY"
        record_result "Pin ephemeral reply (happy path)" "PASS"
    else
        echo "  Failed to pin reply"
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Pin ephemeral reply (happy path)" "FAIL"
    fi
else
    echo "  Skipped (no ephemeral reply available)"
    record_result "Pin ephemeral reply (happy path)" "FAIL"
fi

# ========================================================================
# TEST 4: Fail — pin a non-ephemeral post (ErrContentNotEphemeral)
# ========================================================================
echo "--- TEST 4: Fail — pin a non-ephemeral post ---"

if [ -n "$PERMANENT_POST_ID" ]; then
    TX_RES=$($BINARY tx blog pin-post \
        $PERMANENT_POST_ID \
        --from blogger1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: post is not ephemeral"
        record_result "Pin non-ephemeral post rejected" "PASS"
    else
        echo "  Should have been rejected"
        record_result "Pin non-ephemeral post rejected" "FAIL"
    fi
else
    echo "  Skipped (no permanent post)"
    record_result "Pin non-ephemeral post rejected" "FAIL"
fi

# ========================================================================
# TEST 5: Fail — pin a non-existent post (ErrPostNotFound)
# ========================================================================
echo "--- TEST 5: Fail — pin a non-existent post ---"

TX_RES=$($BINARY tx blog pin-post \
    999999 \
    --from blogger1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    echo "  Correctly rejected: post not found"
    record_result "Pin non-existent post rejected" "PASS"
else
    echo "  Should have been rejected"
    record_result "Pin non-existent post rejected" "FAIL"
fi

# ========================================================================
# TEST 6: Fail — pin a deleted post (ErrPostDeleted)
# ========================================================================
echo "--- TEST 6: Fail — pin a deleted post ---"

if [ -n "$DELETED_POST_ID" ]; then
    TX_RES=$($BINARY tx blog pin-post \
        $DELETED_POST_ID \
        --from blogger1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: post is deleted"
        record_result "Pin deleted post rejected" "PASS"
    else
        echo "  Should have been rejected"
        record_result "Pin deleted post rejected" "FAIL"
    fi
else
    echo "  Skipped (no deleted post)"
    record_result "Pin deleted post rejected" "FAIL"
fi

# ========================================================================
# TEST 7: Fail — pin a hidden post (ErrPostHidden)
# ========================================================================
echo "--- TEST 7: Fail — pin a hidden post ---"

if [ -n "$HIDDEN_POST_FOR_PIN_ID" ]; then
    # HIDDEN_POST_FOR_PIN_ID is a regular (permanent) post that has been hidden.
    # PinPost checks HIDDEN status before checking ephemeral, so this will return ErrPostHidden.
    TX_RES=$($BINARY tx blog pin-post \
        $HIDDEN_POST_FOR_PIN_ID \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: post is hidden"
        record_result "Pin hidden post rejected" "PASS"
    else
        echo "  Should have been rejected (post is hidden)"
        record_result "Pin hidden post rejected" "FAIL"
    fi
else
    echo "  Skipped (no hidden post available)"
    record_result "Pin hidden post rejected" "FAIL"
fi

# ========================================================================
# TEST 8: Fail — pin an already-pinned post (ErrAlreadyPinned)
# ========================================================================
echo "--- TEST 8: Fail — pin an already-pinned post ---"

if [ -n "$EPHEMERAL_POST_ID" ]; then
    # Try to pin the same ephemeral post again (already pinned by alice in TEST 1)
    TX_RES=$($BINARY tx blog pin-post \
        $EPHEMERAL_POST_ID \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: post already pinned"
        record_result "Pin already-pinned post rejected" "PASS"
    else
        echo "  Should have been rejected"
        record_result "Pin already-pinned post rejected" "FAIL"
    fi
else
    echo "  Skipped (no pinned post)"
    record_result "Pin already-pinned post rejected" "FAIL"
fi

# ========================================================================
# TEST 9: Fail — pin a non-existent reply (ErrReplyNotFound)
# ========================================================================
echo "--- TEST 9: Fail — pin a non-existent reply ---"

TX_RES=$($BINARY tx blog pin-reply \
    999999 \
    --from blogger1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 50000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    echo "  Correctly rejected: reply not found"
    record_result "Pin non-existent reply rejected" "PASS"
else
    echo "  Should have been rejected"
    record_result "Pin non-existent reply rejected" "FAIL"
fi

# ========================================================================
# TEST 10: Fail — pin an already-pinned reply (ErrAlreadyPinned)
# ========================================================================
echo "--- TEST 10: Fail — pin an already-pinned reply ---"

if [ -n "$EPHEMERAL_REPLY_ID" ]; then
    # Try to pin the same ephemeral reply again (already pinned by alice in TEST 3)
    TX_RES=$($BINARY tx blog pin-reply \
        $EPHEMERAL_REPLY_ID \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: reply already pinned"
        record_result "Pin already-pinned reply rejected" "PASS"
    else
        echo "  Should have been rejected"
        record_result "Pin already-pinned reply rejected" "FAIL"
    fi
else
    echo "  Skipped (no pinned reply)"
    record_result "Pin already-pinned reply rejected" "FAIL"
fi

# ========================================================================
# TEST 11: Query pinned reply — verify ExpiresAt=0
# ========================================================================
echo "--- TEST 11: Query pinned reply — verify permanence ---"

if [ -n "$EPHEMERAL_REPLY_ID" ]; then
    REPLY_Q=$($BINARY query blog show-reply $EPHEMERAL_REPLY_ID --output json 2>&1)
    # expires_at=0 means permanent; proto3 omits zero-value fields from JSON,
    # so use // 0 to substitute the default when the field is absent.
    EXPIRES_AT=$(echo "$REPLY_Q" | jq -r '(.reply.expires_at // 0)')
    PINNED_BY=$(echo "$REPLY_Q" | jq -r '.reply.pinned_by // ""')

    if [ "$EXPIRES_AT" == "0" ] && [ -n "$PINNED_BY" ] && [ "$PINNED_BY" != "null" ]; then
        echo "  ExpiresAt=$EXPIRES_AT (permanent), PinnedBy=${PINNED_BY:0:20}..."
        record_result "Query pinned reply permanence" "PASS"
    else
        echo "  Expected ExpiresAt=0 and PinnedBy set, got ExpiresAt=$EXPIRES_AT, PinnedBy=$PINNED_BY"
        record_result "Query pinned reply permanence" "FAIL"
    fi
else
    echo "  Skipped (no pinned reply)"
    record_result "Query pinned reply permanence" "FAIL"
fi

# ========================================================================
# SUMMARY
# ========================================================================
echo "============================================"
echo "PIN TEST RESULTS"
echo "============================================"

for i in "${!TEST_NAMES[@]}"; do
    printf "  %-45s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done

echo ""
echo "  Passed: $PASS_COUNT / $((PASS_COUNT + FAIL_COUNT))"
echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME PIN TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL PIN TESTS PASSED <<<"
    exit 0
fi
