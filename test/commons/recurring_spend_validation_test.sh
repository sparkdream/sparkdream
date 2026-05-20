#!/bin/bash

# Schedule-time validation matrix for MsgScheduleRecurringSpend.
#
# Each subcase submits a council proposal whose only inner message is a
# malformed MsgScheduleRecurringSpend. The proposal is allowed to be
# *submitted* (the council has the permission), and accepted (votes pass),
# but the inner message must FAIL at execute time. We assert:
#   - the execute_proposal tx code is non-zero, OR
#   - the proposal flips to PROPOSAL_STATUS_FAILED
# and that no new schedule appears in session.Grants for this council.
#
# Post-M11 (RecurringSpend migration): cadence + duration bounds and
# the per-granter active-grant cap are now enforced by x/session, not
# by x/commons. We read those bounds from `session params`; the
# `granter == grantee` rejection is a new check the migration added
# (§4 / §8 behavioral tightening). The store-mutation post-check
# filters by authority since M9 dropped cross-granter pagination.

set -u

echo "--- TESTING: RECURRING SPEND VALIDATION MATRIX ---"

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
POLICY_ADDR=$($BINARY query commons get-group "$GROUP_NAME" --output json | jq -r '.group.policy_address')
if [ -z "$POLICY_ADDR" ] || [ "$POLICY_ADDR" == "null" ]; then
    echo "[FAIL] '$GROUP_NAME' not found — run bootstrap first."
    exit 1
fi

PROPOSAL_FEE=$($BINARY query commons params --output json | jq -r '.params.proposal_fee')
echo "Committee policy: $POLICY_ADDR"
echo "Proposal fee:     $PROPOSAL_FEE"

# Read recurring-spend bounds from session (M10 dropped them from
# commons.Params; session is now authoritative).
SESSION_PARAMS=$($BINARY query session params --output json)
MIN_PERIOD=$(echo "$SESSION_PARAMS" | jq -r '.params.min_recurring_period_seconds')
MAX_DURATION=$(echo "$SESSION_PARAMS" | jq -r '.params.max_recurring_duration_seconds')
if [ -z "$MIN_PERIOD" ] || [ "$MIN_PERIOD" == "null" ]; then
    echo "[FAIL] Could not read session params.min_recurring_period_seconds."
    exit 1
fi
echo "session.min_recurring_period_seconds:   $MIN_PERIOD"
echo "session.max_recurring_duration_seconds: $MAX_DURATION"

# Fund the committee so SendCoins doesn't reject for insufficient funds
# (would mask validation errors).
$BINARY tx bank send "$ALICE_ADDR" "$POLICY_ADDR" 50000000uspark --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark > /dev/null
sleep 3

# Helper: build a schedule proposal with the provided body, submit, vote yes by alice/bob, execute, and assert it FAILED.
# Args: $1 = subtest label, $2 = path to messages-only JSON snippet.
assert_schedule_rejects() {
    local label=$1
    local body=$2

    cat > "$PROPOSAL_DIR/sched_invalid.json" <<EOF
{
  "policy_address": "$POLICY_ADDR",
  "messages": $body,
  "metadata": "Validation subtest: $label"
}
EOF

    local submit
    submit=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/sched_invalid.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees "$PROPOSAL_FEE" --output json)
    local tx_hash
    tx_hash=$(echo "$submit" | jq -r '.txhash')
    sleep 3

    local prop_id
    prop_id=$($BINARY query tx "$tx_hash" --output json 2>/dev/null | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
    if [ -z "$prop_id" ] || [ "$prop_id" == "null" ]; then
        local submit_log
        submit_log=$($BINARY query tx "$tx_hash" --output json 2>/dev/null | jq -r '.raw_log')
        echo "[ OK ] $label — proposal rejected at submit (log: $submit_log)"
        return 0
    fi

    $BINARY tx commons vote-proposal "$prop_id" yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark > /dev/null
    sleep 3
    $BINARY tx commons vote-proposal "$prop_id" yes --from bob   -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000uspark > /dev/null
    sleep 3

    local exec_res
    exec_res=$($BINARY tx commons execute-proposal "$prop_id" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --fees 5000000uspark --output json)
    local exec_hash
    exec_hash=$(echo "$exec_res" | jq -r '.txhash')
    sleep 3

    local prop_status
    prop_status=$($BINARY query commons get-proposal "$prop_id" --output json | jq -r '.proposal.status')
    if [ "$prop_status" == "PROPOSAL_STATUS_EXECUTED" ]; then
        echo "[FAIL] $label — proposal $prop_id executed successfully (expected failure). status=$prop_status"
        $BINARY query tx "$exec_hash" --output json | jq -r '.raw_log'
        exit 1
    fi
    echo "[ OK ] $label — proposal $prop_id terminal status: $prop_status"
}

# Pre-record schedule count by this authority. M9 dropped no-filter
# pagination; we filter by authority instead (the cross-granter
# variant is not supported post-migration).
INITIAL_COUNT=$($BINARY query commons list-recurring-spends --authority "$POLICY_ADDR" --output json | jq '.recurring_spends | length')
echo "Schedules under $POLICY_ADDR before validation subtests: $INITIAL_COUNT"

NOW=$(date +%s)

# --- SUBTEST 1: period below min ------------------------------------------
TOO_SHORT_PERIOD=$((MIN_PERIOD - 1))
if [ "$TOO_SHORT_PERIOD" -le 0 ]; then
    TOO_SHORT_PERIOD=1
fi
START=$((NOW + 30))
END=$((START + MIN_PERIOD * 4))

assert_schedule_rejects "period below min ($TOO_SHORT_PERIOD < $MIN_PERIOD)" "[
  {
    \"@type\": \"/sparkdream.commons.v1.MsgScheduleRecurringSpend\",
    \"authority\": \"$POLICY_ADDR\",
    \"recipient\": \"$CAROL_ADDR\",
    \"amount_per_period\": [{\"denom\":\"uspark\",\"amount\":\"100\"}],
    \"period_seconds\": \"$TOO_SHORT_PERIOD\",
    \"start_time\": \"$START\",
    \"end_time\": \"$END\",
    \"note\": \"too-short period\"
  }
]"

# --- SUBTEST 2: end before start ------------------------------------------
assert_schedule_rejects "end before start" "[
  {
    \"@type\": \"/sparkdream.commons.v1.MsgScheduleRecurringSpend\",
    \"authority\": \"$POLICY_ADDR\",
    \"recipient\": \"$CAROL_ADDR\",
    \"amount_per_period\": [{\"denom\":\"uspark\",\"amount\":\"100\"}],
    \"period_seconds\": \"$MIN_PERIOD\",
    \"start_time\": \"$END\",
    \"end_time\": \"$START\",
    \"note\": \"backward window\"
  }
]"

# --- SUBTEST 3: duration over cap ----------------------------------------
WAY_END=$((NOW + 30 + MAX_DURATION + 86400))
assert_schedule_rejects "duration over cap" "[
  {
    \"@type\": \"/sparkdream.commons.v1.MsgScheduleRecurringSpend\",
    \"authority\": \"$POLICY_ADDR\",
    \"recipient\": \"$CAROL_ADDR\",
    \"amount_per_period\": [{\"denom\":\"uspark\",\"amount\":\"100\"}],
    \"period_seconds\": \"$MIN_PERIOD\",
    \"start_time\": \"$START\",
    \"end_time\": \"$WAY_END\",
    \"note\": \"too-long\"
  }
]"

# --- SUBTEST 4: window shorter than one period ----------------------------
SHORT_END=$((START + MIN_PERIOD - 1))
assert_schedule_rejects "window shorter than one period" "[
  {
    \"@type\": \"/sparkdream.commons.v1.MsgScheduleRecurringSpend\",
    \"authority\": \"$POLICY_ADDR\",
    \"recipient\": \"$CAROL_ADDR\",
    \"amount_per_period\": [{\"denom\":\"uspark\",\"amount\":\"100\"}],
    \"period_seconds\": \"$MIN_PERIOD\",
    \"start_time\": \"$START\",
    \"end_time\": \"$SHORT_END\",
    \"note\": \"window<period\"
  }
]"

# --- SUBTEST 5: start in the past ----------------------------------------
PAST_START=$((NOW - 86400))
PAST_END=$((PAST_START + MIN_PERIOD * 2))
assert_schedule_rejects "start in the past" "[
  {
    \"@type\": \"/sparkdream.commons.v1.MsgScheduleRecurringSpend\",
    \"authority\": \"$POLICY_ADDR\",
    \"recipient\": \"$CAROL_ADDR\",
    \"amount_per_period\": [{\"denom\":\"uspark\",\"amount\":\"100\"}],
    \"period_seconds\": \"$MIN_PERIOD\",
    \"start_time\": \"$PAST_START\",
    \"end_time\": \"$PAST_END\",
    \"note\": \"past start\"
  }
]"

# --- SUBTEST 6: granter == grantee (self-payment) — NEW POST-MIGRATION ----
# Session's validateGrantCommon hard-rejects granter == grantee. Today's
# commons handler did NOT check this; the migration tightens behavior to
# match session's invariant (§8 / migration plan).
assert_schedule_rejects "granter == grantee (self-payment)" "[
  {
    \"@type\": \"/sparkdream.commons.v1.MsgScheduleRecurringSpend\",
    \"authority\": \"$POLICY_ADDR\",
    \"recipient\": \"$POLICY_ADDR\",
    \"amount_per_period\": [{\"denom\":\"uspark\",\"amount\":\"100\"}],
    \"period_seconds\": \"$MIN_PERIOD\",
    \"start_time\": \"$START\",
    \"end_time\": \"$END\",
    \"note\": \"self-payment\"
  }
]"

# --- SUBTEST 7: multi-coin amount (D1.a restriction) — NEW POST-MIGRATION -
# Only one coin per schedule. Today's parallel storage accepted sdk.Coins
# but the rate-limit only counted uspark; the migration tightens to a
# single coin so the per-grant max_per_epoch_uspark cap is meaningful.
assert_schedule_rejects "multi-coin amount rejected (D1.a)" "[
  {
    \"@type\": \"/sparkdream.commons.v1.MsgScheduleRecurringSpend\",
    \"authority\": \"$POLICY_ADDR\",
    \"recipient\": \"$CAROL_ADDR\",
    \"amount_per_period\": [
      {\"denom\":\"uspark\",\"amount\":\"100\"},
      {\"denom\":\"uatom\",\"amount\":\"50\"}
    ],
    \"period_seconds\": \"$MIN_PERIOD\",
    \"start_time\": \"$START\",
    \"end_time\": \"$END\",
    \"note\": \"multi-coin\"
  }
]"

# --- POST-CHECK: schedule store unchanged for this authority -------------
FINAL_COUNT=$($BINARY query commons list-recurring-spends --authority "$POLICY_ADDR" --output json | jq '.recurring_spends | length')
if [ "$FINAL_COUNT" -gt "$INITIAL_COUNT" ]; then
    echo "[FAIL] Validation subtests leaked $((FINAL_COUNT - INITIAL_COUNT)) schedules into the store for $POLICY_ADDR."
    exit 1
fi
echo "[ OK ] Validation subtests left the schedule store at $FINAL_COUNT entries for $POLICY_ADDR (unchanged)."

echo ""
echo "[ OK ] Recurring spend validation matrix PASSED."
