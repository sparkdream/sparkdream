#!/bin/bash
# ------------------------------------------------------------------
# P3.3 -- peer setup for the Mastodon link, on a LOCAL chain.
#
#   1. register the instance as an ActivityPub peer, then activate it
#      (a PENDING peer is inert: no bridge, no content)
#   2. inbound policy: blog_post + blog_reply, every field written (the
#      policy is stored whole, so omitted fields become zero)
#   3. bond the bridge operator on federation-bridge-activitypub at the
#      service type's min bond (1000 SPARK = 1000000000, not 10^12)
#   4. bond the verifier as ROLE_TYPE_FEDERATION_VERIFIER (>= 500 DREAM)
#      from an ESTABLISHED persona, checked BEFORE bonding
#
# Built on test/federation/peer_fixtures.sh, whose proposals are voted by
# the local genesis council (alice/bob/carol) -- so this runs against a
# local chain after test/federation/setup_test_accounts.sh. On devnet the
# same four steps apply, but the proposals are voted by the real council
# and committee members.
#
# Idempotent: every step checks chain state first.
#
# Env:
#   PEER_ID        chain peer id (default: localhost). Must match
#                  ^[a-z0-9][a-z0-9.-]{1,62}[a-z0-9]$ -- no port, so a
#                  localhost:3000 instance registers as "localhost".
#   ENDPOINT       bridge endpoint (default: http://localhost:3000)
#   OPERATOR_KEY   bridge operator key (default: operator1)
#   VERIFIER_KEY   verifier key (default: bob -- ESTABLISHED at genesis;
#                  invited accounts start at trust level 0 and cannot bond)
#   VERIFIER_BOND  DREAM bond in micro-units (default: 500000000 = 500)
#   INBOUND_RATE_LIMIT  posts per rate_limit_window the peer accepts
#                  (default: 100). The window is a chain param (devnet 1h,
#                  testnet 12h, mainnet 24h) and the limit is PER PEER, so
#                  every mirrored author on the instance shares it. Size it
#                  to:  authors x posts-per-author-per-window x 1.3 (edits
#                  re-anchor) + new-authors-per-window x SDA_BACKFILL (each
#                  newly seen author costs one outbox pass). A submission
#                  over the limit is deferred and retried with backoff (not
#                  lost, and other peers keep flowing), but every rejected
#                  attempt still pays its fee and delays the post.
# ------------------------------------------------------------------
set -euo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
FED_DIR="$( cd "$SCRIPT_DIR/.." && pwd )"

BINARY="sparkdreamd"
CHAIN_ID="sparkdream"
PEER_ID="${PEER_ID:-localhost}"
ENDPOINT="${ENDPOINT:-http://localhost:3000}"
OPERATOR_KEY="${OPERATOR_KEY:-operator1}"
VERIFIER_KEY="${VERIFIER_KEY:-bob}"
VERIFIER_BOND="${VERIFIER_BOND:-500000000}"
INBOUND_RATE_LIMIT="${INBOUND_RATE_LIMIT:-100}"

[[ -f "$FED_DIR/.test_env" ]] || { echo "run test/federation/setup_test_accounts.sh first" >&2; exit 1; }
# shellcheck disable=SC1091
source "$FED_DIR/.test_env"
PROPOSAL_DIR="$SCRIPT_DIR/proposals"
# shellcheck disable=SC1091
set +u; source "$FED_DIR/peer_fixtures.sh"; set -u

OPERATOR_ADDR=$($BINARY keys show "$OPERATOR_KEY" -a --keyring-backend test)
VERIFIER_ADDR=$($BINARY keys show "$VERIFIER_KEY" -a --keyring-backend test)
[[ "$OPERATOR_ADDR" != "$VERIFIER_ADDR" ]] || { echo "operator and verifier must differ (ErrSelfVerification)" >&2; exit 1; }

echo "=== 1/4 peer $PEER_ID: register + activate ==="
register_test_peer "$PEER_ID" "PEER_TYPE_ACTIVITYPUB" "Mastodon Test ($ENDPOINT)" "" "yes"

echo "=== 2/4 inbound policy: blog_post, blog_reply, $INBOUND_RATE_LIMIT per window ==="
set_peer_policy "$PEER_ID" "blog_post,blog_reply" "" "" "false" "false" "$INBOUND_RATE_LIMIT"

echo "=== 3/4 bridge: $OPERATOR_KEY -> $PEER_ID ($ENDPOINT) ==="
register_test_bridge "$OPERATOR_KEY" "$OPERATOR_ADDR" "$PEER_ID" "activitypub" "$ENDPOINT"

echo "=== 4/4 verifier: $VERIFIER_KEY bonds $VERIFIER_BOND DREAM ==="
BONDED=$($BINARY query rep bonded-role federation-verifier "$VERIFIER_ADDR" --output json 2>/dev/null \
  | jq -r '.bonded_role.current_bond // empty' || true)
if [[ -n "$BONDED" && "$BONDED" != "0" ]]; then
  echo "  already bonded: $BONDED"
else
  TRUST=$($BINARY query rep get-member "$VERIFIER_ADDR" --output json | jq -r '.member.trust_level // "TRUST_LEVEL_NEW"')
  case "$TRUST" in
    TRUST_LEVEL_ESTABLISHED|TRUST_LEVEL_TRUSTED|TRUST_LEVEL_CORE) echo "  $VERIFIER_KEY is $TRUST" ;;
    *) echo "  $VERIFIER_KEY is $TRUST; federation verifiers must be ESTABLISHED or above" >&2; exit 1 ;;
  esac
  TX=$($BINARY tx rep bond-role federation-verifier "$VERIFIER_BOND" --from "$VERIFIER_KEY" \
    --chain-id "$CHAIN_ID" --keyring-backend test --fees "5000${BOND_DENOM}" -y --output json)
  HASH=$(echo "$TX" | jq -r '.txhash')
  for _ in $(seq 1 15); do
    RES=$($BINARY query tx "$HASH" --output json 2>/dev/null || true)
    [[ -n "$RES" ]] && break
    sleep 2
  done
  [[ "$(echo "$RES" | jq -r '.code')" == "0" ]] || { echo "  bond-role failed: $(echo "$RES" | jq -r '.raw_log')" >&2; exit 1; }
fi

echo ""
echo "=== state ==="
echo "  peer:     $($BINARY query federation get-peer "$PEER_ID" --output json | jq -r '.peer.status')"
echo "  policy:   $($BINARY query federation get-peer-policy "$PEER_ID" --output json | jq -c '.policy | {inbound_content_types, inbound_rate_limit_per_epoch}')"
echo "  bridge:   $($BINARY query service operator "$OPERATOR_ADDR" federation-bridge-activitypub --output json | jq -c '.operator | {status, bond_amount}')"
echo "  verifier: $($BINARY query rep bonded-role federation-verifier "$VERIFIER_ADDR" --output json | jq -c '.bonded_role | {bond_status, current_bond}')"
