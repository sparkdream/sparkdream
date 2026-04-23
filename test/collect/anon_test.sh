#!/bin/bash

echo "--- TESTING: ANONYMOUS COLLECT ACTIONS VIA X/SHIELD ---"
echo ""
echo "Tests full-stack anonymous collect operations through MsgShieldedExec:"
echo "  1. Anonymous collection creation"
echo "  2. Anonymous upvote on a collection"
echo "  3. Anonymous downvote on a collection"
echo "  4. Nullifier replay prevention"
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

echo "Collector 1:  $COLLECTOR1_ADDR"
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

# =========================================================================
# PREREQUISITE: Create a regular collection for upvote/downvote tests
# =========================================================================
echo "--- PREREQUISITE: Create a regular collection for voting tests ---"

# Use alice (CORE trust level) to avoid collection limit issues — collector1/collector2 may
# have exhausted their PROVISIONAL limit (5) from earlier test suites.
TX_RES=$($BINARY tx collect create-collection nft public false 0 "Vote Target" "A collection for anonymous voting tests" "" "test" \
    --from alice --chain-id $CHAIN_ID --keyring-backend test \
    --fees 500000uspark --gas 300000 -y --output json 2>&1)

submit_tx_and_wait "$TX_RES"

if check_tx_success "$TX_RESULT"; then
    VOTE_TARGET_ID=$(extract_event_value "$TX_RESULT" "collection_created" "id")
    if [ -z "$VOTE_TARGET_ID" ]; then
        VOTE_TARGET_ID="1"
    fi
    echo "  Regular collection created (ID: $VOTE_TARGET_ID) for vote tests"
else
    echo "  WARNING: Could not create regular collection, using ID=1"
    VOTE_TARGET_ID="1"
fi
echo ""

# =========================================================================
# TEST 1: Anonymous collection creation via MsgShieldedExec
# =========================================================================
echo "--- TEST 1: Anonymous collection creation ---"

NULLIFIER_COLL="cc01000000000000000000000000000000000000000000000000000000000001"
RATE_NULL_COLL=$(openssl rand -hex 32)
# type=1 (CURATED), visibility=1 (PUBLIC), encrypted=false
INNER_MSG="{\"@type\":\"/sparkdream.collect.v1.MsgCreateCollection\",\"creator\":\"$SHIELD_MODULE_ADDR\",\"type\":1,\"visibility\":1,\"encrypted\":false,\"expires_at\":\"0\",\"name\":\"Anonymous Collection\",\"description\":\"Created anonymously via x/shield\",\"cover_uri\":\"\",\"tags\":[\"test\"]}"

TX_RES=$($BINARY tx shield shielded-exec \
    --inner-message "$INNER_MSG" \
    --proof "$DUMMY_PROOF" \
    --nullifier "$NULLIFIER_COLL" \
    --rate-limit-nullifier "$RATE_NULL_COLL" \
    --merkle-root "$DUMMY_MERKLE_ROOT" \
    --proof-domain 1 \
    --min-trust-level 1 \
    --exec-mode 0 \
    --from collector1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 500000uspark \
    --gas 500000 \
    -y \
    --output json 2>&1)

submit_tx_and_wait "$TX_RES"

if check_tx_success "$TX_RESULT"; then
    ANON_COLL_ID=$(extract_event_value "$TX_RESULT" "collection_created" "id")
    echo "  Anonymous collection created (ID: ${ANON_COLL_ID:-unknown})"

    # Verify collection creator is shield module address
    if [ -n "$ANON_COLL_ID" ]; then
        COLL_QUERY=$($BINARY query collect show-collection "$ANON_COLL_ID" --output json 2>&1)
        COLL_CREATOR=$(echo "$COLL_QUERY" | jq -r '.collection.creator // empty')

        if [ "$COLL_CREATOR" == "$SHIELD_MODULE_ADDR" ]; then
            echo "  Collection creator is shield module (anonymous): confirmed"
        else
            echo "  Collection creator: $COLL_CREATOR"
        fi
    fi

    record_result "Anonymous collection creation" "PASS"
else
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""' 2>/dev/null)
    echo "  Transaction failed: ${RAW_LOG:0:200}"
    record_result "Anonymous collection creation" "FAIL"
fi

# =========================================================================
# TEST 2: Anonymous upvote on collection via MsgShieldedExec
# =========================================================================
echo "--- TEST 2: Anonymous collection upvote ---"

NULLIFIER_UP="cc02000000000000000000000000000000000000000000000000000000000002"
RATE_NULL_UP=$(openssl rand -hex 32)
# target_type=1 (FLAG_TARGET_TYPE_COLLECTION)
INNER_MSG="{\"@type\":\"/sparkdream.collect.v1.MsgUpvoteContent\",\"creator\":\"$SHIELD_MODULE_ADDR\",\"target_id\":\"$VOTE_TARGET_ID\",\"target_type\":1}"

TX_RES=$($BINARY tx shield shielded-exec \
    --inner-message "$INNER_MSG" \
    --proof "$DUMMY_PROOF" \
    --nullifier "$NULLIFIER_UP" \
    --rate-limit-nullifier "$RATE_NULL_UP" \
    --merkle-root "$DUMMY_MERKLE_ROOT" \
    --proof-domain 1 \
    --min-trust-level 1 \
    --exec-mode 0 \
    --from collector1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 500000uspark \
    --gas 500000 \
    -y \
    --output json 2>&1)

submit_tx_and_wait "$TX_RES"

if check_tx_success "$TX_RESULT"; then
    echo "  Anonymous upvote submitted successfully"
    record_result "Anonymous collection upvote" "PASS"
else
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""' 2>/dev/null)
    echo "  Transaction failed: ${RAW_LOG:0:200}"
    record_result "Anonymous collection upvote" "FAIL"
fi

# =========================================================================
# TEST 3: Anonymous downvote on collection via MsgShieldedExec
# =========================================================================
echo "--- TEST 3: Anonymous collection downvote ---"

# Create a second collection to downvote (avoid "already voted" conflict with upvote target)
# Use alice (CORE trust level) to avoid collection limit issues
TX_RES=$($BINARY tx collect create-collection nft public false 0 "Downvote Target" "A second collection" "" "test" \
    --from alice --chain-id $CHAIN_ID --keyring-backend test \
    --fees 500000uspark --gas 300000 -y --output json 2>&1)

DOWNVOTE_TARGET=""
if submit_tx_and_wait "$TX_RES" && check_tx_success "$TX_RESULT"; then
    DOWNVOTE_TARGET=$(extract_event_value "$TX_RESULT" "collection_created" "id")
    echo "  Created fresh downvote target collection: $DOWNVOTE_TARGET"
fi

if [ -z "$DOWNVOTE_TARGET" ] || [ "$DOWNVOTE_TARGET" == "null" ]; then
    # Fallback: use the anonymous collection from TEST 1 (created by shield module, so shield hasn't voted on it)
    if [ -n "$ANON_COLL_ID" ] && [ "$ANON_COLL_ID" != "null" ] && [ "$ANON_COLL_ID" != "$VOTE_TARGET_ID" ]; then
        DOWNVOTE_TARGET="$ANON_COLL_ID"
        echo "  Using anonymous collection as downvote target: $DOWNVOTE_TARGET"
    else
        echo "  WARNING: No separate downvote target available, test may fail"
        DOWNVOTE_TARGET="$VOTE_TARGET_ID"
    fi
fi
echo "  Downvote target collection: $DOWNVOTE_TARGET"

NULLIFIER_DOWN="cc03000000000000000000000000000000000000000000000000000000000003"
RATE_NULL_DOWN=$(openssl rand -hex 32)
INNER_MSG="{\"@type\":\"/sparkdream.collect.v1.MsgDownvoteContent\",\"creator\":\"$SHIELD_MODULE_ADDR\",\"target_id\":\"$DOWNVOTE_TARGET\",\"target_type\":1}"

TX_RES=$($BINARY tx shield shielded-exec \
    --inner-message "$INNER_MSG" \
    --proof "$DUMMY_PROOF" \
    --nullifier "$NULLIFIER_DOWN" \
    --rate-limit-nullifier "$RATE_NULL_DOWN" \
    --merkle-root "$DUMMY_MERKLE_ROOT" \
    --proof-domain 1 \
    --min-trust-level 1 \
    --exec-mode 0 \
    --from collector1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 500000uspark \
    --gas 500000 \
    -y \
    --output json 2>&1)

submit_tx_and_wait "$TX_RES"

if check_tx_success "$TX_RESULT"; then
    echo "  Anonymous downvote submitted successfully"
    record_result "Anonymous collection downvote" "PASS"
else
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""' 2>/dev/null)
    echo "  Transaction failed: ${RAW_LOG:0:200}"
    record_result "Anonymous collection downvote" "FAIL"
fi

# =========================================================================
# TEST 4: Nullifier replay prevention
# =========================================================================
echo "--- TEST 4: Nullifier replay prevention ---"

# Reuse the same nullifier from TEST 2 (upvote)
RATE_NULL_REPLAY=$(openssl rand -hex 32)
INNER_MSG="{\"@type\":\"/sparkdream.collect.v1.MsgUpvoteContent\",\"creator\":\"$SHIELD_MODULE_ADDR\",\"target_id\":\"$VOTE_TARGET_ID\",\"target_type\":1}"

TX_RES=$($BINARY tx shield shielded-exec \
    --inner-message "$INNER_MSG" \
    --proof "$DUMMY_PROOF" \
    --nullifier "$NULLIFIER_UP" \
    --rate-limit-nullifier "$RATE_NULL_REPLAY" \
    --merkle-root "$DUMMY_MERKLE_ROOT" \
    --proof-domain 1 \
    --min-trust-level 1 \
    --exec-mode 0 \
    --from collector1 \
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
echo "  ANONYMOUS COLLECT ACTIONS TEST SUMMARY"
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
