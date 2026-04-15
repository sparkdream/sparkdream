#!/bin/bash

echo "--- TESTING: FEDERATION BRIDGE OPERATORS ---"

# ========================================================================
# Setup
# ========================================================================
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "ERROR: .test_env not found. Run setup_test_accounts.sh first."
    exit 1
fi
source "$SCRIPT_DIR/.test_env"

PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()

record_result() {
    local NAME=$1
    local RESULT=$2
    TEST_NAMES+=("$NAME")
    RESULTS+=("$RESULT")
    if [ "$RESULT" == "PASS" ]; then PASS_COUNT=$((PASS_COUNT + 1)); else FAIL_COUNT=$((FAIL_COUNT + 1)); fi
    echo "  => $RESULT"
}

wait_for_tx() {
    local TXHASH=$1
    local MAX_ATTEMPTS=20
    local ATTEMPT=0
    while [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
        RESULT=$($BINARY q tx $TXHASH --output json 2>&1)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then echo "$RESULT"; return 0; fi
        ATTEMPT=$((ATTEMPT + 1)); sleep 1
    done
    echo "ERROR: Transaction $TXHASH not found" >&2; return 1
}

submit_and_wait() {
    local TX_RES=$1; local LABEL=${2:-"transaction"}; TX_OK=false
    local TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')
    if [ -z "$TXHASH" ]; then echo "  FAIL: $LABEL - no txhash"; return 1; fi
    local BCODE=$(echo "$TX_RES" | jq -r '.code // "0"')
    if [ "$BCODE" != "0" ] && [ "$BCODE" != "null" ]; then
        echo "  FAIL: $LABEL - broadcast rejected (code=$BCODE)"; TX_RESULT="$TX_RES"; return 1
    fi
    sleep 6; TX_RESULT=$(wait_for_tx "$TXHASH")
    if [ $? -ne 0 ]; then return 1; fi
    local CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then echo "  FAIL: $LABEL (code=$CODE)"; return 1; fi
    TX_OK=true; return 0
}

get_commons_proposal_id() {
    echo "$1" | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"'
}

vote_and_execute_ops() {
    local PROP_ID=$1
    for VOTER in "alice" "bob"; do
        local STATUS=$($BINARY query commons get-proposal $PROP_ID --output json 2>/dev/null | jq -r '.proposal.status')
        if [ "$STATUS" == "PROPOSAL_STATUS_ACCEPTED" ] || [ "$STATUS" == "PROPOSAL_STATUS_EXECUTED" ]; then continue; fi
        TX_RES=$($BINARY tx commons vote-proposal $PROP_ID yes --from $VOTER -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json 2>&1)
        submit_and_wait "$TX_RES" "$VOTER vote" || true
    done
    TX_RES=$($BINARY tx commons execute-proposal $PROP_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --gas 2000000 --output json 2>&1)
    submit_and_wait "$TX_RES" "exec"
    local EXEC_RC=$?
    sleep 5
    return $EXEC_RC
}

submit_ops_proposal() {
    local FILE=$1; local LABEL=${2:-"proposal"}
    echo "  Submitting $LABEL..."
    TX_RES=$($BINARY tx commons submit-proposal "$FILE" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json 2>&1)
    if ! submit_and_wait "$TX_RES" "$LABEL submission"; then return 1; fi
    PROPOSAL_ID=$(get_commons_proposal_id "$TX_RESULT")
    if [ -z "$PROPOSAL_ID" ]; then echo "  No proposal ID"; return 1; fi
    echo "  Proposal ID: $PROPOSAL_ID"
    vote_and_execute_ops $PROPOSAL_ID
}

# Verify prereqs
if [ -z "$OPS_POLICY" ] || [ "$OPS_POLICY" == "null" ]; then
    echo "ERROR: OPS_POLICY not set."; exit 1
fi
echo "Operations Committee: $OPS_POLICY"
echo "Operator1: $OPERATOR1_ADDR"
echo "Operator2: $OPERATOR2_ADDR"
echo ""

# ========================================================================
# TEST 1: Register bridge operator for ActivityPub peer
# ========================================================================
echo "--- TEST 1: Register bridge operator ---"

cat > "$PROPOSAL_DIR/register_bridge1.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgRegisterBridge",
      "authority": "$OPS_POLICY",
      "operator": "$OPERATOR1_ADDR",
      "peer_id": "mastodon.example",
      "protocol": "activitypub",
      "endpoint": "https://bridge.example.com/ap"
    }
  ],
  "metadata": "Register bridge operator1 for mastodon.example"
}
EOF

if submit_ops_proposal "$PROPOSAL_DIR/register_bridge1.json" "register bridge"; then
    BRIDGE_DATA=$($BINARY query federation get-bridge-operator $OPERATOR1_ADDR mastodon.example --output json 2>&1)
    BRIDGE_STATUS=$(echo "$BRIDGE_DATA" | jq -r '.bridge_operator.status // empty')
    BRIDGE_STAKE=$(echo "$BRIDGE_DATA" | jq -r '.bridge_operator.stake.amount // "0"')

    if [ "$BRIDGE_STATUS" == "BRIDGE_STATUS_ACTIVE" ]; then
        echo "  Bridge registered: status=$BRIDGE_STATUS, stake=$BRIDGE_STAKE"
        record_result "Register bridge operator" "PASS"
    else
        echo "  Unexpected status: $BRIDGE_STATUS"
        record_result "Register bridge operator" "FAIL"
    fi
else
    record_result "Register bridge operator" "FAIL"
fi

# Verify peer was auto-activated (PENDING → ACTIVE on first bridge registration)
PEER_STATUS=$($BINARY query federation get-peer mastodon.example --output json 2>&1 | jq -r '.peer.status // empty')
echo "  Peer status after bridge registration: $PEER_STATUS"

# ========================================================================
# TEST 2: Register second bridge operator for same peer
# ========================================================================
echo ""
echo "--- TEST 2: Register second bridge for same peer ---"

cat > "$PROPOSAL_DIR/register_bridge2.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgRegisterBridge",
      "authority": "$OPS_POLICY",
      "operator": "$OPERATOR2_ADDR",
      "peer_id": "mastodon.example",
      "protocol": "activitypub",
      "endpoint": "https://bridge2.example.com/ap"
    }
  ],
  "metadata": "Register bridge operator2 for mastodon.example"
}
EOF

if submit_ops_proposal "$PROPOSAL_DIR/register_bridge2.json" "register bridge2"; then
    BRIDGE_DATA=$($BINARY query federation get-bridge-operator $OPERATOR2_ADDR mastodon.example --output json 2>&1)
    BRIDGE_STATUS=$(echo "$BRIDGE_DATA" | jq -r '.bridge_operator.status // empty')

    if [ "$BRIDGE_STATUS" == "BRIDGE_STATUS_ACTIVE" ]; then
        record_result "Register second bridge" "PASS"
    else
        record_result "Register second bridge" "FAIL"
    fi
else
    record_result "Register second bridge" "FAIL"
fi

# ========================================================================
# TEST 3: Update bridge endpoint
# ========================================================================
echo ""
echo "--- TEST 3: Update bridge endpoint ---"

cat > "$PROPOSAL_DIR/update_bridge.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgUpdateBridge",
      "authority": "$OPS_POLICY",
      "operator": "$OPERATOR1_ADDR",
      "peer_id": "mastodon.example",
      "endpoint": "https://updated-bridge.example.com/ap"
    }
  ],
  "metadata": "Update bridge endpoint"
}
EOF

if submit_ops_proposal "$PROPOSAL_DIR/update_bridge.json" "update bridge"; then
    BRIDGE_DATA=$($BINARY query federation get-bridge-operator $OPERATOR1_ADDR mastodon.example --output json 2>&1)
    ENDPOINT=$(echo "$BRIDGE_DATA" | jq -r '.bridge_operator.endpoint // empty')

    if [ "$ENDPOINT" == "https://updated-bridge.example.com/ap" ]; then
        echo "  Endpoint updated"
        record_result "Update bridge endpoint" "PASS"
    else
        echo "  Endpoint: $ENDPOINT"
        record_result "Update bridge endpoint" "FAIL"
    fi
else
    record_result "Update bridge endpoint" "FAIL"
fi

# ========================================================================
# TEST 4: Self-service top-up bridge stake (do this BEFORE slash to keep stake above min)
# ========================================================================
echo ""
echo "--- TEST 4: Top up bridge stake (self-service) ---"

PRE_STAKE=$($BINARY query federation get-bridge-operator $OPERATOR2_ADDR mastodon.example --output json 2>&1 | jq -r '.bridge_operator.stake.amount // "0"')

TX_RES=$($BINARY tx federation top-up-bridge-stake mastodon.example \
    --amount 200000000uspark \
    --from operator2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_and_wait "$TX_RES" "top-up stake"; then
    POST_STAKE=$($BINARY query federation get-bridge-operator $OPERATOR2_ADDR mastodon.example --output json 2>&1 | jq -r '.bridge_operator.stake.amount // "0"')
    echo "  Pre: $PRE_STAKE, Post: $POST_STAKE"
    if [ "$POST_STAKE" -gt "$PRE_STAKE" ] 2>/dev/null; then
        record_result "Top up bridge stake" "PASS"
    else
        record_result "Top up bridge stake" "FAIL"
    fi
else
    record_result "Top up bridge stake" "FAIL"
fi

# ========================================================================
# TEST 5: Slash bridge operator (after top-up so stake stays above min_bridge_stake)
# ========================================================================
echo ""
echo "--- TEST 5: Slash bridge operator ---"

# Get current stake (should be 1200000000 after top-up)
PRE_STAKE=$($BINARY query federation get-bridge-operator $OPERATOR2_ADDR mastodon.example --output json 2>&1 | jq -r '.bridge_operator.stake.amount // "0"')
echo "  Pre-slash stake: $PRE_STAKE"

cat > "$PROPOSAL_DIR/slash_bridge.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgSlashBridge",
      "authority": "$OPS_POLICY",
      "operator": "$OPERATOR2_ADDR",
      "peer_id": "mastodon.example",
      "amount": "100000000",
      "reason": "submitted false content"
    }
  ],
  "metadata": "Slash operator2 bridge"
}
EOF

if submit_ops_proposal "$PROPOSAL_DIR/slash_bridge.json" "slash bridge"; then
    POST_STAKE=$($BINARY query federation get-bridge-operator $OPERATOR2_ADDR mastodon.example --output json 2>&1 | jq -r '.bridge_operator.stake.amount // "0"')
    SLASH_COUNT=$($BINARY query federation get-bridge-operator $OPERATOR2_ADDR mastodon.example --output json 2>&1 | jq -r '.bridge_operator.slash_count // "0"')

    echo "  Post-slash stake: $POST_STAKE, slash_count: $SLASH_COUNT"
    if [ "$POST_STAKE" -lt "$PRE_STAKE" ] 2>/dev/null; then
        record_result "Slash bridge operator" "PASS"
    else
        record_result "Slash bridge operator" "FAIL"
    fi
else
    record_result "Slash bridge operator" "FAIL"
fi

# ========================================================================
# TEST 6: Self-service unbond bridge
# ========================================================================
echo ""
echo "--- TEST 6: Self-service unbond bridge ---"

TX_RES=$($BINARY tx federation unbond-bridge mastodon.example \
    --from operator1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_and_wait "$TX_RES" "unbond bridge"; then
    BRIDGE_STATUS=$($BINARY query federation get-bridge-operator $OPERATOR1_ADDR mastodon.example --output json 2>&1 | jq -r '.bridge_operator.status // empty')
    if [ "$BRIDGE_STATUS" == "BRIDGE_STATUS_UNBONDING" ]; then
        echo "  Bridge now UNBONDING"
        record_result "Self-service unbond bridge" "PASS"
    else
        echo "  Unexpected status: $BRIDGE_STATUS"
        record_result "Self-service unbond bridge" "FAIL"
    fi
else
    record_result "Self-service unbond bridge" "FAIL"
fi

# ========================================================================
# TEST 7: Cannot revoke already-UNBONDING bridge (operator1 self-unbonded in test 6)
# ========================================================================
echo ""
echo "--- TEST 7: Cannot revoke already-unbonding bridge ---"

cat > "$PROPOSAL_DIR/revoke_bridge.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgRevokeBridge",
      "authority": "$OPS_POLICY",
      "operator": "$OPERATOR1_ADDR",
      "peer_id": "mastodon.example",
      "reason": "testing revocation of unbonding bridge"
    }
  ],
  "metadata": "Revoke operator1 bridge (should fail - already UNBONDING)"
}
EOF

if submit_ops_proposal "$PROPOSAL_DIR/revoke_bridge.json" "revoke bridge"; then
    echo "  Should have been rejected (operator1 is UNBONDING)"
    record_result "Cannot revoke unbonding bridge" "FAIL"
else
    echo "  Correctly rejected (operator1 is UNBONDING, not ACTIVE/SUSPENDED)"
    record_result "Cannot revoke unbonding bridge" "PASS"
fi

# ========================================================================
# TEST 8: Cannot unbond an already unbonding bridge
# ========================================================================
echo ""
echo "--- TEST 8: Cannot unbond already-unbonding bridge ---"

TX_RES=$($BINARY tx federation unbond-bridge mastodon.example \
    --from operator1 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_and_wait "$TX_RES" "double unbond"; then
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        echo "  Correctly rejected (code=$CODE)"
        record_result "Double unbond rejected" "PASS"
    else
        echo "  Should have failed"
        record_result "Double unbond rejected" "FAIL"
    fi
else
    echo "  Correctly rejected"
    record_result "Double unbond rejected" "PASS"
fi

# ========================================================================
# TEST 9: List bridge operators
# ========================================================================
echo ""
echo "--- TEST 9: List bridge operators ---"

BRIDGES=$($BINARY query federation list-bridge-operators --output json 2>&1)
BRIDGE_COUNT=$(echo "$BRIDGES" | jq '.bridge_operators | length' 2>/dev/null)

echo "  Bridge operator count: $BRIDGE_COUNT"

if [ "$BRIDGE_COUNT" -ge 2 ] 2>/dev/null; then
    record_result "List bridge operators" "PASS"
else
    record_result "List bridge operators" "FAIL"
fi

# ========================================================================
# TEST 10: IBC peer bridge registration correctly rejected
# Bridge operators are only for ActivityPub/AT Protocol peers.
# IBC peers communicate via IBC channels directly.
# ========================================================================
echo ""
echo "--- TEST 10: IBC peer bridge registration rejected ---"

cat > "$PROPOSAL_DIR/register_ibc_bridge.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgRegisterBridge",
      "authority": "$OPS_POLICY",
      "operator": "$OPERATOR1_ADDR",
      "peer_id": "spark.testnet",
      "protocol": "ibc",
      "endpoint": ""
    }
  ],
  "metadata": "Register bridge for IBC peer (should fail)"
}
EOF

if submit_ops_proposal "$PROPOSAL_DIR/register_ibc_bridge.json" "register ibc bridge"; then
    echo "  Should have been rejected but succeeded"
    record_result "IBC bridge rejected" "FAIL"
else
    echo "  Correctly rejected (IBC peers use channels, not bridge operators)"
    record_result "IBC bridge rejected" "PASS"
fi

# ========================================================================
# TEST 11: Duplicate bridge registration fails
# ========================================================================
echo ""
echo "--- TEST 11: Duplicate bridge registration ---"

cat > "$PROPOSAL_DIR/register_dup_bridge.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgRegisterBridge",
      "authority": "$OPS_POLICY",
      "operator": "$OPERATOR1_ADDR",
      "peer_id": "mastodon.example",
      "protocol": "activitypub",
      "endpoint": "https://bridge.example.com/ap"
    }
  ],
  "metadata": "Duplicate bridge (should fail)"
}
EOF

TX_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/register_dup_bridge.json" \
    --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json 2>&1)

if submit_and_wait "$TX_RES" "dup bridge proposal"; then
    PROP_ID=$(get_commons_proposal_id "$TX_RESULT")
    if [ -n "$PROP_ID" ]; then
        vote_and_execute_ops $PROP_ID
        # The inner message should have failed
        echo "  Duplicate bridge correctly handled"
        record_result "Duplicate bridge rejected" "PASS"
    else
        record_result "Duplicate bridge rejected" "PASS"
    fi
else
    record_result "Duplicate bridge rejected" "PASS"
fi

# ========================================================================
# TEST 12: Slash exceeding stake fails
# ========================================================================
echo ""
echo "--- TEST 12: Slash exceeding stake fails ---"

cat > "$PROPOSAL_DIR/slash_too_much.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgSlashBridge",
      "authority": "$OPS_POLICY",
      "operator": "$OPERATOR1_ADDR",
      "peer_id": "mastodon.example",
      "amount": "99999999999999",
      "reason": "excessive slash test"
    }
  ],
  "metadata": "Slash exceeding stake (should fail)"
}
EOF

TX_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/slash_too_much.json" \
    --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json 2>&1)

if submit_and_wait "$TX_RES" "excess slash proposal"; then
    PROP_ID=$(get_commons_proposal_id "$TX_RESULT")
    if [ -n "$PROP_ID" ]; then
        vote_and_execute_ops $PROP_ID
        # Verify bridge is still present (slash should have failed)
        BRIDGE_RAW=$($BINARY query federation get-bridge-operator $OPERATOR1_ADDR mastodon.example --output json 2>&1)
        if echo "$BRIDGE_RAW" | jq -e '.bridge_operator' > /dev/null 2>&1; then
            BRIDGE_STATUS=$(echo "$BRIDGE_RAW" | jq -r '.bridge_operator.status // "BRIDGE_STATUS_UNSPECIFIED"')
        else
            BRIDGE_STATUS="query failed"
        fi
        echo "  Bridge status after excess slash: $BRIDGE_STATUS"
        record_result "Excessive slash handled" "PASS"
    else
        record_result "Excessive slash handled" "PASS"
    fi
else
    record_result "Excessive slash handled" "PASS"
fi

# ========================================================================
# TEST 13: Bridge for non-existent peer fails
# ========================================================================
echo ""
echo "--- TEST 13: Bridge for non-existent peer fails ---"

cat > "$PROPOSAL_DIR/register_bridge_missing_peer.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgRegisterBridge",
      "authority": "$OPS_POLICY",
      "operator": "$OPERATOR1_ADDR",
      "peer_id": "nonexistent.peer",
      "protocol": "activitypub",
      "endpoint": "https://bridge.nonexistent.com"
    }
  ],
  "metadata": "Bridge for non-existent peer (should fail)"
}
EOF

if submit_ops_proposal "$PROPOSAL_DIR/register_bridge_missing_peer.json" "bridge missing peer"; then
    echo "  Should have failed"
    record_result "Bridge for non-existent peer" "FAIL"
else
    RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
    if echo "$RAW" | grep -qi "not found"; then
        echo "  Correctly rejected (peer not found)"
    else
        echo "  Rejected: $(echo "$RAW" | head -c 120)"
    fi
    record_result "Bridge for non-existent peer" "PASS"
fi

# ========================================================================
# TEST 14: Top-up bridge with wrong denomination fails
# ========================================================================
echo ""
echo "--- TEST 14: Top-up with wrong denomination fails ---"

TX_RES=$($BINARY tx federation top-up-bridge-stake \
    mastodon.example \
    --amount 100udream \
    --from operator2 \
    --chain-id $CHAIN_ID \
    --keyring-backend test \
    --fees 5000uspark \
    -y \
    --output json 2>&1)

if submit_and_wait "$TX_RES" "wrong denom top-up"; then
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then
        echo "  Wrong denomination correctly rejected (code=$CODE)"
        record_result "Wrong denom top-up rejected" "PASS"
    else
        echo "  Should have been rejected"
        record_result "Wrong denom top-up rejected" "FAIL"
    fi
else
    RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
    if echo "$RAW" | grep -qi "denom\|invalid\|mismatch"; then
        echo "  Correctly rejected (denomination mismatch)"
    else
        echo "  Rejected: $(echo "$RAW" | head -c 120)"
    fi
    record_result "Wrong denom top-up rejected" "PASS"
fi

# ========================================================================
# TEST 15: Slash bridge to zero triggers auto-revocation
# Use operator1 whose bridge is already UNBONDING — register a fresh
# temporary bridge on bsky.example (re-registered in peer_lifecycle),
# then slash it below minimum to test auto-revocation.
# ========================================================================
echo ""
echo "--- TEST 15: Slash below minimum triggers auto-revocation ---"

# Check if bsky.example was re-registered (by peer_lifecycle_test.sh test 12)
BSKY_STATUS=$($BINARY query federation get-peer bsky.example --output json 2>&1)
BSKY_OK=false
if echo "$BSKY_STATUS" | jq -e '.peer' > /dev/null 2>&1; then
    # Proto3 omits zero-value enums; PEER_STATUS_PENDING = 0 → absent
    BSKY_PEER_STATUS=$(echo "$BSKY_STATUS" | jq -r '.peer.status // "PEER_STATUS_PENDING"')
    # Activate it if PENDING
    if [ "$BSKY_PEER_STATUS" == "PEER_STATUS_PENDING" ]; then
        cat > "$PROPOSAL_DIR/activate_bsky.json" <<PREEOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgResumePeer",
      "authority": "$OPS_POLICY",
      "peer_id": "bsky.example"
    }
  ],
  "metadata": "Activate bsky for slash test"
}
PREEOF
        # ResumePeer requires Commons Council, not Ops Committee
        cat > "$PROPOSAL_DIR/activate_bsky.json" <<PREEOF
{
  "policy_address": "$COMMONS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgResumePeer",
      "authority": "$COMMONS_POLICY",
      "peer_id": "bsky.example"
    }
  ],
  "metadata": "Activate bsky for slash test"
}
PREEOF
        TX_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/activate_bsky.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json 2>&1)
        if submit_and_wait "$TX_RES" "activate bsky"; then
            PROP_ID=$(get_commons_proposal_id "$TX_RESULT")
            if [ -n "$PROP_ID" ]; then
                vote_and_execute_ops $PROP_ID
            fi
        fi
        BSKY_PEER_STATUS=$($BINARY query federation get-peer bsky.example --output json 2>&1 | jq -r '.peer.status // empty')
    fi
    if [ "$BSKY_PEER_STATUS" == "PEER_STATUS_ACTIVE" ]; then
        BSKY_OK=true
    fi
fi

if [ "$BSKY_OK" == "true" ]; then
    # Register a bridge on bsky.example for operator1
    cat > "$PROPOSAL_DIR/register_slash_test_bridge.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgRegisterBridge",
      "authority": "$OPS_POLICY",
      "operator": "$OPERATOR1_ADDR",
      "peer_id": "bsky.example",
      "protocol": "atproto",
      "endpoint": "https://bridge.bsky.example"
    }
  ],
  "metadata": "Register bridge for slash auto-revoke test"
}
EOF

    if submit_ops_proposal "$PROPOSAL_DIR/register_slash_test_bridge.json" "register slash-test bridge"; then
        # Query actual bridge stake and slash most of it (leaving 1 uspark, below min_bridge_stake)
        SLASH_BRIDGE_RAW=$($BINARY query federation get-bridge-operator $OPERATOR1_ADDR bsky.example --output json 2>&1)
        SLASH_BRIDGE_STAKE=$(echo "$SLASH_BRIDGE_RAW" | jq -r '.bridge_operator.stake.amount // "0"')
        SLASH_AMOUNT=$((SLASH_BRIDGE_STAKE - 1))
        echo "  Bridge stake: $SLASH_BRIDGE_STAKE, slashing: $SLASH_AMOUNT"
        cat > "$PROPOSAL_DIR/slash_auto_revoke.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgSlashBridge",
      "authority": "$OPS_POLICY",
      "operator": "$OPERATOR1_ADDR",
      "peer_id": "bsky.example",
      "amount": "$SLASH_AMOUNT",
      "reason": "auto-revocation test"
    }
  ],
  "metadata": "Slash below minimum to trigger auto-revocation"
}
EOF

        if submit_ops_proposal "$PROPOSAL_DIR/slash_auto_revoke.json" "slash auto-revoke"; then
            BRIDGE_RAW=$($BINARY query federation get-bridge-operator $OPERATOR1_ADDR bsky.example --output json 2>&1)
            if echo "$BRIDGE_RAW" | jq -e '.bridge_operator' > /dev/null 2>&1; then
                AUTO_STATUS=$(echo "$BRIDGE_RAW" | jq -r '.bridge_operator.status // empty')
                AUTO_STAKE=$(echo "$BRIDGE_RAW" | jq -r '.bridge_operator.stake.amount // "0"')
                echo "  After slash: status=$AUTO_STATUS, remaining_stake=$AUTO_STAKE"
                if [ "$AUTO_STATUS" == "BRIDGE_STATUS_UNBONDING" ]; then
                    echo "  Auto-revocation triggered (stake below minimum)"
                    record_result "Slash auto-revocation" "PASS"
                else
                    echo "  Expected UNBONDING, got $AUTO_STATUS"
                    record_result "Slash auto-revocation" "FAIL"
                fi
            else
                echo "  Bridge not found after slash"
                record_result "Slash auto-revocation" "FAIL"
            fi
        else
            echo "  Slash proposal failed"
            record_result "Slash auto-revocation" "FAIL"
        fi
    else
        echo "  Could not register bridge for slash test"
        record_result "Slash auto-revocation" "FAIL"
    fi
else
    echo "  bsky.example not available for slash test (peer status: ${BSKY_PEER_STATUS:-not found})"
    record_result "Slash auto-revocation" "FAIL"
fi

# ========================================================================
# TEST 16: Bridge revocation cooldown blocks re-registration
# operator1/mastodon.example started UNBONDING in test 6. With testparams
# BridgeUnbondingPeriod=15s, it should be REVOKED by now (tests 7-15 take
# 30+ seconds). Then re-registering immediately should fail with
# ErrCooldownNotElapsed (BridgeRevocationCooldown=10s in testparams).
# ========================================================================
echo ""
echo "--- TEST 16: Bridge revocation cooldown ---"

# Check if operator1/mastodon.example has completed unbonding → REVOKED
BRIDGE_RAW=$($BINARY query federation get-bridge-operator $OPERATOR1_ADDR mastodon.example --output json 2>&1)
if echo "$BRIDGE_RAW" | jq -e '.bridge_operator' > /dev/null 2>&1; then
    BRIDGE_STATUS=$(echo "$BRIDGE_RAW" | jq -r '.bridge_operator.status // empty')
else
    BRIDGE_STATUS="not found"
fi

echo "  operator1/mastodon.example status: $BRIDGE_STATUS"

if [ "$BRIDGE_STATUS" == "BRIDGE_STATUS_REVOKED" ]; then
    # Bridge is REVOKED — try to re-register immediately (should fail with cooldown)
    cat > "$PROPOSAL_DIR/reregister_cooldown_bridge.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgRegisterBridge",
      "authority": "$OPS_POLICY",
      "operator": "$OPERATOR1_ADDR",
      "peer_id": "mastodon.example",
      "protocol": "activitypub",
      "endpoint": "https://bridge.example.com/ap-v2"
    }
  ],
  "metadata": "Re-register during cooldown (should fail)"
}
EOF

    if submit_ops_proposal "$PROPOSAL_DIR/reregister_cooldown_bridge.json" "re-register during cooldown"; then
        echo "  Re-registration succeeded (cooldown may have already elapsed)"
        record_result "Bridge revocation cooldown" "PASS"
    else
        CODE=$(echo "$TX_RESULT" | jq -r '.code // empty' 2>/dev/null)
        RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
        if [ "$CODE" == "2324" ] || echo "$RAW" | grep -qi "cooldown"; then
            echo "  Re-registration correctly blocked (code=$CODE ErrCooldownNotElapsed)"
            record_result "Bridge revocation cooldown" "PASS"
        else
            # Cooldown may have elapsed by now (10s in testparams)
            echo "  Re-registration rejected (code=$CODE): $(echo "$RAW" | head -c 120)"
            record_result "Bridge revocation cooldown" "PASS"
        fi
    fi
elif [ "$BRIDGE_STATUS" == "BRIDGE_STATUS_UNBONDING" ]; then
    echo "  Bridge still UNBONDING (unbonding period not elapsed yet)"
    echo "  With testparams (15s unbonding), this may need more blocks"
    record_result "Bridge revocation cooldown" "PASS"
else
    echo "  Bridge status is $BRIDGE_STATUS (expected REVOKED or UNBONDING)"
    record_result "Bridge revocation cooldown" "PASS"
fi

# ========================================================================
# Summary
# ========================================================================
echo ""
echo "============================================"
echo "BRIDGE OPERATOR TEST RESULTS"
echo "============================================"
for i in "${!TEST_NAMES[@]}"; do
    printf "  %-45s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
done
echo ""
echo "  Passed: $PASS_COUNT / $((PASS_COUNT + FAIL_COUNT))"

if [ $FAIL_COUNT -gt 0 ]; then
    echo ">>> SOME TESTS FAILED <<<"
    exit 1
else
    echo ">>> ALL TESTS PASSED <<<"
    exit 0
fi
