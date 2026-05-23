#!/bin/bash

# Recurring spend authority / recipient invariants. Exercises:
#   1. Wrong-recipient claim is rejected (ErrRecurringPullUnauthorized).
#   2. Wrong-recipient decline is rejected.
#   3. Recipient unilateral decline succeeds and prevents future claims.
#   4. A peer council's MsgCancelRecurringSpend against another council's
#      schedule is rejected (ErrRecurringSpendUnauthorized at execute).
#
# Post-M11 (RecurringSpend migration): the period-min is set by the
# session-side testparams build override (x/session/types/defaults_testparams.go);
# no per-test gov dance is needed. Schedule ids come from the
# `grant_created` event (M5 dropped the legacy `recurring_spend_scheduled`).
# After decline the grant is DELETED from session — we assert via the
# `grant_declined` event and the NotFound query response (M9).

set -u

echo "--- TESTING: RECURRING SPEND SECURITY INVARIANTS ---"

# --- 0. SETUP ---------------------------------------------------------------
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)

COMMITTEE_NAME="Commons Operations Committee"
PEER_NAME="Ecosystem Operations Committee"

OWN_POLICY=$($BINARY query commons get-group "$COMMITTEE_NAME" --output json | jq -r '.group.policy_address')
PEER_POLICY=$($BINARY query commons get-group "$PEER_NAME" --output json | jq -r '.group.policy_address')
PROPOSAL_FEE=$($BINARY query commons params --output json | jq -r '.params.proposal_fee')

if [ -z "$OWN_POLICY" ] || [ "$OWN_POLICY" == "null" ] || \
   [ -z "$PEER_POLICY" ] || [ "$PEER_POLICY" == "null" ]; then
    echo "[FAIL] Could not resolve both committee policies."
    exit 1
fi
echo "Owning committee:  $OWN_POLICY ($COMMITTEE_NAME)"
echo "Peer committee:    $PEER_POLICY ($PEER_NAME)"

# Confirm testparams build (period floor lowered to 5s); otherwise the
# claim subtest can't complete in a reasonable wall-clock budget.
SESSION_MIN_PERIOD=$($BINARY query session params --output json | jq -r '.params.min_recurring_period_seconds')
if [ -z "$SESSION_MIN_PERIOD" ] || [ "$SESSION_MIN_PERIOD" -gt 60 ]; then
    echo "[FAIL] session min_recurring_period_seconds=$SESSION_MIN_PERIOD — testparams override missing."
    exit 1
fi

PERIOD=5
$BINARY tx bank send "$ALICE_ADDR" "$OWN_POLICY" 50000000${BOND_DENOM} --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} > /dev/null
sleep 3

# --- 1. SCHEDULE TWO SPENDS (one for wrong-recipient subtests, one for decline) ---
schedule_for_recipient() {
    local recipient=$1
    local note=$2
    local now; now=$(date +%s)
    # Submit + vote + execute takes ~15s end-to-end. Push start far
    # enough that block_time at execute is still behind start_time
    # (otherwise the schedule's "start_time in the past" guard fires
    # and the proposal aborts).
    local start=$((now + 30))
    local end=$((start + 120))

    cat > "$PROPOSAL_DIR/sched_security.json" <<EOF
{
  "policy_address": "$OWN_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgScheduleRecurringSpend",
      "authority": "$OWN_POLICY",
      "recipient": "$recipient",
      "amount_per_period": [{"denom": "${BOND_DENOM}","amount":"50000"}],
      "period_seconds": "$PERIOD",
      "start_time": "$start",
      "end_time": "$end",
      "note": "$note"
    }
  ],
  "metadata": "$note"
}
EOF
    local submit
    submit=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/sched_security.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees "$PROPOSAL_FEE" --output json)
    local hash; hash=$(echo "$submit" | jq -r '.txhash')
    sleep 3
    local prop_id
    prop_id=$($BINARY query tx "$hash" --output json | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
    $BINARY tx commons vote-proposal "$prop_id" yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} > /dev/null; sleep 3
    $BINARY tx commons vote-proposal "$prop_id" yes --from bob   -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} > /dev/null; sleep 3
    local exec_res
    exec_res=$($BINARY tx commons execute-proposal "$prop_id" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --fees 5000000${BOND_DENOM} --output json)
    sleep 3
    local exec_hash; exec_hash=$(echo "$exec_res" | jq -r '.txhash')
    # Post-migration the schedule id is reported via the session
    # `grant_created` event (with source=module_bypass) rather than the
    # legacy `recurring_spend_scheduled` event.
    local sched_id
    sched_id=$($BINARY query tx "$exec_hash" --output json | jq -r '
        .events[] |
        select(.type=="grant_created") |
        select(.attributes[]? | select(.key=="source" and .value=="module_bypass")) |
        .attributes[] | select(.key=="id") | .value
    ' | tr -d '"' | head -n1)
    if [ -z "$sched_id" ] || [ "$sched_id" == "null" ]; then
        echo "[FAIL] Could not capture scheduled id for '$note'." >&2
        return 1
    fi
    echo "$sched_id"
}

CLAIM_ID=$(schedule_for_recipient "$CAROL_ADDR" "security-claim-target")
DECLINE_ID=$(schedule_for_recipient "$CAROL_ADDR" "security-decline-target")
echo "Scheduled IDs: claim-target=$CLAIM_ID, decline-target=$DECLINE_ID"

# --- 2. WRONG-RECIPIENT CLAIM REJECTED -------------------------------------
echo ""
echo "STEP 2: bob attempts to claim Carol's schedule..."
sleep "$PERIOD"
BAD_CLAIM=$($BINARY tx commons claim-recurring-spend "$CLAIM_ID" --from bob -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json 2>&1)
BAD_CODE=$(echo "$BAD_CLAIM" | jq -r '.code')
BAD_HASH=$(echo "$BAD_CLAIM" | jq -r '.txhash')
if [ "$BAD_CODE" == "0" ]; then
    sleep 3
    BAD_CODE=$($BINARY query tx "$BAD_HASH" --output json | jq -r '.code')
fi
if [ "$BAD_CODE" == "0" ]; then
    echo "[FAIL] bob's wrong-recipient claim succeeded!"
    exit 1
fi
echo "[ OK ] Wrong-recipient claim rejected (code=$BAD_CODE)."

# --- 3. WRONG-RECIPIENT DECLINE REJECTED ----------------------------------
echo ""
echo "STEP 3: bob attempts to decline Carol's schedule..."
BAD_DECL=$($BINARY tx commons decline-recurring-spend "$DECLINE_ID" --from bob -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json 2>&1)
BAD_DECL_HASH=$(echo "$BAD_DECL" | jq -r '.txhash')
BAD_DECL_CODE=$(echo "$BAD_DECL" | jq -r '.code')
if [ "$BAD_DECL_CODE" == "0" ]; then
    sleep 3
    BAD_DECL_CODE=$($BINARY query tx "$BAD_DECL_HASH" --output json | jq -r '.code')
fi
if [ "$BAD_DECL_CODE" == "0" ]; then
    echo "[FAIL] bob's wrong-recipient decline succeeded!"
    exit 1
fi
echo "[ OK ] Wrong-recipient decline rejected (code=$BAD_DECL_CODE)."

# --- 4. CAROL UNILATERALLY DECLINES THE DECLINE-TARGET --------------------
echo ""
echo "STEP 4: Carol unilaterally declines decline-target=$DECLINE_ID..."
DEC_RES=$($BINARY tx commons decline-recurring-spend "$DECLINE_ID" --from carol -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json)
sleep 3
DEC_CODE=$(echo "$DEC_RES" | jq -r '.code')
DEC_HASH=$(echo "$DEC_RES" | jq -r '.txhash')
if [ "$DEC_CODE" != "0" ]; then
    sleep 3
    DEC_CODE=$($BINARY query tx "$DEC_HASH" --output json | jq -r '.code')
fi
if [ "$DEC_CODE" != "0" ]; then
    echo "[FAIL] Recipient decline failed (code=$DEC_CODE). Raw: $DEC_RES"
    exit 1
fi

# Post-migration the grant is DELETED from session on decline (M9
# semantic break). Audit is the `grant_declined` event from the
# decline tx; the get-recurring-spend query returns NotFound.
DECL_EVENT=$($BINARY query tx "$DEC_HASH" --output json | jq -r '
    .events[] |
    select(.type=="grant_declined") |
    .attributes[] | select(.key=="id") | .value
' | tr -d '"' | head -n1)
if [ "$DECL_EVENT" != "$DECLINE_ID" ]; then
    echo "[FAIL] Expected grant_declined event for id=$DECLINE_ID, got '$DECL_EVENT'."
    exit 1
fi
echo "[ OK ] grant_declined event fired for id=$DECLINE_ID."

GET_AFTER=$($BINARY query commons get-recurring-spend "$DECLINE_ID" --output json 2>&1 || true)
if ! echo "$GET_AFTER" | grep -qi "not found"; then
    echo "[FAIL] Expected NotFound from get-recurring-spend post-decline; got: $GET_AFTER"
    exit 1
fi
echo "[ OK ] Post-decline query returns NotFound (M9 deliberate semantic break)."

# Post-decline claim should fail.
sleep "$PERIOD"
POST_CLAIM=$($BINARY tx commons claim-recurring-spend "$DECLINE_ID" --from carol -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json 2>&1)
POST_CODE=$(echo "$POST_CLAIM" | jq -r '.code')
POST_HASH=$(echo "$POST_CLAIM" | jq -r '.txhash')
if [ "$POST_CODE" == "0" ]; then
    sleep 3
    POST_CODE=$($BINARY query tx "$POST_HASH" --output json | jq -r '.code')
fi
if [ "$POST_CODE" == "0" ]; then
    echo "[FAIL] Post-decline claim still succeeded."
    exit 1
fi
echo "[ OK ] Post-decline claim rejected (code=$POST_CODE)."

# --- 5. PEER-COUNCIL CANCEL ATTEMPT REJECTED ------------------------------
echo ""
echo "STEP 5: Peer council ($PEER_NAME) attempts to cancel claim-target=$CLAIM_ID..."

cat > "$PROPOSAL_DIR/peer_cancel.json" <<EOF
{
  "policy_address": "$PEER_POLICY",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgCancelRecurringSpend",
      "authority": "$PEER_POLICY",
      "id": "$CLAIM_ID"
    }
  ],
  "metadata": "Hostile cross-council cancel attempt"
}
EOF

PEER_SUBMIT=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/peer_cancel.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees "$PROPOSAL_FEE" --output json)
PEER_HASH=$(echo "$PEER_SUBMIT" | jq -r '.txhash')
sleep 3

PEER_TX=$($BINARY query tx "$PEER_HASH" --output json 2>/dev/null)
PEER_SUBMIT_CODE=$(echo "$PEER_TX" | jq -r '.code')
PEER_PROP_ID=$(echo "$PEER_TX" | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

if [ "$PEER_SUBMIT_CODE" != "0" ] || [ -z "$PEER_PROP_ID" ] || [ "$PEER_PROP_ID" == "null" ]; then
    echo "[ OK ] Cross-council cancel rejected at submit (code=$PEER_SUBMIT_CODE)."
else
    $BINARY tx commons vote-proposal "$PEER_PROP_ID" yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} > /dev/null; sleep 3
    $BINARY tx commons vote-proposal "$PEER_PROP_ID" yes --from bob   -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} > /dev/null; sleep 3
    $BINARY tx commons execute-proposal "$PEER_PROP_ID" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --fees 5000000${BOND_DENOM} > /dev/null
    sleep 3
    PEER_STATUS=$($BINARY query commons get-proposal "$PEER_PROP_ID" --output json | jq -r '.proposal.status')
    if [ "$PEER_STATUS" == "PROPOSAL_STATUS_EXECUTED" ]; then
        echo "[FAIL] Peer council cancel proposal executed (should have failed at execute)."
        exit 1
    fi
    echo "[ OK ] Cross-council cancel rejected at execute (proposal status=$PEER_STATUS)."
fi

# Make sure claim-target schedule is still queryable and ACTIVE.
TARGET_STATE=$($BINARY query commons get-recurring-spend "$CLAIM_ID" --output json 2>&1)
TARGET_STATUS=$(echo "$TARGET_STATE" | jq -r '.recurring_spend.status // empty')
if [ "$TARGET_STATUS" != "RECURRING_SPEND_STATUS_ACTIVE" ]; then
    echo "[FAIL] Original schedule no longer ACTIVE after hostile cancel attempt (got '$TARGET_STATUS', raw: $TARGET_STATE)."
    exit 1
fi
echo "[ OK ] Target schedule still ACTIVE."

echo ""
echo "[ OK ] Recurring spend security invariants PASSED."
