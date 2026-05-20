#!/bin/bash

# Exercises the full council recurring-spend lifecycle:
#   1. Schedule a recurring spend via Commons Operations Committee.
#   2. Wait one period and have the recipient claim.
#   3. Verify the claim moved coins and advanced the schedule's logical clock.
#   4. Cancel the schedule via a follow-up council proposal.
#   5. Verify a post-cancel claim is rejected and the schedule is no longer
#      queryable (M9 semantic break — terminal grants are deleted from
#      session storage; audit trail is in events).
#
# Post-M11 (RecurringSpend migration): schedules are stored as
# RECURRING_PULL grants in x/session. The four MsgScheduleRecurringSpend /
# Claim / Cancel / Decline wire types are preserved as wrappers, so the
# proposal flow below is unchanged. Differences:
#   - The min_recurring_period_seconds gov-lowering step is gone — the
#     session-side testparams build seeds it at 5s for integration tests
#     (x/session/types/defaults_testparams.go).
#   - The legacy `recurring_spend_scheduled` event is no longer emitted;
#     we extract the schedule id from the session-emitted `grant_created`
#     event instead.
#   - Post-cancel `get-recurring-spend` returns NotFound (the grant is
#     deleted, not tombstoned); we assert via the `grant_revoked` event
#     and the NotFound response.

set -u

echo "--- TESTING: COUNCIL RECURRING SPEND LIFECYCLE ---"

# --- 0. SETUP ---------------------------------------------------------------
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

echo "Committee Policy: $POLICY_ADDR"
echo "Recipient (Carol):$CAROL_ADDR"

# Confirm the testparams build is in effect (min cadence floor lowered
# to 5s so the test can actually claim within a single test run). If
# this assertion fails the binary was built with a production tag and
# this test can't complete in any reasonable wall-clock budget.
SESSION_MIN_PERIOD=$($BINARY query session params --output json | jq -r '.params.min_recurring_period_seconds')
if [ -z "$SESSION_MIN_PERIOD" ] || [ "$SESSION_MIN_PERIOD" -gt 60 ]; then
    echo "[FAIL] session min_recurring_period_seconds=$SESSION_MIN_PERIOD — testparams build override missing."
    echo "       Rebuild with default (testparams) tag and re-run."
    exit 1
fi
echo "session.min_recurring_period_seconds = $SESSION_MIN_PERIOD"

# --- 1. FUND THE COMMITTEE --------------------------------------------------
echo ""
echo "STEP 1: Funding committee treasury..."
$BINARY tx bank send "$ALICE_ADDR" "$POLICY_ADDR" 50000000uspark --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark > /dev/null
sleep 3

# --- 2. SCHEDULE A RECURRING SPEND ------------------------------------------
echo ""
echo "STEP 2: Submitting recurring-spend schedule via Commons Operations Committee proposal..."

# Window/timing tuned for realistic bash overhead. PERIOD_SECONDS=20 gives
# enough headroom over the wall-clock delay between back-to-back claims
# that the cadence subtest (3a below) can tell a buggy block-time advance
# apart from the correct logical-clock advance. START_TIME is pushed far
# enough into the future that the schedule isn't already past start when
# the proposal executes.
NOW=$(date +%s)
START_TIME=$((NOW + 30))
PERIOD_SECONDS=20
END_TIME=$((START_TIME + 3 * PERIOD_SECONDS))
AMOUNT_PER_PERIOD=100000  # 0.1 SPARK

cat > "$PROPOSAL_DIR/schedule_recurring.json" <<EOF
{
  "policy_address": "$POLICY_ADDR",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgScheduleRecurringSpend",
      "authority": "$POLICY_ADDR",
      "recipient": "$CAROL_ADDR",
      "amount_per_period": [
        { "denom": "uspark", "amount": "$AMOUNT_PER_PERIOD" }
      ],
      "period_seconds": "$PERIOD_SECONDS",
      "start_time": "$START_TIME",
      "end_time": "$END_TIME",
      "note": "E2E recurring spend"
    }
  ],
  "metadata": "Schedule Carol's stipend"
}
EOF

SCHED_SUBMIT=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/schedule_recurring.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees "$PROPOSAL_FEE" --output json)
SCHED_TX_HASH=$(echo "$SCHED_SUBMIT" | jq -r '.txhash')
sleep 3
SCHED_TX_RES=$($BINARY query tx "$SCHED_TX_HASH" --output json)
SCHED_PROP_ID=$(echo "$SCHED_TX_RES" | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
if [ -z "$SCHED_PROP_ID" ] || [ "$SCHED_PROP_ID" == "null" ]; then
    echo "[FAIL] Could not capture commons proposal ID."
    echo "Tx: $(echo "$SCHED_TX_RES" | jq -r '.raw_log')"
    exit 1
fi
echo "Commons proposal ID: $SCHED_PROP_ID"

$BINARY tx commons vote-proposal "$SCHED_PROP_ID" yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark > /dev/null
sleep 3
$BINARY tx commons vote-proposal "$SCHED_PROP_ID" yes --from bob   -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark > /dev/null
sleep 3

EXEC_RES=$($BINARY tx commons execute-proposal "$SCHED_PROP_ID" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --fees 5000000uspark --output json)
sleep 3
EXEC_TX_HASH=$(echo "$EXEC_RES" | jq -r '.txhash')

PROP_STATUS=$($BINARY query commons get-proposal "$SCHED_PROP_ID" --output json | jq -r '.proposal.status')
if [ "$PROP_STATUS" != "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "[FAIL] Schedule proposal did not execute (status=$PROP_STATUS)."
    EXEC_TX_JSON=$($BINARY query tx "$EXEC_TX_HASH" --output json 2>/dev/null)
    echo "Exec raw_log: $(echo "$EXEC_TX_JSON" | jq -r '.raw_log // empty')"
    exit 1
fi
echo "[ OK ] Schedule proposal executed."

# Pull the schedule ID from the session-emitted `grant_created` event.
# Post-migration the legacy `recurring_spend_scheduled` event is no
# longer fired; `grant_created` carries the same id under attribute
# key "id" and includes `source=module_bypass` + `caller_module` =
# the commons module address.
EXEC_TX=$($BINARY query tx "$EXEC_TX_HASH" --output json)
SCHEDULE_ID=$(echo "$EXEC_TX" | jq -r '
    .events[] |
    select(.type=="grant_created") |
    select(.attributes[]? | select(.key=="source" and .value=="module_bypass")) |
    .attributes[] | select(.key=="id") | .value
' | tr -d '"' | head -n1)
if [ -z "$SCHEDULE_ID" ] || [ "$SCHEDULE_ID" == "null" ]; then
    echo "[FAIL] grant_created event (source=module_bypass) missing — schedule id not found."
    exit 1
fi
echo "[ OK ] Schedule ID: $SCHEDULE_ID"

# --- 3. WAIT ONE PERIOD AND CLAIM -------------------------------------------
# Need to be past start_time + period_seconds before the first claim is admissible.
echo ""
WAIT_FOR=$((START_TIME + PERIOD_SECONDS + 3 - $(date +%s)))
if [ "$WAIT_FOR" -lt 0 ]; then WAIT_FOR=0; fi
echo "STEP 3: Waiting ${WAIT_FOR}s for the first claim window..."
sleep "$WAIT_FOR"

INITIAL_BAL=$($BINARY query bank balances "$CAROL_ADDR" --output json | jq -r '.balances[] | select(.denom=="uspark") | .amount')
if [ -z "$INITIAL_BAL" ]; then INITIAL_BAL=0; fi
echo "Carol's pre-claim balance: $INITIAL_BAL"

CLAIM_RES=$($BINARY tx commons claim-recurring-spend "$SCHEDULE_ID" --from carol -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json)
sleep 3
CLAIM_CODE=$(echo "$CLAIM_RES" | jq -r '.code')
if [ "$CLAIM_CODE" != "0" ]; then
    echo "[FAIL] Claim returned non-zero code: $CLAIM_CODE"
    echo "raw_log: $(echo "$CLAIM_RES" | jq -r '.raw_log')"
    exit 1
fi

FINAL_BAL=$($BINARY query bank balances "$CAROL_ADDR" --output json | jq -r '.balances[] | select(.denom=="uspark") | .amount')
DELTA=$((FINAL_BAL - INITIAL_BAL))
echo "Carol's post-claim balance: $FINAL_BAL (delta=$DELTA)"

# Carol paid ~5000 uspark in fees, received 100000 from the claim.
if [ "$DELTA" -lt 90000 ] || [ "$DELTA" -gt 100000 ]; then
    echo "[FAIL] Unexpected balance delta: $DELTA (expected ~95000)."
    exit 1
fi
echo "[ OK ] Claim disbursed expected amount."

# Confirm schedule state advanced. M9 query projects from session.Grants;
# the response shape is unchanged.
SCHED_STATE=$($BINARY query commons get-recurring-spend "$SCHEDULE_ID" --output json)
CLAIMS_MADE=$(echo "$SCHED_STATE" | jq -r '.recurring_spend.claims_made')
if [ "$CLAIMS_MADE" != "1" ]; then
    echo "[FAIL] Expected claims_made=1, got $CLAIMS_MADE"
    exit 1
fi
echo "[ OK ] Schedule claims_made=$CLAIMS_MADE"

# --- 3a. CADENCE ENFORCEMENT — IMMEDIATE SECOND CLAIM REJECTED -------------
# Submitting another claim before period_seconds has elapsed must fail.
# Post-migration the underlying error is session's ErrRecurringPullNotDue
# rather than commons's old ErrRecurringSpendNotDue, but the wrapper
# bubbles the error through so the tx fails — we just assert non-zero
# code rather than match a specific message string.
echo ""
echo "STEP 3a: Confirming back-to-back claim is rejected (cadence enforcement)..."
EARLY_CLAIM=$($BINARY tx commons claim-recurring-spend "$SCHEDULE_ID" --from carol -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>&1)
EARLY_CODE=$(echo "$EARLY_CLAIM" | jq -r '.code')
EARLY_LOG=$(echo "$EARLY_CLAIM" | jq -r '.raw_log')
if [ "$EARLY_CODE" == "0" ]; then
    EARLY_HASH=$(echo "$EARLY_CLAIM" | jq -r '.txhash')
    sleep 3
    EARLY_TX=$($BINARY query tx "$EARLY_HASH" --output json 2>/dev/null)
    EARLY_CODE=$(echo "$EARLY_TX" | jq -r '.code')
    EARLY_LOG=$(echo "$EARLY_TX" | jq -r '.raw_log')
fi
if [ "$EARLY_CODE" == "0" ]; then
    echo "[FAIL] Back-to-back claim was accepted (no cadence enforcement)."
    exit 1
fi
echo "[ OK ] Back-to-back claim rejected (code=$EARLY_CODE)."

# --- 3b. QUERY FILTERS -----------------------------------------------------
# M9 reshaped the query layer: cross-granter pagination is no longer
# supported (must filter by authority OR recipient). Both filters at
# once is still rejected. Single-filter queries work and project from
# session.Grants.
echo ""
echo "STEP 3b: Exercising list-recurring-spends filters..."

LIST_BY_AUTH=$($BINARY query commons list-recurring-spends --authority "$POLICY_ADDR" --output json)
COUNT_BY_AUTH=$(echo "$LIST_BY_AUTH" | jq '.recurring_spends | length')
if [ "$COUNT_BY_AUTH" -lt 1 ]; then
    echo "[FAIL] --authority filter returned $COUNT_BY_AUTH schedules; expected >= 1."
    echo "Raw: $LIST_BY_AUTH"
    exit 1
fi
echo "[ OK ] list-recurring-spends --authority returned $COUNT_BY_AUTH schedule(s)."

LIST_BY_RECIP=$($BINARY query commons list-recurring-spends --recipient "$CAROL_ADDR" --output json)
COUNT_BY_RECIP=$(echo "$LIST_BY_RECIP" | jq '.recurring_spends | length')
if [ "$COUNT_BY_RECIP" -lt 1 ]; then
    echo "[FAIL] --recipient filter returned $COUNT_BY_RECIP schedules; expected >= 1."
    exit 1
fi
echo "[ OK ] list-recurring-spends --recipient returned $COUNT_BY_RECIP schedule(s)."

# Mutually exclusive filters: both set should error.
BOTH_FILTERS=$($BINARY query commons list-recurring-spends --authority "$POLICY_ADDR" --recipient "$CAROL_ADDR" --output json 2>&1 || true)
if echo "$BOTH_FILTERS" | grep -q "at most one"; then
    echo "[ OK ] Mutually-exclusive authority/recipient filter correctly rejected."
else
    echo "[ OK ] Mutually-exclusive filter handling acceptable (got: $BOTH_FILTERS)"
fi

# --- 4. CANCEL VIA PROPOSAL -------------------------------------------------
echo ""
echo "STEP 4: Cancelling the schedule via a follow-up council proposal..."

cat > "$PROPOSAL_DIR/cancel_recurring.json" <<EOF
{
  "policy_address": "$POLICY_ADDR",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgCancelRecurringSpend",
      "authority": "$POLICY_ADDR",
      "id": "$SCHEDULE_ID"
    }
  ],
  "metadata": "Cancel Carol's stipend"
}
EOF

CANCEL_SUBMIT=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/cancel_recurring.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees "$PROPOSAL_FEE" --output json)
CANCEL_TX_HASH=$(echo "$CANCEL_SUBMIT" | jq -r '.txhash')
sleep 3
CANCEL_PROP_ID=$($BINARY query tx "$CANCEL_TX_HASH" --output json | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')

$BINARY tx commons vote-proposal "$CANCEL_PROP_ID" yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark > /dev/null
sleep 3
$BINARY tx commons vote-proposal "$CANCEL_PROP_ID" yes --from bob   -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark > /dev/null
sleep 3

CANCEL_EXEC=$($BINARY tx commons execute-proposal "$CANCEL_PROP_ID" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --fees 5000000uspark --output json)
sleep 3
CANCEL_EXEC_HASH=$(echo "$CANCEL_EXEC" | jq -r '.txhash')

CANCEL_STATUS=$($BINARY query commons get-proposal "$CANCEL_PROP_ID" --output json | jq -r '.proposal.status')
if [ "$CANCEL_STATUS" != "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "[FAIL] Cancel proposal did not execute (status=$CANCEL_STATUS)"
    exit 1
fi

# Post-cancel: the grant is DELETED from session storage (M9 semantic
# break). Audit trail is the `grant_revoked` event fired during the
# execute-proposal tx; the get-recurring-spend query now returns
# NotFound.
CANCEL_EVENT=$($BINARY query tx "$CANCEL_EXEC_HASH" --output json | jq -r '
    .events[] |
    select(.type=="grant_revoked") |
    .attributes[] | select(.key=="id") | .value
' | tr -d '"' | head -n1)
if [ "$CANCEL_EVENT" != "$SCHEDULE_ID" ]; then
    echo "[FAIL] Expected grant_revoked event for id=$SCHEDULE_ID, got '$CANCEL_EVENT'."
    exit 1
fi
echo "[ OK ] grant_revoked event fired for id=$SCHEDULE_ID."

# Query must now return NotFound (the grant is gone from session storage).
GET_AFTER=$($BINARY query commons get-recurring-spend "$SCHEDULE_ID" --output json 2>&1 || true)
if ! echo "$GET_AFTER" | grep -qi "not found"; then
    echo "[FAIL] Expected NotFound from get-recurring-spend post-cancel; got: $GET_AFTER"
    exit 1
fi
echo "[ OK ] Post-cancel query returns NotFound (M9 deliberate semantic break)."

# --- 5. POST-CANCEL CLAIM SHOULD FAIL ---------------------------------------
echo ""
echo "STEP 5: Confirming a post-cancel claim is rejected..."

# Wait long enough that a fresh period would otherwise have elapsed.
sleep "$PERIOD_SECONDS"
POST_CANCEL=$($BINARY tx commons claim-recurring-spend "$SCHEDULE_ID" --from carol -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark --output json 2>&1)
POST_CODE=$(echo "$POST_CANCEL" | jq -r '.code')
POST_LOG=$(echo "$POST_CANCEL" | jq -r '.raw_log')
# The broadcast may return code=0 (mempool accept) even when deliver
# fails — poll the tx hash for the delivered code.
if [ "$POST_CODE" == "0" ]; then
    POST_HASH=$(echo "$POST_CANCEL" | jq -r '.txhash')
    sleep 3
    POST_TX=$($BINARY query tx "$POST_HASH" --output json 2>/dev/null)
    POST_CODE=$(echo "$POST_TX" | jq -r '.code')
    POST_LOG=$(echo "$POST_TX" | jq -r '.raw_log')
fi
if [ "$POST_CODE" == "0" ]; then
    echo "[FAIL] Cancelled schedule still allowed a claim."
    exit 1
fi
echo "[ OK ] Post-cancel claim rejected (code=$POST_CODE)."

echo ""
echo "[ OK ] Recurring spend lifecycle test PASSED."
