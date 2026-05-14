#!/bin/bash

# Recurring spend authority / recipient invariants. Exercises:
#   1. Wrong-recipient claim is rejected (ErrRecurringSpendUnauthorized).
#   2. Wrong-recipient decline is rejected.
#   3. Recipient unilateral decline succeeds and prevents future claims.
#   4. A peer council's MsgCancelRecurringSpend against another council's
#      schedule is rejected (ErrRecurringSpendUnauthorized at execute).
#
# This test requires the period-min to be small so claims can be exercised
# in a single test run; if `recurring_spend_test.sh` already lowered the
# param it's reused, otherwise we lower it here.

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
GOV_ADDR=$($BINARY query auth module-account gov --output json | jq -r '.account.base_account.address // .account.value.address')

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

# Helper: pluck the submit_proposal.proposal_id from a tx hash.
get_gov_proposal_id() {
    local tx_hash=$1; local retries=0; local prop_id=""
    while [ $retries -lt 10 ]; do
        sleep 1
        local tx_res
        tx_res=$($BINARY query tx "$tx_hash" --output json 2>/dev/null)
        if [ $? -eq 0 ]; then
            prop_id=$(echo "$tx_res" | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
            if [ -n "$prop_id" ] && [ "$prop_id" != "null" ]; then echo "$prop_id"; return 0; fi
        fi
        retries=$((retries + 1))
    done
    return 1
}

# Ensure min_recurring_period_seconds is small enough to claim.
CURRENT_MIN=$($BINARY query commons params --output json | jq -r '.params.min_recurring_period_seconds')
if [ "$CURRENT_MIN" -gt "10" ]; then
    echo "STEP 0: Lowering min_recurring_period_seconds via gov (current=$CURRENT_MIN)..."
    cat > "$PROPOSAL_DIR/gov_lower_min_period_sec.json" <<EOF
{
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgUpdateParams",
      "authority": "$GOV_ADDR",
      "params": {
        "proposal_fee": "$PROPOSAL_FEE",
        "min_recurring_period_seconds": "5",
        "max_recurring_duration_seconds": "300",
        "max_active_recurring_spends_per_group": 50
      }
    }
  ],
  "deposit": "100000000uspark",
  "title": "Lower recurring spend min period (security test)",
  "summary": "5s cadence floor for E2E security test."
}
EOF
    GOV_SUBMIT=$($BINARY tx gov submit-proposal "$PROPOSAL_DIR/gov_lower_min_period_sec.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --output json)
    GOV_TX_HASH=$(echo "$GOV_SUBMIT" | jq -r '.txhash')
    GOV_PROP_ID=$(get_gov_proposal_id "$GOV_TX_HASH")
    [ -z "$GOV_PROP_ID" ] && { echo "[FAIL] Failed to find gov proposal ID."; exit 1; }
    $BINARY tx gov vote "$GOV_PROP_ID" yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test > /dev/null
    echo "Waiting 70s for gov voting period..."
    sleep 70
fi

PERIOD=5
$BINARY tx bank send "$ALICE_ADDR" "$OWN_POLICY" 50000000uspark --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark > /dev/null
sleep 3

# --- 1. SCHEDULE TWO SPENDS (one for the wrong-recipient subtests, one for the decline subtest) ---
schedule_for_recipient() {
    local recipient=$1
    local note=$2
    local now; now=$(date +%s)
    # Submit + vote + execute takes ~15s end-to-end (3-4 sleeps of 3s plus
    # per-tx wait). A small start-time buffer (e.g. now+8) leaves the inner
    # MsgScheduleRecurringSpend running after start_time has already passed,
    # which trips the keeper's "start_time is in the past" guard and the
    # proposal aborts with no recurring_spend_scheduled event — the
    # downstream sched_id extraction then sees empty. Push the buffer far
    # enough out that block_time at execute is still behind start_time.
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
      "amount_per_period": [{"denom":"uspark","amount":"50000"}],
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
    $BINARY tx commons vote-proposal "$prop_id" yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark > /dev/null; sleep 3
    $BINARY tx commons vote-proposal "$prop_id" yes --from bob   -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark > /dev/null; sleep 3
    local exec_res
    exec_res=$($BINARY tx commons execute-proposal "$prop_id" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --fees 5000000uspark --output json)
    sleep 3
    local exec_hash; exec_hash=$(echo "$exec_res" | jq -r '.txhash')
    local sched_id
    sched_id=$($BINARY query tx "$exec_hash" --output json | jq -r '.events[] | select(.type=="recurring_spend_scheduled") | .attributes[] | select(.key=="id") | .value' | tr -d '"')
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
BAD_CLAIM=$($BINARY tx commons claim-recurring-spend "$CLAIM_ID" --from bob -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>&1)
BAD_CODE=$(echo "$BAD_CLAIM" | jq -r '.code')
BAD_HASH=$(echo "$BAD_CLAIM" | jq -r '.txhash')
if [ "$BAD_CODE" == "0" ]; then
    sleep 3
    BAD_CODE=$($BINARY query tx "$BAD_HASH" --output json | jq -r '.code')
    BAD_LOG=$($BINARY query tx "$BAD_HASH" --output json | jq -r '.raw_log')
fi
if [ "$BAD_CODE" == "0" ]; then
    echo "[FAIL] bob's wrong-recipient claim succeeded!"
    exit 1
fi
echo "[ OK ] Wrong-recipient claim rejected (code=$BAD_CODE)."

# --- 3. WRONG-RECIPIENT DECLINE REJECTED ----------------------------------
echo ""
echo "STEP 3: bob attempts to decline Carol's schedule..."
BAD_DECL=$($BINARY tx commons decline-recurring-spend "$DECLINE_ID" --from bob -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>&1)
BAD_DECL_HASH=$(echo "$BAD_DECL" | jq -r '.txhash')
BAD_DECL_CODE=$(echo "$BAD_DECL" | jq -r '.code')
if [ "$BAD_DECL_CODE" == "0" ]; then
    sleep 3
    BAD_DECL_CODE=$($BINARY query tx "$BAD_DECL_HASH" --output json | jq -r '.code')
    BAD_DECL_LOG=$($BINARY query tx "$BAD_DECL_HASH" --output json | jq -r '.raw_log')
fi
if [ "$BAD_DECL_CODE" == "0" ]; then
    echo "[FAIL] bob's wrong-recipient decline succeeded!"
    exit 1
fi
echo "[ OK ] Wrong-recipient decline rejected (code=$BAD_DECL_CODE)."

# --- 4. CAROL UNILATERALLY DECLINES THE DECLINE-TARGET --------------------
echo ""
echo "STEP 4: Carol unilaterally declines decline-target=$DECLINE_ID..."
DEC_RES=$($BINARY tx commons decline-recurring-spend "$DECLINE_ID" --from carol -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json)
sleep 3
DEC_CODE=$(echo "$DEC_RES" | jq -r '.code')
if [ "$DEC_CODE" != "0" ]; then
    sleep 3
    DEC_HASH=$(echo "$DEC_RES" | jq -r '.txhash')
    DEC_CODE=$($BINARY query tx "$DEC_HASH" --output json | jq -r '.code')
fi
if [ "$DEC_CODE" != "0" ]; then
    echo "[FAIL] Recipient decline failed (code=$DEC_CODE). Raw: $DEC_RES"
    exit 1
fi

DEC_STATUS=$($BINARY query commons get-recurring-spend "$DECLINE_ID" --output json | jq -r '.recurring_spend.status')
if [ "$DEC_STATUS" != "RECURRING_SPEND_STATUS_RECIPIENT_DECLINED" ]; then
    echo "[FAIL] Expected status RECIPIENT_DECLINED, got $DEC_STATUS"
    exit 1
fi
echo "[ OK ] Recipient decline succeeded; status=$DEC_STATUS"

# Post-decline claim should fail.
sleep "$PERIOD"
POST_CLAIM=$($BINARY tx commons claim-recurring-spend "$DECLINE_ID" --from carol -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>&1)
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
    # Submit-time rejection (likely: peer committee's StandardPermissions don't include MsgCancelRecurringSpend, or
    # the policy refuses the authority mismatch). Either way the test goal is met.
    echo "[ OK ] Cross-council cancel rejected at submit (code=$PEER_SUBMIT_CODE)."
else
    $BINARY tx commons vote-proposal "$PEER_PROP_ID" yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark > /dev/null; sleep 3
    $BINARY tx commons vote-proposal "$PEER_PROP_ID" yes --from bob   -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark > /dev/null; sleep 3
    $BINARY tx commons execute-proposal "$PEER_PROP_ID" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --fees 5000000uspark > /dev/null
    sleep 3
    PEER_STATUS=$($BINARY query commons get-proposal "$PEER_PROP_ID" --output json | jq -r '.proposal.status')
    if [ "$PEER_STATUS" == "PROPOSAL_STATUS_EXECUTED" ]; then
        echo "[FAIL] Peer council cancel proposal executed (should have failed at execute)."
        exit 1
    fi
    echo "[ OK ] Cross-council cancel rejected at execute (proposal status=$PEER_STATUS)."
fi

# Make sure claim-target schedule is still ACTIVE.
TARGET_STATUS=$($BINARY query commons get-recurring-spend "$CLAIM_ID" --output json | jq -r '.recurring_spend.status')
if [ "$TARGET_STATUS" != "RECURRING_SPEND_STATUS_ACTIVE" ]; then
    echo "[FAIL] Original schedule no longer ACTIVE after hostile cancel attempt (got $TARGET_STATUS)."
    exit 1
fi
echo "[ OK ] Target schedule still ACTIVE."

echo ""
echo "[ OK ] Recurring spend security invariants PASSED."
