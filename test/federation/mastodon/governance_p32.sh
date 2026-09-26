#!/bin/bash
# ------------------------------------------------------------------
# P3.2 -- one governance proposal, two messages, for the Mastodon link.
#
#   1. x/federation MsgUpdateParams: verification_window,
#      challenge_window, arbiter_quorum. None of these is in the
#      Operations-Committee subset, so they need x/gov.
#   2. x/service MsgUpdateServiceTypeConfig: a short
#      unbonding_period_blocks on federation-bridge-activitypub. Its only
#      accepted authority is the x/gov module address.
#
# BOTH messages are FULL REPLACEMENT: any field left out is written as
# zero. So this script never hand-writes either object. It reads the
# chain's current Params and ServiceTypeConfig, changes only the target
# fields, and submits the whole object back. The three LegacyDec params
# fields come back from the query as padded integers and are converted
# to decimal strings first, or the message fails to unmarshal.
#
# Env (defaults are the intended devnet values):
#   BINARY               sparkdreamd (default: on PATH)
#   NODE                 --node (default: the binary's own config)
#   CHAIN_ID             (default: sparkdream-dev-1)
#   FROM                 proposer key (default: alice)
#   KEYRING              keyring backend (default: test)
#   BOND_DENOM           fee/deposit denom (default: usparz.sparkdreamdev)
#   DEPOSIT              proposal deposit amount (default: 100000000)
#   EXPEDITED            true|false (default: true)
#   VERIFICATION_WINDOW  Go duration (default: 24h0m0s)
#   CHALLENGE_WINDOW     Go duration (default: 24h0m0s)
#   ARBITER_QUORUM       >= 2 (default: 2)
#   UNBONDING_BLOCKS     federation-bridge-activitypub unbonding (default: 720)
#   VOTERS               space-separated keys that vote YES (local chains
#                        only; on devnet validators vote, so leave empty)
#   WAIT                 seconds to wait for the result (default: 90)
#   DRY_RUN=1            write the proposal JSON and stop
#
# Local example (short windows so a record reaches VERIFIED in minutes):
#   CHAIN_ID=sparkdream BOND_DENOM=uspark VOTERS="alice bob" \
#   VERIFICATION_WINDOW=30m0s CHALLENGE_WINDOW=5m0s UNBONDING_BLOCKS=10 \
#   ./governance_p32.sh
# ------------------------------------------------------------------
set -euo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

BINARY="${BINARY:-sparkdreamd}"
CHAIN_ID="${CHAIN_ID:-sparkdream-dev-1}"
FROM="${FROM:-alice}"
KEYRING="${KEYRING:-test}"
BOND_DENOM="${BOND_DENOM:-usparz.sparkdreamdev}"
DEPOSIT="${DEPOSIT:-100000000}"
EXPEDITED="${EXPEDITED:-true}"
VERIFICATION_WINDOW="${VERIFICATION_WINDOW:-24h0m0s}"
CHALLENGE_WINDOW="${CHALLENGE_WINDOW:-24h0m0s}"
ARBITER_QUORUM="${ARBITER_QUORUM:-2}"
UNBONDING_BLOCKS="${UNBONDING_BLOCKS:-720}"
VOTERS="${VOTERS:-}"
WAIT="${WAIT:-90}"
SERVICE_TYPE="federation-bridge-activitypub"

NODE_FLAGS=()
[[ -n "${NODE:-}" ]] && NODE_FLAGS=(--node "$NODE")
TX_FLAGS=("${NODE_FLAGS[@]+"${NODE_FLAGS[@]}"}" --chain-id "$CHAIN_ID" --keyring-backend "$KEYRING"
  --fees "5000${BOND_DENOM}" -y --output json)

q() { "$BINARY" query "$@" "${NODE_FLAGS[@]+"${NODE_FLAGS[@]}"}" --output json; }

(( ARBITER_QUORUM >= 2 )) || { echo "ARBITER_QUORUM must be >= 2 (validation floor)" >&2; exit 2; }

GOV_ADDR=$(q auth module-account gov | jq -r '.account.base_account.address // .account.value.address')
FED_PARAMS=$(q federation params | jq '.params')
SVC_CFG=$(q service service-type "$SERVICE_TYPE" | jq '.config')
[[ -n "$GOV_ADDR" && "$FED_PARAMS" != "null" && "$SVC_CFG" != "null" ]] || {
  echo "could not read gov address / federation params / $SERVICE_TYPE config" >&2; exit 1; }

# LegacyDec round-trip: the query prints the internal 18-digit integer form.
FED_NEW=$(echo "$FED_PARAMS" | python3 -c "
import json, sys
d = json.load(sys.stdin)
for f in ('operator_reward_inflation_share', 'operator_reward_pool_overflow_burn_ratio', 'max_unverified_rate'):
    if d.get(f) is not None:
        s = str(d[f]).replace('.', '')
        if len(s) <= 18:
            s = s.zfill(19)
        d[f] = (s[:-18].lstrip('0') or '0') + '.' + s[-18:]
d['verification_window'] = '$VERIFICATION_WINDOW'
d['challenge_window'] = '$CHALLENGE_WINDOW'
d['arbiter_quorum'] = $ARBITER_QUORUM
json.dump(d, sys.stdout)
")
SVC_NEW=$(echo "$SVC_CFG" | jq --arg ub "$UNBONDING_BLOCKS" '.unbonding_period_blocks = $ub')

mkdir -p "$SCRIPT_DIR/proposals"
PROP_FILE="$SCRIPT_DIR/proposals/p32_${CHAIN_ID}.json"
jq -n --arg auth "$GOV_ADDR" --arg deposit "${DEPOSIT}${BOND_DENOM}" \
  --argjson fed "$FED_NEW" --argjson svc "$SVC_NEW" --argjson exp "$EXPEDITED" '
{
  "messages": [
    {"@type": "/sparkdream.federation.v1.MsgUpdateParams", "authority": $auth, "params": $fed},
    {"@type": "/sparkdream.service.v1.MsgUpdateServiceTypeConfig", "authority": $auth, "config": $svc}
  ],
  "deposit": $deposit,
  "title": "Mastodon live link: federation windows and bridge unbonding",
  "summary": "P3.2: widen the verification/challenge windows for a human-paced live link, set arbiter_quorum, and shorten federation-bridge-activitypub unbonding. Both messages are full replacements built from current chain state.",
  "expedited": $exp
}' > "$PROP_FILE"
echo "proposal written: $PROP_FILE"
echo "  verification_window: $(echo "$FED_PARAMS" | jq -r .verification_window) -> $VERIFICATION_WINDOW"
echo "  challenge_window:    $(echo "$FED_PARAMS" | jq -r .challenge_window) -> $CHALLENGE_WINDOW"
echo "  arbiter_quorum:      $(echo "$FED_PARAMS" | jq -r .arbiter_quorum) -> $ARBITER_QUORUM"
echo "  $SERVICE_TYPE unbonding_period_blocks: $(echo "$SVC_CFG" | jq -r .unbonding_period_blocks) -> $UNBONDING_BLOCKS"
[[ -n "${DRY_RUN:-}" ]] && exit 0

TX=$("$BINARY" tx gov submit-proposal --gas 1000000 "$PROP_FILE" --from "$FROM" "${TX_FLAGS[@]}")
TXHASH=$(echo "$TX" | jq -r '.txhash // empty')
[[ -n "$TXHASH" && "$(echo "$TX" | jq -r '.code')" == "0" ]] || { echo "submit rejected: $TX" >&2; exit 1; }

PROP_ID=""
for _ in $(seq 1 20); do
  RES=$(q tx "$TXHASH" 2>/dev/null || true)
  if [[ -n "$RES" ]]; then
    [[ "$(echo "$RES" | jq -r '.code')" == "0" ]] || { echo "submit failed: $(echo "$RES" | jq -r .raw_log)" >&2; exit 1; }
    PROP_ID=$(echo "$RES" | jq -r '.events[] | select(.type=="submit_proposal").attributes[] | select(.key=="proposal_id").value' | head -n1)
    break
  fi
  sleep 2
done
[[ -n "$PROP_ID" ]] || { echo "no proposal id for tx $TXHASH" >&2; exit 1; }
echo "proposal $PROP_ID submitted (tx $TXHASH)"

for VOTER in $VOTERS; do
  "$BINARY" tx gov vote "$PROP_ID" yes --from "$VOTER" "${TX_FLAGS[@]}" > /dev/null
  echo "  $VOTER voted yes"
  sleep 3
done

STATUS=""
for _ in $(seq 1 $(( WAIT / 5 ))); do
  STATUS=$(q gov proposal "$PROP_ID" | jq -r '.proposal.status')
  case "$STATUS" in PROPOSAL_STATUS_PASSED|PROPOSAL_STATUS_REJECTED|PROPOSAL_STATUS_FAILED) break ;; esac
  sleep 5
done
echo "proposal $PROP_ID: $STATUS"
if [[ "$STATUS" != "PROPOSAL_STATUS_PASSED" ]]; then
  [[ "$STATUS" == PROPOSAL_STATUS_VOTING_PERIOD ]] && echo "still voting; re-check with: $BINARY query gov proposal $PROP_ID" >&2
  exit 1
fi

AFTER_FED=$(q federation params | jq -c '.params | {verification_window, challenge_window, arbiter_quorum}')
AFTER_UB=$(q service service-type "$SERVICE_TYPE" | jq -r '.config.unbonding_period_blocks')
echo "now: $AFTER_FED unbonding_period_blocks=$AFTER_UB"
# Full-replacement guard: nothing else in either object may have moved.
DRIFT=$(jq -n --argjson a "$FED_PARAMS" --argjson b "$(q federation params | jq '.params')" \
  '[$a | keys[] as $k | select($k != "verification_window" and $k != "challenge_window" and $k != "arbiter_quorum" and $a[$k] != $b[$k]) | $k]')
[[ "$DRIFT" == "[]" ]] || { echo "UNEXPECTED federation params drift: $DRIFT" >&2; exit 1; }
echo "PASS"
