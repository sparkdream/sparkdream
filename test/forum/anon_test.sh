#!/bin/bash

echo "--- TESTING: ANONYMOUS FORUM POSTS, REPLIES & REACTIONS (ZK PROOF BYPASS MODE) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

echo "Poster 1:   $POSTER1_ADDR"
echo "Poster 2:   $POSTER2_ADDR"
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
# PREREQUISITE: Register voters and build trust tree
# ========================================================================
echo "=== PREREQUISITE: Voter Registration & Trust Tree ==="
echo ""

# Deterministic test ZK public keys (32-byte hex).
# Must be unique per voter to avoid ErrDuplicatePublicKey.
POSTER1_ZK_KEY="f10f10f10f10f10f10f10f10f10f10f10f10f10f10f10f10f10f10f10f10f10f"
POSTER2_ZK_KEY="f20f20f20f20f20f20f20f20f20f20f20f20f20f20f20f20f20f20f20f20f20f"
ALICE_ZK_KEY_FORUM="af01af01af01af01af01af01af01af01af01af01af01af01af01af01af01af01"

# Encryption public keys (32 bytes, not used for forum but required by register-voter).
POSTER1_ENC_KEY="f111111111111111111111111111111111111111111111111111111111111111"
POSTER2_ENC_KEY="f222222222222222222222222222222222222222222222222222222222222222"
ALICE_ENC_KEY_FORUM="afafafafafafafafafafafafafafafafafafafafafafafafafafafafafafafafafaf"

register_voter() {
    local ACCOUNT=$1
    local ZK_KEY_HEX=$2
    local ENC_KEY_HEX=$3

    # Convert hex to base64 for protobuf bytes fields
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
        echo "  Response: $TX_RES"
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

# Register voters (poster1, poster2, alice)
register_voter "poster1" "$POSTER1_ZK_KEY" "$POSTER1_ENC_KEY"
register_voter "poster2" "$POSTER2_ZK_KEY" "$POSTER2_ENC_KEY"
register_voter "alice" "$ALICE_ZK_KEY_FORUM" "$ALICE_ENC_KEY_FORUM"

echo ""

# Check if trust tree root already exists
echo "  Checking if trust tree is already built..."
ABCI_RESPONSE=$(curl -s "http://localhost:26657/abci_query?path=\"/store/rep/key\"&data=0x74727573745f747265652f726f6f74" 2>&1)
TRUST_ROOT_B64=$(echo "$ABCI_RESPONSE" | jq -r '.result.response.value // ""')

if [ -z "$TRUST_ROOT_B64" ] || [ "$TRUST_ROOT_B64" == "null" ]; then
    # Trust tree hasn't been built with voters yet. Trigger MarkTrustTreeDirty
    # by accepting an invitation (AcceptInvitation sets the dirty flag).
    echo "  Trust tree not built yet. Creating a dummy invitation to trigger rebuild..."

    # Create a dummy account key
    if ! $BINARY keys show forum_anon_trigger --keyring-backend test > /dev/null 2>&1; then
        $BINARY keys add forum_anon_trigger --keyring-backend test --output json > /dev/null 2>&1
    fi
    TRIGGER_ADDR=$($BINARY keys show forum_anon_trigger -a --keyring-backend test)

    # Fund it
    TX_RES=$($BINARY tx bank send \
        alice $TRIGGER_ADDR \
        10000000uspark \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
    fi

    # Invite
    TX_RES=$($BINARY tx rep invite-member \
        $TRIGGER_ADDR \
        "100000000" \
        --from alice \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 5000uspark \
        -y \
        --output json 2>&1)
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
        sleep 6
        TX_RESULT=$(wait_for_tx $TXHASH)
        TRIGGER_INV_ID=$(extract_event_value "$TX_RESULT" "create_invitation" "invitation_id")
        echo "  Trigger invitation created: #$TRIGGER_INV_ID"
    fi

    # Accept invitation (this marks the trust tree dirty)
    if [ -n "$TRIGGER_INV_ID" ]; then
        TX_RES=$($BINARY tx rep accept-invitation \
            $TRIGGER_INV_ID \
            --from forum_anon_trigger \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 5000uspark \
            -y \
            --output json 2>&1)
        TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
        if [ -n "$TXHASH" ] && [ "$TXHASH" != "null" ]; then
            sleep 6
            TX_RESULT=$(wait_for_tx $TXHASH)
            if check_tx_success "$TX_RESULT"; then
                echo "  Trigger accepted — trust tree dirty flag set"
            fi
        fi
    fi

    echo "  Waiting for EndBlocker to rebuild trust tree (2 blocks)..."
    sleep 12
fi

# Query the trust tree root via CometBFT ABCI raw store query.
# Key = "trust_tree/root" in the "rep" store.
# Hex of "trust_tree/root" = 74727573745f747265652f726f6f74
echo "  Querying trust tree root..."

ABCI_RESPONSE=$(curl -s "http://localhost:26657/abci_query?path=\"/store/rep/key\"&data=0x74727573745f747265652f726f6f74" 2>&1)
TRUST_ROOT_B64=$(echo "$ABCI_RESPONSE" | jq -r '.result.response.value // ""')

if [ -z "$TRUST_ROOT_B64" ] || [ "$TRUST_ROOT_B64" == "null" ]; then
    echo "  Trust tree root still not found. Waiting one more cycle..."
    sleep 12
    ABCI_RESPONSE=$(curl -s "http://localhost:26657/abci_query?path=\"/store/rep/key\"&data=0x74727573745f747265652f726f6f74" 2>&1)
    TRUST_ROOT_B64=$(echo "$ABCI_RESPONSE" | jq -r '.result.response.value // ""')
fi

if [ -z "$TRUST_ROOT_B64" ] || [ "$TRUST_ROOT_B64" == "null" ]; then
    echo "  ERROR: Trust tree root still not available after waiting."
    echo "  Anonymous tests cannot proceed without a trust tree."
    echo "  ABCI response: $ABCI_RESPONSE"
    exit 1
fi

# Convert base64 root to hex for display
TRUST_ROOT_HEX=$(echo "$TRUST_ROOT_B64" | base64 -d | xxd -p | tr -d '\n')
echo "  Trust tree root (hex): ${TRUST_ROOT_HEX:0:16}..."
echo ""

# Dummy proof: a single non-empty byte (verification is skipped in dev mode since
# no AnonActionVerifyingKey is configured, but the handler checks len(proof) > 0).
DUMMY_PROOF_B64=$(echo -n "deadbeef" | xxd -r -p | base64)

# ========================================================================
# PREREQUISITE: Create a category that allows anonymous posts
# ========================================================================
echo "=== PREREQUISITE: Create anonymous-enabled category ==="

TX_RES=$($BINARY tx forum create-category \
    "Anonymous Discussion" \
    "Category that allows anonymous posts" \
    false \
    false \
    --allow-anonymous \
    --from alice \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    ANON_CATEGORY_ID=$(extract_event_value "$TX_RESULT" "category_created" "category_id")
    echo "  Anonymous-enabled category created: ID=$ANON_CATEGORY_ID"
else
    echo "  Failed to create anonymous category"
    echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
    # Try to use the existing test category as fallback
    ANON_CATEGORY_ID=""
fi

# If we couldn't get the category ID from events, query the list
if [ -z "$ANON_CATEGORY_ID" ] || [ "$ANON_CATEGORY_ID" == "null" ]; then
    echo "  Querying category list to find anonymous-enabled category..."
    CATS=$($BINARY query forum list-category --output json 2>&1)
    # Find the last category (highest ID, which is the one we just created)
    ANON_CATEGORY_ID=$(echo "$CATS" | jq -r '.category[-1].category_id // empty' 2>/dev/null)
    echo "  Using category ID: $ANON_CATEGORY_ID"
fi

if [ -z "$ANON_CATEGORY_ID" ] || [ "$ANON_CATEGORY_ID" == "null" ]; then
    echo "  ERROR: Could not determine anonymous category ID. Cannot proceed."
    exit 1
fi

echo ""

# We also need a regular post in the anonymous category for reply tests.
# Create it now so we have a valid parent post ID.
echo "=== PREREQUISITE: Create a regular post for reply testing ==="

TX_RES=$($BINARY tx forum create-post \
    $ANON_CATEGORY_ID \
    0 \
    "Regular post for anonymous reply testing" \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 500000uspark \
    -y \
    --output json 2>&1)

REGULAR_POST_ID=""
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    REGULAR_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
    echo "  Regular post created: ID=$REGULAR_POST_ID"
else
    echo "  Failed to create regular post for reply testing"
    echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
fi

echo ""

# ========================================================================
# TEST 1: Create anonymous post (happy path)
# ========================================================================
echo "--- TEST 1: Create anonymous forum post ---"

NULLIFIER_1="aa00000000000000000000000000000000000000000000000000000000000001"
NULLIFIER_1_B64=$(echo "$NULLIFIER_1" | xxd -r -p | base64)

TX_RES=$($BINARY tx forum create-anonymous-post \
    $ANON_CATEGORY_ID \
    "This is an anonymous forum post, verified by ZK proof." \
    --proof "$DUMMY_PROOF_B64" \
    --nullifier "$NULLIFIER_1_B64" \
    --merkle-root "$TRUST_ROOT_B64" \
    --min-trust-level 2 \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 500000uspark \
    -y \
    --output json 2>&1)

ANON_POST_ID=""
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    ANON_POST_ID=$(extract_event_value "$TX_RESULT" "forum.anonymous_post.created" "post_id")
    echo "  Anonymous post created with ID: $ANON_POST_ID"
    record_result "Create anonymous post" "PASS"
else
    echo "  Failed to create anonymous post"
    echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
    record_result "Create anonymous post" "FAIL"
fi

# ========================================================================
# TEST 2: Query anonymous post metadata
# ========================================================================
echo "--- TEST 2: Query anonymous post metadata ---"

if [ -n "$ANON_POST_ID" ]; then
    META=$($BINARY query forum anonymous-post-meta $ANON_POST_ID --output json 2>&1)

    if echo "$META" | jq -e '.metadata' > /dev/null 2>&1; then
        PROVEN_LEVEL=$(echo "$META" | jq -r '.metadata.proven_trust_level // "0"')
        echo "  Metadata found: proven_trust_level=$PROVEN_LEVEL"
        record_result "Query anonymous post metadata" "PASS"
    else
        echo "  Metadata not found or unexpected format"
        echo "  Response: $META"
        record_result "Query anonymous post metadata" "FAIL"
    fi
else
    echo "  Skipped (no anonymous post ID from test 1)"
    record_result "Query anonymous post metadata" "FAIL"
fi

# ========================================================================
# TEST 3: Verify anonymous post creator is module address
# ========================================================================
echo "--- TEST 3: Verify creator is module address ---"

if [ -n "$ANON_POST_ID" ]; then
    POST_Q=$($BINARY query forum get-post $ANON_POST_ID --output json 2>&1)
    POST_CREATOR=$(echo "$POST_Q" | jq -r '.post.author // .author // ""')

    # The creator should NOT be poster1's address (it should be the forum module address)
    if [ "$POST_CREATOR" != "$POSTER1_ADDR" ] && [ -n "$POST_CREATOR" ]; then
        echo "  Creator is module address: ${POST_CREATOR:0:20}... (not submitter)"
        record_result "Creator is module address" "PASS"
    else
        echo "  Creator is submitter address (expected module address)"
        echo "  Creator: $POST_CREATOR"
        record_result "Creator is module address" "FAIL"
    fi
else
    echo "  Skipped (no anonymous post ID)"
    record_result "Creator is module address" "FAIL"
fi

# ========================================================================
# TEST 4: Check nullifier is marked as used (domain=3, scope=epoch)
# ========================================================================
echo "--- TEST 4: Check nullifier is used ---"

# The nullifier is stored as hex. Domain=3 (forum post), Scope=epoch.
# Default epoch duration is 13140000 seconds (~5 months).
# For a fresh chain with small block times, epoch = 0 or a small number.

NULL_QUERY=$($BINARY query forum is-nullifier-used "$NULLIFIER_1" 3 0 --output json 2>&1)

# Try scope=0 first; if not found, compute the actual epoch from block time
IS_USED=$(echo "$NULL_QUERY" | jq -r '.used // "false"')
if [ "$IS_USED" == "true" ]; then
    echo "  Nullifier correctly marked as used (domain=3, scope=0)"
    record_result "Nullifier marked as used" "PASS"
else
    # The epoch might not be 0 if the chain has been running a while.
    # Get current block time and compute epoch.
    BLOCK_TIME=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_time // ""')
    if [ -n "$BLOCK_TIME" ]; then
        UNIX_TIME=$(date -d "$BLOCK_TIME" +%s 2>/dev/null || echo "0")
        EPOCH=$((UNIX_TIME / 13140000))
        NULL_QUERY=$($BINARY query forum is-nullifier-used "$NULLIFIER_1" 3 $EPOCH --output json 2>&1)
        IS_USED=$(echo "$NULL_QUERY" | jq -r '.used // "false"')
    fi

    if [ "$IS_USED" == "true" ]; then
        echo "  Nullifier correctly marked as used (domain=3, scope=$EPOCH)"
        record_result "Nullifier marked as used" "PASS"
    else
        echo "  Nullifier not found as used"
        echo "  Response: $NULL_QUERY"
        record_result "Nullifier marked as used" "FAIL"
    fi
fi

# ========================================================================
# TEST 5: Duplicate nullifier rejected
# ========================================================================
echo "--- TEST 5: Duplicate nullifier rejected ---"

# Re-use the same nullifier — should be rejected
TX_RES=$($BINARY tx forum create-anonymous-post \
    $ANON_CATEGORY_ID \
    "Trying to reuse the same nullifier." \
    --proof "$DUMMY_PROOF_B64" \
    --nullifier "$NULLIFIER_1_B64" \
    --merkle-root "$TRUST_ROOT_B64" \
    --min-trust-level 2 \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 500000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    echo "  Correctly rejected: duplicate nullifier"
    record_result "Duplicate nullifier rejected" "PASS"
else
    echo "  Should have been rejected but was not"
    record_result "Duplicate nullifier rejected" "FAIL"
fi

# ========================================================================
# TEST 6: Anonymous post from second submitter
# ========================================================================
echo "--- TEST 6: Anonymous post from poster2 ---"

NULLIFIER_2="bb00000000000000000000000000000000000000000000000000000000000002"
NULLIFIER_2_B64=$(echo "$NULLIFIER_2" | xxd -r -p | base64)

TX_RES=$($BINARY tx forum create-anonymous-post \
    $ANON_CATEGORY_ID \
    "A different anonymous member shares their thoughts on the forum." \
    --proof "$DUMMY_PROOF_B64" \
    --nullifier "$NULLIFIER_2_B64" \
    --merkle-root "$TRUST_ROOT_B64" \
    --min-trust-level 2 \
    --from poster2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 500000uspark \
    -y \
    --output json 2>&1)

ANON_POST_2_ID=""
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    ANON_POST_2_ID=$(extract_event_value "$TX_RESULT" "forum.anonymous_post.created" "post_id")
    echo "  Anonymous post from poster2 created with ID: $ANON_POST_2_ID"
    record_result "Anonymous post from second submitter" "PASS"
else
    echo "  Failed to create anonymous post from poster2"
    echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
    record_result "Anonymous post from second submitter" "FAIL"
fi

# ========================================================================
# TEST 7: Invalid merkle root rejected
# ========================================================================
echo "--- TEST 7: Invalid merkle root rejected ---"

NULLIFIER_3="cc00000000000000000000000000000000000000000000000000000000000003"
NULLIFIER_3_B64=$(echo "$NULLIFIER_3" | xxd -r -p | base64)
BAD_ROOT_B64=$(echo "0000000000000000000000000000000000000000000000000000000000000000" | xxd -r -p | base64)

TX_RES=$($BINARY tx forum create-anonymous-post \
    $ANON_CATEGORY_ID \
    "This should fail because the merkle root is wrong." \
    --proof "$DUMMY_PROOF_B64" \
    --nullifier "$NULLIFIER_3_B64" \
    --merkle-root "$BAD_ROOT_B64" \
    --min-trust-level 2 \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 500000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    echo "  Correctly rejected: invalid merkle root"
    record_result "Invalid merkle root rejected" "PASS"
else
    echo "  Should have been rejected but was not"
    record_result "Invalid merkle root rejected" "FAIL"
fi

# ========================================================================
# TEST 8: Trust level too low rejected
# ========================================================================
echo "--- TEST 8: Trust level too low rejected ---"

NULLIFIER_4="dd00000000000000000000000000000000000000000000000000000000000004"
NULLIFIER_4_B64=$(echo "$NULLIFIER_4" | xxd -r -p | base64)

TX_RES=$($BINARY tx forum create-anonymous-post \
    $ANON_CATEGORY_ID \
    "This should fail because min_trust_level is too low." \
    --proof "$DUMMY_PROOF_B64" \
    --nullifier "$NULLIFIER_4_B64" \
    --merkle-root "$TRUST_ROOT_B64" \
    --min-trust-level 1 \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 500000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    echo "  Correctly rejected: trust level too low (1 < required 2)"
    record_result "Trust level too low rejected" "PASS"
else
    echo "  Should have been rejected but was not"
    record_result "Trust level too low rejected" "FAIL"
fi

# ========================================================================
# TEST 9: Empty proof rejected
# ========================================================================
echo "--- TEST 9: Empty proof rejected ---"

NULLIFIER_5="ee00000000000000000000000000000000000000000000000000000000000005"
NULLIFIER_5_B64=$(echo "$NULLIFIER_5" | xxd -r -p | base64)

TX_RES=$($BINARY tx forum create-anonymous-post \
    $ANON_CATEGORY_ID \
    "This should fail because proof is empty." \
    --proof "" \
    --nullifier "$NULLIFIER_5_B64" \
    --merkle-root "$TRUST_ROOT_B64" \
    --min-trust-level 2 \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 500000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    echo "  Correctly rejected: empty proof"
    record_result "Empty proof rejected" "PASS"
else
    echo "  Should have been rejected but was not"
    record_result "Empty proof rejected" "FAIL"
fi

# ========================================================================
# TEST 10: Empty content rejected for anonymous post
# ========================================================================
echo "--- TEST 10: Empty content rejected ---"

NULLIFIER_6="aa00000000000000000000000000000000000000000000000000000000000077"
NULLIFIER_6_B64=$(echo "$NULLIFIER_6" | xxd -r -p | base64)

TX_RES=$($BINARY tx forum create-anonymous-post \
    $ANON_CATEGORY_ID \
    "" \
    --proof "$DUMMY_PROOF_B64" \
    --nullifier "$NULLIFIER_6_B64" \
    --merkle-root "$TRUST_ROOT_B64" \
    --min-trust-level 2 \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 500000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    echo "  Correctly rejected: empty content"
    record_result "Empty content rejected" "PASS"
else
    echo "  Should have been rejected but was not"
    record_result "Empty content rejected" "FAIL"
fi

# ========================================================================
# TEST 11: Create anonymous reply (happy path)
# ========================================================================
echo "--- TEST 11: Create anonymous reply ---"

# Use either the anonymous post or the regular post as parent
REPLY_PARENT_ID="${ANON_POST_ID:-$REGULAR_POST_ID}"

if [ -n "$REPLY_PARENT_ID" ]; then
    NULLIFIER_R1="ff00000000000000000000000000000000000000000000000000000000000010"
    NULLIFIER_R1_B64=$(echo "$NULLIFIER_R1" | xxd -r -p | base64)

    TX_RES=$($BINARY tx forum create-anonymous-reply \
        $REPLY_PARENT_ID \
        "An anonymous reply to the forum post." \
        --proof "$DUMMY_PROOF_B64" \
        --nullifier "$NULLIFIER_R1_B64" \
        --merkle-root "$TRUST_ROOT_B64" \
        --min-trust-level 2 \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 500000uspark \
        -y \
        --output json 2>&1)

    ANON_REPLY_ID=""
    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        ANON_REPLY_ID=$(extract_event_value "$TX_RESULT" "forum.anonymous_reply.created" "post_id")
        echo "  Anonymous reply created with ID: $ANON_REPLY_ID"
        record_result "Create anonymous reply" "PASS"
    else
        echo "  Failed to create anonymous reply"
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Create anonymous reply" "FAIL"
    fi
else
    echo "  Skipped (no parent post ID available)"
    record_result "Create anonymous reply" "FAIL"
    ANON_REPLY_ID=""
fi

# ========================================================================
# TEST 12: Query anonymous reply metadata
# ========================================================================
echo "--- TEST 12: Query anonymous reply metadata ---"

if [ -n "$ANON_REPLY_ID" ]; then
    META=$($BINARY query forum anonymous-reply-meta $ANON_REPLY_ID --output json 2>&1)

    if echo "$META" | jq -e '.metadata' > /dev/null 2>&1; then
        PROVEN_LEVEL=$(echo "$META" | jq -r '.metadata.proven_trust_level // "0"')
        echo "  Reply metadata found: proven_trust_level=$PROVEN_LEVEL"
        record_result "Query anonymous reply metadata" "PASS"
    else
        echo "  Metadata not found or unexpected format"
        echo "  Response: $META"
        record_result "Query anonymous reply metadata" "FAIL"
    fi
else
    echo "  Skipped (no anonymous reply ID from test 11)"
    record_result "Query anonymous reply metadata" "FAIL"
fi

# ========================================================================
# TEST 13: Duplicate reply nullifier rejected (same thread)
# ========================================================================
echo "--- TEST 13: Duplicate reply nullifier rejected ---"

if [ -n "$REPLY_PARENT_ID" ]; then
    # Re-use NULLIFIER_R1 on the same thread — should be rejected (domain=4, scope=rootID)
    TX_RES=$($BINARY tx forum create-anonymous-reply \
        $REPLY_PARENT_ID \
        "Duplicate reply attempt." \
        --proof "$DUMMY_PROOF_B64" \
        --nullifier "$NULLIFIER_R1_B64" \
        --merkle-root "$TRUST_ROOT_B64" \
        --min-trust-level 2 \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 500000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: duplicate reply nullifier on same thread"
        record_result "Duplicate reply nullifier rejected" "PASS"
    else
        echo "  Should have been rejected but was not"
        record_result "Duplicate reply nullifier rejected" "FAIL"
    fi
else
    echo "  Skipped (no parent post ID)"
    record_result "Duplicate reply nullifier rejected" "FAIL"
fi

# ========================================================================
# TEST 14: Same nullifier on different thread (should succeed)
# ========================================================================
echo "--- TEST 14: Same reply nullifier on different thread ---"

# Reply nullifiers are scoped by rootID (domain=4, scope=rootID),
# so the same nullifier should work on a different thread.
if [ -n "$ANON_POST_2_ID" ]; then
    TX_RES=$($BINARY tx forum create-anonymous-reply \
        $ANON_POST_2_ID \
        "Replying to the second anonymous post with the same nullifier." \
        --proof "$DUMMY_PROOF_B64" \
        --nullifier "$NULLIFIER_R1_B64" \
        --merkle-root "$TRUST_ROOT_B64" \
        --min-trust-level 2 \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 500000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        CROSS_REPLY_ID=$(extract_event_value "$TX_RESULT" "forum.anonymous_reply.created" "post_id")
        echo "  Same nullifier accepted on different thread (reply ID: $CROSS_REPLY_ID)"
        record_result "Same nullifier on different thread" "PASS"
    else
        echo "  Failed: same nullifier should work on different thread"
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Same nullifier on different thread" "FAIL"
    fi
else
    echo "  Skipped (no second anonymous post ID)"
    record_result "Same nullifier on different thread" "FAIL"
fi

# ========================================================================
# TEST 15: Anonymous reply to non-existent post rejected
# ========================================================================
echo "--- TEST 15: Reply to non-existent post rejected ---"

NULLIFIER_R2="ff00000000000000000000000000000000000000000000000000000000000099"
NULLIFIER_R2_B64=$(echo "$NULLIFIER_R2" | xxd -r -p | base64)

TX_RES=$($BINARY tx forum create-anonymous-reply \
    999999 \
    "This post does not exist." \
    --proof "$DUMMY_PROOF_B64" \
    --nullifier "$NULLIFIER_R2_B64" \
    --merkle-root "$TRUST_ROOT_B64" \
    --min-trust-level 2 \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 500000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    echo "  Correctly rejected: post does not exist"
    record_result "Reply to non-existent post rejected" "PASS"
else
    echo "  Should have been rejected but was not"
    record_result "Reply to non-existent post rejected" "FAIL"
fi

# ========================================================================
# TEST 16: Empty content rejected for anonymous reply
# ========================================================================
echo "--- TEST 16: Empty content rejected for anonymous reply ---"

if [ -n "$REPLY_PARENT_ID" ]; then
    NULLIFIER_R4="ff00000000000000000000000000000000000000000000000000000000000077"
    NULLIFIER_R4_B64=$(echo "$NULLIFIER_R4" | xxd -r -p | base64)

    TX_RES=$($BINARY tx forum create-anonymous-reply \
        $REPLY_PARENT_ID \
        "" \
        --proof "$DUMMY_PROOF_B64" \
        --nullifier "$NULLIFIER_R4_B64" \
        --merkle-root "$TRUST_ROOT_B64" \
        --min-trust-level 2 \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 500000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: empty content"
        record_result "Empty content rejected for reply" "PASS"
    else
        echo "  Should have been rejected but was not"
        record_result "Empty content rejected for reply" "FAIL"
    fi
else
    echo "  Skipped (no parent post ID)"
    record_result "Empty content rejected for reply" "FAIL"
fi

# ========================================================================
# TEST 17: Anonymous upvote (happy path)
# ========================================================================
echo "--- TEST 17: Anonymous upvote ---"

REACT_TARGET_ID="${ANON_POST_ID:-$REGULAR_POST_ID}"

if [ -n "$REACT_TARGET_ID" ]; then
    NULLIFIER_REACT1="ab00000000000000000000000000000000000000000000000000000000000001"
    NULLIFIER_REACT1_B64=$(echo "$NULLIFIER_REACT1" | xxd -r -p | base64)

    TX_RES=$($BINARY tx forum anonymous-react \
        $REACT_TARGET_ID \
        1 \
        --proof "$DUMMY_PROOF_B64" \
        --nullifier "$NULLIFIER_REACT1_B64" \
        --merkle-root "$TRUST_ROOT_B64" \
        --min-trust-level 2 \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 500000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        REACT_TYPE=$(extract_event_value "$TX_RESULT" "forum.anonymous_react" "reaction_type")
        echo "  Anonymous upvote succeeded (reaction_type=$REACT_TYPE)"
        record_result "Anonymous upvote" "PASS"
    else
        echo "  Failed to anonymous upvote"
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Anonymous upvote" "FAIL"
    fi
else
    echo "  Skipped (no target post ID)"
    record_result "Anonymous upvote" "FAIL"
fi

# ========================================================================
# TEST 18: Anonymous downvote (happy path)
# ========================================================================
echo "--- TEST 18: Anonymous downvote ---"

# Use a different post (anon post 2) so we have a fresh nullifier scope
DOWNVOTE_TARGET_ID="${ANON_POST_2_ID:-$REGULAR_POST_ID}"

if [ -n "$DOWNVOTE_TARGET_ID" ]; then
    NULLIFIER_REACT2="ab00000000000000000000000000000000000000000000000000000000000002"
    NULLIFIER_REACT2_B64=$(echo "$NULLIFIER_REACT2" | xxd -r -p | base64)

    TX_RES=$($BINARY tx forum anonymous-react \
        $DOWNVOTE_TARGET_ID \
        2 \
        --proof "$DUMMY_PROOF_B64" \
        --nullifier "$NULLIFIER_REACT2_B64" \
        --merkle-root "$TRUST_ROOT_B64" \
        --min-trust-level 2 \
        --from poster2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 500000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        REACT_TYPE=$(extract_event_value "$TX_RESULT" "forum.anonymous_react" "reaction_type")
        echo "  Anonymous downvote succeeded (reaction_type=$REACT_TYPE)"
        record_result "Anonymous downvote" "PASS"
    else
        echo "  Failed to anonymous downvote"
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Anonymous downvote" "FAIL"
    fi
else
    echo "  Skipped (no target post ID)"
    record_result "Anonymous downvote" "FAIL"
fi

# ========================================================================
# TEST 19: Verify upvote count incremented
# ========================================================================
echo "--- TEST 19: Verify upvote count on post ---"

if [ -n "$REACT_TARGET_ID" ]; then
    POST_Q=$($BINARY query forum get-post $REACT_TARGET_ID --output json 2>&1)
    UPVOTE_COUNT=$(echo "$POST_Q" | jq -r '.post.upvote_count // .upvote_count // "0"')

    if [ "$UPVOTE_COUNT" -ge 1 ] 2>/dev/null; then
        echo "  Post $REACT_TARGET_ID has upvote_count=$UPVOTE_COUNT"
        record_result "Upvote count incremented" "PASS"
    else
        echo "  Expected upvote_count >= 1, got $UPVOTE_COUNT"
        echo "  Response: $POST_Q"
        record_result "Upvote count incremented" "FAIL"
    fi
else
    echo "  Skipped (no target post ID)"
    record_result "Upvote count incremented" "FAIL"
fi

# ========================================================================
# TEST 20: Duplicate reaction nullifier rejected (same post)
# ========================================================================
echo "--- TEST 20: Duplicate reaction nullifier rejected ---"

if [ -n "$REACT_TARGET_ID" ]; then
    # Re-use NULLIFIER_REACT1 on the same post — should be rejected (domain=5, scope=postId)
    TX_RES=$($BINARY tx forum anonymous-react \
        $REACT_TARGET_ID \
        1 \
        --proof "$DUMMY_PROOF_B64" \
        --nullifier "$NULLIFIER_REACT1_B64" \
        --merkle-root "$TRUST_ROOT_B64" \
        --min-trust-level 2 \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 500000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: duplicate reaction nullifier on same post"
        record_result "Duplicate reaction nullifier rejected" "PASS"
    else
        echo "  Should have been rejected but was not"
        record_result "Duplicate reaction nullifier rejected" "FAIL"
    fi
else
    echo "  Skipped (no target post ID)"
    record_result "Duplicate reaction nullifier rejected" "FAIL"
fi

# ========================================================================
# TEST 21: Same reaction nullifier on different post (should succeed)
# ========================================================================
echo "--- TEST 21: Same reaction nullifier on different post ---"

# Reaction nullifiers are scoped by postId (domain=5, scope=postId),
# so the same nullifier should work on a different post.
REACT_OTHER_ID="${ANON_POST_2_ID}"
if [ -z "$REACT_OTHER_ID" ] && [ -n "$REGULAR_POST_ID" ] && [ "$REGULAR_POST_ID" != "$REACT_TARGET_ID" ]; then
    REACT_OTHER_ID="$REGULAR_POST_ID"
fi

if [ -n "$REACT_OTHER_ID" ] && [ "$REACT_OTHER_ID" != "$REACT_TARGET_ID" ]; then
    TX_RES=$($BINARY tx forum anonymous-react \
        $REACT_OTHER_ID \
        1 \
        --proof "$DUMMY_PROOF_B64" \
        --nullifier "$NULLIFIER_REACT1_B64" \
        --merkle-root "$TRUST_ROOT_B64" \
        --min-trust-level 2 \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 500000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        echo "  Same nullifier accepted on different post"
        record_result "Same reaction nullifier on different post" "PASS"
    else
        echo "  Failed: same nullifier should work on different post"
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Same reaction nullifier on different post" "FAIL"
    fi
else
    echo "  Skipped (no different target post available)"
    record_result "Same reaction nullifier on different post" "FAIL"
fi

# ========================================================================
# TEST 22: Invalid reaction type rejected
# ========================================================================
echo "--- TEST 22: Invalid reaction type rejected ---"

if [ -n "$REACT_TARGET_ID" ]; then
    NULLIFIER_REACT3="ab00000000000000000000000000000000000000000000000000000000000003"
    NULLIFIER_REACT3_B64=$(echo "$NULLIFIER_REACT3" | xxd -r -p | base64)

    TX_RES=$($BINARY tx forum anonymous-react \
        $REACT_TARGET_ID \
        99 \
        --proof "$DUMMY_PROOF_B64" \
        --nullifier "$NULLIFIER_REACT3_B64" \
        --merkle-root "$TRUST_ROOT_B64" \
        --min-trust-level 2 \
        --from poster1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 500000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: invalid reaction type (99)"
        record_result "Invalid reaction type rejected" "PASS"
    else
        echo "  Should have been rejected but was not"
        record_result "Invalid reaction type rejected" "FAIL"
    fi
else
    echo "  Skipped (no target post ID)"
    record_result "Invalid reaction type rejected" "FAIL"
fi

# ========================================================================
# TEST 23: Reaction on non-existent post rejected
# ========================================================================
echo "--- TEST 23: Reaction on non-existent post rejected ---"

NULLIFIER_REACT4="ab00000000000000000000000000000000000000000000000000000000000004"
NULLIFIER_REACT4_B64=$(echo "$NULLIFIER_REACT4" | xxd -r -p | base64)

TX_RES=$($BINARY tx forum anonymous-react \
    999999 \
    1 \
    --proof "$DUMMY_PROOF_B64" \
    --nullifier "$NULLIFIER_REACT4_B64" \
    --merkle-root "$TRUST_ROOT_B64" \
    --min-trust-level 2 \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 500000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    echo "  Correctly rejected: post does not exist"
    record_result "Reaction on non-existent post rejected" "PASS"
else
    echo "  Should have been rejected but was not"
    record_result "Reaction on non-existent post rejected" "FAIL"
fi

# ========================================================================
# TEST 24: Check IsNullifierUsed query for reaction (domain=5)
# ========================================================================
echo "--- TEST 24: IsNullifierUsed query for reaction ---"

if [ -n "$REACT_TARGET_ID" ]; then
    NULL_QUERY=$($BINARY query forum is-nullifier-used "$NULLIFIER_REACT1" 5 $REACT_TARGET_ID --output json 2>&1)
    IS_USED=$(echo "$NULL_QUERY" | jq -r '.used // "false"')

    if [ "$IS_USED" == "true" ]; then
        echo "  Reaction nullifier correctly marked as used (domain=5, scope=$REACT_TARGET_ID)"
        record_result "IsNullifierUsed query for reaction" "PASS"
    else
        echo "  Reaction nullifier not found as used"
        echo "  Response: $NULL_QUERY"
        record_result "IsNullifierUsed query for reaction" "FAIL"
    fi
else
    echo "  Skipped (no target post ID)"
    record_result "IsNullifierUsed query for reaction" "FAIL"
fi

# ========================================================================
# SUMMARY
# ========================================================================
echo "============================================================================"
echo "  ANONYMOUS FORUM POST/REPLY/REACTION TEST RESULTS"
echo "============================================================================"
echo ""

for i in "${!TEST_NAMES[@]}"; do
    printf "  %-50s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done

echo ""
echo "  PASSED: $PASS_COUNT / $((PASS_COUNT + FAIL_COUNT))"
echo "  FAILED: $FAIL_COUNT / $((PASS_COUNT + FAIL_COUNT))"
echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME ANONYMOUS FORUM TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL ANONYMOUS FORUM TESTS PASSED <<<"
fi

echo ""
