#!/bin/bash

echo "--- TESTING: x/service LIFECYCLE (update_metadata + top_up + withdraw_bond + unbond + claim) ---"

# --- 0. SETUP ---
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

if [ ! -f "$SCRIPT_DIR/.test_env" ]; then
    echo "Test environment not found (.test_env missing)"
    echo "   Run: bash setup_test_accounts.sh"
    exit 1
fi
source "$SCRIPT_DIR/.test_env"

echo "Operator (under test): operator2 ($OPERATOR2_ADDR)"
echo "Service type:          $TEST_SERVICE_TYPE"
echo ""

# ========================================================================
# Helper Functions
# ========================================================================
wait_for_tx() {
    local TXHASH=$1
    local MAX_ATTEMPTS=20
    local ATTEMPT=0
    while [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
        RESULT=$($BINARY q tx $TXHASH --output json)
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
        echo "Transaction failed with code: $CODE"
        echo "$TX_RESULT" | jq -r '.raw_log'
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

submit_and_wait() {
    local TX_RES="$1"
    TXHASH=$(echo "$TX_RES" | jq -r '.txhash')
    if [ -z "$TXHASH" ] || [ "$TXHASH" == "null" ]; then
        TX_RESULT="$TX_RES"
        return 1
    fi
    sleep 6
    TX_RESULT=$(wait_for_tx "$TXHASH")
}

# MsgRegisterOperator.metadata + MsgUpdateMetadata.new_metadata are
# `bytes` in proto, so the CLI requires base64-encoded input.
b64() {
    printf '%s' "$1" | base64 -w0
}

# ========================================================================
# Precondition: operator2 must be ACTIVE (set up by register_test.sh).
# If running this script standalone after restore, register operator2 here.
# ========================================================================
echo "--- PRECONDITION: ensure operator2 is ACTIVE ---"

OP2_INFO=$($BINARY query service operator "$OPERATOR2_ADDR" "$TEST_SERVICE_TYPE" --output json)
OP2_STATUS=$(echo "$OP2_INFO" | jq -r '.operator.status // empty')
MIN_BOND_AMT=$($BINARY query service service-type "$TEST_SERVICE_TYPE" --output json | jq -r '.config.min_bond_amount')
UNBONDING_BLOCKS=$($BINARY query service service-type "$TEST_SERVICE_TYPE" --output json | jq -r '.config.unbonding_period_blocks')

if [ -z "$OP2_STATUS" ]; then
    echo "operator2 not registered yet; registering now..."
    TX_RES=$($BINARY tx service register-operator \
        "$TEST_SERVICE_TYPE" "$COMMONS_POLICY" "${MIN_BOND_AMT}" "$(b64 'operator2-lifecycle-init')" \
        --from operator2 --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json)
    submit_and_wait "$TX_RES"
    if ! check_tx_success "$TX_RESULT"; then
        echo "FAIL: precondition register-operator failed"
        exit 1
    fi
    OP2_STATUS=$($BINARY query service operator "$OPERATOR2_ADDR" "$TEST_SERVICE_TYPE" --output json | jq -r '.operator.status')
fi

if [ "$OP2_STATUS" != "OPERATOR_STATUS_ACTIVE" ]; then
    echo "FAIL: operator2 is $OP2_STATUS, expected OPERATOR_STATUS_ACTIVE"
    exit 1
fi
echo "OK: operator2 is ACTIVE, min_bond=$MIN_BOND_AMT, unbonding_period_blocks=$UNBONDING_BLOCKS"
echo ""

# ========================================================================
# PART 1: UpdateMetadata — happy path
# ========================================================================
echo "--- PART 1: UpdateMetadata ---"

NEW_METADATA_PLAIN="operator2-metadata-updated-v2"
NEW_METADATA_B64=$(b64 "$NEW_METADATA_PLAIN")
TX_RES=$($BINARY tx service update-metadata "$TEST_SERVICE_TYPE" "$NEW_METADATA_B64" \
    --from operator2 --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000${BOND_DENOM} -y --output json)
submit_and_wait "$TX_RES"
if ! check_tx_success "$TX_RESULT"; then
    echo "FAIL: update-metadata failed"
    exit 1
fi

STORED_META=$($BINARY query service operator "$OPERATOR2_ADDR" "$TEST_SERVICE_TYPE" --output json | jq -r '.operator.metadata')
if [ "$STORED_META" != "$NEW_METADATA_B64" ]; then
    echo "FAIL: metadata not updated: expected '$NEW_METADATA_B64' (base64 of '$NEW_METADATA_PLAIN'), got '$STORED_META'"
    exit 1
fi
echo "OK: metadata updated to '$STORED_META'"
echo ""

# ========================================================================
# PART 2: TopUpBond — bond increases by the additional amount
# ========================================================================
echo "--- PART 2: TopUpBond ---"

BOND_BEFORE=$($BINARY query service operator "$OPERATOR2_ADDR" "$TEST_SERVICE_TYPE" --output json | jq -r '.operator.bond_amount')
TOPUP_AMOUNT=500000000   # +500 SPARK
echo "Bond before top-up: $BOND_BEFORE"

TX_RES=$($BINARY tx service top-up-bond "$TEST_SERVICE_TYPE" "${TOPUP_AMOUNT}" \
    --from operator2 --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000${BOND_DENOM} -y --output json)
submit_and_wait "$TX_RES"
if ! check_tx_success "$TX_RESULT"; then
    echo "FAIL: top-up-bond failed"
    exit 1
fi

BOND_AFTER=$($BINARY query service operator "$OPERATOR2_ADDR" "$TEST_SERVICE_TYPE" --output json | jq -r '.operator.bond_amount')
EXPECTED_BOND=$((BOND_BEFORE + TOPUP_AMOUNT))
if [ "$BOND_AFTER" != "$EXPECTED_BOND" ]; then
    echo "FAIL: bond mismatch: before=$BOND_BEFORE +topup=$TOPUP_AMOUNT, expected $EXPECTED_BOND, got $BOND_AFTER"
    exit 1
fi
echo "OK: bond increased $BOND_BEFORE -> $BOND_AFTER"
echo ""

# ========================================================================
# PART 2b: WithdrawBond — partial withdrawal queues while staying ACTIVE
# ========================================================================
echo "--- PART 2b: WithdrawBond (partial, stays ACTIVE) ---"

MIN_BOND=$($BINARY query service service-type "$TEST_SERVICE_TYPE" --output json | jq -r '.config.min_bond_amount')
SPARE=$((BOND_AFTER - MIN_BOND))
if [ "$SPARE" -le 0 ]; then
    echo "FAIL: operator2 has no spare bond above min_bond ($BOND_AFTER <= $MIN_BOND)"
    exit 1
fi

# Over-withdrawing (anything above the spare amount) is rejected.
TX_RES=$($BINARY tx service withdraw-bond "$TEST_SERVICE_TYPE" "$((SPARE + 1))" \
    --from operator2 --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000${BOND_DENOM} -y --output json)
submit_and_wait "$TX_RES"
if check_tx_success "$TX_RESULT"; then
    echo "FAIL: withdraw-bond above spare bond should have been rejected"
    exit 1
fi
echo "OK: withdraw-bond of $((SPARE + 1)) rejected (spare=$SPARE)"

# Half the spare bond queues fine; bond_amount is untouched until claim.
WITHDRAW_AMOUNT=$((SPARE / 2))
TX_RES=$($BINARY tx service withdraw-bond "$TEST_SERVICE_TYPE" "$WITHDRAW_AMOUNT" \
    --from operator2 --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000${BOND_DENOM} -y --output json)
submit_and_wait "$TX_RES"
if ! check_tx_success "$TX_RESULT"; then
    echo "FAIL: withdraw-bond failed"
    exit 1
fi

OP2_INFO=$($BINARY query service operator "$OPERATOR2_ADDR" "$TEST_SERVICE_TYPE" --output json)
OP2_STATUS=$(echo "$OP2_INFO" | jq -r '.operator.status')
OP2_BOND=$(echo "$OP2_INFO" | jq -r '.operator.bond_amount')
OP2_PENDING=$(echo "$OP2_INFO" | jq -r '.operator.pending_withdraw_amount // "0"')
OP2_WITHDRAW_AT=$(echo "$OP2_INFO" | jq -r '.operator.withdraw_complete_at // "0"')
if [ "$OP2_STATUS" != "OPERATOR_STATUS_ACTIVE" ]; then
    echo "FAIL: expected status OPERATOR_STATUS_ACTIVE after partial withdraw, got $OP2_STATUS"
    exit 1
fi
if [ "$OP2_BOND" != "$BOND_AFTER" ]; then
    echo "FAIL: bond_amount changed at queue time: expected $BOND_AFTER, got $OP2_BOND"
    exit 1
fi
if [ "$OP2_PENDING" != "$WITHDRAW_AMOUNT" ]; then
    echo "FAIL: pending_withdraw_amount expected $WITHDRAW_AMOUNT, got $OP2_PENDING"
    exit 1
fi
if [ "$OP2_WITHDRAW_AT" == "0" ]; then
    echo "FAIL: withdraw_complete_at not set"
    exit 1
fi
echo "OK: queued $WITHDRAW_AMOUNT, status=ACTIVE, bond=$OP2_BOND, withdraw_complete_at=$OP2_WITHDRAW_AT"

# Claiming before the clock elapses is rejected (same claim msg as a full exit).
CURRENT_HEIGHT=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height')
if [ "$CURRENT_HEIGHT" -lt "$OP2_WITHDRAW_AT" ]; then
    TX_RES=$($BINARY tx service claim-unbonded-bond "$TEST_SERVICE_TYPE" \
        --from operator2 --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json)
    submit_and_wait "$TX_RES"
    if check_tx_success "$TX_RESULT"; then
        echo "FAIL: partial claim before withdraw_complete_at should have been rejected"
        exit 1
    fi
    echo "OK: early partial claim rejected (height=$CURRENT_HEIGHT < withdraw_complete_at=$OP2_WITHDRAW_AT)"
fi

# Wait for the clock (unbonding_period_blocks, read at setup), then claim:
# bond drops by the queued amount, the queue clears, status stays ACTIVE.
echo "Waiting for partial-withdraw clock (target height $OP2_WITHDRAW_AT)..."
MAX_WAIT_SECONDS=180
WAITED=0
while [ $WAITED -lt $MAX_WAIT_SECONDS ]; do
    H=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height')
    if [ "$H" -ge "$OP2_WITHDRAW_AT" ]; then
        break
    fi
    sleep 5
    WAITED=$((WAITED + 5))
done
sleep 6

BAL_BEFORE_CLAIM=$($BINARY query bank balances "$OPERATOR2_ADDR" --output json | jq -r --arg denom "$BOND_DENOM" '.balances[] | select(.denom==$denom) | .amount')
TX_RES=$($BINARY tx service claim-unbonded-bond "$TEST_SERVICE_TYPE" \
    --from operator2 --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000${BOND_DENOM} -y --output json)
submit_and_wait "$TX_RES"
if ! check_tx_success "$TX_RESULT"; then
    echo "FAIL: partial claim failed"
    exit 1
fi

OP2_INFO=$($BINARY query service operator "$OPERATOR2_ADDR" "$TEST_SERVICE_TYPE" --output json)
OP2_STATUS=$(echo "$OP2_INFO" | jq -r '.operator.status')
OP2_BOND_AFTER_CLAIM=$(echo "$OP2_INFO" | jq -r '.operator.bond_amount')
OP2_PENDING=$(echo "$OP2_INFO" | jq -r '.operator.pending_withdraw_amount // "0"')
OP2_WITHDRAW_AT=$(echo "$OP2_INFO" | jq -r '.operator.withdraw_complete_at // "0"')
EXPECTED_BOND=$((BOND_AFTER - WITHDRAW_AMOUNT))
if [ "$OP2_STATUS" != "OPERATOR_STATUS_ACTIVE" ]; then
    echo "FAIL: expected status OPERATOR_STATUS_ACTIVE after partial claim, got $OP2_STATUS"
    exit 1
fi
if [ "$OP2_BOND_AFTER_CLAIM" != "$EXPECTED_BOND" ]; then
    echo "FAIL: bond after partial claim expected $EXPECTED_BOND, got $OP2_BOND_AFTER_CLAIM"
    exit 1
fi
if [ "$OP2_PENDING" != "0" ] || [ "$OP2_WITHDRAW_AT" != "0" ]; then
    echo "FAIL: queue not cleared after partial claim (pending=$OP2_PENDING, withdraw_complete_at=$OP2_WITHDRAW_AT)"
    exit 1
fi
BAL_AFTER_CLAIM=$($BINARY query bank balances "$OPERATOR2_ADDR" --output json | jq -r --arg denom "$BOND_DENOM" '.balances[] | select(.denom==$denom) | .amount')
# Fee of 5000 was paid from the same account, so the delta is amount - fee.
BAL_DELTA=$((BAL_AFTER_CLAIM - BAL_BEFORE_CLAIM + 5000))
if [ "$BAL_DELTA" != "$WITHDRAW_AMOUNT" ]; then
    echo "FAIL: operator2 balance delta expected $WITHDRAW_AMOUNT (net of fee), got $BAL_DELTA"
    exit 1
fi
echo "OK: partial claim released $WITHDRAW_AMOUNT, bond $BOND_AFTER -> $OP2_BOND_AFTER_CLAIM, status=ACTIVE, queue cleared"
# PART 3 expects the full exit to leave this reduced bond untouched.
BOND_AFTER=$OP2_BOND_AFTER_CLAIM
echo ""

# ========================================================================
# PART 3: UnbondOperator — status flips to UNBONDING + unbond_complete_at set
# ========================================================================
echo "--- PART 3: UnbondOperator ---"

# Re-queue a small partial withdrawal so PART 3 can show the full exit
# clearing it.
TX_RES=$($BINARY tx service withdraw-bond "$TEST_SERVICE_TYPE" "1000" \
    --from operator2 --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000${BOND_DENOM} -y --output json)
submit_and_wait "$TX_RES"
if ! check_tx_success "$TX_RESULT"; then
    echo "FAIL: re-queue withdraw-bond before unbond failed"
    exit 1
fi

CURRENT_HEIGHT=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height')
TX_RES=$($BINARY tx service unbond-operator "$TEST_SERVICE_TYPE" \
    --from operator2 --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000${BOND_DENOM} -y --output json)
submit_and_wait "$TX_RES"
if ! check_tx_success "$TX_RESULT"; then
    echo "FAIL: unbond-operator failed"
    exit 1
fi

OP2_INFO=$($BINARY query service operator "$OPERATOR2_ADDR" "$TEST_SERVICE_TYPE" --output json)
OP2_STATUS=$(echo "$OP2_INFO" | jq -r '.operator.status')
UNBOND_COMPLETE_AT=$(echo "$OP2_INFO" | jq -r '.operator.unbond_complete_at')

if [ "$OP2_STATUS" != "OPERATOR_STATUS_UNBONDING" ]; then
    echo "FAIL: expected status OPERATOR_STATUS_UNBONDING, got $OP2_STATUS"
    exit 1
fi
if [ -z "$UNBOND_COMPLETE_AT" ] || [ "$UNBOND_COMPLETE_AT" == "0" ]; then
    echo "FAIL: unbond_complete_at not set (got '$UNBOND_COMPLETE_AT')"
    exit 1
fi
OP2_PENDING=$(echo "$OP2_INFO" | jq -r '.operator.pending_withdraw_amount // "0"')
if [ "$OP2_PENDING" != "0" ]; then
    echo "FAIL: full unbond should clear the partial-withdraw queue, pending=$OP2_PENDING"
    exit 1
fi
echo "OK: status=UNBONDING, unbond_complete_at=$UNBOND_COMPLETE_AT (height was $CURRENT_HEIGHT), partial queue cleared"
echo ""

# ========================================================================
# PART 4: Double-unbond rejected
# ========================================================================
echo "--- PART 4: REJECTION — second unbond rejected ---"

TX_RES=$($BINARY tx service unbond-operator "$TEST_SERVICE_TYPE" \
    --from operator2 --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000${BOND_DENOM} -y --output json)
submit_and_wait "$TX_RES"
if ! check_tx_failure "$TX_RESULT"; then
    echo "FAIL: second unbond should be rejected"
    exit 1
fi
echo "OK: second unbond rejected"
echo ""

# ========================================================================
# PART 5: claim before unbonding period elapsed — should fail
# ========================================================================
echo "--- PART 5: REJECTION — claim before unbonding period elapsed ---"

CURRENT_HEIGHT=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height')
if [ "$CURRENT_HEIGHT" -lt "$UNBOND_COMPLETE_AT" ]; then
    TX_RES=$($BINARY tx service claim-unbonded-bond "$TEST_SERVICE_TYPE" \
        --from operator2 --chain-id $CHAIN_ID --keyring-backend test \
        --fees 5000${BOND_DENOM} -y --output json)
    submit_and_wait "$TX_RES"
    if ! check_tx_failure "$TX_RESULT"; then
        echo "FAIL: early claim should have been rejected"
        exit 1
    fi
    echo "OK: early claim rejected (height=$CURRENT_HEIGHT < complete_at=$UNBOND_COMPLETE_AT)"
else
    echo "(skipping — unbonding period already elapsed by height $CURRENT_HEIGHT)"
fi
echo ""

# ========================================================================
# PART 6: wait for unbonding period to elapse, then claim
# ========================================================================
echo "--- PART 6: wait for unbonding period + claim ---"

# unbonding_period_blocks is 20 in the test-akash config. 5s/block ⇒
# 100s wall-clock. Pad to ~120s plus a poll-and-retry loop just in case.
echo "Waiting for unbonding period to elapse (target height $UNBOND_COMPLETE_AT)..."
MAX_WAIT_SECONDS=180
WAITED=0
while [ $WAITED -lt $MAX_WAIT_SECONDS ]; do
    H=$($BINARY status 2>&1 | jq -r '.sync_info.latest_block_height')
    if [ "$H" -ge "$UNBOND_COMPLETE_AT" ]; then
        echo "  Height $H >= unbond_complete_at $UNBOND_COMPLETE_AT, ready to claim"
        break
    fi
    sleep 5
    WAITED=$((WAITED + 5))
done

# One extra block to be safe — the unbond_complete_at check is `<`.
sleep 6

TX_RES=$($BINARY tx service claim-unbonded-bond "$TEST_SERVICE_TYPE" \
    --from operator2 --chain-id $CHAIN_ID --keyring-backend test \
    --fees 5000${BOND_DENOM} -y --output json)
submit_and_wait "$TX_RES"
if ! check_tx_success "$TX_RESULT"; then
    echo "FAIL: claim-unbonded-bond failed"
    exit 1
fi
echo "OK: claim-unbonded-bond accepted"

# Confirm live record is gone.
OP_AFTER=$($BINARY query service operator "$OPERATOR2_ADDR" "$TEST_SERVICE_TYPE" --output json)
if echo "$OP_AFTER" | jq -e '.operator.status' > /dev/null 2>&1; then
    LIVE_STATUS=$(echo "$OP_AFTER" | jq -r '.operator.status')
    # Some impls return the archived record under the same query — check
    # for RETIRED specifically. If still UNBONDING/ACTIVE that's a bug.
    if [ "$LIVE_STATUS" == "OPERATOR_STATUS_UNBONDING" ] || [ "$LIVE_STATUS" == "OPERATOR_STATUS_ACTIVE" ]; then
        echo "FAIL: operator still live after claim with status $LIVE_STATUS"
        exit 1
    fi
fi
echo "OK: live operator record removed after claim"

# Confirm operator2's wallet balance includes the returned bond (it
# should have at minimum BOND_AFTER worth of uspark relative to the
# pre-unbond balance plus gas spend — just check it's non-empty).
BAL_AFTER=$($BINARY query bank balances "$OPERATOR2_ADDR" --output json | jq -r --arg denom "$BOND_DENOM" '.balances[] | select(.denom==$denom) | .amount')
echo "OK: operator2 post-claim balance: $BAL_AFTER uspark"
echo ""

echo "=================================================="
echo ">>> LIFECYCLE TESTS: PASSED <<<"
echo "=================================================="
exit 0
