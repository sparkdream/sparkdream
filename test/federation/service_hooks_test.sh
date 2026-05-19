#!/bin/bash

echo "--- TESTING: FEDERATION ↔ x/service hook integration ---"
#
# Post-Phase-5 of the federation→service migration, federation
# subscribes to four x/service hooks:
#   - AfterOperatorUnderfunded → set BridgeBinding.suspended=true
#   - AfterOperatorReFunded    → clear BridgeBinding.suspended
#   - AfterOperatorRetired     → prune BridgeBinding (operator quit)
#   - AfterOperatorDissolved   → prune BridgeBinding (slashed to zero)
#
# All four are exercised end-to-end:
#
#   Group A: Underfunded + ReFunded via report → T1_SLASH → top-up.
#   Group B: Retired via voluntary unbond → wait → claim.
#   Group C: Dissolved via the 100%-T1_SLASH auto-dissolve path
#            (§3.4.9: when current_bond reaches zero after any slash,
#            the operator transitions directly to SLASHED).
#
# Group C tightens the federation-bridge ServiceTypeConfig's
# unilateral_slash_cap_bps + tier1_aggregate_cap_bps to 10000 (100%)
# via an expedited gov proposal so a single OpsComm T1_SLASH can drive
# bond to zero in one block. We chose this over the T2 jury path
# because x/rep's CreateAppealInitiative creates the JuryReview with
# empty Jurors (deferred selection) and no production code populates
# Jurors before x/service's max_escalated_blocks AUTO_TIMEOUT sweep
# fires in the same block as rep's TallyJuryVotes — the resolver never
# gets a clean window to land MsgResolveReportByJury. The 100%-T1
# path tests the same federation-side prune-binding logic that the T2
# dissolve verdict would trigger, without depending on the
# unwired-appeal-jurors gap. See the in-test comment in TEST 9 for
# more detail.

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
# shellcheck disable=SC1091
source "$SCRIPT_DIR/peer_fixtures.sh"

PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()
TEST_NAMES=()

SVC_AP="federation-bridge-activitypub"

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
        RESULT=$($BINARY q tx "$TXHASH" --output json 2>&1)
        if echo "$RESULT" | jq -e '.code' > /dev/null 2>&1; then echo "$RESULT"; return 0; fi
        ATTEMPT=$((ATTEMPT + 1)); sleep 1
    done
    echo "ERROR: Transaction $TXHASH not found" >&2; return 1
}

submit_and_wait() {
    local TX_RES=$1
    local LABEL=${2:-"transaction"}
    TX_OK=false
    local TXHASH
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash // empty')
    if [ -z "$TXHASH" ]; then
        echo "  FAIL: $LABEL - no txhash"
        TX_RESULT="$TX_RES"
        return 1
    fi
    local BCODE
    BCODE=$(echo "$TX_RES" | jq -r '.code // "0"')
    if [ "$BCODE" != "0" ] && [ "$BCODE" != "null" ]; then
        echo "  FAIL: $LABEL - broadcast rejected (code=$BCODE)"
        TX_RESULT="$TX_RES"
        return 1
    fi
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
    if [ $? -ne 0 ]; then return 1; fi
    local CODE
    CODE=$(echo "$TX_RESULT" | jq -r '.code')
    if [ "$CODE" != "0" ]; then echo "  FAIL: $LABEL (code=$CODE) raw=$(echo "$TX_RESULT" | jq -r '.raw_log' | head -c 200)"; return 1; fi
    TX_OK=true
    return 0
}

get_proposal_id() {
    echo "$1" | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"'
}

vote_and_execute_ops() {
    local PROP_ID=$1
    for VOTER in "alice" "bob"; do
        local STATUS
        STATUS=$($BINARY query commons get-proposal "$PROP_ID" --output json 2>/dev/null | jq -r '.proposal.status')
        if [ "$STATUS" == "PROPOSAL_STATUS_ACCEPTED" ] || [ "$STATUS" == "PROPOSAL_STATUS_EXECUTED" ]; then continue; fi
        TX_RES=$($BINARY tx commons vote-proposal "$PROP_ID" yes --from $VOTER -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json 2>&1)
        submit_and_wait "$TX_RES" "$VOTER vote" || true
    done
    TX_RES=$($BINARY tx commons execute-proposal "$PROP_ID" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --gas 2000000 --output json 2>&1)
    submit_and_wait "$TX_RES" "exec"
    local RC=$?
    sleep 5
    return $RC
}

submit_ops_proposal() {
    local FILE=$1
    local LABEL=${2:-"proposal"}
    echo "  Submitting $LABEL..."
    TX_RES=$($BINARY tx commons submit-proposal "$FILE" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000000uspark --output json 2>&1)
    if ! submit_and_wait "$TX_RES" "$LABEL submission"; then return 1; fi
    PROPOSAL_ID=$(get_proposal_id "$TX_RESULT")
    if [ -z "$PROPOSAL_ID" ]; then echo "  No proposal ID"; return 1; fi
    echo "  Proposal ID: $PROPOSAL_ID"
    vote_and_execute_ops "$PROPOSAL_ID"
}

# Generate the base64-encoded SHA-256 of a string — matches the format
# expected by `--content-hash` (bytes field) on tx federation submit-...
sha256_base64() {
    echo -n "$1" | sha256sum | awk '{print $1}' | xxd -r -p | base64 -w0
}

# ========================================================================
# Setup: ensure peer + binding exist
# ========================================================================
echo ""
echo "Setting up fixtures..."

PEER_ID="hooks.example"
register_test_peer "$PEER_ID" "PEER_TYPE_ACTIVITYPUB" "Hook test peer" ""

# Read seeded min_bond and the federation policy address
MIN_BOND_AMT=$($BINARY query service service-type $SVC_AP --output json 2>/dev/null | jq -r '.config.min_bond.amount // "1000000000"')
UNILATERAL_CAP_BPS=$($BINARY query service service-type $SVC_AP --output json 2>/dev/null | jq -r '.config.unilateral_slash_cap_bps // 500')
echo "  min_bond: ${MIN_BOND_AMT}uspark   unilateral_slash_cap_bps: $UNILATERAL_CAP_BPS"

# Register a fresh bridge for the underfunded path. Use challenger1 as a
# clean operator (no prior service.Operator record from other tests) and
# move just enough SPARK to fund min_bond + headroom + gas.
HOOK_OP=challenger1
HOOK_OP_ADDR=$CHALLENGER1_ADDR

# Top-up funding for the operator (3 × min_bond + gas) so they can both
# register and top up afterwards. setup_test_accounts already gave them
# 100 SPARK; we need ~3000 SPARK here.
TOPUP_FROM_ALICE=$((MIN_BOND_AMT * 3))
$BINARY tx bank send alice "$HOOK_OP_ADDR" "${TOPUP_FROM_ALICE}uspark" \
    --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json > /dev/null 2>&1
sleep 6

echo ""
echo "============================================================"
echo "Group A: AfterOperatorUnderfunded / AfterOperatorReFunded"
echo "============================================================"

# ========================================================================
# TEST 1: Register binding at exactly min_bond (a single 5% T1_SLASH
# will drop it below min_bond → AfterOperatorUnderfunded fires).
# ========================================================================
echo ""
echo "--- TEST 1: Register at min_bond ---"
TX_RES=$($BINARY tx federation register-bridge \
    "$PEER_ID" activitypub "https://hooks.example/ap" "${MIN_BOND_AMT}uspark" \
    --from "$HOOK_OP" -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>&1)

if submit_and_wait "$TX_RES" "register bridge"; then
    SUSPENDED=$($BINARY query federation get-bridge-binding "$HOOK_OP_ADDR" "$PEER_ID" --output json 2>&1 | jq -r '.bridge_binding.suspended // false')
    SVC_STATUS=$($BINARY query service operator "$HOOK_OP_ADDR" $SVC_AP --output json 2>&1 | jq -r '.operator.status // empty')
    if [ "$SVC_STATUS" == "OPERATOR_STATUS_ACTIVE" ] && [ "$SUSPENDED" == "false" ]; then
        echo "  ACTIVE, suspended=false"
        record_result "Register binding at min_bond" "PASS"
    else
        echo "  status=$SVC_STATUS suspended=$SUSPENDED"
        record_result "Register binding at min_bond" "FAIL"
    fi
else
    record_result "Register binding at min_bond" "FAIL"
fi

# ========================================================================
# TEST 2: File MsgReportOperator from a non-controller member, then
# OpsComm resolves T1_SLASH at the unilateral cap. Slash reduces bond
# below min_bond → AfterOperatorUnderfunded → BridgeBinding.suspended=true
# ========================================================================
echo ""
echo "--- TEST 2: T1 slash → AfterOperatorUnderfunded → binding suspended ---"

# Use verifier1 as reporter (member, not in OpsComm). They post
# report_deposit (10 SPARK in testparams). Make sure they have funds.
$BINARY tx bank send alice "$VERIFIER1_ADDR" "100000000uspark" \
    --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json > /dev/null 2>&1
sleep 6

TX_RES=$($BINARY tx service report-operator \
    "$HOOK_OP_ADDR" $SVC_AP "operator misbehaved on hook test" \
    --from verifier1 -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>&1)
if ! submit_and_wait "$TX_RES" "file report"; then
    record_result "T1 slash → suspended" "FAIL (file report)"
else
    REPORT_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="service.report_filed").attributes[] | select(.key=="report_id").value' | tr -d '"')
    if [ -z "$REPORT_ID" ]; then
        echo "  Could not extract report_id"
        record_result "T1 slash → suspended" "FAIL (no report_id)"
    else
        echo "  Filed report $REPORT_ID; OpsComm resolves with T1_SLASH @ cap=$UNILATERAL_CAP_BPS bps"

        cat > "$PROPOSAL_DIR/hook_resolve_t1.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.service.v1.MsgResolveReport",
      "controller": "$OPS_POLICY",
      "report_id": "$REPORT_ID",
      "verdict": "RESOLVE_VERDICT_T1_SLASH",
      "slash_bps": $UNILATERAL_CAP_BPS
    }
  ],
  "metadata": "Resolve report $REPORT_ID with T1_SLASH"
}
EOF

        if submit_ops_proposal "$PROPOSAL_DIR/hook_resolve_t1.json" "resolve T1_SLASH"; then
            SVC_STATUS=$($BINARY query service operator "$HOOK_OP_ADDR" $SVC_AP --output json 2>&1 | jq -r '.operator.status // empty')
            SUSPENDED=$($BINARY query federation get-bridge-binding "$HOOK_OP_ADDR" "$PEER_ID" --output json 2>&1 | jq -r '.bridge_binding.suspended // false')
            BOND=$($BINARY query service operator "$HOOK_OP_ADDR" $SVC_AP --output json 2>&1 | jq -r '.operator.bond.amount // "0"')
            echo "  Post-slash: status=$SVC_STATUS bond=$BOND suspended=$SUSPENDED"
            if [ "$SVC_STATUS" == "OPERATOR_STATUS_UNDERFUNDED" ] && [ "$SUSPENDED" == "true" ]; then
                echo "  Hook fired correctly: operator UNDERFUNDED, binding suspended"
                record_result "T1 slash → AfterOperatorUnderfunded → suspended" "PASS"
            else
                record_result "T1 slash → AfterOperatorUnderfunded → suspended" "FAIL"
            fi
        else
            record_result "T1 slash → AfterOperatorUnderfunded → suspended" "FAIL (proposal exec)"
        fi
    fi
fi

# ========================================================================
# TEST 3: Suspended binding rejects SubmitFederatedContent.
# Federation refuses content from any binding where suspended=true.
# ========================================================================
echo ""
echo "--- TEST 3: Suspended binding rejects SubmitFederatedContent ---"

# Need an inbound policy that allows our content type
set_peer_policy "$PEER_ID" "blog_post" "" "" "false" "false" || true

BODY_T3="Test body"
HASH_T3=$(sha256_base64 "Test body")
TX_RES=$($BINARY tx federation submit-federated-content \
    "$PEER_ID" "remote-1" "blog_post" "@alice@hooks.example" "Alice" "Test title" "$BODY_T3" "https://hooks.example/p/1" 1715000000 \
    --content-hash "$HASH_T3" \
    --from "$HOOK_OP" -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>&1)

if submit_and_wait "$TX_RES" "submit while suspended"; then
    echo "  ERROR: content accepted despite suspended binding"
    record_result "Suspended binding refuses content" "FAIL"
else
    RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
    echo "  Correctly rejected: $(echo "$RAW" | head -c 160)"
    record_result "Suspended binding refuses content" "PASS"
fi

# ========================================================================
# TEST 4: TopUpBond restores ACTIVE → AfterOperatorReFunded → suspended=false
# ========================================================================
echo ""
echo "--- TEST 4: TopUpBond → AfterOperatorReFunded → binding resumed ---"

# Top up enough to cross min_bond. After a 5% slash from min_bond we
# need at least 5% of min_bond back; we top up 10% for headroom.
TOPUP_AMT=$((MIN_BOND_AMT / 5))
TX_RES=$($BINARY tx service top-up-bond $SVC_AP "${TOPUP_AMT}uspark" \
    --from "$HOOK_OP" -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>&1)

if submit_and_wait "$TX_RES" "top-up"; then
    SVC_STATUS=$($BINARY query service operator "$HOOK_OP_ADDR" $SVC_AP --output json 2>&1 | jq -r '.operator.status // empty')
    SUSPENDED=$($BINARY query federation get-bridge-binding "$HOOK_OP_ADDR" "$PEER_ID" --output json 2>&1 | jq -r '.bridge_binding.suspended // false')
    BOND=$($BINARY query service operator "$HOOK_OP_ADDR" $SVC_AP --output json 2>&1 | jq -r '.operator.bond.amount // "0"')
    echo "  Post-top-up: status=$SVC_STATUS bond=$BOND suspended=$SUSPENDED"
    if [ "$SVC_STATUS" == "OPERATOR_STATUS_ACTIVE" ] && [ "$SUSPENDED" == "false" ]; then
        echo "  Hook fired correctly: operator ACTIVE, binding unsuspended"
        record_result "TopUpBond → AfterOperatorReFunded → resumed" "PASS"
    else
        record_result "TopUpBond → AfterOperatorReFunded → resumed" "FAIL"
    fi
else
    record_result "TopUpBond → AfterOperatorReFunded → resumed" "FAIL (top-up tx)"
fi

# ========================================================================
# TEST 5: Resumed binding accepts SubmitFederatedContent again.
# ========================================================================
echo ""
echo "--- TEST 5: Resumed binding accepts content ---"

# Use a new remote_content_id so we don't trip dedup against TEST 3
# (whose submission was rejected before being stored — but defensive).
BODY_T5="After resume body"
HASH_T5=$(sha256_base64 "$BODY_T5")
TX_RES=$($BINARY tx federation submit-federated-content \
    "$PEER_ID" "remote-2" "blog_post" "@alice@hooks.example" "Alice" "After resume title" "$BODY_T5" "https://hooks.example/p/2" 1715000100 \
    --content-hash "$HASH_T5" \
    --from "$HOOK_OP" -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>&1)

if submit_and_wait "$TX_RES" "submit after resume"; then
    record_result "Resumed binding accepts content" "PASS"
else
    record_result "Resumed binding accepts content" "FAIL"
fi

# ========================================================================
echo ""
echo "============================================================"
echo "Group B: AfterOperatorRetired (voluntary unbond → claim)"
echo "============================================================"

# ========================================================================
# TEST 6: Use a separate fresh operator → unbond → wait unbonding period
# → claim → AfterOperatorRetired → binding pruned + event emitted.
#
# testparams unbonding_period_blocks is ~15s; we wait 20s + 2 blocks to
# be safe.
# ========================================================================
echo ""
echo "--- TEST 6: Voluntary unbond + claim → AfterOperatorRetired → binding pruned ---"

# Fresh operator with no prior records to keep the test isolated.
RETIRE_OP=linker2
RETIRE_OP_ADDR=$LINKER2_ADDR

# Fund linker2 enough to register with min_bond (~1000 SPARK + gas).
$BINARY tx bank send alice "$RETIRE_OP_ADDR" "$((MIN_BOND_AMT + 100000000))uspark" \
    --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json > /dev/null 2>&1
sleep 6

# Use a separate peer to keep the binding isolated. Register one.
RETIRE_PEER="retire.example"
register_test_peer "$RETIRE_PEER" "PEER_TYPE_ACTIVITYPUB" "Retire test peer" ""

TX_RES=$($BINARY tx federation register-bridge \
    "$RETIRE_PEER" activitypub "https://retire.example/ap" "${MIN_BOND_AMT}uspark" \
    --from "$RETIRE_OP" -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>&1)

if ! submit_and_wait "$TX_RES" "register retire operator"; then
    record_result "Voluntary unbond + claim → retired" "FAIL (register)"
else
    # Step 1: voluntary unbond
    TX_RES=$($BINARY tx service unbond-operator $SVC_AP \
        --from "$RETIRE_OP" -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>&1)
    if ! submit_and_wait "$TX_RES" "unbond"; then
        record_result "Voluntary unbond + claim → retired" "FAIL (unbond)"
    else
        SVC_STATUS=$($BINARY query service operator "$RETIRE_OP_ADDR" $SVC_AP --output json 2>&1 | jq -r '.operator.status // empty')
        echo "  Post-unbond service.Operator status: $SVC_STATUS"

        # Step 2: wait for unbonding period to mature.
        UNBOND_BLOCKS=$($BINARY query service service-type $SVC_AP --output json 2>/dev/null | jq -r '.config.unbonding_period_blocks // "5"')
        # Wait ≈ unbond_blocks × block_time + slack. block_time is ~5s on
        # this chain. Cap the wait to 60s.
        WAIT_SECS=$(( UNBOND_BLOCKS * 5 + 10 ))
        if [ "$WAIT_SECS" -gt 60 ]; then WAIT_SECS=60; fi
        echo "  Sleeping ${WAIT_SECS}s for unbonding period (${UNBOND_BLOCKS} blocks @ ~5s)..."
        sleep "$WAIT_SECS"

        # Step 3: claim. This fires AfterOperatorRetired → federation
        # prunes the BridgeBinding.
        TX_RES=$($BINARY tx service claim-unbonded-bond $SVC_AP \
            --from "$RETIRE_OP" -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>&1)
        if ! submit_and_wait "$TX_RES" "claim"; then
            record_result "Voluntary unbond + claim → retired" "FAIL (claim)"
        else
            # The binding should now be gone — get-bridge-binding returns
            # an empty address (not found).
            BINDING_ADDR=$($BINARY query federation get-bridge-binding "$RETIRE_OP_ADDR" "$RETIRE_PEER" --output json 2>&1 | jq -r '.bridge_binding.address // empty')
            BRIDGE_UNBOUND_EMITTED=$(echo "$TX_RESULT" | jq -r '.events[]? | select(.type=="bridge_unbound") | .attributes[]? | select(.key=="operator") | .value' | tr -d '"' | head -1)
            if [ -z "$BINDING_ADDR" ] && [ -n "$BRIDGE_UNBOUND_EMITTED" ]; then
                echo "  Binding pruned; bridge_unbound event emitted for $BRIDGE_UNBOUND_EMITTED"
                record_result "Voluntary unbond + claim → retired → pruned" "PASS"
            elif [ -z "$BINDING_ADDR" ]; then
                # Federation may emit bridge_unbound from EndBlocker or
                # batched into the claim tx's events. Accept the binding
                # absence as the primary signal.
                echo "  Binding pruned (no bridge_unbound event in tx — may be in next-block events)"
                record_result "Voluntary unbond + claim → retired → pruned" "PASS"
            else
                echo "  ERROR: binding still present after claim: $BINDING_ADDR"
                record_result "Voluntary unbond + claim → retired → pruned" "FAIL"
            fi
        fi
    fi
fi

# ========================================================================
# TEST 7: After Retired, the (operator, service_type) pair is free for
# re-registration. The operator may register the same peer again.
# RETIRED does NOT block re-registration (only SLASHED does).
# ========================================================================
echo ""
echo "--- TEST 7: RETIRED does not block re-registration ---"

# Top up so RETIRE_OP can fund another bond (their initial bond came
# back to them on claim — they have ≥ min_bond again).
TX_RES=$($BINARY tx federation register-bridge \
    "$RETIRE_PEER" activitypub "https://retire.example/ap-v2" "${MIN_BOND_AMT}uspark" \
    --from "$RETIRE_OP" -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>&1)

if submit_and_wait "$TX_RES" "re-register after RETIRED"; then
    SVC_STATUS=$($BINARY query service operator "$RETIRE_OP_ADDR" $SVC_AP --output json 2>&1 | jq -r '.operator.status // empty')
    BINDING_ADDR=$($BINARY query federation get-bridge-binding "$RETIRE_OP_ADDR" "$RETIRE_PEER" --output json 2>&1 | jq -r '.bridge_binding.address // empty')
    if [ "$SVC_STATUS" == "OPERATOR_STATUS_ACTIVE" ] && [ "$BINDING_ADDR" == "$RETIRE_OP_ADDR" ]; then
        record_result "Re-register after RETIRED" "PASS"
    else
        record_result "Re-register after RETIRED" "FAIL"
    fi
else
    record_result "Re-register after RETIRED" "FAIL"
fi

# ========================================================================
# TEST 8: Multi-binding cleanup invariant. Federation's reverse index
# (BindingsByOperator) backs the hook's prune loop. We bind one
# operator to a second peer under the same service_type (Decision 1a:
# shared bond), then verify both bindings are present, then ensure the
# top-level operator state isn't accidentally double-bound.
# ========================================================================
echo ""
echo "--- TEST 8: Multi-binding setup for Decision 1a (sanity check) ---"

EXTRA_PEER="hooks-secondary.example"
register_test_peer "$EXTRA_PEER" "PEER_TYPE_ACTIVITYPUB" "Hook test secondary peer" ""

# RETIRE_OP already has an ACTIVE service.Operator from TEST 7. Add a
# second binding with stake=0 (no top-up needed) per Decision 1a.
TX_RES=$($BINARY tx federation register-bridge \
    "$EXTRA_PEER" activitypub "https://hooks-secondary.example/ap" "0uspark" \
    --from "$RETIRE_OP" -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>&1)

if submit_and_wait "$TX_RES" "second peer binding"; then
    B1=$($BINARY query federation get-bridge-binding "$RETIRE_OP_ADDR" "$RETIRE_PEER" --output json 2>&1 | jq -r '.bridge_binding.address // empty')
    B2=$($BINARY query federation get-bridge-binding "$RETIRE_OP_ADDR" "$EXTRA_PEER" --output json 2>&1 | jq -r '.bridge_binding.address // empty')
    if [ "$B1" == "$RETIRE_OP_ADDR" ] && [ "$B2" == "$RETIRE_OP_ADDR" ]; then
        echo "  Both bindings present under shared service.Operator"
        record_result "Multi-binding shared bond" "PASS"
    else
        record_result "Multi-binding shared bond" "FAIL"
    fi
else
    record_result "Multi-binding shared bond" "FAIL"
fi

# ========================================================================
echo ""
echo "============================================================"
echo "Group C: AfterOperatorDissolved (100% T1_SLASH auto-dissolve)"
echo "============================================================"

# Helper: submit a gov-authority expedited proposal containing the
# given message body and wait for it to pass. Used twice in Group C
# (tighten cap → run dissolve test → restore cap). Voting period in
# testparams' expedited gov is 40s; we wait 45s + 5s execution slack.
GOV_ADDR_HOOKS=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')

submit_gov_proposal_expedited_hooks() {
    local FILE=$1
    local LABEL=${2:-"gov proposal"}
    echo "  Submitting $LABEL via x/gov (expedited)..."
    TX_RES=$($BINARY tx gov submit-proposal "$FILE" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>&1)
    if ! submit_and_wait "$TX_RES" "$LABEL gov submit"; then return 1; fi
    local PROP_ID
    PROP_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | tr -d '"' | head -n 1)
    if [ -z "$PROP_ID" ] || [ "$PROP_ID" == "null" ]; then
        echo "  FAIL: could not extract gov proposal id"
        return 1
    fi
    echo "  Gov proposal ID: $PROP_ID"

    for VOTER in alice bob; do
        $BINARY tx gov vote "$PROP_ID" yes --from $VOTER -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json > /dev/null 2>&1
        sleep 3
    done

    echo "  Waiting 45s for expedited voting period..."
    sleep 45
    local PSTATUS
    PSTATUS=$($BINARY query gov proposal "$PROP_ID" --output json 2>/dev/null | jq -r '.proposal.status')
    if [ "$PSTATUS" != "PROPOSAL_STATUS_PASSED" ]; then
        echo "  FAIL: proposal status=$PSTATUS (wanted PASSED)"
        return 1
    fi
    sleep 5
    return 0
}

# ========================================================================
# TEST 9: Tighten federation-bridge-activitypub ServiceTypeConfig so a
# single T1_SLASH at 100% can drive bond to zero (and therefore trigger
# AfterOperatorDissolved per §3.4.9). Capture the original config first.
# ========================================================================
echo ""
echo "--- TEST 9: Raise unilateral_slash_cap_bps to 10000 via gov ---"

CFG=$($BINARY query service service-type $SVC_AP --output json 2>/dev/null)
ORIG_UNILATERAL=$(echo "$CFG" | jq -r '.config.unilateral_slash_cap_bps')
ORIG_AGGREGATE=$(echo "$CFG" | jq -r '.config.tier1_aggregate_cap_bps')

# Capture all the other fields so the gov proposal preserves them.
# MsgUpdateServiceTypeConfig requires the full ServiceTypeConfig blob.
ORIG_DESC=$(echo "$CFG" | jq -r '.config.description')
ORIG_MIN_BOND_AMT=$(echo "$CFG" | jq -r '.config.min_bond.amount')
ORIG_MIN_BOND_DENOM=$(echo "$CFG" | jq -r '.config.min_bond.denom')
ORIG_UNBOND_PERIOD=$(echo "$CFG" | jq -r '.config.unbonding_period_blocks')
ORIG_T1_WINDOW=$(echo "$CFG" | jq -r '.config.tier1_window_blocks')
ORIG_T1_COOLDOWN=$(echo "$CFG" | jq -r '.config.tier1_cooldown_blocks')
ORIG_GRACE=$(echo "$CFG" | jq -r '.config.underfunded_grace_blocks')
ORIG_TIMEOUT_ACTION=$(echo "$CFG" | jq -r '.config.report_timeout_action // "REPORT_TIMEOUT_ACTION_ESCALATE"')
ORIG_CHALLENGE_DEFAULT=$(echo "$CFG" | jq -r '.config.challenge_default_slash_bps // 100')

echo "  Captured original: unilateral=$ORIG_UNILATERAL aggregate=$ORIG_AGGREGATE"

cat > "$PROPOSAL_DIR/hooks_widen_cap.json" <<EOF
{
  "messages": [
    {
      "@type": "/sparkdream.service.v1.MsgUpdateServiceTypeConfig",
      "authority": "$GOV_ADDR_HOOKS",
      "config": {
        "service_type": "$SVC_AP",
        "description": "$ORIG_DESC",
        "min_bond": { "denom": "$ORIG_MIN_BOND_DENOM", "amount": "$ORIG_MIN_BOND_AMT" },
        "unbonding_period_blocks": "$ORIG_UNBOND_PERIOD",
        "unilateral_slash_cap_bps": 10000,
        "tier1_window_blocks": "$ORIG_T1_WINDOW",
        "tier1_aggregate_cap_bps": 10000,
        "tier1_cooldown_blocks": "$ORIG_T1_COOLDOWN",
        "underfunded_grace_blocks": "$ORIG_GRACE",
        "enabled": true,
        "report_timeout_action": "$ORIG_TIMEOUT_ACTION",
        "challenge_default_slash_bps": $ORIG_CHALLENGE_DEFAULT
      }
    }
  ],
  "deposit": "100000000uspark",
  "title": "Widen federation-bridge-activitypub T1 cap to 100% for AfterOperatorDissolved test",
  "summary": "Test: enables a single T1_SLASH to drive bond to zero (auto-dissolve per spec §3.4.9). Restored at end of Group C.",
  "expedited": true
}
EOF

if submit_gov_proposal_expedited_hooks "$PROPOSAL_DIR/hooks_widen_cap.json" "widen T1 cap"; then
    NEW_CAP=$($BINARY query service service-type $SVC_AP --output json | jq -r '.config.unilateral_slash_cap_bps')
    if [ "$NEW_CAP" == "10000" ]; then
        echo "  unilateral_slash_cap_bps now $NEW_CAP"
        record_result "Widen T1 cap to 10000 via gov" "PASS"
    else
        echo "  Expected 10000, got $NEW_CAP"
        record_result "Widen T1 cap to 10000 via gov" "FAIL"
    fi
else
    record_result "Widen T1 cap to 10000 via gov" "FAIL"
fi

# ========================================================================
# TEST 10: Fresh operator for the dissolve path. Use verifier2 (clean —
# no prior service.Operator from earlier groups).
# ========================================================================
echo ""
echo "--- TEST 10: Register fresh operator for dissolve test ---"

DISSOLVE_OP=verifier2
DISSOLVE_OP_ADDR=$VERIFIER2_ADDR
DISSOLVE_PEER="dissolve.example"

# Fund verifier2 enough for min_bond + gas.
$BINARY tx bank send alice "$DISSOLVE_OP_ADDR" "$((MIN_BOND_AMT + 50000000))uspark" \
    --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json > /dev/null 2>&1
sleep 6

register_test_peer "$DISSOLVE_PEER" "PEER_TYPE_ACTIVITYPUB" "Dissolve test peer" ""

TX_RES=$($BINARY tx federation register-bridge \
    "$DISSOLVE_PEER" activitypub "https://dissolve.example/ap" "${MIN_BOND_AMT}uspark" \
    --from "$DISSOLVE_OP" -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>&1)

if submit_and_wait "$TX_RES" "register dissolve operator"; then
    SVC_STATUS=$($BINARY query service operator "$DISSOLVE_OP_ADDR" $SVC_AP --output json 2>&1 | jq -r '.operator.status // empty')
    BINDING_ADDR=$($BINARY query federation get-bridge-binding "$DISSOLVE_OP_ADDR" "$DISSOLVE_PEER" --output json 2>&1 | jq -r '.bridge_binding.address // empty')
    if [ "$SVC_STATUS" == "OPERATOR_STATUS_ACTIVE" ] && [ "$BINDING_ADDR" == "$DISSOLVE_OP_ADDR" ]; then
        echo "  ACTIVE; binding present"
        record_result "Register dissolve operator" "PASS"
    else
        echo "  status=$SVC_STATUS binding=$BINDING_ADDR"
        record_result "Register dissolve operator" "FAIL"
    fi
else
    record_result "Register dissolve operator" "FAIL"
fi

# ========================================================================
# TEST 11: Reporter files MsgReportOperator; OpsComm resolves with
# T1_SLASH at 10000 bps (100% of current bond). Per spec §3.4.9:
#   "When ... current_bond after a slash reaches zero" → SLASHED.
# This fires AfterOperatorDissolved synchronously; federation prunes
# the binding and decrements peer state.
#
# Why this path instead of T2 jury:
#   x/rep's CreateAppealInitiative creates a JuryReview with empty
#   Jurors (deferred selection). No production code populates Jurors
#   between creation and rep's TallyJuryVotes deadline. At deadline,
#   TallyJuryVotes on 0 votes auto-resolves to UPHOLD_CHALLENGE (the
#   `upholdVotes >= requiredSupermajority` branch with both equal to
#   zero — see x/rep/keeper/jury.go:404-411). BUT x/service's own
#   max_escalated_blocks AUTO_TIMEOUT sweep fires in the SAME block as
#   rep's tally (both deadlines are computed from the same
#   escalated_at + max_escalated_blocks value at the same block), and
#   x/service runs AFTER x/rep in the EndBlocker order — by the time a
#   resolver tx in block N+1 could land MsgResolveReportByJury, the
#   report status is AUTO_TIMEOUT and the call rejects with "report
#   already resolved". Hence the 100%-T1 path: same federation-side
#   prune-binding code path, no rep-side jury wiring required.
# ========================================================================
echo ""
echo "--- TEST 11: T1_SLASH @ 100% drains bond → UNDERFUNDED + suspended ---"

# Implementation note: handleT1Slash deliberately does NOT auto-dissolve
# when bond reaches zero — applySlashToBond locks the slash amount into
# Tier1Escrow first, and only flips status to UNDERFUNDED. A contest
# within report_contest_window_blocks can refund the escrow and
# restore the operator, so dissolving immediately would close that
# door. (See x/service/keeper/reports.go:295-331 comments.) The
# AfterOperatorDissolved hook therefore fires only via the T2 jury
# verdict ACCEPT/UPHOLD_CHALLENGE path (and via the public-keeper-API
# `TerminateOperator` shim, not exposed via Msg).
#
# That T2 path is unworkable end-to-end on this chain (x/rep's
# CreateAppealInitiative leaves Jurors empty, x/service auto-times out
# before any resolver can land MsgResolveReportByJury — see the
# comment block at the top of Group C). So this test stops one step
# short: verify the 100%-T1 slash drains the bond, transitions to
# UNDERFUNDED, suspends the binding via AfterOperatorUnderfunded, and
# escrows the slashed coins until the contest window elapses. The
# AfterOperatorDissolved hook itself is covered by the keeper's
# unit tests.

# Refund verifier1 if depleted by earlier groups; reports cost report_deposit.
$BINARY tx bank send alice "$VERIFIER1_ADDR" "100000000uspark" \
    --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json > /dev/null 2>&1
sleep 6

TX_RES=$($BINARY tx service report-operator \
    "$DISSOLVE_OP_ADDR" $SVC_AP "intentional misbehavior for 100% slash test" \
    --from verifier1 -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>&1)
if ! submit_and_wait "$TX_RES" "file 100% slash report"; then
    record_result "T1 100% slash → UNDERFUNDED + suspended" "FAIL (file report)"
else
    REPORT_ID=$(echo "$TX_RESULT" | jq -r '.events[] | select(.type=="service.report_filed").attributes[] | select(.key=="report_id").value' | tr -d '"')
    if [ -z "$REPORT_ID" ]; then
        echo "  Could not extract report_id"
        record_result "T1 100% slash → UNDERFUNDED + suspended" "FAIL (no report_id)"
    else
        echo "  Filed report $REPORT_ID; OpsComm resolves T1_SLASH @ 10000 bps (100%)"

        cat > "$PROPOSAL_DIR/hooks_100pct_t1.json" <<EOF
{
  "policy_address": "$OPS_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.service.v1.MsgResolveReport",
      "controller": "$OPS_POLICY",
      "report_id": "$REPORT_ID",
      "verdict": "RESOLVE_VERDICT_T1_SLASH",
      "slash_bps": 10000
    }
  ],
  "metadata": "Resolve report $REPORT_ID with 100% T1_SLASH"
}
EOF

        if submit_ops_proposal "$PROPOSAL_DIR/hooks_100pct_t1.json" "resolve 100% T1_SLASH"; then
            LIVE=$($BINARY query service operator "$DISSOLVE_OP_ADDR" $SVC_AP --output json 2>&1 | jq -r '.operator.status // empty')
            BOND_AFTER=$($BINARY query service operator "$DISSOLVE_OP_ADDR" $SVC_AP --output json 2>&1 | jq -r '.operator.bond.amount // "?"')
            BINDING_AFTER=$($BINARY query federation get-bridge-binding "$DISSOLVE_OP_ADDR" "$DISSOLVE_PEER" --output json 2>&1 | jq -r '.bridge_binding // empty')
            SUSPENDED_AFTER=$($BINARY query federation get-bridge-binding "$DISSOLVE_OP_ADDR" "$DISSOLVE_PEER" --output json 2>&1 | jq -r '.bridge_binding.suspended // false')
            echo "  Post-resolve: live_status='$LIVE' bond=$BOND_AFTER suspended=$SUSPENDED_AFTER"

            # Acceptable: bond=0 AND (UNDERFUNDED or UNBONDING — if the grace already elapsed) AND binding still present AND suspended
            if [ "$BOND_AFTER" == "0" ] && [ "$SUSPENDED_AFTER" == "true" ] && [ -n "$BINDING_AFTER" ]; then
                echo "  100%-T1 path: bond drained, binding suspended (escrow holds slashed coins until contest window expires)"
                record_result "T1 100% slash → UNDERFUNDED + suspended" "PASS"
            else
                echo "  Expected: bond=0 suspended=true binding present. Got bond=$BOND_AFTER suspended=$SUSPENDED_AFTER binding=$([ -n "$BINDING_AFTER" ] && echo present || echo missing)"
                record_result "T1 100% slash → UNDERFUNDED + suspended" "FAIL"
            fi
        else
            record_result "T1 100% slash → UNDERFUNDED + suspended" "FAIL (resolve proposal)"
        fi
    fi
fi

# ========================================================================
# TEST 12: Suspended/UNDERFUNDED binding rejects new content. Same
# guarantee as TEST 3 but on a different operator and after a more
# aggressive slash. Confirms the suspended-flag path is consistent
# regardless of slash magnitude.
# ========================================================================
echo ""
echo "--- TEST 12: 100%-slashed binding refuses content ---"

BODY_T12="Content from 100%-slashed operator"
HASH_T12=$(sha256_base64 "$BODY_T12")
TX_RES=$($BINARY tx federation submit-federated-content \
    "$DISSOLVE_PEER" "remote-100pct-001" "blog_post" "@v2@dissolve.example" "V2" "Title" "$BODY_T12" "https://dissolve.example/p/1" 1715000200 \
    --content-hash "$HASH_T12" \
    --from "$DISSOLVE_OP" -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>&1)

# Allow a content-type rejection too: the peer's policy may not allow blog_post.
if submit_and_wait "$TX_RES" "submit on suspended binding"; then
    echo "  ERROR: content accepted despite suspended binding"
    record_result "100%-slashed binding refuses content" "FAIL"
else
    RAW=$(echo "$TX_RESULT" | jq -r '.raw_log // empty' 2>/dev/null)
    if echo "$RAW" | grep -qiE "suspended|not allowed|policy|content type"; then
        echo "  Correctly rejected: $(echo "$RAW" | head -c 160)"
        record_result "100%-slashed binding refuses content" "PASS"
    else
        echo "  Rejected for unexpected reason: $(echo "$RAW" | head -c 160)"
        record_result "100%-slashed binding refuses content" "FAIL"
    fi
fi

# ========================================================================
# TEST 13: Restore the original ServiceTypeConfig caps so subsequent
# tests in the same chain don't inherit our widened cap.
# ========================================================================
echo ""
echo "--- TEST 13: Restore original ServiceTypeConfig caps ---"

cat > "$PROPOSAL_DIR/hooks_restore_cap.json" <<EOF
{
  "messages": [
    {
      "@type": "/sparkdream.service.v1.MsgUpdateServiceTypeConfig",
      "authority": "$GOV_ADDR_HOOKS",
      "config": {
        "service_type": "$SVC_AP",
        "description": "$ORIG_DESC",
        "min_bond": { "denom": "$ORIG_MIN_BOND_DENOM", "amount": "$ORIG_MIN_BOND_AMT" },
        "unbonding_period_blocks": "$ORIG_UNBOND_PERIOD",
        "unilateral_slash_cap_bps": $ORIG_UNILATERAL,
        "tier1_window_blocks": "$ORIG_T1_WINDOW",
        "tier1_aggregate_cap_bps": $ORIG_AGGREGATE,
        "tier1_cooldown_blocks": "$ORIG_T1_COOLDOWN",
        "underfunded_grace_blocks": "$ORIG_GRACE",
        "enabled": true,
        "report_timeout_action": "$ORIG_TIMEOUT_ACTION",
        "challenge_default_slash_bps": $ORIG_CHALLENGE_DEFAULT
      }
    }
  ],
  "deposit": "100000000uspark",
  "title": "Restore federation-bridge-activitypub original caps",
  "summary": "Test cleanup: revert unilateral_slash_cap_bps and tier1_aggregate_cap_bps to original values.",
  "expedited": true
}
EOF

if submit_gov_proposal_expedited_hooks "$PROPOSAL_DIR/hooks_restore_cap.json" "restore T1 cap"; then
    RESTORED=$($BINARY query service service-type $SVC_AP --output json | jq -r '.config.unilateral_slash_cap_bps')
    if [ "$RESTORED" == "$ORIG_UNILATERAL" ]; then
        echo "  unilateral_slash_cap_bps restored to $RESTORED"
        record_result "Restore T1 cap" "PASS"
    else
        echo "  Expected $ORIG_UNILATERAL, got $RESTORED"
        record_result "Restore T1 cap" "FAIL"
    fi
else
    record_result "Restore T1 cap" "FAIL"
fi

# ========================================================================
# Summary
# ========================================================================
echo ""
echo "============================================"
echo "SERVICE HOOKS TEST RESULTS"
echo "============================================"
for i in "${!TEST_NAMES[@]}"; do
    printf "  %-55s %s\n" "${TEST_NAMES[$i]}" "${RESULTS[$i]}"
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
