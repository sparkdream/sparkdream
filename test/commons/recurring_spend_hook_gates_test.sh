#!/bin/bash

# SessionClaimHook end-to-end coverage.
#
# Verifies that the M4 SessionClaimHook is wired up correctly in
# production — `MsgSpendFromCommons` and `MsgClaimRecurringSpend` share
# the same per-epoch budget bucket, and a recurring claim that would
# breach the council's `max_spend_per_epoch` is vetoed by the hook
# PreCheck. This is the cross-path parity invariant migration §M4 / §8
# was designed to preserve.
#
# Flow:
#   1. Fund the Commons Operations Committee with > max_spend_per_epoch
#      uspark so the cap binds rather than the balance.
#   2. Drain the committee's per-epoch budget to ~99.99% via a
#      MsgSpendFromCommons council proposal.
#   3. Schedule a recurring spend whose first claim would overflow the
#      remaining headroom.
#   4. Wait for the claim window and attempt the claim — the SessionClaimHook
#      PreCheck must reject with a rate-limit error before the bank send.
#   5. Verify claims_made is still 0 (no spurious advance from a vetoed
#      claim) and the grant is still ACTIVE (hook veto does not auto-pause).
#   6. Cleanup: cancel the schedule.
#
# Note: this is an e2e analog of the unit-test coverage in
# x/commons/keeper/session_claim_hook_test.go (TestSessionClaimHook_PreCheck_RateLimitCumulative,
# TestSessionClaimHook_DoubleDebitRegression). The unit tests are
# authoritative for the hook semantics; this script confirms the
# wiring lands in production state and that the cross-path
# EpochSpending share works through baseapp's real msg dispatcher.

set -u

echo "--- TESTING: SESSIONCLAIMHOOK RATE-LIMIT GATE ---"

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
mkdir -p "$PROPOSAL_DIR"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"

ALICE_ADDR=$($BINARY keys show alice -a --keyring-backend test)
BOB_ADDR=$($BINARY keys show bob -a --keyring-backend test)
CAROL_ADDR=$($BINARY keys show carol -a --keyring-backend test)

GROUP_NAME="Commons Operations Committee"
GROUP_INFO=$($BINARY query commons get-group "$GROUP_NAME" --output json)
POLICY_ADDR=$(echo "$GROUP_INFO" | jq -r '.group.policy_address')
if [ -z "$POLICY_ADDR" ] || [ "$POLICY_ADDR" == "null" ]; then
    echo "[FAIL] '$GROUP_NAME' not found — run bootstrap first."
    exit 1
fi
PROPOSAL_FEE=$($BINARY query commons params --output json | jq -r '.params.proposal_fee')
MAX_SPEND_PER_EPOCH=$(echo "$GROUP_INFO" | jq -r '.group.max_spend_per_epoch')
if [ -z "$MAX_SPEND_PER_EPOCH" ] || [ "$MAX_SPEND_PER_EPOCH" == "null" ] || [ "$MAX_SPEND_PER_EPOCH" == "0" ]; then
    echo "[FAIL] Council has no max_spend_per_epoch configured (got '$MAX_SPEND_PER_EPOCH') — rate-limit gate cannot be exercised."
    exit 1
fi
echo "Committee policy: $POLICY_ADDR"
echo "max_spend_per_epoch: $MAX_SPEND_PER_EPOCH uspark"

# Define amounts so that:
#   spend_amount + claim_amount > max_spend_per_epoch
#   spend_amount < max_spend_per_epoch (first MsgSpendFromCommons succeeds)
# The recipient (carol) gets the spend; the schedule's claim is for a
# different recipient (bob) so a successful claim wouldn't be masked by
# the spend's bank credit.
# Leave a HEADROOM-uspark window so the recurring claim overflows it
# but the prior-tests' bucket use doesn't push the drain itself over
# the cap. recurring_spend_test + recurring_spend_security_test each
# debit ~0.5M from THIS council's per-epoch bucket before we run, so
# 5M slack is comfortably defensive while still leaving the test's
# overflow assertion meaningful (CLAIM_AMOUNT=6M > 5M headroom).
HEADROOM=5000000
SPEND_AMOUNT=$((MAX_SPEND_PER_EPOCH - HEADROOM))
CLAIM_AMOUNT=$((HEADROOM + 1000000))  # overflow the headroom by 1M
FUND_AMOUNT=$((MAX_SPEND_PER_EPOCH * 2))  # double the cap so balance never binds

# --- 1. FUND THE COMMITTEE --------------------------------------------------
echo ""
echo "STEP 1: Funding committee with ${FUND_AMOUNT}${BOND_DENOM}..."
$BINARY tx bank send "$ALICE_ADDR" "$POLICY_ADDR" "${FUND_AMOUNT}${BOND_DENOM}" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} > /dev/null
sleep 3

POLICY_BAL_PRE=$($BINARY query bank balances "$POLICY_ADDR" --output json | jq -r --arg denom "$BOND_DENOM" '.balances[] | select(.denom==$denom) | .amount')
if [ -z "$POLICY_BAL_PRE" ] || [ "$POLICY_BAL_PRE" -lt "$FUND_AMOUNT" ]; then
    echo "[FAIL] Funding did not arrive: policy balance=$POLICY_BAL_PRE"
    exit 1
fi
echo "Policy balance: $POLICY_BAL_PRE uspark"

# --- 2. DRAIN THE PER-EPOCH BUDGET TO ~99.99% --------------------------------
echo ""
echo "STEP 2: MsgSpendFromCommons drains the per-epoch bucket to ${SPEND_AMOUNT}/${MAX_SPEND_PER_EPOCH}..."

cat > "$PROPOSAL_DIR/spend_drain.json" <<EOF
{
  "policy_address": "$POLICY_ADDR",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgSpendFromCommons",
      "authority": "$POLICY_ADDR",
      "recipient": "$CAROL_ADDR",
      "amount": [{"denom": "${BOND_DENOM}","amount":"$SPEND_AMOUNT"}]
    }
  ],
  "metadata": "drain bucket"
}
EOF

DRAIN_SUBMIT=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/spend_drain.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees "$PROPOSAL_FEE" --output json)
DRAIN_HASH=$(echo "$DRAIN_SUBMIT" | jq -r '.txhash')
sleep 3
DRAIN_PROP=$($BINARY query tx "$DRAIN_HASH" --output json | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
$BINARY tx commons vote-proposal "$DRAIN_PROP" yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} > /dev/null; sleep 3
$BINARY tx commons vote-proposal "$DRAIN_PROP" yes --from bob   -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} > /dev/null; sleep 3
DRAIN_EXEC_RES=$($BINARY tx commons execute-proposal "$DRAIN_PROP" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --fees 5000000${BOND_DENOM} --output json 2>&1)
DRAIN_EXEC_HASH=$(echo "$DRAIN_EXEC_RES" | jq -r '.txhash // empty')
sleep 5

DRAIN_STATUS=$($BINARY query commons get-proposal "$DRAIN_PROP" --output json | jq -r '.proposal.status')
if [ "$DRAIN_STATUS" != "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "[FAIL] Drain proposal did not execute (status=$DRAIN_STATUS)."
    echo "  Exec broadcast response:"
    echo "$DRAIN_EXEC_RES" | head -c 400
    echo ""
    if [ -n "$DRAIN_EXEC_HASH" ]; then
        DRAIN_EXEC_LOG=$($BINARY query tx "$DRAIN_EXEC_HASH" --output json 2>/dev/null)
        echo "  Exec raw_log: $(echo "$DRAIN_EXEC_LOG" | jq -r '.raw_log // "(empty)"' | head -c 400)"
    fi
    exit 1
fi
echo "[ OK ] Drain proposal executed."

# --- 3. SCHEDULE A RECURRING SPEND WHOSE FIRST CLAIM OVERFLOWS THE BUDGET ---
echo ""
echo "STEP 3: Scheduling a recurring spend with amount_per_period=${CLAIM_AMOUNT}${BOND_DENOM} (overflows the remaining ${HEADROOM}${BOND_DENOM} headroom)..."

NOW=$(date +%s)
START_TIME=$((NOW + 30))
PERIOD_SECONDS=20
END_TIME=$((START_TIME + 3 * PERIOD_SECONDS))

cat > "$PROPOSAL_DIR/schedule_overflow.json" <<EOF
{
  "policy_address": "$POLICY_ADDR",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgScheduleRecurringSpend",
      "authority": "$POLICY_ADDR",
      "recipient": "$BOB_ADDR",
      "amount_per_period": [{"denom": "${BOND_DENOM}","amount":"$CLAIM_AMOUNT"}],
      "period_seconds": "$PERIOD_SECONDS",
      "start_time": "$START_TIME",
      "end_time": "$END_TIME",
      "note": "claim should be vetoed by SessionClaimHook"
    }
  ],
  "metadata": "rate-limit veto demo"
}
EOF

SCHED_SUBMIT=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/schedule_overflow.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees "$PROPOSAL_FEE" --output json)
SCHED_HASH=$(echo "$SCHED_SUBMIT" | jq -r '.txhash')
sleep 3
SCHED_PROP=$($BINARY query tx "$SCHED_HASH" --output json | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
$BINARY tx commons vote-proposal "$SCHED_PROP" yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} > /dev/null; sleep 3
$BINARY tx commons vote-proposal "$SCHED_PROP" yes --from bob   -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} > /dev/null; sleep 3
SCHED_EXEC=$($BINARY tx commons execute-proposal "$SCHED_PROP" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --fees 5000000${BOND_DENOM} --output json)
sleep 3
SCHED_EXEC_HASH=$(echo "$SCHED_EXEC" | jq -r '.txhash')

SCHED_STATUS=$($BINARY query commons get-proposal "$SCHED_PROP" --output json | jq -r '.proposal.status')
if [ "$SCHED_STATUS" != "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "[FAIL] Schedule proposal did not execute (status=$SCHED_STATUS)."
    exit 1
fi

SCHEDULE_ID=$($BINARY query tx "$SCHED_EXEC_HASH" --output json | jq -r '
    .events[] |
    select(.type=="grant_created") |
    select(.attributes[]? | select(.key=="source" and .value=="module_bypass")) |
    .attributes[] | select(.key=="id") | .value
' | tr -d '"' | head -n1)
if [ -z "$SCHEDULE_ID" ] || [ "$SCHEDULE_ID" == "null" ]; then
    echo "[FAIL] grant_created event missing on schedule execute."
    exit 1
fi
echo "[ OK ] Scheduled id=$SCHEDULE_ID"

# --- 4. WAIT AND ATTEMPT THE CLAIM — MUST BE VETOED -------------------------
echo ""
WAIT_FOR=$((START_TIME + PERIOD_SECONDS + 3 - $(date +%s)))
if [ "$WAIT_FOR" -lt 0 ]; then WAIT_FOR=0; fi
echo "STEP 4: Waiting ${WAIT_FOR}s for the first claim window..."
sleep "$WAIT_FOR"

# Capture bob's balance BEFORE the claim attempt to confirm no bank send.
BOB_BAL_PRE=$($BINARY query bank balances "$BOB_ADDR" --output json | jq -r --arg denom "$BOND_DENOM" '.balances[] | select(.denom==$denom) | .amount')
if [ -z "$BOB_BAL_PRE" ]; then BOB_BAL_PRE=0; fi
echo "Bob's pre-claim balance: $BOB_BAL_PRE uspark"

CLAIM_RES=$($BINARY tx commons claim-recurring-spend "$SCHEDULE_ID" --from bob -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} --output json 2>&1)
CLAIM_CODE=$(echo "$CLAIM_RES" | jq -r '.code')
CLAIM_HASH=$(echo "$CLAIM_RES" | jq -r '.txhash')
if [ "$CLAIM_CODE" == "0" ]; then
    sleep 3
    CLAIM_TX=$($BINARY query tx "$CLAIM_HASH" --output json 2>/dev/null)
    CLAIM_CODE=$(echo "$CLAIM_TX" | jq -r '.code')
    CLAIM_LOG=$(echo "$CLAIM_TX" | jq -r '.raw_log')
fi
if [ "$CLAIM_CODE" == "0" ]; then
    echo "[FAIL] Rate-limit overflow claim succeeded! SessionClaimHook PreCheck is not gating."
    exit 1
fi
echo "[ OK ] Overflow claim rejected (code=$CLAIM_CODE)."

# Bob's balance must be unchanged (minus fees for the failed tx).
BOB_BAL_POST=$($BINARY query bank balances "$BOB_ADDR" --output json | jq -r --arg denom "$BOND_DENOM" '.balances[] | select(.denom==$denom) | .amount')
if [ -z "$BOB_BAL_POST" ]; then BOB_BAL_POST=0; fi
DELTA=$((BOB_BAL_POST - BOB_BAL_PRE))
# Bob paid up to 5000${BOND_DENOM} in fees; the claim itself must not have credited.
if [ "$DELTA" -gt 0 ]; then
    echo "[FAIL] Bob's balance increased by $DELTA — vetoed claim still moved coins."
    exit 1
fi
echo "[ OK ] Bob's balance unchanged on vetoed claim (delta=$DELTA, fees-only)."

# Grant should still be ACTIVE (PreCheck veto does not auto-pause).
SCHED_AFTER=$($BINARY query commons get-recurring-spend "$SCHEDULE_ID" --output json)
# proto3 JSON omits zero-valued uint64 fields; treat absent as 0.
CLAIMS_MADE=$(echo "$SCHED_AFTER" | jq -r '.recurring_spend.claims_made // "0"')
SCHED_STATUS_AFTER=$(echo "$SCHED_AFTER" | jq -r '.recurring_spend.status')
if [ "$CLAIMS_MADE" != "0" ]; then
    echo "[FAIL] Vetoed claim still advanced claims_made (got $CLAIMS_MADE)."
    exit 1
fi
if [ "$SCHED_STATUS_AFTER" != "RECURRING_SPEND_STATUS_ACTIVE" ]; then
    echo "[FAIL] Vetoed claim left grant in unexpected status: $SCHED_STATUS_AFTER (expected ACTIVE)."
    exit 1
fi
echo "[ OK ] Schedule untouched: claims_made=$CLAIMS_MADE, status=$SCHED_STATUS_AFTER."

# --- 5. CLEANUP: CANCEL THE SCHEDULE ----------------------------------------
echo ""
echo "STEP 5: Cleanup — cancelling the schedule..."

cat > "$PROPOSAL_DIR/cancel_overflow.json" <<EOF
{
  "policy_address": "$POLICY_ADDR",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgCancelRecurringSpend",
      "authority": "$POLICY_ADDR",
      "id": "$SCHEDULE_ID"
    }
  ],
  "metadata": "cleanup"
}
EOF

CANCEL_SUBMIT=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/cancel_overflow.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees "$PROPOSAL_FEE" --output json)
CANCEL_HASH=$(echo "$CANCEL_SUBMIT" | jq -r '.txhash')
sleep 3
CANCEL_PROP=$($BINARY query tx "$CANCEL_HASH" --output json | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
$BINARY tx commons vote-proposal "$CANCEL_PROP" yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} > /dev/null; sleep 3
$BINARY tx commons vote-proposal "$CANCEL_PROP" yes --from bob   -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} > /dev/null; sleep 3
$BINARY tx commons execute-proposal "$CANCEL_PROP" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --fees 5000000${BOND_DENOM} > /dev/null
sleep 3
echo "[ OK ] Schedule cancelled."

echo ""
echo "[ OK ] SessionClaimHook rate-limit gate test PASSED."
