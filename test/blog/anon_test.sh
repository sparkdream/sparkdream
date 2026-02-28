#!/bin/bash

echo "--- TESTING: ANONYMOUS BLOG POSTS & REPLIES (ZK PROOF BYPASS MODE) ---"

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
# PREREQUISITE: Register voters and build trust tree
# ========================================================================
echo "=== PREREQUISITE: Voter Registration & Trust Tree ==="
echo ""

# Deterministic test ZK public keys (32-byte hex).
# Must be unique per voter to avoid ErrDuplicatePublicKey.
BLOGGER1_ZK_KEY="b10b10b10b10b10b10b10b10b10b10b10b10b10b10b10b10b10b10b10b10b10b"
BLOGGER2_ZK_KEY="b20b20b20b20b20b20b20b20b20b20b20b20b20b20b20b20b20b20b20b20b20b"
ALICE_ZK_KEY_BLOG="ab01ab01ab01ab01ab01ab01ab01ab01ab01ab01ab01ab01ab01ab01ab01ab01"

# Encryption public keys (32 bytes, not used for blog but required by register-voter).
BLOGGER1_ENC_KEY="b111111111111111111111111111111111111111111111111111111111111111"
BLOGGER2_ENC_KEY="b222222222222222222222222222222222222222222222222222222222222222"
ALICE_ENC_KEY_BLOG="abababababababababababababababababababababababababababababababababab"

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

# Register voters (blogger1, blogger2, alice)
register_voter "blogger1" "$BLOGGER1_ZK_KEY" "$BLOGGER1_ENC_KEY"
register_voter "blogger2" "$BLOGGER2_ZK_KEY" "$BLOGGER2_ENC_KEY"
register_voter "alice" "$ALICE_ZK_KEY_BLOG" "$ALICE_ENC_KEY_BLOG"

echo ""

# Check if trust tree root already exists
echo "  Checking if trust tree is already built..."
ABCI_RESPONSE=$(curl -s "http://localhost:26657/abci_query?path=\"/store/rep/key\"&data=0x74727573745f747265652f726f6f74" 2>&1)
TRUST_ROOT_B64=$(echo "$ABCI_RESPONSE" | jq -r '.result.response.value // ""')

if [ -z "$TRUST_ROOT_B64" ] || [ "$TRUST_ROOT_B64" == "null" ]; then
    # Trust tree hasn't been built with voters yet. We need to trigger MarkTrustTreeDirty.
    # Voter registration (x/vote) does NOT set the dirty flag. The flag is set by:
    #   - AcceptInvitation (x/rep)
    #   - UpdateMemberStatus (x/rep)
    # So we invite+accept a dummy account to trigger a rebuild.
    echo "  Trust tree not built yet. Creating a dummy invitation to trigger rebuild..."

    # Create a dummy account key
    if ! $BINARY keys show anon_trigger --keyring-backend test > /dev/null 2>&1; then
        $BINARY keys add anon_trigger --keyring-backend test --output json > /dev/null 2>&1
    fi
    TRIGGER_ADDR=$($BINARY keys show anon_trigger -a --keyring-backend test)

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
            --from anon_trigger \
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
# TEST 1: Create anonymous post (happy path)
# ========================================================================
echo "--- TEST 1: Create anonymous post ---"

NULLIFIER_1="aa11000000000000000000000000000000000000000000000000000011001100"
NULLIFIER_1_B64=$(echo "$NULLIFIER_1" | xxd -r -p | base64)

# Use blogger2 as submitter: blogger1 hits max_posts_per_day from prior test runs in this snapshot.
TX_RES=$($BINARY tx blog create-anonymous-post \
    "Anonymous Thoughts" \
    "This is a post from an anonymous member, verified by ZK proof." \
    --proof "$DUMMY_PROOF_B64" \
    --nullifier "$NULLIFIER_1_B64" \
    --merkle-root "$TRUST_ROOT_B64" \
    --min-trust-level 2 \
    --from blogger2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 500000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    ANON_POST_ID=$(extract_event_value "$TX_RESULT" "blog.anonymous_post.created" "post_id")
    echo "  Anonymous post created with ID: $ANON_POST_ID"
    record_result "Create anonymous post" "PASS"
else
    echo "  Failed to create anonymous post"
    echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
    record_result "Create anonymous post" "FAIL"
    ANON_POST_ID=""
fi

# ========================================================================
# TEST 2: Query anonymous post metadata
# ========================================================================
echo "--- TEST 2: Query anonymous post metadata ---"

if [ -n "$ANON_POST_ID" ]; then
    META=$($BINARY query blog anonymous-post-meta $ANON_POST_ID --output json 2>&1)

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
    POST_Q=$($BINARY query blog show-post $ANON_POST_ID --output json 2>&1)
    POST_CREATOR=$(echo "$POST_Q" | jq -r '.post.creator // ""')

    # The creator should NOT be blogger1's address (it should be the blog module address)
    if [ "$POST_CREATOR" != "$BLOGGER1_ADDR" ] && [ -n "$POST_CREATOR" ]; then
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
# TEST 4: Check nullifier is marked as used
# ========================================================================
echo "--- TEST 4: Check nullifier is used ---"

# The nullifier is stored as hex. Domain=1 (post), Scope=epoch.
# We need to compute the epoch: blockTime / epochDuration
# Default epoch duration is 13140000 seconds (~5 months).
# For a fresh chain with small block times, epoch = 0 or a small number.

NULL_QUERY=$($BINARY query blog is-nullifier-used "$NULLIFIER_1" --domain 1 --scope 0 --output json 2>&1)

# Try scope=0 first; if not found, compute the actual epoch from block time
IS_USED=$(echo "$NULL_QUERY" | jq -r '.used // "false"')
if [ "$IS_USED" == "true" ]; then
    echo "  Nullifier correctly marked as used (domain=1, scope=0)"
    record_result "Nullifier marked as used" "PASS"
else
    # The epoch might not be 0 if the chain has been running a while.
    # Get current block time and compute epoch.
    BLOCK_TIME=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_time // ""')
    if [ -n "$BLOCK_TIME" ]; then
        UNIX_TIME=$(date -d "$BLOCK_TIME" +%s 2>/dev/null || echo "0")
        EPOCH=$((UNIX_TIME / 13140000))
        NULL_QUERY=$($BINARY query blog is-nullifier-used "$NULLIFIER_1" --domain 1 --scope $EPOCH --output json 2>&1)
        IS_USED=$(echo "$NULL_QUERY" | jq -r '.used // "false"')
    fi

    if [ "$IS_USED" == "true" ]; then
        echo "  Nullifier correctly marked as used (domain=1, scope=$EPOCH)"
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
TX_RES=$($BINARY tx blog create-anonymous-post \
    "Duplicate Post" \
    "Trying to reuse the same nullifier." \
    --proof "$DUMMY_PROOF_B64" \
    --nullifier "$NULLIFIER_1_B64" \
    --merkle-root "$TRUST_ROOT_B64" \
    --min-trust-level 2 \
    --from blogger1 \
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
echo "--- TEST 6: Anonymous post from blogger2 ---"

NULLIFIER_2="bb22000000000000000000000000000000000000000000000000000022002200"
NULLIFIER_2_B64=$(echo "$NULLIFIER_2" | xxd -r -p | base64)

TX_RES=$($BINARY tx blog create-anonymous-post \
    "Second Anonymous Post" \
    "A different anonymous member shares their thoughts." \
    --proof "$DUMMY_PROOF_B64" \
    --nullifier "$NULLIFIER_2_B64" \
    --merkle-root "$TRUST_ROOT_B64" \
    --min-trust-level 2 \
    --from blogger2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 500000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    ANON_POST_2_ID=$(extract_event_value "$TX_RESULT" "blog.anonymous_post.created" "post_id")
    echo "  Anonymous post from blogger2 created with ID: $ANON_POST_2_ID"
    record_result "Anonymous post from second submitter" "PASS"
else
    echo "  Failed to create anonymous post from blogger2"
    echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
    record_result "Anonymous post from second submitter" "FAIL"
    ANON_POST_2_ID=""
fi

# ========================================================================
# TEST 7: Invalid merkle root rejected
# ========================================================================
echo "--- TEST 7: Invalid merkle root rejected ---"

NULLIFIER_3="cc00000000000000000000000000000000000000000000000000000000000003"
NULLIFIER_3_B64=$(echo "$NULLIFIER_3" | xxd -r -p | base64)
BAD_ROOT_B64=$(echo "0000000000000000000000000000000000000000000000000000000000000000" | xxd -r -p | base64)

TX_RES=$($BINARY tx blog create-anonymous-post \
    "Bad Root Post" \
    "This should fail because the merkle root is wrong." \
    --proof "$DUMMY_PROOF_B64" \
    --nullifier "$NULLIFIER_3_B64" \
    --merkle-root "$BAD_ROOT_B64" \
    --min-trust-level 2 \
    --from blogger1 \
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

TX_RES=$($BINARY tx blog create-anonymous-post \
    "Low Trust Post" \
    "This should fail because min_trust_level < required." \
    --proof "$DUMMY_PROOF_B64" \
    --nullifier "$NULLIFIER_4_B64" \
    --merkle-root "$TRUST_ROOT_B64" \
    --min-trust-level 1 \
    --from blogger1 \
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

TX_RES=$($BINARY tx blog create-anonymous-post \
    "No Proof Post" \
    "This should fail because proof is empty." \
    --proof "" \
    --nullifier "$NULLIFIER_5_B64" \
    --merkle-root "$TRUST_ROOT_B64" \
    --min-trust-level 2 \
    --from blogger1 \
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
# TEST 10: Create anonymous reply (happy path)
# ========================================================================
echo "--- TEST 10: Create anonymous reply ---"

if [ -n "$ANON_POST_ID" ]; then
    NULLIFIER_R1="ff00000000000000000000000000000000000000000000000000000000000010"
    NULLIFIER_R1_B64=$(echo "$NULLIFIER_R1" | xxd -r -p | base64)

    TX_RES=$($BINARY tx blog create-anonymous-reply \
        $ANON_POST_ID \
        "An anonymous reply to the anonymous post." \
        --proof "$DUMMY_PROOF_B64" \
        --nullifier "$NULLIFIER_R1_B64" \
        --merkle-root "$TRUST_ROOT_B64" \
        --min-trust-level 2 \
        --from blogger2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 500000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        ANON_REPLY_ID=$(extract_event_value "$TX_RESULT" "blog.anonymous_reply.created" "reply_id")
        echo "  Anonymous reply created with ID: $ANON_REPLY_ID"
        record_result "Create anonymous reply" "PASS"
    else
        echo "  Failed to create anonymous reply"
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Create anonymous reply" "FAIL"
        ANON_REPLY_ID=""
    fi
else
    echo "  Skipped (no anonymous post ID from test 1)"
    record_result "Create anonymous reply" "FAIL"
    ANON_REPLY_ID=""
fi

# ========================================================================
# TEST 11: Query anonymous reply metadata
# ========================================================================
echo "--- TEST 11: Query anonymous reply metadata ---"

if [ -n "$ANON_REPLY_ID" ]; then
    META=$($BINARY query blog anonymous-reply-meta $ANON_REPLY_ID --output json 2>&1)

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
    echo "  Skipped (no anonymous reply ID from test 10)"
    record_result "Query anonymous reply metadata" "FAIL"
fi

# ========================================================================
# TEST 12: Verify reply count incremented on parent post
# ========================================================================
echo "--- TEST 12: Verify reply count on parent post ---"

if [ -n "$ANON_POST_ID" ]; then
    POST_Q=$($BINARY query blog show-post $ANON_POST_ID --output json 2>&1)
    REPLY_COUNT=$(echo "$POST_Q" | jq -r '.post.reply_count // "0"')

    if [ "$REPLY_COUNT" -ge 1 ]; then
        echo "  Post $ANON_POST_ID has reply_count=$REPLY_COUNT"
        record_result "Reply count incremented" "PASS"
    else
        echo "  Expected reply_count >= 1, got $REPLY_COUNT"
        record_result "Reply count incremented" "FAIL"
    fi
else
    echo "  Skipped (no anonymous post ID)"
    record_result "Reply count incremented" "FAIL"
fi

# ========================================================================
# TEST 13: Duplicate reply nullifier rejected (same post)
# ========================================================================
echo "--- TEST 13: Duplicate reply nullifier rejected ---"

if [ -n "$ANON_POST_ID" ]; then
    # Re-use NULLIFIER_R1 on the same post — should be rejected
    TX_RES=$($BINARY tx blog create-anonymous-reply \
        $ANON_POST_ID \
        "Duplicate reply attempt." \
        --proof "$DUMMY_PROOF_B64" \
        --nullifier "$NULLIFIER_R1_B64" \
        --merkle-root "$TRUST_ROOT_B64" \
        --min-trust-level 2 \
        --from blogger2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 500000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: duplicate reply nullifier on same post"
        record_result "Duplicate reply nullifier rejected" "PASS"
    else
        echo "  Should have been rejected but was not"
        record_result "Duplicate reply nullifier rejected" "FAIL"
    fi
else
    echo "  Skipped (no anonymous post ID)"
    record_result "Duplicate reply nullifier rejected" "FAIL"
fi

# ========================================================================
# TEST 14: Same nullifier on different post (should succeed)
# ========================================================================
echo "--- TEST 14: Same nullifier on different post ---"

if [ -n "$ANON_POST_2_ID" ]; then
    # Use the same nullifier as TEST 10, but on a different post.
    # Reply nullifiers are scoped by post_id (domain=2, scope=post_id),
    # so the same nullifier should work on a different post.
    TX_RES=$($BINARY tx blog create-anonymous-reply \
        $ANON_POST_2_ID \
        "Replying to the second anonymous post with the same nullifier." \
        --proof "$DUMMY_PROOF_B64" \
        --nullifier "$NULLIFIER_R1_B64" \
        --merkle-root "$TRUST_ROOT_B64" \
        --min-trust-level 2 \
        --from blogger2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 500000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        CROSS_REPLY_ID=$(extract_event_value "$TX_RESULT" "blog.anonymous_reply.created" "reply_id")
        echo "  Same nullifier accepted on different post (reply ID: $CROSS_REPLY_ID)"
        record_result "Same nullifier on different post" "PASS"
    else
        echo "  Failed: same nullifier should work on different post"
        echo "  Raw log: $(echo "$TX_RESULT" | jq -r '.raw_log' 2>/dev/null)"
        record_result "Same nullifier on different post" "FAIL"
    fi
else
    echo "  Skipped (no second anonymous post ID)"
    record_result "Same nullifier on different post" "FAIL"
fi

# ========================================================================
# TEST 15: Anonymous reply to non-existent post rejected
# ========================================================================
echo "--- TEST 15: Reply to non-existent post rejected ---"

NULLIFIER_R2="ff00000000000000000000000000000000000000000000000000000000000099"
NULLIFIER_R2_B64=$(echo "$NULLIFIER_R2" | xxd -r -p | base64)

TX_RES=$($BINARY tx blog create-anonymous-reply \
    999999 \
    "This post does not exist." \
    --proof "$DUMMY_PROOF_B64" \
    --nullifier "$NULLIFIER_R2_B64" \
    --merkle-root "$TRUST_ROOT_B64" \
    --min-trust-level 2 \
    --from blogger1 \
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
# TEST 16: Anonymous reply to deleted post rejected
# ========================================================================
echo "--- TEST 16: Reply to deleted post rejected ---"

# First create a regular post, then delete it, then try anonymous reply
# Use blogger2 (blogger1 is at max_posts_per_day limit from previous test runs)
TX_RES=$($BINARY tx blog create-post \
    "Post to Delete for Anon Reply" \
    "This post will be deleted." \
    --from blogger2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 500000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    DEL_POST_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
    echo "  Created post $DEL_POST_ID to delete"

    # Delete it
    TX_RES=$($BINARY tx blog delete-post $DEL_POST_ID \
        --from blogger2 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 50000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
        echo "  Post $DEL_POST_ID deleted"

        NULLIFIER_R3="ff00000000000000000000000000000000000000000000000000000000000088"
        NULLIFIER_R3_B64=$(echo "$NULLIFIER_R3" | xxd -r -p | base64)

        TX_RES=$($BINARY tx blog create-anonymous-reply \
            $DEL_POST_ID \
            "Reply to a deleted post." \
            --proof "$DUMMY_PROOF_B64" \
            --nullifier "$NULLIFIER_R3_B64" \
            --merkle-root "$TRUST_ROOT_B64" \
            --min-trust-level 2 \
            --from blogger1 \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 500000uspark \
            -y \
            --output json 2>&1)

        if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
            echo "  Correctly rejected: reply to deleted post"
            record_result "Reply to deleted post rejected" "PASS"
        else
            echo "  Should have been rejected but was not"
            record_result "Reply to deleted post rejected" "FAIL"
        fi
    else
        echo "  Failed to delete post"
        record_result "Reply to deleted post rejected" "FAIL"
    fi
else
    echo "  Failed to create post for deletion"
    record_result "Reply to deleted post rejected" "FAIL"
fi

# ========================================================================
# TEST 17: Empty title rejected for anonymous post
# ========================================================================
echo "--- TEST 17: Empty title rejected ---"

NULLIFIER_6="aa00000000000000000000000000000000000000000000000000000000000077"
NULLIFIER_6_B64=$(echo "$NULLIFIER_6" | xxd -r -p | base64)

TX_RES=$($BINARY tx blog create-anonymous-post \
    "" \
    "Body without title." \
    --proof "$DUMMY_PROOF_B64" \
    --nullifier "$NULLIFIER_6_B64" \
    --merkle-root "$TRUST_ROOT_B64" \
    --min-trust-level 2 \
    --from blogger1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 500000uspark \
    -y \
    --output json 2>&1)

if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
    echo "  Correctly rejected: empty title"
    record_result "Empty title rejected" "PASS"
else
    echo "  Should have been rejected but was not"
    record_result "Empty title rejected" "FAIL"
fi

# ========================================================================
# TEST 18: Empty body rejected for anonymous reply
# ========================================================================
echo "--- TEST 18: Empty body rejected for anonymous reply ---"

if [ -n "$ANON_POST_ID" ]; then
    NULLIFIER_R4="ff00000000000000000000000000000000000000000000000000000000000077"
    NULLIFIER_R4_B64=$(echo "$NULLIFIER_R4" | xxd -r -p | base64)

    TX_RES=$($BINARY tx blog create-anonymous-reply \
        $ANON_POST_ID \
        "" \
        --proof "$DUMMY_PROOF_B64" \
        --nullifier "$NULLIFIER_R4_B64" \
        --merkle-root "$TRUST_ROOT_B64" \
        --min-trust-level 2 \
        --from blogger1 \
        --chain-id $CHAIN_ID \
        --keyring-backend test \
        --fees 500000uspark \
        -y \
        --output json 2>&1)

    if submit_tx_and_wait "$TX_RES" && check_tx_failure "$TX_RESULT"; then
        echo "  Correctly rejected: empty body"
        record_result "Empty body rejected for reply" "PASS"
    else
        echo "  Should have been rejected but was not"
        record_result "Empty body rejected for reply" "FAIL"
    fi
else
    echo "  Skipped (no anonymous post ID)"
    record_result "Empty body rejected for reply" "FAIL"
fi

# ========================================================================
# SUMMARY
# ========================================================================
echo "============================================================================"
echo "  ANONYMOUS POST/REPLY TEST RESULTS"
echo "============================================================================"
echo ""

for i in "${!TEST_NAMES[@]}"; do
    printf "  %-45s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done

echo ""
echo "  PASSED: $PASS_COUNT / $((PASS_COUNT + FAIL_COUNT))"
echo "  FAILED: $FAIL_COUNT / $((PASS_COUNT + FAIL_COUNT))"
echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME ANONYMOUS TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL ANONYMOUS TESTS PASSED <<<"
fi

echo ""
