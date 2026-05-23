#!/bin/bash

echo "--- TESTING: FEDERATION BRIDGE OPERATORS (post-Phase-4 federation→service migration) ---"

# Post-migration semantics:
#   - MsgRegisterBridge is now operator-signed (no OpsComm proposal).
#     The operator pays their own bond into x/service.
#   - Removed messages: SlashBridge, RevokeBridge, UnbondBridge, TopUpBridgeStake.
#     Operators call x/service equivalents directly:
#       tx service unbond-operator      federation-bridge-<protocol>
#       tx service top-up-bond          federation-bridge-<protocol> <amount>
#   - BridgeBinding holds only federation-side fields; bond/status live on
#     x/service.Operator keyed by (address, federation-bridge-<protocol>).
#   - Per Decision 1a, one (address, protocol) shares a single service.Operator
#     across multiple peer bindings.

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

# Service types — one per supported peer protocol (genesis-seeded).
SVC_AP="federation-bridge-activitypub"
SVC_AT="federation-bridge-atproto"

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
        RESULT=$($BINARY q tx $TXHASH --output json)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then echo "$RESULT"; return 0; fi
        ATTEMPT=$((ATTEMPT + 1)); sleep 1
    done
    echo "ERROR: Transaction $TXHASH not found" >&2; return 1
}

submit_and_wait() {
    local TX_RES=$1; local LABEL=${2:-"transaction"}; TX_OK=false
    local TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')
    if [ -z "$TXHASH" ]; then echo "  FAIL: $LABEL - no txhash"; TX_RESULT="$TX_RES"; return 1; fi
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
        TX_RES=$($BINARY tx commons vote-proposal $PROP_ID yes --from $VOTER -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000${BOND_DENOM} --output json)
        submit_and_wait "$TX_RES" "$VOTER vote" || true
    done
    TX_RES=$($BINARY tx commons execute-proposal $PROP_ID --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000${BOND_DENOM} --gas 2000000 --output json)
    submit_and_wait "$TX_RES" "exec"
    local EXEC_RC=$?
    sleep 5
    return $EXEC_RC
}

submit_ops_proposal() {
    local FILE=$1; local LABEL=${2:-"proposal"}
    echo "  Submitting $LABEL..."
    TX_RES=$($BINARY tx commons submit-proposal "$FILE" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000${BOND_DENOM} --output json)
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

# Read the canonical bond floor from the seeded ServiceTypeConfig. With
# testparams genesis_vals.go, min_bond is kept at 1000 SPARK but the
# unbonding period is shortened so the unbond/claim cycle fits in test
# wall-clock. Falls back to 1000 SPARK if the query fails (the genesis
# default).
MIN_BOND_AMT=$($BINARY query service service-type $SVC_AP --output json 2>/dev/null | jq -r '.config.min_bond_amount // "1000000000"')
# stake_amount is now a bare bond-denom amount (denom resolved at runtime from x/identity).
BOND_AMOUNT="${MIN_BOND_AMT}"
echo "Min bond (federation-bridge-activitypub): ${BOND_AMOUNT}${BOND_DENOM}"
echo ""

# ========================================================================
# TEST 1: Register bridge operator for ActivityPub peer (operator-signed,
# escrows bond into x/service)
# ========================================================================
echo "--- TEST 1: Register bridge operator (operator-signed) ---"

TX_RES=$($BINARY tx federation register-bridge \
    mastodon.example activitypub https://bridge.example.com/ap "$BOND_AMOUNT" \
    --from operator1 -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json)

if submit_and_wait "$TX_RES" "register bridge"; then
    BINDING=$($BINARY query federation get-bridge-binding $OPERATOR1_ADDR mastodon.example --output json)
    BINDING_ADDR=$(echo "$BINDING" | jq -r '.bridge_binding.address // empty')
    SVC_STATUS=$($BINARY query service operator $OPERATOR1_ADDR $SVC_AP --output json 2>&1 | jq -r '.operator.status // empty')
    SVC_BOND=$($BINARY query service operator $OPERATOR1_ADDR $SVC_AP --output json 2>&1 | jq -r '.operator.bond_amount // "0"')

    if [ "$BINDING_ADDR" == "$OPERATOR1_ADDR" ] && [ "$SVC_STATUS" == "OPERATOR_STATUS_ACTIVE" ] && [ "$SVC_BOND" == "$MIN_BOND_AMT" ]; then
        echo "  Bridge binding present; service.Operator ACTIVE; bond=$SVC_BOND"
        record_result "Register bridge operator" "PASS"
    else
        echo "  binding=$BINDING_ADDR service_status=$SVC_STATUS bond=$SVC_BOND"
        record_result "Register bridge operator" "FAIL"
    fi
else
    record_result "Register bridge operator" "FAIL"
fi

# Verify peer was auto-activated (PENDING → ACTIVE on first bridge registration)
PEER_STATUS=$($BINARY query federation get-peer mastodon.example --output json 2>&1 | jq -r '.peer.status // empty')
echo "  Peer status after bridge registration: $PEER_STATUS"

# ========================================================================
# TEST 2: Register second bridge operator (different address) for same peer
# Different operator address → independent service.Operator + bond.
# ========================================================================
echo ""
echo "--- TEST 2: Register second bridge for same peer ---"

TX_RES=$($BINARY tx federation register-bridge \
    mastodon.example activitypub https://bridge2.example.com/ap "$BOND_AMOUNT" \
    --from operator2 -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json)

if submit_and_wait "$TX_RES" "register bridge2"; then
    SVC_STATUS=$($BINARY query service operator $OPERATOR2_ADDR $SVC_AP --output json 2>&1 | jq -r '.operator.status // empty')
    if [ "$SVC_STATUS" == "OPERATOR_STATUS_ACTIVE" ]; then
        record_result "Register second bridge" "PASS"
    else
        record_result "Register second bridge" "FAIL"
    fi
else
    record_result "Register second bridge" "FAIL"
fi

# ========================================================================
# TEST 3: Update bridge endpoint (still authority-gated to OpsComm)
# ========================================================================
echo ""
echo "--- TEST 3: Update bridge endpoint (OpsComm) ---"

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
    ENDPOINT=$($BINARY query federation get-bridge-binding $OPERATOR1_ADDR mastodon.example --output json 2>&1 | jq -r '.bridge_binding.endpoint // empty')
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
# TEST 4: Top up bond via x/service (replaces MsgTopUpBridgeStake)
# Operator calls x/service directly — federation has no top-up entry point.
# ========================================================================
echo ""
echo "--- TEST 4: Top up bond via x/service ---"

PRE_BOND=$($BINARY query service operator $OPERATOR2_ADDR $SVC_AP --output json 2>&1 | jq -r '.operator.bond_amount // "0"')

TX_RES=$($BINARY tx service top-up-bond $SVC_AP 200000000 \
    --from operator2 -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json)

if submit_and_wait "$TX_RES" "top-up bond"; then
    POST_BOND=$($BINARY query service operator $OPERATOR2_ADDR $SVC_AP --output json 2>&1 | jq -r '.operator.bond_amount // "0"')
    echo "  Pre: $PRE_BOND, Post: $POST_BOND"
    if [ "$POST_BOND" -gt "$PRE_BOND" ] 2>/dev/null; then
        record_result "Top up bond via x/service" "PASS"
    else
        record_result "Top up bond via x/service" "FAIL"
    fi
else
    record_result "Top up bond via x/service" "FAIL"
fi

# ========================================================================
# TEST 5: Wrong-denom top-up rejected
# ========================================================================
echo ""
echo "--- TEST 5: Wrong-denom top-up rejected ---"

TX_RES=$($BINARY tx service top-up-bond $SVC_AP 100${DREAM_DENOM} \
    --from operator2 -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json)

if submit_and_wait "$TX_RES" "wrong denom"; then
    echo "  Should have failed"
    record_result "Wrong denom top-up rejected" "FAIL"
else
    RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
    echo "  Correctly rejected: $(echo "$RAW" | head -c 120)"
    record_result "Wrong denom top-up rejected" "PASS"
fi

# ========================================================================
# TEST 6: Self-service unbond via x/service (replaces MsgUnbondBridge)
# ========================================================================
echo ""
echo "--- TEST 6: Self-service unbond via x/service ---"

TX_RES=$($BINARY tx service unbond-operator $SVC_AP \
    --from operator1 -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json)

if submit_and_wait "$TX_RES" "unbond"; then
    SVC_STATUS=$($BINARY query service operator $OPERATOR1_ADDR $SVC_AP --output json 2>&1 | jq -r '.operator.status // empty')
    if [ "$SVC_STATUS" == "OPERATOR_STATUS_UNBONDING" ]; then
        echo "  service.Operator now UNBONDING"
        record_result "Self-service unbond" "PASS"
    else
        echo "  Unexpected status: $SVC_STATUS"
        record_result "Self-service unbond" "FAIL"
    fi
else
    record_result "Self-service unbond" "FAIL"
fi

# ========================================================================
# TEST 7: Cannot double-unbond
# ========================================================================
echo ""
echo "--- TEST 7: Cannot double-unbond ---"

TX_RES=$($BINARY tx service unbond-operator $SVC_AP \
    --from operator1 -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json)

if submit_and_wait "$TX_RES" "double unbond"; then
    echo "  Should have been rejected"
    record_result "Double unbond rejected" "FAIL"
else
    echo "  Correctly rejected"
    record_result "Double unbond rejected" "PASS"
fi

# ========================================================================
# TEST 8: List bridge bindings
# ========================================================================
echo ""
echo "--- TEST 8: List bridge bindings ---"

BRIDGES=$($BINARY query federation list-bridge-bindings --output json)
BRIDGE_COUNT=$(echo "$BRIDGES" | jq '.bridge_bindings | length' 2>/dev/null)

echo "  Bridge binding count: $BRIDGE_COUNT"
if [ "$BRIDGE_COUNT" -ge 2 ] 2>/dev/null; then
    record_result "List bridge bindings" "PASS"
else
    record_result "List bridge bindings" "FAIL"
fi

# ========================================================================
# TEST 9: IBC peer bridge registration rejected
# Bridges only valid for ActivityPub/AT Protocol peer types.
# ========================================================================
echo ""
echo "--- TEST 9: IBC peer bridge registration rejected ---"

TX_RES=$($BINARY tx federation register-bridge \
    spark.testnet ibc "" "$BOND_AMOUNT" \
    --from operator2 -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json)

if submit_and_wait "$TX_RES" "register ibc bridge"; then
    echo "  Should have been rejected"
    record_result "IBC bridge rejected" "FAIL"
else
    echo "  Correctly rejected"
    record_result "IBC bridge rejected" "PASS"
fi

# ========================================================================
# TEST 10: Duplicate bridge registration on (operator, peer) rejected
# operator2 already bound to mastodon.example in TEST 2.
# ========================================================================
echo ""
echo "--- TEST 10: Duplicate bridge registration rejected ---"

TX_RES=$($BINARY tx federation register-bridge \
    mastodon.example activitypub https://bridge2.example.com/ap "$BOND_AMOUNT" \
    --from operator2 -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json)

if submit_and_wait "$TX_RES" "dup bridge"; then
    echo "  Should have been rejected"
    record_result "Duplicate bridge rejected" "FAIL"
else
    echo "  Correctly rejected"
    record_result "Duplicate bridge rejected" "PASS"
fi

# ========================================================================
# TEST 11: Bridge for non-existent peer rejected
# ========================================================================
echo ""
echo "--- TEST 11: Bridge for non-existent peer rejected ---"

TX_RES=$($BINARY tx federation register-bridge \
    nonexistent.peer activitypub https://bridge.nonexistent.com "$BOND_AMOUNT" \
    --from operator1 -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json)

if submit_and_wait "$TX_RES" "bridge missing peer"; then
    echo "  Should have been rejected"
    record_result "Bridge for non-existent peer rejected" "FAIL"
else
    RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
    echo "  Correctly rejected: $(echo "$RAW" | head -c 120)"
    record_result "Bridge for non-existent peer rejected" "PASS"
fi

# ========================================================================
# TEST 12: Insufficient stake (below min_bond) rejected
# ========================================================================
echo ""
echo "--- TEST 12: Insufficient stake rejected ---"

LOW_STAKE=$((MIN_BOND_AMT - 1))

# Use linker1 as a fresh operator that has no existing service.Operator
# (operator1 / operator2 already have a record; topping up from a low
# stake would succeed as a no-op TopUpBond).
TX_RES=$($BINARY tx federation register-bridge \
    mastodon.example activitypub https://bridge.lowstake.example "${LOW_STAKE}${BOND_DENOM}" \
    --from linker1 -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json)

if submit_and_wait "$TX_RES" "low stake"; then
    echo "  Should have been rejected"
    record_result "Insufficient stake rejected" "FAIL"
else
    RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
    echo "  Correctly rejected: $(echo "$RAW" | head -c 120)"
    record_result "Insufficient stake rejected" "PASS"
fi

# ========================================================================
# TEST 13: Same address, second peer on same protocol shares one
# service.Operator (Decision 1a — one bond, multiple bindings)
# ========================================================================
echo ""
echo "--- TEST 13: Second peer on same protocol shares service.Operator ---"

# Need an additional ActivityPub peer to bind to. Re-register if absent.
LEMMY_STATUS=$($BINARY query federation get-peer lemmy.example --output json 2>&1 | jq -r '.peer.status // empty')
if [ -z "$LEMMY_STATUS" ]; then
    echo "  Registering lemmy.example for shared-bond test..."
    cat > "$PROPOSAL_DIR/register_lemmy.json" <<EOF
{
  "policy_address": "$COMMONS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgRegisterPeer",
      "authority": "$COMMONS_POLICY",
      "peer_id": "lemmy.example",
      "display_name": "Lemmy test peer",
      "type": "PEER_TYPE_ACTIVITYPUB"
    }
  ],
  "metadata": "Register lemmy.example for bridge tests"
}
EOF
    TX_RES=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/register_lemmy.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000${BOND_DENOM} --output json)
    if submit_and_wait "$TX_RES" "register lemmy"; then
        PROP_ID=$(get_commons_proposal_id "$TX_RESULT")
        [ -n "$PROP_ID" ] && vote_and_execute_ops "$PROP_ID"
    fi
fi

# Capture operator2's current bond — should be unchanged after the new
# binding because the (address, service_type) already exists.
PRE_BOND_OP2=$($BINARY query service operator $OPERATOR2_ADDR $SVC_AP --output json 2>&1 | jq -r '.operator.bond_amount // "0"')

# Register operator2 for the new peer with stake=0 (no top-up). Per
# the migration plan, when (operator, service_type) already has an
# Operator, MsgRegisterBridge just writes a new BridgeBinding and only
# calls TopUpBond if stake > 0.
TX_RES=$($BINARY tx federation register-bridge \
    lemmy.example activitypub https://bridge.lemmy.example "0" \
    --from operator2 -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json)

if submit_and_wait "$TX_RES" "register second peer same protocol"; then
    POST_BOND_OP2=$($BINARY query service operator $OPERATOR2_ADDR $SVC_AP --output json 2>&1 | jq -r '.operator.bond_amount // "0"')
    LEMMY_BINDING=$($BINARY query federation get-bridge-binding $OPERATOR2_ADDR lemmy.example --output json 2>&1 | jq -r '.bridge_binding.address // empty')
    if [ "$LEMMY_BINDING" == "$OPERATOR2_ADDR" ] && [ "$POST_BOND_OP2" == "$PRE_BOND_OP2" ]; then
        echo "  Second binding written; bond unchanged ($POST_BOND_OP2)"
        record_result "Shared service.Operator across peers" "PASS"
    else
        echo "  binding=$LEMMY_BINDING pre=$PRE_BOND_OP2 post=$POST_BOND_OP2"
        record_result "Shared service.Operator across peers" "FAIL"
    fi
else
    record_result "Shared service.Operator across peers" "FAIL"
fi

# ========================================================================
# TEST 14: Service-type queries return seeded configs
# ========================================================================
echo ""
echo "--- TEST 14: Federation-bridge service types seeded at genesis ---"

AP_CFG=$($BINARY query service service-type $SVC_AP --output json 2>&1 | jq -r '.config.service_type // empty')
AT_CFG=$($BINARY query service service-type $SVC_AT --output json 2>&1 | jq -r '.config.service_type // empty')

if [ "$AP_CFG" == "$SVC_AP" ] && [ "$AT_CFG" == "$SVC_AT" ]; then
    echo "  Both federation-bridge ServiceTypeConfigs present"
    record_result "Service types seeded" "PASS"
else
    echo "  AP=$AP_CFG AT=$AT_CFG"
    record_result "Service types seeded" "FAIL"
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
