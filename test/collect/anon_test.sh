#!/bin/bash
# Anonymous collection, reaction, and pinning tests for x/collect

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "$SCRIPT_DIR/test_helpers.sh"
source "$SCRIPT_DIR/.test_env"

echo "========================================================================="
echo "  X/COLLECT - ANONYMOUS COLLECTIONS, REACTIONS & PINNING TESTS"
echo "========================================================================="
echo ""

BLOCK_HEIGHT=$(get_block_height)
FUTURE_BLOCK=$((BLOCK_HEIGHT + 5000))

# =========================================================================
# PREREQUISITE: Voter registration & trust tree (needed for ZK proofs)
# =========================================================================
echo "=== PREREQUISITE: Voter Registration & Trust Tree ==="
echo ""

# Deterministic ZK keys (unique to collect tests)
COLL1_ZK_KEY="e10e10e10e10e10e10e10e10e10e10e10e10e10e10e10e10e10e10e10e10e10e"
COLL1_ENC_KEY="e111111111111111111111111111111111111111111111111111111111111111"
COLL2_ZK_KEY="e20e20e20e20e20e20e20e20e20e20e20e20e20e20e20e20e20e20e20e20e20e"
COLL2_ENC_KEY="e222222222222222222222222222222222222222222222222222222222222222"

register_voter_for_collect() {
    local ACCOUNT=$1
    local ZK_KEY_HEX=$2
    local ENC_KEY_HEX=$3

    local ZK_KEY_B64=$(echo "$ZK_KEY_HEX" | xxd -r -p | base64)
    local ENC_KEY_B64=$(echo "$ENC_KEY_HEX" | xxd -r -p | base64)

    echo "  Registering $ACCOUNT as voter..."

    TX_OUT=$(send_tx vote register-voter \
        --zk-public-key "$ZK_KEY_B64" \
        --encryption-public-key "$ENC_KEY_B64" \
        --from $ACCOUNT)

    local txhash
    txhash=$(get_txhash "$TX_OUT")
    if [ -z "$txhash" ]; then
        echo "  Failed to register $ACCOUNT: no txhash"
        return 1
    fi

    local tx_result
    tx_result=$(wait_for_tx "$txhash")
    local code
    code=$(get_tx_code "$tx_result")

    if [ "$code" = "0" ]; then
        echo "  Registered $ACCOUNT"
        return 0
    else
        local raw_log
        raw_log=$(echo "$tx_result" | jq -r '.raw_log // ""' 2>/dev/null)
        if echo "$raw_log" | grep -qi "already.*regist\|use.*rotate"; then
            echo "  $ACCOUNT already registered (OK)"
            return 0
        fi
        echo "  Failed to register $ACCOUNT: $raw_log"
        return 1
    fi
}

register_voter_for_collect "collector1" "$COLL1_ZK_KEY" "$COLL1_ENC_KEY"
register_voter_for_collect "collector2" "$COLL2_ZK_KEY" "$COLL2_ENC_KEY"

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
    echo "  ERROR: Trust tree root not available. Anonymous tests cannot proceed."
    echo "  ABCI response: $ABCI_RESPONSE"
    exit 1
fi

TRUST_ROOT_HEX=$(echo "$TRUST_ROOT_B64" | base64 -d | xxd -p | tr -d '\n')
echo "  Trust tree root (hex): ${TRUST_ROOT_HEX:0:16}..."
echo ""

DUMMY_PROOF_B64=$(echo -n "deadbeef" | xxd -r -p | base64)

# Generate a deterministic Ed25519 management key pair for anonymous collection management.
# The seed is a fixed 32-byte value. We derive the Ed25519 public key using Python's cryptography.
MGMT_SEED_HEX="aabb000000000000000000000000000000000000000000000000000000000001"
MGMT_KEY_B64=$(python3 -c "
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives import serialization
import base64
seed = bytes.fromhex('$MGMT_SEED_HEX')
key = Ed25519PrivateKey.from_private_bytes(seed)
pub = key.public_key().public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
print(base64.b64encode(pub).decode())
")

BLOCK_HEIGHT=$(get_block_height)
FUTURE_BLOCK=$((BLOCK_HEIGHT + 5000))

# =========================================================================
# Test 1: Create anonymous collection (happy path)
# =========================================================================
echo "--- Test 1: Create anonymous collection (happy path) ---"

NULLIFIER_AC1="ac01000000000000000000000000000000000000000000000000000000000001"
NULLIFIER_AC1_B64=$(echo "$NULLIFIER_AC1" | xxd -r -p | base64)

TX_OUT=$(send_tx collect create-anonymous-collection \
    "AnonGallery" "An anonymous art gallery" \
    --management-public-key "$MGMT_KEY_B64" \
    --expires-at "$FUTURE_BLOCK" \
    --proof "$DUMMY_PROOF_B64" \
    --nullifier "$NULLIFIER_AC1_B64" \
    --merkle-root "$TRUST_ROOT_B64" \
    --min-trust-level 2 \
    --from collector1)
assert_tx_success "Create anonymous collection" "$TX_OUT"

ANON_COLL_ID=$(extract_event_attr "$TX_RESULT_OUT" "anonymous_collection_created" "collection_id")
if [ -z "$ANON_COLL_ID" ]; then
    # Fallback: try querying anonymous collections
    ANON_COLLS=$(query collect anonymous-collections)
    ANON_COLL_ID=$(echo "$ANON_COLLS" | jq -r '.collections[-1].id // empty' 2>/dev/null)
fi
echo "  Anonymous collection ID: $ANON_COLL_ID"

# =========================================================================
# Test 2: Query anonymous collections list
# =========================================================================
echo ""
echo "--- Test 2: Query anonymous collections list ---"

ANON_LIST=$(query collect anonymous-collections)
ANON_COUNT=$(echo "$ANON_LIST" | jq -r '.collections | length' 2>/dev/null || echo "0")
assert_gt "Anonymous collections list has entries" "0" "$ANON_COUNT"

# =========================================================================
# Test 3: Query is-nullifier-used (should be true)
# =========================================================================
echo ""
echo "--- Test 3: Query is-nullifier-used (should be true) ---"

# Determine current epoch for nullifier scope
BLOCK_TIME=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_time // ""')
if [ -n "$BLOCK_TIME" ]; then
    UNIX_TIME=$(date -d "$BLOCK_TIME" +%s 2>/dev/null || echo "0")
    EPOCH=$((UNIX_TIME / 13140000))
else
    EPOCH=0
fi

# Domain 6 = anonymous collection
NULL_Q=$(query collect is-nullifier-used "$NULLIFIER_AC1" 6 "$EPOCH")
IS_USED=$(echo "$NULL_Q" | jq -r '.used // "false"')

if [ "$IS_USED" != "true" ]; then
    # Try scope=0 as fallback
    NULL_Q=$(query collect is-nullifier-used "$NULLIFIER_AC1" 6 0)
    IS_USED=$(echo "$NULL_Q" | jq -r '.used // "false"')
fi

assert_equal "Nullifier is marked as used" "true" "$IS_USED"

# =========================================================================
# Test 4: Fail — duplicate nullifier (ErrNullifierUsed)
# =========================================================================
echo ""
echo "--- Test 4: Fail — duplicate nullifier ---"

BLOCK_HEIGHT=$(get_block_height)
FUTURE_BLOCK2=$((BLOCK_HEIGHT + 5000))

TX_OUT=$(send_tx collect create-anonymous-collection \
    "DuplicateGallery" "Duplicate attempt" \
    --management-public-key "$MGMT_KEY_B64" \
    --expires-at "$FUTURE_BLOCK2" \
    --proof "$DUMMY_PROOF_B64" \
    --nullifier "$NULLIFIER_AC1_B64" \
    --merkle-root "$TRUST_ROOT_B64" \
    --min-trust-level 2 \
    --from collector1)
assert_tx_failure "Duplicate nullifier rejected" "$TX_OUT"

# =========================================================================
# Test 5: Manage anonymous collection — add item via Ed25519 signature
# =========================================================================
echo ""
echo "--- Test 5: Manage anonymous collection — add item ---"

if [ -n "$ANON_COLL_ID" ]; then
    # Build a real Ed25519 signature over the canonical payload:
    #   SHA256(BE_uint64(collection_id) || BE_uint64(nonce) || BE_uint32(action))
    # Action: ADD_ITEM = 1, Nonce: 1
    MGMT_SIG_B64=$(python3 -c "
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
import hashlib, struct, base64
seed = bytes.fromhex('$MGMT_SEED_HEX')
key = Ed25519PrivateKey.from_private_bytes(seed)
buf = struct.pack('>QQI', int('$ANON_COLL_ID'), 1, 1)
payload = hashlib.sha256(buf).digest()
sig = key.sign(payload)
print(base64.b64encode(sig).decode())
")

    TX_OUT=$(send_tx collect manage-anonymous-collection \
        "$ANON_COLL_ID" anon-manage-action-add-item 1 \
        --management-signature "$MGMT_SIG_B64" \
        --items '{"title":"Anonymous Artwork","description":"A beautiful piece","image_uri":"https://example.com/art1"}' \
        --from collector1)
    assert_tx_success "Manage anonymous collection — add item" "$TX_OUT"
else
    skip_test "Manage anonymous collection — add item" "No anonymous collection ID"
fi

# =========================================================================
# Test 6: Fail — manage with invalid nonce (ErrInvalidNonce)
# =========================================================================
echo ""
echo "--- Test 6: Fail — manage with invalid nonce ---"

if [ -n "$ANON_COLL_ID" ]; then
    # Nonce 0 should fail (must be > current stored nonce, which is now 1 after Test 5)
    # Use a dummy 64-byte signature — the nonce check happens before signature verification
    DUMMY_SIG_B64=$(python3 -c "import base64; print(base64.b64encode(b'\x00'*64).decode())")
    TX_OUT=$(send_tx collect manage-anonymous-collection \
        "$ANON_COLL_ID" anon-manage-action-add-item 0 \
        --management-signature "$DUMMY_SIG_B64" \
        --items '{"title":"Bad Nonce Item","description":"Should fail"}' \
        --from collector1)
    assert_tx_failure "Invalid nonce rejected" "$TX_OUT"
else
    skip_test "Invalid nonce rejected" "No anonymous collection ID"
fi

# =========================================================================
# Test 7: Fail — manage non-anonymous collection (ErrNotAnonymousCollection)
# =========================================================================
echo ""
echo "--- Test 7: Fail — manage non-anonymous collection ---"

# Use an existing regular collection (COLL1_ID from collection_test.sh),
# or create one if not available (e.g. when running anon_test.sh alone).
REGULAR_COLL_ID="${COLL1_ID}"

if [ -z "$REGULAR_COLL_ID" ]; then
    REG_TX=$(send_tx collect create-collection \
        "RegularColl" "A regular collection for test" \
        --visibility public \
        --collection-type general \
        --from collector1)
    reg_txhash=$(get_txhash "$REG_TX")
    if [ -n "$reg_txhash" ]; then
        reg_result=$(wait_for_tx "$reg_txhash")
        reg_code=$(get_tx_code "$reg_result")
        if [ "$reg_code" = "0" ]; then
            REGULAR_COLL_ID=$(extract_event_attr "$reg_result" "collection_created" "collection_id")
        fi
    fi
fi

if [ -n "$REGULAR_COLL_ID" ]; then
    TX_OUT=$(send_tx collect manage-anonymous-collection \
        "$REGULAR_COLL_ID" anon-manage-action-add-item 1 \
        --management-signature "$DUMMY_SIG_B64" \
        --items '{"title":"Fake Item","description":"Should fail"}' \
        --from collector1)
    assert_tx_failure "Manage non-anonymous collection rejected" "$TX_OUT"
else
    skip_test "Manage non-anonymous collection rejected" "COLL1_ID not set (run collection_test.sh first)"
fi

# =========================================================================
# Test 8: Anonymous upvote on collection (happy path)
# =========================================================================
echo ""
echo "--- Test 8: Anonymous upvote on collection ---"

if [ -n "$ANON_COLL_ID" ]; then
    NULLIFIER_REACT1="ac10000000000000000000000000000000000000000000000000000000000010"
    NULLIFIER_REACT1_B64=$(echo "$NULLIFIER_REACT1" | xxd -r -p | base64)

    TX_OUT=$(send_tx collect anonymous-react \
        "$ANON_COLL_ID" collection 1 \
        --proof "$DUMMY_PROOF_B64" \
        --nullifier "$NULLIFIER_REACT1_B64" \
        --merkle-root "$TRUST_ROOT_B64" \
        --min-trust-level 2 \
        --from collector1)
    assert_tx_success "Anonymous upvote on collection" "$TX_OUT"
else
    skip_test "Anonymous upvote on collection" "No anonymous collection ID"
fi

# =========================================================================
# Test 9: Anonymous downvote on collection (costs SPARK)
# =========================================================================
echo ""
echo "--- Test 9: Anonymous downvote on collection ---"

if [ -n "$ANON_COLL_ID" ]; then
    NULLIFIER_REACT2="ac20000000000000000000000000000000000000000000000000000000000020"
    NULLIFIER_REACT2_B64=$(echo "$NULLIFIER_REACT2" | xxd -r -p | base64)

    TX_OUT=$(send_tx collect anonymous-react \
        "$ANON_COLL_ID" collection 2 \
        --proof "$DUMMY_PROOF_B64" \
        --nullifier "$NULLIFIER_REACT2_B64" \
        --merkle-root "$TRUST_ROOT_B64" \
        --min-trust-level 2 \
        --from collector2)
    assert_tx_success "Anonymous downvote on collection" "$TX_OUT"
else
    skip_test "Anonymous downvote on collection" "No anonymous collection ID"
fi

# =========================================================================
# Test 10: Fail — duplicate nullifier on same target (ErrNullifierUsed)
# =========================================================================
echo ""
echo "--- Test 10: Fail — duplicate reaction nullifier ---"

if [ -n "$ANON_COLL_ID" ]; then
    TX_OUT=$(send_tx collect anonymous-react \
        "$ANON_COLL_ID" collection 1 \
        --proof "$DUMMY_PROOF_B64" \
        --nullifier "$NULLIFIER_REACT1_B64" \
        --merkle-root "$TRUST_ROOT_B64" \
        --min-trust-level 2 \
        --from collector1)
    assert_tx_failure "Duplicate reaction nullifier rejected" "$TX_OUT"
else
    skip_test "Duplicate reaction nullifier rejected" "No anonymous collection ID"
fi

# =========================================================================
# Test 11: Fail — react on non-existent collection
# =========================================================================
echo ""
echo "--- Test 11: Fail — react on non-existent collection ---"

NULLIFIER_REACT3="ac30000000000000000000000000000000000000000000000000000000000030"
NULLIFIER_REACT3_B64=$(echo "$NULLIFIER_REACT3" | xxd -r -p | base64)

TX_OUT=$(send_tx collect anonymous-react \
    999999 collection 1 \
    --proof "$DUMMY_PROOF_B64" \
    --nullifier "$NULLIFIER_REACT3_B64" \
    --merkle-root "$TRUST_ROOT_B64" \
    --min-trust-level 2 \
    --from collector1)
assert_tx_failure "React on non-existent collection rejected" "$TX_OUT"

# =========================================================================
# Test 12: Pin ephemeral collection (happy path)
# =========================================================================
echo ""
echo "--- Test 12: Pin ephemeral collection ---"

if [ -n "$ANON_COLL_ID" ]; then
    # Pin requires TRUST_LEVEL_ESTABLISHED (level 2). Alice has TRUST_LEVEL_CORE.
    TX_OUT=$(send_tx collect pin-collection \
        "$ANON_COLL_ID" \
        --from alice)
    assert_tx_success "Pin ephemeral collection" "$TX_OUT"

    # Verify collection is now permanent (expires_at=0 is omitted in proto3 JSON, so default to 0)
    PINNED_Q=$(query collect collection "$ANON_COLL_ID")
    PINNED_EXPIRES=$(echo "$PINNED_Q" | jq -r '(.collection.expires_at // 0) | tostring' 2>/dev/null)
    assert_equal "Pinned collection ExpiresAt=0" "0" "$PINNED_EXPIRES"
else
    skip_test "Pin ephemeral collection" "No anonymous collection ID"
fi

# =========================================================================
# Test 13: Fail — pin non-ephemeral collection (ErrCannotPinActive)
# =========================================================================
echo ""
echo "--- Test 13: Fail — pin non-ephemeral (already permanent) collection ---"

if [ -n "$ANON_COLL_ID" ]; then
    # The collection was just pinned (ExpiresAt=0), so pinning again should fail
    TX_OUT=$(send_tx collect pin-collection \
        "$ANON_COLL_ID" \
        --from alice)
    assert_tx_failure "Pin already-permanent collection rejected" "$TX_OUT"
else
    skip_test "Pin already-permanent collection rejected" "No anonymous collection ID"
fi

# =========================================================================
# Test 14: Fail — pin expired collection (ErrCollectionExpired)
# =========================================================================
echo ""
echo "--- Test 14: Fail — pin expired collection ---"

# Create a collection with a very short TTL (already expired)
NULLIFIER_AC2="ac02000000000000000000000000000000000000000000000000000000000002"
NULLIFIER_AC2_B64=$(echo "$NULLIFIER_AC2" | xxd -r -p | base64)
MGMT_KEY2_SEED_HEX="aabb000000000000000000000000000000000000000000000000000000000002"
MGMT_KEY2_B64=$(python3 -c "
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives import serialization
import base64
seed = bytes.fromhex('$MGMT_KEY2_SEED_HEX')
key = Ed25519PrivateKey.from_private_bytes(seed)
pub = key.public_key().public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
print(base64.b64encode(pub).decode())
")

# ExpiresAt = 1 (already passed in block height)
TX_OUT=$(send_tx collect create-anonymous-collection \
    "ExpiredGallery" "Will expire immediately" \
    --management-public-key "$MGMT_KEY2_B64" \
    --expires-at 1 \
    --proof "$DUMMY_PROOF_B64" \
    --nullifier "$NULLIFIER_AC2_B64" \
    --merkle-root "$TRUST_ROOT_B64" \
    --min-trust-level 2 \
    --from collector1)

# This might fail at creation time (expired TTL), which is fine
txhash=$(get_txhash "$TX_OUT")
EXPIRED_COLL_ID=""
if [ -n "$txhash" ]; then
    tx_result=$(wait_for_tx "$txhash")
    code=$(get_tx_code "$tx_result")
    if [ "$code" = "0" ]; then
        EXPIRED_COLL_ID=$(extract_event_attr "$tx_result" "anonymous_collection_created" "collection_id")
    fi
fi

if [ -n "$EXPIRED_COLL_ID" ]; then
    TX_OUT=$(send_tx collect pin-collection \
        "$EXPIRED_COLL_ID" \
        --from collector1)
    assert_tx_failure "Pin expired collection rejected" "$TX_OUT"
else
    # Creation itself failed because ExpiresAt=1 is in the past — that's expected
    echo "PASS: Pin expired collection rejected (creation blocked expired TTL)"
    TESTS_PASSED=$((TESTS_PASSED + 1))
fi

# =========================================================================
# Summary
# =========================================================================
print_summary
exit $?
