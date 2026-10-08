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
SHIELD_BAL=$($BINARY query bank balances "$SHIELD_MODULE_ADDR" --output json 2>/dev/null | jq -r --arg denom "$BOND_DENOM" '.balances[] | select(.denom==$denom) | .amount' 2>/dev/null || echo "0")
if [ -z "$SHIELD_BAL" ] || [ "$SHIELD_BAL" == "0" ] || [ "$SHIELD_BAL" == "null" ]; then
    echo "Shield module has no gas — funding from alice..."
    ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test 2>/dev/null)
    $BINARY tx bank send "$ALICE_ADDR" "$SHIELD_MODULE_ADDR" 50000000${BOND_DENOM} \
        --from alice --chain-id $CHAIN_ID --keyring-backend test \
        --fees 500000${BOND_DENOM} -y --output json > /dev/null 2>&1
    sleep 6
    echo "  Shield balance: $($BINARY query bank balances "$SHIELD_MODULE_ADDR" --output json 2>/dev/null | jq -r --arg denom "$BOND_DENOM" '.balances[] | select(.denom==$denom) | .amount' 2>/dev/null) uspark"
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
    --fees 500000${BOND_DENOM} --gas 300000 -y --output json 2>&1)

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
# Anonymous collections must have a TTL (expires_at is a block height, at
# most max_ttl_blocks ahead).
CUR_HEIGHT=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height')
MAX_TTL=$($BINARY query collect params --output json 2>/dev/null | jq -r '.params.max_ttl_blocks // "0"')
TTL_BLOCKS=1000
if [ "$MAX_TTL" != "0" ] && [ "$MAX_TTL" -lt "$TTL_BLOCKS" ] 2>/dev/null; then
    TTL_BLOCKS=$MAX_TTL
fi
ANON_EXPIRES=$((CUR_HEIGHT + TTL_BLOCKS))
INNER_MSG="{\"@type\":\"/sparkdream.collect.v1.MsgCreateCollection\",\"creator\":\"$SHIELD_MODULE_ADDR\",\"type\":1,\"visibility\":1,\"encrypted\":false,\"expires_at\":\"$ANON_EXPIRES\",\"name\":\"Anonymous Collection\",\"description\":\"Created anonymously via x/shield\",\"cover_uri\":\"\",\"tags\":[\"test\"]}"

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
    --fees 25000${BOND_DENOM} \
    --gas 500000 \
    -y \
    --output json 2>&1)

submit_tx_and_wait "$TX_RES"

if check_tx_success "$TX_RESULT"; then
    ANON_COLL_ID=$(extract_event_value "$TX_RESULT" "collection_created" "id")
    echo "  Anonymous collection created (ID: ${ANON_COLL_ID:-unknown})"

    # Verify collection creator is shield module address
    if [ -n "$ANON_COLL_ID" ]; then
        COLL_QUERY=$($BINARY query collect collection "$ANON_COLL_ID" --output json 2>&1)
        COLL_CREATOR=$(echo "$COLL_QUERY" | jq -r '.collection.owner // empty')

        if [ "$COLL_CREATOR" == "$SHIELD_MODULE_ADDR" ]; then
            echo "  Collection creator is shield module (anonymous): confirmed"
        else
            echo "  Collection creator: $COLL_CREATOR"
        fi

        # The shield account's balance is the communal gas reserve: an
        # anonymous collection takes no deposit. (The balance itself can't be
        # compared here because the shield account also pays this tx's gas.)
        COLL_DEPOSIT=$(echo "$COLL_QUERY" | jq -r '.collection.deposit_amount // "0"')
        if [ "$COLL_DEPOSIT" == "0" ]; then
            echo "  No deposit recorded for the anonymous collection: confirmed"
        else
            echo "  ERROR: anonymous collection recorded deposit_amount=$COLL_DEPOSIT"
            ANON_CREATE_PASS=false
        fi
    fi

    record_result "Anonymous collection creation" "$([ "${ANON_CREATE_PASS:-true}" = true ] && echo PASS || echo FAIL)"
else
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""' 2>/dev/null)
    echo "  Transaction failed: ${RAW_LOG:0:200}"
    record_result "Anonymous collection creation" "FAIL"
fi

# =========================================================================
# TEST 1b: The anonymous creator manages the collection (ownership mode)
# =========================================================================
echo "--- TEST 1b: Anonymous owner manages the collection ---"

# The creation nullifier is stored as the collection's owner tag. Management
# ops run in ownership mode: the exec's nullifier must equal that tag (in
# production the ZK proof can only reproduce it with the creator's secret
# key), and the collection's owner sequence advances after each op. This
# chain has no verifying key, so the proof itself and the sequence binding
# are covered by Go tests (TestOwnershipModeWithRealProofs).
OWNER_PASS=true
if [ -z "$ANON_COLL_ID" ]; then
    echo "  [WARN] No anonymous collection from TEST 1; skipping"
    OWNER_PASS=false
else
    COLL_JSON=$($BINARY query collect collection "$ANON_COLL_ID" --output json 2>/dev/null)
    OWNER_TAG_HEX=$(echo "$COLL_JSON" | jq -r '.collection.anon_owner_tag // ""' | base64 -d 2>/dev/null | od -An -v -tx1 | tr -d " \n")
    SEQ_BEFORE=$(echo "$COLL_JSON" | jq -r '.collection.anon_owner_sequence // "0"')
    ITEMS_BEFORE=$(echo "$COLL_JSON" | jq -r '.collection.item_count // "0"')
    echo "  Owner tag: ${OWNER_TAG_HEX:0:16}... (creation nullifier: ${NULLIFIER_COLL:0:16}...)"
    if [ "$OWNER_TAG_HEX" != "$NULLIFIER_COLL" ]; then
        echo "  ERROR: owner tag is not the creation nullifier"
        OWNER_PASS=false
    fi

    ADD_INNER="{\"@type\":\"/sparkdream.collect.v1.MsgAddItem\",\"creator\":\"$SHIELD_MODULE_ADDR\",\"collection_id\":\"$ANON_COLL_ID\",\"position\":\"0\",\"title\":\"Anonymous item\"}"
    owner_exec() {
        $BINARY tx shield shielded-exec \
            --inner-message "$1" \
            --proof "$DUMMY_PROOF" \
            --nullifier "$2" \
            --rate-limit-nullifier "$(openssl rand -hex 32)" \
            --merkle-root "$DUMMY_MERKLE_ROOT" \
            --proof-domain 1 \
            --min-trust-level 1 \
            --exec-mode 0 \
            --from collector1 \
            --chain-id $CHAIN_ID \
            --keyring-backend test \
            --fees 25000${BOND_DENOM} \
            --gas 500000 \
            -y \
            --output json 2>&1
    }

    # Someone else's nullifier is not the owner tag: refused before paying.
    TX_RES=$(owner_exec "$ADD_INNER" "$(openssl rand -hex 32)")
    submit_tx_and_wait "$TX_RES"
    if check_tx_success "$TX_RESULT"; then
        echo "  ERROR: a non-owner added an item to the anonymous collection"
        OWNER_PASS=false
    else
        echo "  Non-owner refused: $(echo "$TX_RESULT" | jq -r '.raw_log // ""' 2>/dev/null | head -c 120)"
    fi

    # The owner adds an item.
    TX_RES=$(owner_exec "$ADD_INNER" "$OWNER_TAG_HEX")
    submit_tx_and_wait "$TX_RES"
    if check_tx_success "$TX_RESULT"; then
        COLL_JSON=$($BINARY query collect collection "$ANON_COLL_ID" --output json 2>/dev/null)
        SEQ_AFTER=$(echo "$COLL_JSON" | jq -r '.collection.anon_owner_sequence // "0"')
        ITEMS_AFTER=$(echo "$COLL_JSON" | jq -r '.collection.item_count // "0"')
        echo "  Owner added an item: items $ITEMS_BEFORE -> $ITEMS_AFTER, sequence $SEQ_BEFORE -> $SEQ_AFTER"
        if [ "$ITEMS_AFTER" -ne $((ITEMS_BEFORE + 1)) ] 2>/dev/null || [ "$SEQ_AFTER" -ne $((SEQ_BEFORE + 1)) ] 2>/dev/null; then
            echo "  ERROR: expected one more item and the owner sequence advanced by one"
            OWNER_PASS=false
        fi
        # Items in an anonymous collection carry no per-item deposit.
        ITEM_DEPOSITS=$(echo "$COLL_JSON" | jq -r '.collection.item_deposit_total // "0"')
        if [ "$ITEM_DEPOSITS" != "0" ]; then
            echo "  ERROR: anonymous collection recorded item_deposit_total=$ITEM_DEPOSITS"
            OWNER_PASS=false
        fi
    else
        echo "  ERROR: owner's add-item failed: $(echo "$TX_RESULT" | jq -r '.raw_log // ""' 2>/dev/null | head -c 200)"
        OWNER_PASS=false
    fi

    # The anonymous owner can't make the collection permanent.
    UPD_INNER="{\"@type\":\"/sparkdream.collect.v1.MsgUpdateCollection\",\"creator\":\"$SHIELD_MODULE_ADDR\",\"id\":\"$ANON_COLL_ID\",\"type\":1,\"expires_at\":\"0\",\"name\":\"Anonymous Collection\",\"tags\":[\"test\"]}"
    TX_RES=$(owner_exec "$UPD_INNER" "$OWNER_TAG_HEX")
    submit_tx_and_wait "$TX_RES"
    if check_tx_success "$TX_RESULT"; then
        echo "  ERROR: the anonymous owner made the collection permanent"
        OWNER_PASS=false
    elif echo "$TX_RESULT" | jq -r '.raw_log // ""' 2>/dev/null | grep -qi "expires_at"; then
        echo "  Owner cannot change expires_at: confirmed"
    else
        echo "  ERROR: update refused for an unexpected reason: $(echo "$TX_RESULT" | jq -r '.raw_log // ""' 2>/dev/null | head -c 200)"
        OWNER_PASS=false
    fi
fi
record_result "Anonymous owner manages collection" "$([ "$OWNER_PASS" = true ] && echo PASS || echo FAIL)"

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
    --fees 25000${BOND_DENOM} \
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
# TEST 2b: A second anonymous upvote (distinct nullifier) on the same collection counts
# =========================================================================
echo "--- TEST 2b: Second anonymous upvote on the same collection ---"

# Every anonymous vote carries the shield address, so collect keeps no
# per-voter record for it; x/shield's nullifier (a different one per member)
# is what allows one vote per member per target. The same key signs with a fresh
# nullifier, standing in for a second member (in dummy-proof mode that only
# shows a second distinct nullifier counts).
UP_BEFORE=$($BINARY query collect collection "$VOTE_TARGET_ID" --output json 2>/dev/null | jq -r '.collection.upvote_count // "0"')
INNER_MSG="{\"@type\":\"/sparkdream.collect.v1.MsgUpvoteContent\",\"creator\":\"$SHIELD_MODULE_ADDR\",\"target_id\":\"$VOTE_TARGET_ID\",\"target_type\":1}"

TX_RES=$($BINARY tx shield shielded-exec \
    --inner-message "$INNER_MSG" \
    --proof "$DUMMY_PROOF" \
    --nullifier "$(openssl rand -hex 32)" \
    --rate-limit-nullifier "$(openssl rand -hex 32)" \
    --merkle-root "$DUMMY_MERKLE_ROOT" \
    --proof-domain 1 \
    --min-trust-level 1 \
    --exec-mode 0 \
    --from collector1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 25000${BOND_DENOM} \
    --gas 500000 \
    -y \
    --output json 2>&1)

submit_tx_and_wait "$TX_RES"

if check_tx_success "$TX_RESULT"; then
    UP_AFTER=$($BINARY query collect collection "$VOTE_TARGET_ID" --output json 2>/dev/null | jq -r '.collection.upvote_count // "0"')
    if [ "$UP_AFTER" -eq $((UP_BEFORE + 1)) ] 2>/dev/null; then
        echo "  Second anonymous upvote counted ($UP_BEFORE -> $UP_AFTER)"
        record_result "Second anonymous upvote (distinct nullifier) counts" "PASS"
    else
        echo "  ERROR: upvote_count $UP_BEFORE -> $UP_AFTER, expected +1"
        record_result "Second anonymous upvote (distinct nullifier) counts" "FAIL"
    fi
else
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""' 2>/dev/null)
    echo "  ERROR: second anonymous upvote rejected: ${RAW_LOG:0:200}"
    record_result "Second anonymous upvote (distinct nullifier) counts" "FAIL"
fi

# =========================================================================
# TEST 3: Anonymous downvote on collection via MsgShieldedExec
# =========================================================================
echo "--- TEST 3: Anonymous collection downvote ---"

# Create a second collection to downvote (avoid "already voted" conflict with upvote target)
# Use alice (CORE trust level) to avoid collection limit issues
TX_RES=$($BINARY tx collect create-collection nft public false 0 "Downvote Target" "A second collection" "" "test" \
    --from alice --chain-id $CHAIN_ID --keyring-backend test \
    --fees 500000${BOND_DENOM} --gas 300000 -y --output json 2>&1)

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
    --fees 25000${BOND_DENOM} \
    --gas 500000 \
    -y \
    --output json 2>&1)

submit_tx_and_wait "$TX_RES"

if check_tx_success "$TX_RESULT"; then
    echo "  Anonymous downvote submitted successfully"
    # downvote_cost would come out of the shield account's gas reserve, so
    # anonymous downvotes don't pay it. The shield account's only spend in
    # this tx is the gas fee.
    DOWN_COST=$($BINARY query collect params --output json 2>/dev/null | jq -r '.params.downvote_cost // "0"')
    SHIELD_SPENT=$(echo "$TX_RESULT" | jq -r --arg a "$SHIELD_MODULE_ADDR" '[.events[] | select(.type=="coin_spent") | select(any(.attributes[]; .key=="spender" and .value==$a)) | .attributes[] | select(.key=="amount") | .value] | join(" ")' 2>/dev/null)
    echo "  Shield account spent: ${SHIELD_SPENT:-nothing} (downvote_cost: $DOWN_COST)"
    if [ "$DOWN_COST" != "0" ] && echo " $SHIELD_SPENT " | grep -q " ${DOWN_COST}${BOND_DENOM} "; then
        echo "  ERROR: downvote_cost was charged to the shield account"
        record_result "Anonymous collection downvote" "FAIL"
    else
        record_result "Anonymous collection downvote" "PASS"
    fi
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
    --fees 25000${BOND_DENOM} \
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
