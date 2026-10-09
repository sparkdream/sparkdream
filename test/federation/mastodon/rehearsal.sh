#!/bin/bash
# ------------------------------------------------------------------
# P4 — manual on-chain rehearsal of the full Mastodon anchoring path.
# Zero new code on-chain: the whole pipeline against a REAL AS2 object,
# happy path only. Run this after:
#
#   - the chain runs a binary carrying P0 (P3.0),
#   - the instance exists and AUTHORIZED_FETCH=false (P3.1),
#   - the governance proposal (P3.2, governance_p32.sh) set the windows,
#   - the peer is registered/active with inbound blog_post+blog_reply
#     and a bridge is bonded (P3.3, peer_setup_p33.sh).
#
# DO NOT run a deliberate wrong-hash submission here — that is P7, and
# it is gated on P0.1 being live.
#
# Env:
#   STATUS_URI  the AS2 id of a real status (required). On Mastodon 4.7+
#               that is http(s)://HOST/ap/users/<id>/statuses/<id>; the
#               API returns it as the status's `uri`. The status must
#               carry #cc0 or #publicdomain in its text: the chain
#               accepts only public-domain content.
#   BINARY      sparkdreamd (default: GOPATH bin)
#   NODE        --node flag (default: devnet RPC)
#   CHAIN_ID    (default: sparkdream-dev-1)
#   BOND_DENOM  fee denom (default: usparz.sparkdreamdev)
#   FROM_SUBMIT bridge operator key name in the keyring
#   FROM_VERIFY verifier key name (MUST differ from the operator —
#               ErrSelfVerification, 2352, otherwise)
#   FROM_OPS    Operations-Committee member key (for moderate-content)
#   PEER_ID     chain peer id (default: the URI's host, port stripped)
#   CREATOR     @user@host (default: resolved from the AS2 actor's
#               preferredUsername; 4.7 actor ids carry no username)
#   KEYRING     keyring backend (default test)
#   APCANON     path to an apcanon binary (default: go run from repo)
#   ALLOW_PRIVATE=1  let apcanon fetch a LOCAL instance (http://,
#               loopback). Bring-up only, never a real peer.
#   KEY_ID / KEY_PATH  HTTP-signature credentials for secure-mode peers
#   HASH_RULE   ap-canonical-v2 (default) or ap-canonical-v1
#   VERIFY_TIMEOUT  seconds to wait for VERIFIED (default: challenge
#               window + 120)
#
# Acceptance: one VERIFIED record originating from a real Mastodon
# status, the verifier's committed bond observably released after the
# challenge window (the live confirmation of P0.1), and one
# MsgModerateContent exercised.
# ------------------------------------------------------------------
set -euo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_DIR="$( cd "$SCRIPT_DIR/../../.." && pwd )"

BINARY="${BINARY:-$(go env GOPATH)/bin/sparkdreamd}"
NODE="${NODE:-https://rpc-dev.sparkdream.io:443}"
FROM_SUBMIT="${FROM_SUBMIT:-operator}"
FROM_VERIFY="${FROM_VERIFY:-verifier}"
FROM_OPS="${FROM_OPS:-ops}"
STATUS_URI="${STATUS_URI:?set STATUS_URI to the AS2 id, e.g. https://INSTANCE/ap/users/1/statuses/2}"
KEYRING="${KEYRING:-test}"
# Peer ids reject ':' — a localhost:3000 instance is peer "localhost".
PEER_ID="${PEER_ID:-$(printf '%s' "$STATUS_URI" | sed -E 's#^https?://([^/:]+).*#\1#')}"
BOND_DENOM="${BOND_DENOM:-usparz.sparkdreamdev}"
CHAIN_ID="${CHAIN_ID:-sparkdream-dev-1}"
KEY_ID="${KEY_ID:-}"
# The rule the hash is computed under, recorded in protocol_metadata so the
# verifier recomputes with the same one. v2 also digests every attachment.
HASH_RULE="${HASH_RULE:-ap-canonical-v2}"
KEY_PATH="${KEY_PATH:-}"

TXFLAGS=(--node "$NODE" --chain-id "$CHAIN_ID" --keyring-backend "$KEYRING" --fees "7500${BOND_DENOM}" -y --output json)
QFLAGS=(--node "$NODE" --output json)
# Flags BEFORE the target: Go's flag package stops at the first non-flag
# argument, so trailing flags are silently dropped.
SIGN=( ${KEY_ID:+--key-id "$KEY_ID"} ${KEY_PATH:+--key "$KEY_PATH"} ${ALLOW_PRIVATE:+--allow-private} --rule "$HASH_RULE" )
if [[ -n "${APCANON:-}" ]]; then APCANON_BIN=("$APCANON"); else APCANON_BIN=(go run "$PROJECT_DIR/tools/apcanon/cmd/apcanon"); fi

step() { printf '\n=== %s ===\n' "$*"; }
die()  { echo "FAIL: $*" >&2; exit 1; }

# run_tx <description> <tx args...>: broadcast, then wait for inclusion.
# A -y broadcast is SYNC mode, which reports CheckTx only — no events and
# no DeliverTx outcome — so everything downstream reads the included tx.
run_tx() {
  local what=$1; shift
  local out hash res
  out=$("$BINARY" tx "$@" "${TXFLAGS[@]}")
  hash=$(echo "$out" | jq -r '.txhash // empty')
  [[ -n "$hash" && "$(echo "$out" | jq -r '.code')" == "0" ]] || die "$what rejected at CheckTx: $(echo "$out" | jq -r '.raw_log // .')"
  for _ in $(seq 1 30); do
    res=$("$BINARY" query tx "$hash" "${QFLAGS[@]}" 2>/dev/null || true)
    [[ -n "$res" ]] && break
    sleep 2
  done
  [[ -n "$res" ]] || die "$what: tx $hash not included"
  [[ "$(echo "$res" | jq -r '.code')" == "0" ]] || die "$what failed: $(echo "$res" | jq -r '.raw_log')"
  echo "  $what: ok (tx $hash)" >&2
  echo "$res"
}

# Go duration (e.g. 5m0s, 24h0m0s, 90s) -> seconds.
dur_s() {
  local d=$1 total=0
  [[ $d =~ ([0-9]+)h ]] && total=$(( total + BASH_REMATCH[1] * 3600 ))
  [[ $d =~ ([0-9]+)m ]] && total=$(( total + BASH_REMATCH[1] * 60 ))
  [[ $d =~ ([0-9.]+)s$ ]] && total=$(( total + ${BASH_REMATCH[1]%.*} ))
  echo "$total"
}

# proto3 JSON omits zero values: PENDING_VERIFICATION is enum 0, and the
# first record on a chain has id 0, so both can be absent.
content_status() { "$BINARY" query federation get-federated-content "$1" "${QFLAGS[@]}" \
  | jq -r '.content.status // "FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION"'; }
vrecord() { "$BINARY" query federation get-verification-record "$1" "${QFLAGS[@]}" | jq -c '.record'; }

step "1/6 $HASH_RULE hash of $STATUS_URI"
HASH_HEX=$("${APCANON_BIN[@]}" hash "${SIGN[@]+"${SIGN[@]}"}" "$STATUS_URI")
HASH_B64=$(printf '%s' "$HASH_HEX" | xxd -r -p | base64 -w0)
echo "hash(hex)=$HASH_HEX"
echo "hash(b64)=$HASH_B64   # --content-hash takes base64, not hex"
# The chain accepts only public-domain posts: the status must carry #cc0
# (CC0-1.0) or #publicdomain (PDM-1.0) in its text, as the bridge requires.
LICENSE=$("${APCANON_BIN[@]}" license "${SIGN[@]+"${SIGN[@]}"}" "$STATUS_URI") \
  || die "$STATUS_URI has no #cc0 or #publicdomain hashtag; post one that does"
echo "license=$LICENSE"

# Field mapping as the bridge daemon does it (cmd/sdapbridge buildMsg), read from the same AS2 object the
# bridge would use. Only the hash above is load-bearing; these fields are
# presentation, so a plain curl is enough.
OBJ=$(curl -sSf -H 'Accept: application/activity+json' "$STATUS_URI")
ACTOR=$(echo "$OBJ" | jq -r '.attributedTo')
REMOTE_ID=$(printf '%s' "$STATUS_URI" | sed -E 's#.*/([^/]+)/?$#\1#')
if [[ -z "${CREATOR:-}" ]]; then
  USERNAME=$(curl -sSf -H 'Accept: application/activity+json' "$ACTOR" | jq -r '.preferredUsername')
  CREATOR="@${USERNAME}@${PEER_ID}"
fi
CONTENT_TYPE=blog_post
[[ "$(echo "$OBJ" | jq -r '.inReplyTo // empty')" != "" ]] && CONTENT_TYPE=blog_reply
CREATED_AT=$(date -u -d "$(echo "$OBJ" | jq -r '.published')" +%s)
# Attachment digests, in order, as the rule computed them (v2); a link is
# recorded only for a file with a sha256 digest, like the bridge does.
DIGESTS=$("${APCANON_BIN[@]}" canon "${SIGN[@]+"${SIGN[@]}"}" "$STATUS_URI" | jq -c '[(.attachment // [])[] | .digest]')
META=$(echo "$OBJ" | jq -c --arg uri "$STATUS_URI" --arg rule "$HASH_RULE" --argjson digests "$DIGESTS" '
  [(.attachment // [])[]] as $all
  | {hash_rule:$rule, ap_type:.type, actor:.attributedTo, object_id:$uri, in_reply_to:.inReplyTo,
     visibility:"public", updated:.updated, content_url:.url, rehearsal:true,
     attachments: [range(0; $all|length) as $i | $all[$i] | select((.url|type)=="string" and (.url|test("^https?://")))
       | {media_type: .mediaType, name, width, height, blurhash, digest: $digests[$i]}
         + (if ($digests[$i] // "" | startswith("sha256:")) then {url} else {} end)
       | with_entries(select(.value != null))]}')
# --protocol-metadata is a bytes flag: file path, hex or base64, never raw JSON.
echo "creator=$CREATOR type=$CONTENT_TYPE remote_id=$REMOTE_ID published=$CREATED_AT"

step "2/6 submit-federated-content as $FROM_SUBMIT (nine positionals)"
RES=$(run_tx submit federation submit-federated-content \
  "$PEER_ID" \
  "$REMOTE_ID" \
  "$CONTENT_TYPE" \
  "$CREATOR" \
  "${CREATOR#@}" \
  "$(echo "$OBJ" | jq -r '.summary // ""')" \
  "$(echo "$OBJ" | jq -r '.content // ""' | head -c 4096)" \
  "$STATUS_URI" \
  "$CREATED_AT" \
  --content-hash "$HASH_B64" \
  --license "$LICENSE" \
  --protocol-metadata "$(printf '%s' "$META" | base64 -w0)" \
  --from "$FROM_SUBMIT")
CONTENT_ID=$(echo "$RES" | jq -r '[.events[] | select(.type=="federated_content_received").attributes[] | select(.key=="content_id").value][0] // empty')
[[ -n "$CONTENT_ID" ]] || die "could not extract content_id"
echo "content_id=$CONTENT_ID status=$(content_status "$CONTENT_ID")"

VERIFIER_ADDR=$("$BINARY" keys show "$FROM_VERIFY" --keyring-backend "$KEYRING" -a)
bond_state() { "$BINARY" query rep bonded-role federation-verifier "$VERIFIER_ADDR" "${QFLAGS[@]}" \
  | jq -c '.bonded_role | {current_bond, total_committed_bond: (.total_committed_bond // "0")}'; }
BOND_BEFORE=$(bond_state)

step "3/6 verify-content as $FROM_VERIFY (different account, same hash)"
run_tx verify federation verify-content "$CONTENT_ID" --content-hash "$HASH_B64" --from "$FROM_VERIFY" > /dev/null
BOND_DURING=$(bond_state)
echo "status=$(content_status "$CONTENT_ID")"
echo "verifier bond before=$BOND_BEFORE during=$BOND_DURING"

step "4/6 wait out the challenge window"
# VERIFIED is immediate on a matching verify-content. The challenge window
# is what follows: the verifier's committed bond stays locked until it
# closes and the EndBlocker settles the verification record.
PARAMS=$("$BINARY" query federation params "${QFLAGS[@]}" | jq -c '.params | {verification_window,challenge_window,arbiter_quorum,content_ttl}')
echo "params: $PARAMS"
REC=$(vrecord "$CONTENT_ID")
echo "verification record: $REC"
ENDS=$(echo "$REC" | jq -r '.challenge_window_ends')
TIMEOUT="${VERIFY_TIMEOUT:-$(( $(dur_s "$(echo "$PARAMS" | jq -r .challenge_window)") + 120 ))}"
echo "challenge window ends at $ENDS ($(date -u -d "@$ENDS" +%H:%M:%SZ)); polling up to ${TIMEOUT}s for settlement"
START=$(date +%s)
while :; do
  OUTCOME=$(vrecord "$CONTENT_ID" | jq -r '.outcome // "VERIFICATION_OUTCOME_PENDING"')
  [[ "$OUTCOME" != "VERIFICATION_OUTCOME_PENDING" ]] && break
  (( $(date +%s) - START > TIMEOUT )) && die "verification still $OUTCOME after ${TIMEOUT}s"
  sleep 10
done
echo "settled after $(( $(date +%s) - START ))s: outcome=$OUTCOME"

step "5/6 confirm VERIFIED + bond released (live proof of P0.1)"
"$BINARY" query federation get-federated-content "$CONTENT_ID" "${QFLAGS[@]}" | jq -c '.content | {status,submitted_by,content_uri,expires_at}'
ST=$(content_status "$CONTENT_ID")
[[ "$ST" == "FEDERATED_CONTENT_STATUS_VERIFIED" ]] || die "content is $ST after the window, want VERIFIED"
BOND_AFTER=$(bond_state)
echo "verifier bond before=$BOND_BEFORE during=$BOND_DURING after=$BOND_AFTER"
[[ "$(echo "$BOND_AFTER" | jq -r .total_committed_bond)" == "$(echo "$BOND_BEFORE" | jq -r .total_committed_bond)" ]] \
  || die "verifier committed bond not released"
echo "committed bond released"
OPERATOR_ADDR=$("$BINARY" keys show "$FROM_SUBMIT" --keyring-backend "$KEYRING" -a)
"$BINARY" query federation get-bridge-binding "$OPERATOR_ADDR" "$PEER_ID" "${QFLAGS[@]}" | jq -c .bridge_binding
"$BINARY" query federation operator-reward-pool "${QFLAGS[@]}" | jq -c . || true

step "6/6 exercise MsgModerateContent (OpsComm, the only human override)"
run_tx hide federation moderate-content "$CONTENT_ID" "P4 rehearsal: exercising the moderation override" \
  --new-status hidden --from "$FROM_OPS" > /dev/null
echo "status=$(content_status "$CONTENT_ID")"
run_tx restore federation moderate-content "$CONTENT_ID" "P4 rehearsal: restoring after the override check" \
  --new-status verified --from "$FROM_OPS" > /dev/null
echo "status=$(content_status "$CONTENT_ID")"

printf '\nPASS: content %s VERIFIED from %s, verifier bond released, moderation exercised.\n' "$CONTENT_ID" "$STATUS_URI"
