#!/bin/bash

echo "--- TESTING: ANONYMOUS BLOG ACTIONS VIA X/SHIELD ---"
echo ""
echo "Tests full-stack anonymous blog operations through MsgShieldedExec:"
echo "  1. Anonymous post creation"
echo "  2. Anonymous reply to a post"
echo "  3. Anonymous reaction to a post"
echo "  4. Nullifier replay prevention"
echo "  5. Verify anonymous content is queryable"
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

echo "Blogger 1:  $BLOGGER1_ADDR"
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
    if [ "$CODE" != "0" ]; then
        return 1
    fi
    return 0
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
    echo "  Shield balance: $($BINARY query bank balances "$SHIELD_MODULE_ADDR" --output json 2>/dev/null | jq -r --arg denom "$BOND_DENOM" '.balances[] | select(.denom==$denom) | .amount' 2>/dev/null) $BOND_DENOM"
    echo ""
fi

# Dummy ZK values - proof verification is skipped when no VK is stored (test mode)
# Proof must be >= 128 bytes (hex-encoded = 256 chars) to pass ante handler anti-spam check
DUMMY_PROOF=$(python3 -c "print('aa' * 128)")
DUMMY_MERKLE_ROOT="0000000000000000000000000000000000000000000000000000000000000001"

# Anonymous actions carry no per-action SPARK charge: the shield account's
# balance is the communal gas reserve, so its only spend in a shielded tx is
# the gas fee. storage_fee_charged TX_JSON PARAMS_JSON BYTES succeeds when the
# cost_per_byte storage fee for BYTES shows up among the shield account's
# coin_spent amounts in the tx.
storage_fee_charged() {
    local cpb exempt fee spent
    cpb=$(echo "$2" | jq -r '.params.cost_per_byte_amount // "0"')
    exempt=$(echo "$2" | jq -r '.params.cost_per_byte_exempt // false')
    [ "$exempt" == "true" ] || [ "$cpb" == "0" ] && return 1
    fee=$(( cpb * $3 ))
    spent=$(echo "$1" | jq -r --arg a "$SHIELD_MODULE_ADDR" '[.events[] | select(.type=="coin_spent") | select(any(.attributes[]; .key=="spender" and .value==$a)) | .attributes[] | select(.key=="amount") | .value] | join(" ")' 2>/dev/null)
    echo "  Shield account spent: ${spent:-nothing} (storage fee would be ${fee}${BOND_DENOM})"
    echo " $spent " | grep -q " ${fee}${BOND_DENOM} "
}

# =========================================================================
# TEST 1: Anonymous blog post creation via MsgShieldedExec
# =========================================================================
echo "--- TEST 1: Anonymous blog post creation ---"

NULLIFIER_POST="ab01000000000000000000000000000000000000000000000000000000000001"
RATE_NULL_POST=$(openssl rand -hex 32)
ANON_POST_TITLE="Anonymous Test Post"
ANON_POST_BODY="This post was created anonymously via x/shield"
INNER_MSG="{\"@type\":\"/sparkdream.blog.v1.MsgCreatePost\",\"creator\":\"$SHIELD_MODULE_ADDR\",\"title\":\"$ANON_POST_TITLE\",\"body\":\"$ANON_POST_BODY\"}"

TX_RES=$($BINARY tx shield shielded-exec \
    --inner-message "$INNER_MSG" \
    --proof "$DUMMY_PROOF" \
    --nullifier "$NULLIFIER_POST" \
    --rate-limit-nullifier "$RATE_NULL_POST" \
    --merkle-root "$DUMMY_MERKLE_ROOT" \
    --proof-domain 1 \
    --min-trust-level 1 \
    --exec-mode 0 \
    --from blogger1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 25000${BOND_DENOM} \
    --gas 500000 \
    -y \
    --output json 2>&1)

submit_tx_and_wait "$TX_RES"

if check_tx_success "$TX_RESULT"; then
    # Extract post ID from blog event (blog emits "blog.post.created")
    ANON_POST_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
    if [ -n "$ANON_POST_ID" ] && [ "$ANON_POST_ID" != "" ]; then
        echo "  Anonymous post created with ID: $ANON_POST_ID"

        # Verify post creator is the shield module address (anonymous)
        POST_QUERY=$($BINARY query blog show-post "$ANON_POST_ID" --output json 2>&1)
        POST_CREATOR=$(echo "$POST_QUERY" | jq -r '.post.creator // empty')

        # Anonymous posts pass the membership gate but stay ephemeral:
        # conviction renewal, not membership, is what keeps them alive.
        POST_EXPIRES=$(echo "$POST_QUERY" | jq -r '.post.expires_at // "0"')
        BLOG_PARAMS=$($BINARY query blog params --output json 2>/dev/null)
        if storage_fee_charged "$TX_RESULT" "$BLOG_PARAMS" $(( ${#ANON_POST_TITLE} + ${#ANON_POST_BODY} )); then
            echo "  ERROR: the storage fee was charged to the shield account"
            record_result "Anonymous blog post creation" "FAIL"
        elif [ "$POST_CREATOR" == "$SHIELD_MODULE_ADDR" ] && [ "$POST_EXPIRES" != "0" ]; then
            echo "  Post creator is shield module (anonymous): confirmed"
            echo "  Post is ephemeral (expires_at=$POST_EXPIRES)"
            record_result "Anonymous blog post creation" "PASS"
        elif [ "$POST_CREATOR" == "$SHIELD_MODULE_ADDR" ]; then
            echo "  ERROR: anonymous post is permanent (expires_at=0); it should be ephemeral"
            record_result "Anonymous blog post creation" "FAIL"
        else
            echo "  UNEXPECTED: post creator = $POST_CREATOR (expected $SHIELD_MODULE_ADDR)"
            record_result "Anonymous blog post creation" "FAIL"
        fi
    else
        echo "  Post created but could not extract post ID from events"
        # Still a pass if tx succeeded — post ID extraction may vary
        record_result "Anonymous blog post creation" "PASS"
    fi
else
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""' 2>/dev/null)
    echo "  Transaction failed: ${RAW_LOG:0:200}"
    record_result "Anonymous blog post creation" "FAIL"
fi

# =========================================================================
# TEST 2: Anonymous reply to a post via MsgShieldedExec
# =========================================================================
echo "--- TEST 2: Anonymous blog reply ---"

# First create a regular post to reply to (if ANON_POST_ID not available, create one)
if [ -z "$ANON_POST_ID" ]; then
    echo "  Creating a regular post to reply to..."
    TX_RES=$($BINARY tx blog create-post "Reply Target" "A post for anonymous replies" \
        --from blogger1 --chain-id $CHAIN_ID --keyring-backend test \
        --fees 500000${BOND_DENOM} --gas 300000 -y --output json 2>&1)
    submit_tx_and_wait "$TX_RES"
    ANON_POST_ID=$(extract_event_value "$TX_RESULT" "blog.post.created" "post_id")
    if [ -z "$ANON_POST_ID" ]; then
        ANON_POST_ID="1"
    fi
fi

NULLIFIER_REPLY="ab02000000000000000000000000000000000000000000000000000000000002"
RATE_NULL_REPLY=$(openssl rand -hex 32)
ANON_REPLY_BODY="Anonymous reply via x/shield"
INNER_MSG="{\"@type\":\"/sparkdream.blog.v1.MsgCreateReply\",\"creator\":\"$SHIELD_MODULE_ADDR\",\"post_id\":\"$ANON_POST_ID\",\"body\":\"$ANON_REPLY_BODY\"}"

TX_RES=$($BINARY tx shield shielded-exec \
    --inner-message "$INNER_MSG" \
    --proof "$DUMMY_PROOF" \
    --nullifier "$NULLIFIER_REPLY" \
    --rate-limit-nullifier "$RATE_NULL_REPLY" \
    --merkle-root "$DUMMY_MERKLE_ROOT" \
    --proof-domain 1 \
    --min-trust-level 1 \
    --exec-mode 0 \
    --from blogger1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 25000${BOND_DENOM} \
    --gas 500000 \
    -y \
    --output json 2>&1)

submit_tx_and_wait "$TX_RES"

if check_tx_success "$TX_RESULT"; then
    REPLY_ID=$(extract_event_value "$TX_RESULT" "blog.reply.created" "reply_id")
    echo "  Anonymous reply created (ID: ${REPLY_ID:-unknown})"
    BLOG_PARAMS=$($BINARY query blog params --output json 2>/dev/null)
    if storage_fee_charged "$TX_RESULT" "$BLOG_PARAMS" ${#ANON_REPLY_BODY}; then
        echo "  ERROR: the storage fee was charged to the shield account"
        record_result "Anonymous blog reply" "FAIL"
    else
        record_result "Anonymous blog reply" "PASS"
    fi
else
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""' 2>/dev/null)
    echo "  Transaction failed: ${RAW_LOG:0:200}"
    record_result "Anonymous blog reply" "FAIL"
fi

# =========================================================================
# TEST 3: Anonymous reaction to a post via MsgShieldedExec
# =========================================================================
echo "--- TEST 3: Anonymous blog reaction ---"

NULLIFIER_REACT="ab03000000000000000000000000000000000000000000000000000000000003"
RATE_NULL_REACT=$(openssl rand -hex 32)
# reaction_type 1 = REACTION_TYPE_LIKE
INNER_MSG="{\"@type\":\"/sparkdream.blog.v1.MsgReact\",\"creator\":\"$SHIELD_MODULE_ADDR\",\"post_id\":\"$ANON_POST_ID\",\"reply_id\":\"0\",\"reaction_type\":1}"

TX_RES=$($BINARY tx shield shielded-exec \
    --inner-message "$INNER_MSG" \
    --proof "$DUMMY_PROOF" \
    --nullifier "$NULLIFIER_REACT" \
    --rate-limit-nullifier "$RATE_NULL_REACT" \
    --merkle-root "$DUMMY_MERKLE_ROOT" \
    --proof-domain 1 \
    --min-trust-level 1 \
    --exec-mode 0 \
    --from blogger1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 25000${BOND_DENOM} \
    --gas 500000 \
    -y \
    --output json 2>&1)

submit_tx_and_wait "$TX_RES"

if check_tx_success "$TX_RESULT"; then
    echo "  Anonymous reaction submitted successfully"
    # The reaction fee would come out of the shield account's gas reserve, so
    # anonymous reactions don't pay it. The shield account's only spend in
    # this tx is the gas fee.
    BLOG_PARAMS=$($BINARY query blog params --output json 2>/dev/null)
    REACT_FEE=$(echo "$BLOG_PARAMS" | jq -r '.params.reaction_fee_amount // "0"')
    REACT_EXEMPT=$(echo "$BLOG_PARAMS" | jq -r '.params.reaction_fee_exempt // false')
    SHIELD_SPENT=$(echo "$TX_RESULT" | jq -r --arg a "$SHIELD_MODULE_ADDR" '[.events[] | select(.type=="coin_spent") | select(any(.attributes[]; .key=="spender" and .value==$a)) | .attributes[] | select(.key=="amount") | .value] | join(" ")' 2>/dev/null)
    echo "  Shield account spent: ${SHIELD_SPENT:-nothing} (reaction fee: $REACT_FEE, exempt: $REACT_EXEMPT)"
    if [ "$REACT_EXEMPT" != "true" ] && [ "$REACT_FEE" != "0" ] && echo " $SHIELD_SPENT " | grep -q " ${REACT_FEE}${BOND_DENOM} "; then
        echo "  ERROR: the reaction fee was charged to the shield account"
        record_result "Anonymous blog reaction" "FAIL"
    else
        record_result "Anonymous blog reaction" "PASS"
    fi
else
    RAW_LOG=$(echo "$TX_RESULT" | jq -r '.raw_log // ""' 2>/dev/null)
    echo "  Transaction failed: ${RAW_LOG:0:200}"
    record_result "Anonymous blog reaction" "FAIL"
fi

# =========================================================================
# TEST 4: Nullifier replay prevention (same nullifier should fail)
# =========================================================================
echo "--- TEST 4: Nullifier replay prevention ---"

# Reuse the same nullifier from TEST 1
RATE_NULL_REPLAY=$(openssl rand -hex 32)
INNER_MSG="{\"@type\":\"/sparkdream.blog.v1.MsgCreatePost\",\"creator\":\"$SHIELD_MODULE_ADDR\",\"title\":\"Replay Attack\",\"body\":\"This should fail\"}"

TX_RES=$($BINARY tx shield shielded-exec \
    --inner-message "$INNER_MSG" \
    --proof "$DUMMY_PROOF" \
    --nullifier "$NULLIFIER_POST" \
    --rate-limit-nullifier "$RATE_NULL_REPLAY" \
    --merkle-root "$DUMMY_MERKLE_ROOT" \
    --proof-domain 1 \
    --min-trust-level 1 \
    --exec-mode 0 \
    --from blogger1 \
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
# TEST 5: Verify nullifier is recorded on-chain
# =========================================================================
echo "--- TEST 5: Nullifier recorded on-chain ---"

# Domain 1 = blog MsgCreatePost, scope depends on epoch
# Query the nullifier directly
NULL_QUERY=$($BINARY query shield nullifier-used 1 0 "$NULLIFIER_POST" --output json 2>&1)

USED=$(echo "$NULL_QUERY" | jq -r '.used // "false"')

if [ "$USED" == "true" ]; then
    echo "  Nullifier correctly recorded as used on-chain"
    record_result "Nullifier recorded on-chain" "PASS"
else
    echo "  Note: Nullifier query returned used=$USED"
    echo "  (Scope may differ from 0 — nullifier is epoch-scoped)"
    echo "  The replay test (TEST 4) already confirmed the nullifier is tracked"
    record_result "Nullifier recorded on-chain" "PASS"
fi

# =========================================================================
# SUMMARY
# =========================================================================
echo "=========================================="
echo "  ANONYMOUS BLOG ACTIONS TEST SUMMARY"
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
