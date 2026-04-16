#!/bin/bash

echo "--- TESTING: ANONYMOUS FORUM ACTIONS VIA X/SHIELD ---"
echo ""
echo "Tests full-stack anonymous forum operations through MsgShieldedExec:"
echo "  1. Anonymous forum post creation"
echo "  2. Anonymous upvote on a post"
echo "  3. Anonymous downvote on a post"
echo "  4. Nullifier replay prevention (upvote same post twice)"
echo ""

# === 0. SETUP ===
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    exit 1
fi

source "$SCRIPT_DIR/.test_env"

echo "Poster 1:   $POSTER1_ADDR"
echo ""

# === HELPERS ===

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

# === RESOLVE SHIELD MODULE ADDRESS ===
SHIELD_MODULE_ADDR=$($BINARY query auth module-account shield --output json 2>/dev/null | jq -r '.account.value.address' 2>/dev/null)

if [ -z "$SHIELD_MODULE_ADDR" ] || [ "$SHIELD_MODULE_ADDR" == "null" ]; then
    echo "ERROR: Could not resolve shield module address"
    exit 1
fi

echo "Shield module: $SHIELD_MODULE_ADDR"
echo ""

# === FUND SHIELD MODULE (if needed) ===
SHIELD_BAL=$($BINARY query bank balances "$SHIELD_MODULE_ADDR" --output json 2>/dev/null | jq -r '.balances[] | select(.denom=="uspark") | .amount' 2>/dev/null || echo "0")
if [ -z "$SHIELD_BAL" ] || [ "$SHIELD_BAL" == "0" ] || [ "$SHIELD_BAL" == "null" ]; then
    echo "Shield module has no gas — funding from alice..."
    ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test 2>/dev/null)
    $BINARY tx bank send "$ALICE_ADDR" "$SHIELD_MODULE_ADDR" 50000000uspark \
        --from alice --chain-id $CHAIN_ID --keyring-backend test \
        --fees 500000uspark -y --output json > /dev/null 2>&1
    sleep 6
    echo "  Shield balance: $($BINARY query bank balances "$SHIELD_MODULE_ADDR" --output json 2>/dev/null | jq -r '.balances[] | select(.denom=="uspark") | .amount' 2>/dev/null) uspark"
    echo ""
fi

# Dummy ZK values - proof verification is skipped when no VK is stored (test mode)
DUMMY_PROOF=$(python3 -c "print('aa' * 128)")
DUMMY_MERKLE_ROOT="0000000000000000000000000000000000000000000000000000000000000001"

# We need a category ID. Use TEST_CATEGORY_ID from .test_env or default to 1.
CATEGORY_ID="${TEST_CATEGORY_ID:-1}"
if [ "$CATEGORY_ID" == "null" ] || [ -z "$CATEGORY_ID" ]; then
    CATEGORY_ID="1"
fi
echo "Using category ID: $CATEGORY_ID"
echo ""

# =========================================================================
# PREREQUISITE: Create a regular post for upvote/downvote tests
# =========================================================================
echo "--- PREREQUISITE: Create a regular forum post for voting tests ---"

TX_RES=$($BINARY tx forum create-post "$CATEGORY_ID" 0 "Vote Target Post" \
    --tags "commons-council" \
    --from poster1 --chain-id $CHAIN_ID --keyring-backend test \
    --fees 500000uspark --gas 300000 -y --output json 2>&1)

submit_tx_and_wait "$TX_RES"

if check_tx_success "$TX_RESULT"; then
    VOTE_TARGET_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
    if [ -z "$VOTE_TARGET_POST_ID" ]; then
        VOTE_TARGET_POST_ID="1"
    fi
    echo "  Regular post created (ID: $VOTE_TARGET_POST_ID) for vote tests"
else
    echo "  WARNING: Could not create regular post, using post_id=1"
    VOTE_TARGET_POST_ID="1"
fi
echo ""

# =========================================================================
# TEST 1: Anonymous forum post creation via MsgShieldedExec
# =========================================================================
echo "--- TEST 1: Anonymous forum post creation ---"

NULLIFIER_POST="fc01000000000000000000000000000000000000000000000000000000000001"
RATE_NULL_POST=$(openssl rand -hex 32)
INNER_MSG="{\"@type\":\"/sparkdream.forum.v1.MsgCreatePost\",\"creator\":\"$SHIELD_MODULE_ADDR\",\"category_id\":\"$CATEGORY_ID\",\"parent_id\":\"0\",\"content\":\"Anonymous forum post created via x/shield shielded exec\",\"tags\":[\"commons-council\"]}"

TX_RES=$($BINARY tx shield shielded-exec \
    --inner-message "$INNER_MSG" \
    --proof "$DUMMY_PROOF" \
    --nullifier "$NULLIFIER_POST" \
    --rate-limit-nullifier "$RATE_NULL_POST" \
    --merkle-root "$DUMMY_MERKLE_ROOT" \
    --proof-domain 1 \
    --min-trust-level 1 \
    --exec-mode 0 \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 500000uspark \
    --gas 500000 \
    -y \
    --output json 2>&1)

submit_tx_and_wait "$TX_RES"

if check_tx_success "$TX_RESULT"; then
    ANON_POST_ID=$(extract_event_value "$TX_RESULT" "post_created" "post_id")
    echo "  Anonymous forum post created (ID: ${ANON_POST_ID:-unknown})"

    # Verify post creator is shield module address
    if [ -n "$ANON_POST_ID" ]; then
        POST_QUERY=$($BINARY query forum show-post "$ANON_POST_ID" --output json 2>&1)
        POST_CREATOR=$(echo "$POST_QUERY" | jq -r '.post.creator // empty')

        if [ "$POST_CREATOR" == "$SHIELD_MODULE_ADDR" ]; then
            echo "  Post creator is shield module (anonymous): confirmed"
        else
            echo "  Post creator: $POST_CREATOR"
        fi
    fi

    record_result "Anonymous forum post creation" "PASS"
else
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""' 2>/dev/null)
    echo "  Transaction failed: ${RAW_LOG:0:200}"
    record_result "Anonymous forum post creation" "FAIL"
fi

# =========================================================================
# TEST 2: Anonymous upvote via MsgShieldedExec
# =========================================================================
echo "--- TEST 2: Anonymous forum upvote ---"

NULLIFIER_UP="fc02000000000000000000000000000000000000000000000000000000000002"
RATE_NULL_UP=$(openssl rand -hex 32)
INNER_MSG="{\"@type\":\"/sparkdream.forum.v1.MsgUpvotePost\",\"creator\":\"$SHIELD_MODULE_ADDR\",\"post_id\":\"$VOTE_TARGET_POST_ID\"}"

TX_RES=$($BINARY tx shield shielded-exec \
    --inner-message "$INNER_MSG" \
    --proof "$DUMMY_PROOF" \
    --nullifier "$NULLIFIER_UP" \
    --rate-limit-nullifier "$RATE_NULL_UP" \
    --merkle-root "$DUMMY_MERKLE_ROOT" \
    --proof-domain 1 \
    --min-trust-level 1 \
    --exec-mode 0 \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 500000uspark \
    --gas 500000 \
    -y \
    --output json 2>&1)

submit_tx_and_wait "$TX_RES"

if check_tx_success "$TX_RESULT"; then
    echo "  Anonymous upvote submitted successfully"
    record_result "Anonymous forum upvote" "PASS"
else
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""' 2>/dev/null)
    echo "  Transaction failed: ${RAW_LOG:0:200}"
    record_result "Anonymous forum upvote" "FAIL"
fi

# =========================================================================
# TEST 3: Anonymous downvote via MsgShieldedExec
# =========================================================================
echo "--- TEST 3: Anonymous forum downvote ---"

# Create a separate post for the downvote test to avoid duplicate-vote conflict.
# The upvote test already voted on VOTE_TARGET_POST_ID from the shield module address,
# and our duplicate-vote prevention (FORUM-1 fix) rejects a second vote on the same post.
TX_RES=$($BINARY tx forum create-post "$CATEGORY_ID" 0 "Downvote Target Post" \
    --tags "commons-council" \
    --from poster1 --chain-id $CHAIN_ID --keyring-backend test \
    --fees 500000uspark --gas 300000 -y --output json 2>&1)
submit_tx_and_wait "$TX_RES"
DOWNVOTE_TARGET=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="post_created") | .attributes[] | select(.key=="post_id") | .value' 2>/dev/null)
if [ -z "$DOWNVOTE_TARGET" ] || [ "$DOWNVOTE_TARGET" == "null" ]; then
    DOWNVOTE_TARGET="${ANON_POST_ID:-$VOTE_TARGET_POST_ID}"
fi
echo "  Downvote target post: $DOWNVOTE_TARGET"

NULLIFIER_DOWN="fc03000000000000000000000000000000000000000000000000000000000003"
RATE_NULL_DOWN=$(openssl rand -hex 32)
INNER_MSG="{\"@type\":\"/sparkdream.forum.v1.MsgDownvotePost\",\"creator\":\"$SHIELD_MODULE_ADDR\",\"post_id\":\"$DOWNVOTE_TARGET\"}"

TX_RES=$($BINARY tx shield shielded-exec \
    --inner-message "$INNER_MSG" \
    --proof "$DUMMY_PROOF" \
    --nullifier "$NULLIFIER_DOWN" \
    --rate-limit-nullifier "$RATE_NULL_DOWN" \
    --merkle-root "$DUMMY_MERKLE_ROOT" \
    --proof-domain 1 \
    --min-trust-level 1 \
    --exec-mode 0 \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 500000uspark \
    --gas 500000 \
    -y \
    --output json 2>&1)

submit_tx_and_wait "$TX_RES"

if check_tx_success "$TX_RESULT"; then
    echo "  Anonymous downvote submitted successfully"
    record_result "Anonymous forum downvote" "PASS"
else
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""' 2>/dev/null)
    echo "  Transaction failed: ${RAW_LOG:0:200}"
    record_result "Anonymous forum downvote" "FAIL"
fi

# =========================================================================
# TEST 4: Nullifier replay prevention (upvote same post twice)
# =========================================================================
echo "--- TEST 4: Nullifier replay prevention ---"

# Reuse the same nullifier from TEST 2 (upvote)
RATE_NULL_REPLAY=$(openssl rand -hex 32)
INNER_MSG="{\"@type\":\"/sparkdream.forum.v1.MsgUpvotePost\",\"creator\":\"$SHIELD_MODULE_ADDR\",\"post_id\":\"$VOTE_TARGET_POST_ID\"}"

TX_RES=$($BINARY tx shield shielded-exec \
    --inner-message "$INNER_MSG" \
    --proof "$DUMMY_PROOF" \
    --nullifier "$NULLIFIER_UP" \
    --rate-limit-nullifier "$RATE_NULL_REPLAY" \
    --merkle-root "$DUMMY_MERKLE_ROOT" \
    --proof-domain 1 \
    --min-trust-level 1 \
    --exec-mode 0 \
    --from poster1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 500000uspark \
    --gas 500000 \
    -y \
    --output json 2>&1)

submit_tx_and_wait "$TX_RES"

if check_tx_success "$TX_RESULT"; then
    echo "  ERROR: Replay attack succeeded (should have failed)"
    record_result "Nullifier replay prevention" "FAIL"
else
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""' 2>/dev/null)
    if echo "$RAW_LOG" | grep -qi "nullifier"; then
        echo "  Correctly rejected: nullifier already used"
    else
        echo "  Rejected (reason: ${RAW_LOG:0:150})"
    fi
    record_result "Nullifier replay prevention" "PASS"
fi

# =========================================================================
# SUMMARY
# =========================================================================
echo "=========================================="
echo "  ANONYMOUS FORUM ACTIONS TEST SUMMARY"
echo "=========================================="
echo ""
echo "  Passed: $PASS_COUNT"
echo "  Failed: $FAIL_COUNT"
echo ""

for i in "${!TEST_NAMES[@]}"; do
    echo "  ${RESULTS[$i]}  ${TEST_NAMES[$i]}"
done
echo ""

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL TESTS PASSED <<<"
fi
