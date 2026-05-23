#!/bin/bash

# Cross-module visibility for council recurring spends.
#
# After the M5–M10 migration, council schedules live as
# RECURRING_PULL grants in x/session. The four `Msg*RecurringSpend`
# wrappers preserve the commons-side wire shape; this test asserts
# the same grant is observable through the *session* query surface,
# proving the wrapper actually wrote to session storage rather than
# faking the response.
#
# Coverage:
#   1. Schedule via commons wrapper → assert visible via
#      `query session grant <id>` with the right shape:
#        - grant.type == GRANT_TYPE_RECURRING_PULL
#        - grant.granter == council policy address
#        - grant.grantee == recipient
#        - grant.recurring_pull.amount_per_period == requested coin
#        - grant.recurring_pull.period_seconds == requested period
#   2. Schedule appears in `query session grants-by-granter <council>`
#      AND `query session grants-by-grantee <recipient>`.
#   3. Cancel via commons wrapper → grant DELETED from session
#      (NotFound on `query session grant`, absent from list queries).
#      This is the §1/§8 deliberate semantic break.

set -u

echo "--- TESTING: CROSS-MODULE GRANT VISIBILITY ---"

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
echo "Recipient:        $CAROL_ADDR"

# Fund the council so the schedule is fundable when claimed.
$BINARY tx bank send "$ALICE_ADDR" "$POLICY_ADDR" 50000000${BOND_DENOM} --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} > /dev/null
sleep 3

# --- 1. SCHEDULE VIA COMMONS WRAPPER ----------------------------------------
echo ""
echo "STEP 1: Scheduling a recurring spend via the commons wrapper..."

NOW=$(date +%s)
START_TIME=$((NOW + 30))
PERIOD_SECONDS=20
END_TIME=$((START_TIME + 3 * PERIOD_SECONDS))
AMOUNT=123456

cat > "$PROPOSAL_DIR/sched_visibility.json" <<EOF
{
  "policy_address": "$POLICY_ADDR",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgScheduleRecurringSpend",
      "authority": "$POLICY_ADDR",
      "recipient": "$CAROL_ADDR",
      "amount_per_period": [{"denom": "${BOND_DENOM}","amount":"$AMOUNT"}],
      "period_seconds": "$PERIOD_SECONDS",
      "start_time": "$START_TIME",
      "end_time": "$END_TIME",
      "note": "visibility test"
    }
  ],
  "metadata": "visibility test"
}
EOF

SUBMIT=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/sched_visibility.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees "$PROPOSAL_FEE" --output json)
SUBMIT_HASH=$(echo "$SUBMIT" | jq -r '.txhash')
sleep 3
PROP_ID=$($BINARY query tx "$SUBMIT_HASH" --output json | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
$BINARY tx commons vote-proposal "$PROP_ID" yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} > /dev/null; sleep 3
$BINARY tx commons vote-proposal "$PROP_ID" yes --from bob   -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} > /dev/null; sleep 3
EXEC=$($BINARY tx commons execute-proposal "$PROP_ID" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --fees 5000000${BOND_DENOM} --output json)
sleep 3
EXEC_HASH=$(echo "$EXEC" | jq -r '.txhash')

SCHEDULE_ID=$($BINARY query tx "$EXEC_HASH" --output json | jq -r '
    .events[] |
    select(.type=="grant_created") |
    select(.attributes[]? | select(.key=="source" and .value=="module_bypass")) |
    .attributes[] | select(.key=="id") | .value
' | tr -d '"' | head -n1 | tr -d '[:space:]')
if [ -z "$SCHEDULE_ID" ] || [ "$SCHEDULE_ID" == "null" ]; then
    echo "[FAIL] Could not capture grant id from grant_created event."
    echo "Raw EXEC: $EXEC"
    exit 1
fi
echo "[ OK ] Scheduled grant id=$SCHEDULE_ID"

# --- 2. QUERY SESSION DIRECTLY — VERIFY THE GRANT ---------------------------
echo ""
echo "STEP 2: Querying x/session directly to confirm the grant exists with the right shape..."

# Defensive: the previous failure mode produced jq parse errors at
# column 7 with no "not found" in the output. That means the CLI
# emitted something other than the standard "rpc error" or JSON shape.
# Dump the raw response before we fan out to jq so the failure mode is
# visible.
GRANT=$($BINARY query session grant "$SCHEDULE_ID" --output json 2>&1)
echo "  [debug] SCHEDULE_ID='${SCHEDULE_ID}' len=${#SCHEDULE_ID}"
echo "  [debug] Raw GRANT length=${#GRANT}"
echo "  [debug] Raw GRANT (first 1000 chars):"
echo "$GRANT" | head -c 1000
echo ""
echo "  [debug] Raw GRANT (last 500 chars):"
echo "$GRANT" | tail -c 500
echo ""
if echo "$GRANT" | grep -qi "not found"; then
    echo "[FAIL] Session does not see the grant; wrapper did not write to session storage."
    echo "Raw: $GRANT"
    exit 1
fi

# Post-amino-oneof-name fix: --output json runs through the aminojson
# encoder, which wraps the Grant.payload oneof as
# `.grant.Payload = { type: "<amino_name>", value: { <oneof_field>: {...} } }`
# rather than flattening it. Read from that shape; fall back to the
# proto-JSON-flat shape (`.grant.recurring_pull`) so the test still
# works if a future SDK update flips back.
GRANT_TYPE=$(echo "$GRANT" | jq -r '.grant.type')
GRANTER=$(echo "$GRANT" | jq -r '.grant.granter')
GRANTEE=$(echo "$GRANT" | jq -r '.grant.grantee')
GRANT_STATUS=$(echo "$GRANT" | jq -r '.grant.status')
RP_AMT_DENOM=$(echo "$GRANT" | jq -r '.grant.Payload.value.recurring_pull.amount_per_period.denom // .grant.recurring_pull.amount_per_period.denom // empty')
RP_AMT=$(echo "$GRANT" | jq -r '.grant.Payload.value.recurring_pull.amount_per_period.amount // .grant.recurring_pull.amount_per_period.amount // empty')
RP_PERIOD=$(echo "$GRANT" | jq -r '.grant.Payload.value.recurring_pull.period_seconds // .grant.recurring_pull.period_seconds // empty')

if [ "$GRANT_TYPE" != "GRANT_TYPE_RECURRING_PULL" ]; then
    echo "[FAIL] grant.type='$GRANT_TYPE' (expected GRANT_TYPE_RECURRING_PULL)"
    exit 1
fi
if [ "$GRANTER" != "$POLICY_ADDR" ]; then
    echo "[FAIL] grant.granter='$GRANTER' (expected $POLICY_ADDR)"
    exit 1
fi
if [ "$GRANTEE" != "$CAROL_ADDR" ]; then
    echo "[FAIL] grant.grantee='$GRANTEE' (expected $CAROL_ADDR)"
    exit 1
fi
if [ "$GRANT_STATUS" != "GRANT_STATUS_ACTIVE" ]; then
    echo "[FAIL] grant.status='$GRANT_STATUS' (expected GRANT_STATUS_ACTIVE)"
    exit 1
fi
if [ "$RP_AMT_DENOM" != "$BOND_DENOM" ] || [ "$RP_AMT" != "$AMOUNT" ]; then
    echo "[FAIL] grant.recurring_pull.amount_per_period=${RP_AMT}${RP_AMT_DENOM} (expected ${AMOUNT}${BOND_DENOM})"
    exit 1
fi
if [ "$RP_PERIOD" != "$PERIOD_SECONDS" ]; then
    echo "[FAIL] grant.recurring_pull.period_seconds=$RP_PERIOD (expected $PERIOD_SECONDS)"
    exit 1
fi
echo "[ OK ] session.Grant projection matches: type=$GRANT_TYPE, granter=$GRANTER, grantee=$GRANTEE, amt=${RP_AMT}${RP_AMT_DENOM}, period=$RP_PERIOD"

# --- 3. LIST QUERIES — granter + grantee indexes -----------------------------
echo ""
echo "STEP 3: Verifying session list-by-granter and list-by-grantee indexes..."

BY_GRANTER=$($BINARY query session grants-by-granter "$POLICY_ADDR" --output json 2>&1)
HAS_IN_GRANTER=$(echo "$BY_GRANTER" | jq -r --arg id "$SCHEDULE_ID" '.grants[]? | select(.id == $id) | .id' | head -n1)
if [ "$HAS_IN_GRANTER" != "$SCHEDULE_ID" ]; then
    echo "[FAIL] Grant id=$SCHEDULE_ID not found in grants-by-granter listing for $POLICY_ADDR."
    echo "Raw: $BY_GRANTER"
    exit 1
fi
echo "[ OK ] grants-by-granter includes id=$SCHEDULE_ID"

BY_GRANTEE=$($BINARY query session grants-by-grantee "$CAROL_ADDR" --output json 2>&1)
HAS_IN_GRANTEE=$(echo "$BY_GRANTEE" | jq -r --arg id "$SCHEDULE_ID" '.grants[]? | select(.id == $id) | .id' | head -n1)
if [ "$HAS_IN_GRANTEE" != "$SCHEDULE_ID" ]; then
    echo "[FAIL] Grant id=$SCHEDULE_ID not found in grants-by-grantee listing for $CAROL_ADDR."
    echo "Raw: $BY_GRANTEE"
    exit 1
fi
echo "[ OK ] grants-by-grantee includes id=$SCHEDULE_ID"

# --- 4. CANCEL VIA COMMONS WRAPPER -------------------------------------------
echo ""
echo "STEP 4: Cancelling via commons wrapper..."

cat > "$PROPOSAL_DIR/cancel_visibility.json" <<EOF
{
  "policy_address": "$POLICY_ADDR",
  "messages": [
    {
      "@type": "/sparkdream.commons.v1.MsgCancelRecurringSpend",
      "authority": "$POLICY_ADDR",
      "id": "$SCHEDULE_ID"
    }
  ],
  "metadata": "cancel"
}
EOF

CANCEL_SUBMIT=$($BINARY tx commons submit-proposal "$PROPOSAL_DIR/cancel_visibility.json" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees "$PROPOSAL_FEE" --output json)
CANCEL_HASH=$(echo "$CANCEL_SUBMIT" | jq -r '.txhash')
sleep 3
CANCEL_PROP=$($BINARY query tx "$CANCEL_HASH" --output json | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' | tr -d '"')
$BINARY tx commons vote-proposal "$CANCEL_PROP" yes --from alice -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} > /dev/null; sleep 3
$BINARY tx commons vote-proposal "$CANCEL_PROP" yes --from bob   -y --chain-id $CHAIN_ID --keyring-backend test --fees 5000${BOND_DENOM} > /dev/null; sleep 3
$BINARY tx commons execute-proposal "$CANCEL_PROP" --from alice -y --chain-id $CHAIN_ID --keyring-backend test --gas 2000000 --fees 5000000${BOND_DENOM} > /dev/null
sleep 3

CANCEL_STATUS=$($BINARY query commons get-proposal "$CANCEL_PROP" --output json | jq -r '.proposal.status')
if [ "$CANCEL_STATUS" != "PROPOSAL_STATUS_EXECUTED" ]; then
    echo "[FAIL] Cancel proposal did not execute."
    exit 1
fi
echo "[ OK ] Cancel proposal executed."

# --- 5. POST-CANCEL: GRANT DELETED FROM SESSION (M9 semantic break) ----------
echo ""
echo "STEP 5: Confirming the grant is gone from session storage..."

GRANT_AFTER=$($BINARY query session grant "$SCHEDULE_ID" --output json 2>&1 || true)
if ! echo "$GRANT_AFTER" | grep -qi "not found"; then
    echo "[FAIL] Session still returns the grant post-cancel; deletion did not propagate."
    echo "Raw: $GRANT_AFTER"
    exit 1
fi
echo "[ OK ] session.GetGrant($SCHEDULE_ID) returns NotFound."

# Grant should also be absent from the list indexes.
BY_GRANTER_AFTER=$($BINARY query session grants-by-granter "$POLICY_ADDR" --output json 2>&1)
STILL_IN_GRANTER=$(echo "$BY_GRANTER_AFTER" | jq -r --arg id "$SCHEDULE_ID" '.grants[]? | select(.id == $id) | .id' | head -n1)
if [ "$STILL_IN_GRANTER" == "$SCHEDULE_ID" ]; then
    echo "[FAIL] Grant id=$SCHEDULE_ID still listed under grants-by-granter post-cancel."
    exit 1
fi
echo "[ OK ] grants-by-granter no longer includes id=$SCHEDULE_ID"

BY_GRANTEE_AFTER=$($BINARY query session grants-by-grantee "$CAROL_ADDR" --output json 2>&1)
STILL_IN_GRANTEE=$(echo "$BY_GRANTEE_AFTER" | jq -r --arg id "$SCHEDULE_ID" '.grants[]? | select(.id == $id) | .id' | head -n1)
if [ "$STILL_IN_GRANTEE" == "$SCHEDULE_ID" ]; then
    echo "[FAIL] Grant id=$SCHEDULE_ID still listed under grants-by-grantee post-cancel."
    exit 1
fi
echo "[ OK ] grants-by-grantee no longer includes id=$SCHEDULE_ID"

# Commons-side query also returns NotFound (mirror confirmation).
COMMONS_AFTER=$($BINARY query commons get-recurring-spend "$SCHEDULE_ID" --output json 2>&1 || true)
if ! echo "$COMMONS_AFTER" | grep -qi "not found"; then
    echo "[FAIL] Commons projection still returns the cancelled schedule."
    exit 1
fi
echo "[ OK ] commons.GetRecurringSpend($SCHEDULE_ID) returns NotFound."

echo ""
echo "[ OK ] Cross-module visibility test PASSED."
