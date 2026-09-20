#!/bin/bash
# ------------------------------------------------------------------
# Register, configure and activate the federation peers on both live
# chains, so sparkdream-dev-1 and sparkdream-test-1 will actually
# exchange content over the IBC channel bringup.sh opened.
#
# This is the deployed-network counterpart to
# test/federation/multichain/setup_peers.sh, and the step bringup.sh
# stops short of -- it prints the peer ids and channel ids and leaves the
# registration to a human.
#
# Each chain needs a Peer record naming the OTHER chain, because the two
# halves of the policy do different jobs:
#
#   testnet's peer "sparkdream-dev-1"  -> outbound_content_types gates what
#                                         testnet is willing to send
#   devnet's  peer "sparkdream-test-1" -> inbound_content_types gates what
#                                         devnet is willing to accept
#
# Peers start PENDING and are inert there: MsgFederateContent,
# MsgSubmitFederatedContent and MsgRequestReputationAttestation all require
# PEER_STATUS_ACTIVE. The only way to ACTIVE is MsgResumePeer -- registering
# a bridge no longer activates its peer as a side effect.
#
# Authorization splits in two:
#
#   register-peer, update-peer-policy, suspend-peer  single signature from
#                                                    an Operations Committee
#                                                    member
#   resume-peer (activation)                         a committee VOTE: the
#                                                    Operations Committee
#                                                    policy address, the
#                                                    Commons Council policy
#                                                    address, or governance
#
# So activation is three transactions (submit, vote, execute) plus the
# committee's min_execution_period, and the rest stay cheap. That asymmetry
# is deliberate -- hard to start a trust relationship, easy to stop one. See
# docs/x-federation-peer-authorization-plan.md.
#
# Every step is idempotent: each is guarded by a query and skipped when the
# object already exists in the desired state, so a re-run after a partial
# failure does not stack duplicate peers or re-activate.
#
# Prerequisites:
#   - bringup.sh completed (.ibc_channels exists)
#   - local full nodes running for both chains (local_nodes.sh start)
#   - a key on each chain belonging to that chain's Operations Committee
# ------------------------------------------------------------------
set -euo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
NETWORK_DIR="$SCRIPT_DIR/../config/network"
IBC_ENV="$SCRIPT_DIR/.ibc_channels"

# Binaries and homes match local_nodes.sh so the two scripts agree.
BIN_DEV="${BIN_DEV:-$HOME/.local/bin/sparkdreamd-devnet}"
BIN_TEST="${BIN_TEST:-$HOME/.local/bin/sparkdreamd-testnet}"
NODE_DEV="${NODE_DEV:-tcp://127.0.0.1:26657}"
NODE_TEST="${NODE_TEST:-tcp://127.0.0.1:36657}"

# Signing keys: must be an Operations Committee member on that chain.
#
# A side can instead be given an ADDRESS via SIGNER_DEV / SIGNER_TEST, which
# switches it to unsigned output (see GENERATE_ONLY_SIDES below). Use that
# when the account's key should not be on this machine -- a founder key with
# standing authority does not belong in a workstation keyring just to send
# two transactions.
KEY_DEV="${KEY_DEV:-alice}"
KEY_TEST="${KEY_TEST:-}"
SIGNER_DEV="${SIGNER_DEV:-}"
SIGNER_TEST="${SIGNER_TEST:-}"
# Set LEDGER_DEV=1 / LEDGER_TEST=1 when that side's key is on a hardware
# wallet. The keyring entry is added with `keys add <name> --ledger`, which
# stores a public-key reference and no seed, so the key never exists on disk.
#
# Ledger signs SIGN_MODE_LEGACY_AMINO_JSON only, so those txs are forced to
# amino-json. That works here because every federation peer-lifecycle Msg
# carries an (amino.name) option; a Msg missing one fails on-device with
# "signature verification failed" (see the amino-names rule in CLAUDE.md).
LEDGER_DEV="${LEDGER_DEV:-0}"
LEDGER_TEST="${LEDGER_TEST:-0}"
# Where unsigned txs are written for the sides that cannot sign locally.
UNSIGNED_DIR="${UNSIGNED_DIR:-$SCRIPT_DIR/.unsigned}"
KEYRING="${KEYRING:-test}"
# Default to the relay nodes' homes, which is where local_nodes.sh puts the
# keys. Leaving these empty falls back to sparkdreamd's default keyring
# (~/.sparkdream) -- which on a dev box holds a DIFFERENT chain's accounts
# under the same names, so "alice" silently resolves to the local testparams
# alice and the tx dies with an opaque "account ... not found: key not found".
KEYRING_DIR_DEV="${KEYRING_DIR_DEV:-$HOME/.sparkdream-relay-dev}"
KEYRING_DIR_TEST="${KEYRING_DIR_TEST:-$HOME/.sparkdream-relay-test}"

# Content types to federate. Every entry must appear in the chain's
# federation params known_content_types, or MsgUpdatePeerPolicy is rejected
# with ErrContentTypeNotKnown -- query it with:
#   sparkdreamd query federation params --node <rpc> -o json | jq .params.known_content_types
# The default below mirrors the set the chain ships with. It is deliberately
# NOT pinned to a release: the content-type registry has nothing to do with
# the deploy image version, so a "as of vX.Y.Z" note here would be rewritten
# by every release bump into a claim nobody re-checked. If the query above
# disagrees with this list, trust the query and override CONTENT_TYPES.
CONTENT_TYPES="${CONTENT_TYPES:-blog_post,blog_reply,forum_thread,forum_reply,collection}"
RATE_LIMIT="${RATE_LIMIT:-100}"
MIN_TRUST="${MIN_TRUST:-1}"   # 1 = PROVISIONAL; senders below this cannot federate

DRY_RUN="${DRY_RUN:-0}"
# Which chains to configure. "dev", "test", or both (the default).
SIDES="${SIDES:-dev test}"

# ------------------------------------------------------------------
# Config sourced from the committed chain.env rather than hardcoded --
# the crossnetwork tests already hold those files against each network's
# genesis, so the fee denom here cannot drift from the chain's own.
# ------------------------------------------------------------------
read_env() {  # <network> <key>
    local f="$NETWORK_DIR/$1/chain.env"
    [ -f "$f" ] || { echo "ERROR: $f not found" >&2; exit 1; }
    grep -E "^$2=" "$f" | head -1 | cut -d= -f2- | tr -d '"'"'"
}

CHAIN_DEV="$(read_env devnet CHAIN_ID)"
CHAIN_TEST="$(read_env testnet CHAIN_ID)"
DENOM_DEV="$(read_env devnet DENOM)"
DENOM_TEST="$(read_env testnet DENOM)"
FEES_DEV="${FEES_DEV:-8000$DENOM_DEV}"
FEES_TEST="${FEES_TEST:-8000$DENOM_TEST}"

[ -f "$IBC_ENV" ] || {
    echo "ERROR: $IBC_ENV not found. Run bringup.sh first -- the peer records" >&2
    echo "       need the channel ids it discovers." >&2
    exit 1
}
# shellcheck source=/dev/null
source "$IBC_ENV"

if [[ " $SIDES " == *" test "* ]] && [ -z "$KEY_TEST" ] && [ -z "$SIGNER_TEST" ]; then
    echo "ERROR: neither KEY_TEST nor SIGNER_TEST is set for $CHAIN_TEST." >&2
    echo "" >&2
    echo "  devnet's Operations Committee member is seeded at genesis (default:" >&2
    echo "  alice), but testnet's is a different account. Either:" >&2
    echo "" >&2
    echo "    KEY_TEST=<keyring name>   sign here (key must be in the keyring)" >&2
    echo "    SIGNER_TEST=<address>     emit an UNSIGNED tx to sign elsewhere" >&2
    echo "" >&2
    echo "  Use SIGNER_TEST when the key should stay in a wallet or on another" >&2
    echo "  machine. The tx is written to $UNSIGNED_DIR for you to sign with" >&2
    echo "  Keplr, a Ledger, or 'sparkdreamd tx sign' on the host that holds it." >&2
    exit 1
fi

# A side signs locally only when it was given a key NAME.
signs_locally() {  # <side>
    [ "$1" = dev ] && [ -n "$KEY_DEV" ] && [ -z "$SIGNER_DEV" ] && return 0
    [ "$1" = test ] && [ -n "$KEY_TEST" ] && [ -z "$SIGNER_TEST" ] && return 0
    return 1
}

echo "=================================================="
echo "  FEDERATION PEER SETUP"
echo "=================================================="
echo "  $CHAIN_DEV  <-  $CHAIN_TEST"
echo "  channels:  dev=$CHANNEL_DEV  test=$CHANNEL_TEST"
echo "  signing:   $KEY_DEV (dev) / $KEY_TEST (test)"
[ "$DRY_RUN" = "1" ] && echo "  DRY RUN -- printing txs, broadcasting nothing"
echo ""

# ------------------------------------------------------------------
# Preflight. Both chains have to answer before anything is broadcast:
# registering the peer on one chain and failing on the other leaves a
# half-open link that looks configured from whichever side you check.
# ------------------------------------------------------------------
echo "=== Preflight ==="
pf=0
for side in $SIDES; do
    bin="$([ "$side" = dev ] && echo "$BIN_DEV" || echo "$BIN_TEST")"
    node="$([ "$side" = dev ] && echo "$NODE_DEV" || echo "$NODE_TEST")"
    chain="$([ "$side" = dev ] && echo "$CHAIN_DEV" || echo "$CHAIN_TEST")"
    if [ ! -x "$bin" ]; then
        echo "  MISSING binary for $chain: $bin" >&2
        echo "         build it with the matching tag -- an untagged binary" >&2
        echo "         computes a different app hash and stalls at height 1" >&2
        pf=1; continue
    fi
    got=$("$bin" status --node "$node" --output json 2>/dev/null | jq -r '.node_info.network // empty' 2>/dev/null || true)
    if [ -z "$got" ]; then
        echo "  UNREACHABLE $chain at $node -- is local_nodes.sh running?" >&2
        pf=1
    elif [ "$got" != "$chain" ]; then
        echo "  WRONG CHAIN at $node: node reports '$got', expected '$chain'" >&2
        pf=1
    else
        echo "  ok  $chain at $node"
    fi
done
# Resolve each configured side's signer to an address and confirm the chain
# knows it. A key name that resolves out of the wrong keyring, or an account
# the chain has never seen, both fail deep inside the broadcast with an error
# that names neither the key nor the keyring.
for side in $SIDES; do
    bin="$([ "$side" = dev ] && echo "$BIN_DEV" || echo "$BIN_TEST")"
    node="$([ "$side" = dev ] && echo "$NODE_DEV" || echo "$NODE_TEST")"
    chain="$([ "$side" = dev ] && echo "$CHAIN_DEV" || echo "$CHAIN_TEST")"
    kd="$([ "$side" = dev ] && echo "$KEYRING_DIR_DEV" || echo "$KEYRING_DIR_TEST")"
    signer="$([ "$side" = dev ] && echo "$SIGNER_DEV" || echo "$SIGNER_TEST")"
    key="$([ "$side" = dev ] && echo "$KEY_DEV" || echo "$KEY_TEST")"

    if [ -n "$signer" ]; then
        echo "  $chain: unsigned output for $signer"
        continue
    fi
    addr=$("$bin" keys show "$key" -a --keyring-backend "$KEYRING" --keyring-dir "$kd" 2>/dev/null || true)
    if [ -z "$addr" ]; then
        echo "  KEY '$key' not found in $kd (backend $KEYRING) for $chain" >&2
        pf=1; continue
    fi
    bal=$("$bin" query bank balances "$addr" --node "$node" --output json 2>/dev/null | python3 -c 'import json,sys
try: b=json.load(sys.stdin).get("balances",[])
except Exception: b=[]
print(b[0]["amount"] if b else "0")' 2>/dev/null || echo "0")
    if [ "$bal" = "0" ]; then
        echo "  KEY '$key' -> $addr has NO balance on $chain." >&2
        echo "       Wrong keyring, or wrong account for this chain." >&2
        pf=1
    else
        echo "  ok  $key -> ${addr:0:18}… ($bal) on $chain"
    fi
done

[ "$pf" = "0" ] || { echo "" >&2; echo "Preflight failed; nothing was broadcast." >&2; exit 1; }
echo ""

# ------------------------------------------------------------------
# Helpers. Each tx is broadcast in sync mode and then polled for the
# DELIVERED result: a code 0 from broadcast only means it entered the
# mempool, and a peer that failed in DeliverTx would otherwise look like
# a success and be skipped as "already registered" on the next run.
# ------------------------------------------------------------------
bin_for()     { [ "$1" = dev ] && echo "$BIN_DEV"     || echo "$BIN_TEST"; }
node_for()    { [ "$1" = dev ] && echo "$NODE_DEV"    || echo "$NODE_TEST"; }
denom_for()   { [ "$1" = dev ] && echo "$DENOM_DEV"   || echo "$DENOM_TEST"; }
chain_for()   { [ "$1" = dev ] && echo "$CHAIN_DEV"   || echo "$CHAIN_TEST"; }
key_for()     {
    if [ "$1" = dev ]; then echo "${SIGNER_DEV:-$KEY_DEV}"; else echo "${SIGNER_TEST:-$KEY_TEST}"; fi
}
# TX_FEE_OVERRIDE lets one call use a fee other than the per-chain default.
# Set it immediately before a tx_mod call and clear it after -- the commons
# proposal fee and a high --gas execute both need more than the 8000 a plain
# federation tx costs, and getting either wrong is a CheckTx rejection that
# looks nothing like a fee problem until you read code=13.
TX_FEE_OVERRIDE=""
fees_for()    {
    if [ -n "$TX_FEE_OVERRIDE" ]; then echo "$TX_FEE_OVERRIDE"; return; fi
    [ "$1" = dev ] && echo "$FEES_DEV"    || echo "$FEES_TEST"
}

# The x/commons proposal fee is a param, so read it off the chain rather than
# hardcoding 5000000 here: a governance change to commons params would
# otherwise break activation with an error nobody would connect to this script.
proposal_fee_for() {  # <side>
    local f
    f=$("$(bin_for "$1")" query commons params --node "$(node_for "$1")" -o json 2>/dev/null \
        | jq -r '.params.proposal_fee // empty' 2>/dev/null)
    if [ -n "$f" ]; then echo "$f"; else
        # Fall back to the per-chain default rather than failing outright --
        # a too-low fee is rejected at CheckTx with a clear message.
        fees_for "$1"
    fi
}
keydir_for()  { [ "$1" = dev ] && echo "$KEYRING_DIR_DEV" || echo "$KEYRING_DIR_TEST"; }
ledger_for()  { [ "$1" = dev ] && echo "$LEDGER_DEV"      || echo "$LEDGER_TEST"; }

# Sets COMMON_FLAGS as an array rather than returning a string: a
# keyring-dir with a space in it would word-split out of a string and
# silently become two arguments.
COMMON_FLAGS=()
set_common_flags() {  # <side>
    local kd; kd="$(keydir_for "$1")"
    COMMON_FLAGS=(--node "$(node_for "$1")" --chain-id "$(chain_for "$1")"
                  --keyring-backend "$KEYRING")
    [ -n "$kd" ] && COMMON_FLAGS+=(--keyring-dir "$kd")
    if [ "$(ledger_for "$1")" = "1" ]; then
        COMMON_FLAGS+=(--ledger --sign-mode amino-json)
    fi
    return 0
}

q() {  # <side> <args...>
    local side="$1"; shift
    "$(bin_for "$side")" query federation "$@" \
        --node "$(node_for "$side")" --output json 2>/dev/null
}

# tx_mod runs a transaction against an arbitrary module; tx() is the
# federation-module shorthand every peer step uses. Activation needs
# `tx commons submit-proposal/vote-proposal/execute-proposal`, which is the
# only reason this is parameterized.
#
# On success TX_DELIVERED holds the delivered tx JSON, so a caller can read
# events out of it (the proposal id, specifically). It is unset on failure.
tx() {  # <side> <label> <args...>
    tx_mod "$1" federation "$2" "${@:3}"
}

tx_mod() {  # <side> <module> <label> <args...>
    local side="$1" module="$2" label="$3"; shift 3
    local bin; bin="$(bin_for "$side")"
    TX_DELIVERED=""
    set_common_flags "$side"

    # A side with no local key emits an unsigned tx instead of broadcasting.
    # --generate-only resolves --from as a bare address, so nothing about the
    # account needs to exist in this machine's keyring.
    if ! signs_locally "$side"; then
        local slug out
        if [ "$DRY_RUN" = "1" ]; then
            printf '    [dry-run] would write an unsigned tx for %s (%s)\n' \
                "$(chain_for "$side")" "$label"
            return 0
        fi
        mkdir -p "$UNSIGNED_DIR"
        slug=$(printf '%s' "$label" | tr -c 'A-Za-z0-9._-' '-')
        out="$UNSIGNED_DIR/$(chain_for "$side")--${slug}.json"
        if "$bin" tx "$module" "$@" --from "$(key_for "$side")" \
            --node "$(node_for "$side")" --chain-id "$(chain_for "$side")" \
            --fees "$(fees_for "$side")" --generate-only --output json > "$out" 2>/dev/null
        then
            echo "    unsigned ($label) -> $out"
            return 0
        fi
        echo "    FAILED to build unsigned tx ($label)" >&2
        rm -f "$out"
        return 1
    fi

    local cmd=("$bin" tx "$module" "$@" --from "$(key_for "$side")"
               "${COMMON_FLAGS[@]}" --fees "$(fees_for "$side")" -y --output json)

    if [ "$DRY_RUN" = "1" ]; then
        # %q per argument, not "${cmd[*]}": the display name and metadata
        # contain spaces, and joining them unquoted prints a command that
        # looks like it has extra positional args and does the wrong thing
        # if anyone copy-pastes it.
        printf '    [dry-run]'
        printf ' %q' "${cmd[@]}"
        printf '\n'
        return 0
    fi

    local out txhash code
    out=$("${cmd[@]}" 2>&1) || { echo "    FAILED to broadcast: $out" >&2; return 1; }
    code=$(echo "$out" | jq -r '.code // empty' 2>/dev/null || echo "")
    txhash=$(echo "$out" | jq -r '.txhash // empty' 2>/dev/null || echo "")
    if [ -z "$txhash" ]; then
        echo "    FAILED ($label): $out" >&2
        return 1
    fi
    if [ -n "$code" ] && [ "$code" != "0" ]; then
        echo "    REJECTED at CheckTx ($label): code=$code $(echo "$out" | jq -r '.raw_log // ""')" >&2
        return 1
    fi

    # Poll for the delivered result.
    local i res dcode
    for i in $(seq 1 30); do
        sleep 2
        res=$("$bin" query tx "$txhash" --node "$(node_for "$side")" --output json 2>/dev/null) || continue
        dcode=$(echo "$res" | jq -r '.code // empty')
        [ -z "$dcode" ] && continue
        if [ "$dcode" = "0" ]; then
            echo "    ok ($label) $txhash"
            TX_DELIVERED="$res"
            return 0
        fi
        echo "    FAILED in DeliverTx ($label): code=$dcode $(echo "$res" | jq -r '.raw_log')" >&2
        return 1
    done
    echo "    TIMED OUT waiting for $txhash to be delivered ($label)" >&2
    return 1
}

# Existence is keyed on .peer.id, never on .peer.status.
#
# PEER_STATUS_PENDING is 0 in the enum, and proto3 omits zero values from the
# CLI's JSON -- so a freshly registered peer comes back with NO status field
# at all, indistinguishable from "no such peer" if you test that field. That
# misread makes the script try to register a peer that already exists, which
# then fails with ErrPeerAlreadyExists at a point the caller reads as a fresh
# failure. Same trap as the empty ibc_channel_id below.
#
# Failures are swallowed on purpose: under `set -e` an unreachable node would
# otherwise kill the script at the assignment with no indication of which
# chain was down. The preflight is what reports that.
peer_json() {  # <side> <peer-id>
    q "$1" get-peer "$2" 2>/dev/null || true
}

peer_exists() {  # <side> <peer-id>
    [ -n "$(peer_json "$1" "$2" | jq -r '.peer.id // empty' 2>/dev/null || true)" ]
}

peer_status() {  # <side> <peer-id> -- "" only when the peer does not exist
    peer_json "$1" "$2" \
        | jq -r 'if .peer.id then (.peer.status // "PEER_STATUS_PENDING") else "" end' \
            2>/dev/null || true
}

# proto3 omits an empty string too, so an unbound channel reads as absent.
peer_channel() {  # <side> <peer-id>
    peer_json "$1" "$2" | jq -r '.peer.ibc_channel_id // ""' 2>/dev/null || true
}

# JSON for a PeerPolicy that lets content move in both directions. The
# receiving side's inbound list is what actually gates delivery, but both
# are set so the link is symmetric and either chain can originate.
# ------------------------------------------------------------------
# Activation, via an Operations Committee vote.
#
# MsgResumePeer no longer accepts an individual committee member's
# signature -- activation is the trust decision in a peer's lifecycle, so
# it takes the committee POLICY address, which means a proposal that has
# been voted through. Registration, policy edits and suspension are all
# still single-signature; only this one step changed.
#
# Three transactions: submit, vote, execute. With one committee member the
# single yes vote crosses the 0.5 threshold immediately and the proposal is
# accepted early -- which sets execution_time to now + min_execution_period
# (5 min devnet, 10 min testnet), so execute has to wait that out. The
# 5-day voting_period never applies.
# ------------------------------------------------------------------

# How long to wait for a proposal to become executable before giving up and
# telling the operator to run the execute step themselves. Default covers
# testnet's 10-minute min_execution_period with room to spare.
ACTIVATION_WAIT_SECS="${ACTIVATION_WAIT_SECS:-900}"

ops_policy() {  # <side> -- Operations Committee policy address, "" if unknown
    "$(bin_for "$1")" query commons get-group "Commons Operations Committee" \
        --node "$(node_for "$1")" --output json 2>/dev/null \
        | jq -r '.group.policy_address // empty' 2>/dev/null || echo ""
}

proposal_field() {  # <side> <proposal-id> <jq-path>
    "$(bin_for "$1")" query commons get-proposal "$2" \
        --node "$(node_for "$1")" --output json 2>/dev/null \
        | jq -r "$3" 2>/dev/null || echo ""
}

activate_peer() {  # <side> <peer-id>
    local side="$1" peer="$2"
    local policy; policy="$(ops_policy "$side")"
    if [ -z "$policy" ]; then
        echo "  cannot resolve the Commons Operations Committee policy address on" >&2
        echo "  $(chain_for "$side") -- is the node reachable and the chain bootstrapped?" >&2
        return 1
    fi

    local prop_file="$UNSIGNED_DIR/$(chain_for "$side")--activate-${peer}.proposal.json"
    mkdir -p "$UNSIGNED_DIR"
    cat > "$prop_file" <<JSON
{
  "policy_address": "$policy",
  "messages": [
    {
      "@type": "/sparkdream.federation.v1.MsgResumePeer",
      "authority": "$policy",
      "peer_id": "$peer"
    }
  ],
  "metadata": "Activate federation peer $peer"
}
JSON
    echo "  activation proposal -> $prop_file"

    # Without a local key the vote and execute steps cannot be scripted:
    # their proposal id only exists after the submit lands. Emit the submit
    # and hand the rest to the operator.
    if ! signs_locally "$side"; then
        TX_FEE_OVERRIDE="$(proposal_fee_for "$side")"
        tx_mod "$side" commons "activate $peer (submit)" submit-proposal "$prop_file" || { TX_FEE_OVERRIDE=""; return 1; }
        TX_FEE_OVERRIDE=""
        echo "    then, as the committee member: vote-proposal <id> yes, wait out"
        echo "    min_execution_period, and execute-proposal <id> -- or do all three"
        echo "    from the web UI, which is what it is there for."
        return 2
    fi

    # submit-proposal must carry at least the x/commons proposal_fee, which is
    # far above what a federation tx costs.
    TX_FEE_OVERRIDE="$(proposal_fee_for "$side")"
    tx_mod "$side" commons "activate $peer (submit)" submit-proposal "$prop_file" || { TX_FEE_OVERRIDE=""; return 1; }
    TX_FEE_OVERRIDE=""

    local prop_id
    prop_id=$(echo "$TX_DELIVERED" | jq -r '
        .events[]? | select(.type=="submit_proposal")
        | .attributes[]? | select(.key=="proposal_id") | .value' 2>/dev/null | tr -d '"' | head -1)
    if [ -z "$prop_id" ]; then
        echo "    submitted, but could not read the proposal id back from the tx" >&2
        echo "    events. Find it with: $(bin_for "$side") query commons list-proposals" >&2
        return 1
    fi
    echo "    proposal $prop_id"

    tx_mod "$side" commons "activate $peer (vote)" vote-proposal "$prop_id" yes || return 1

    # Wait for acceptance, then for execution_time. Both are read from the
    # proposal rather than assumed, so this does not need to know the
    # chain's min_execution_period.
    local deadline=$(( $(date +%s) + ACTIVATION_WAIT_SECS ))
    local status exec_time now
    while :; do
        status=$(proposal_field "$side" "$prop_id" '.proposal.status // empty')
        case "$status" in
            PROPOSAL_STATUS_EXECUTED)
                echo "    already executed"
                return 0 ;;
            PROPOSAL_STATUS_ACCEPTED)
                ;;
            PROPOSAL_STATUS_REJECTED|PROPOSAL_STATUS_ABORTED)
                echo "    proposal $prop_id ended $status -- not activating" >&2
                return 1 ;;
        esac

        exec_time=$(proposal_field "$side" "$prop_id" '.proposal.execution_time // "0"')
        now=$(date +%s)
        if [ "$status" = "PROPOSAL_STATUS_ACCEPTED" ] && [ "$exec_time" != "0" ] && [ "$now" -ge "$exec_time" ]; then
            break
        fi
        if [ "$now" -ge "$deadline" ]; then
            echo "    proposal $prop_id not executable after ${ACTIVATION_WAIT_SECS}s" >&2
            echo "    (status=$status, execution_time=$exec_time). Finish with:" >&2
            echo "      $(bin_for "$side") tx commons execute-proposal $prop_id ..." >&2
            return 1
        fi
        if [ "$exec_time" != "0" ] && [ "$exec_time" -gt "$now" ]; then
            echo "    waiting $(( exec_time - now ))s for min_execution_period (status=$status)"
        else
            echo "    waiting for acceptance (status=${status:-unknown})"
        fi
        sleep 15
    done

    # execute-proposal runs with --gas 2000000; at the 0.025/gas these chains
    # charge, the 8000 default fee is an order of magnitude short and the tx is
    # rejected at CheckTx. 100000 covers it with headroom.
    TX_FEE_OVERRIDE="${EXECUTE_FEE_OVERRIDE:-100000$(denom_for "$side")}"
    tx_mod "$side" commons "activate $peer (execute)" execute-proposal "$prop_id" --gas 2000000 || { TX_FEE_OVERRIDE=""; return 1; }
    TX_FEE_OVERRIDE=""
    return 0
}

# Reputation bridging is ON by default here. Both flags are only legal for
# PEER_TYPE_SPARK_DREAM (update_peer_policy rejects them for bridge peers), and
# this script only ever registers sister chains, so there is no peer type these
# defaults are invalid for.
#
# The two flags are not symmetric, and it is worth being precise about why each
# is safe rather than waving at "it's only advisory":
#
#   accept_reputation_attestations (inbound) -- advisory in the strict sense.
#     What lands is capped at params.global_max_trust_credit (default 1,
#     PROVISIONAL-equivalent), expires after attestation_ttl, and nothing on
#     this chain reads local_trust_credit to grant a permission. An invited
#     member still starts at NEW. Enabling it cannot raise anyone's standing.
#
#   allow_reputation_queries (outbound) -- a DISCLOSURE decision, not an
#     advisory one: it controls whether this chain answers a peer's questions
#     about its own members' trust levels and tag scores. What makes it safe is
#     not advisoryness but two other things: MsgRequestReputationAttestation
#     requires the requester to hold a VERIFIED identity link to the exact
#     address being asked about, so a member can only pull reputation for an
#     identity they proved they control; and peers are activated by a
#     governance vote, so you answer only chains you chose to trust. The
#     residual exposure is a peer running modified code that skips the link
#     check -- bounded, since reputation here is public on-chain data anyway.
#
# Override per run with ALLOW_REP_QUERIES / ACCEPT_REP_ATTESTATIONS = false.
ALLOW_REP_QUERIES="${ALLOW_REP_QUERIES:-true}"
ACCEPT_REP_ATTESTATIONS="${ACCEPT_REP_ATTESTATIONS:-true}"

policy_json() {  # <peer-id>
    local types
    types=$(printf '%s' "$CONTENT_TYPES" | jq -R 'split(",")')
    jq -cn --arg peer "$1" --argjson t "$types" \
        --argjson rate "$RATE_LIMIT" --argjson trust "$MIN_TRUST" \
        --argjson allowq "$ALLOW_REP_QUERIES" --argjson accepta "$ACCEPT_REP_ATTESTATIONS" '{
        peer_id: $peer,
        outbound_content_types: $t,
        inbound_content_types: $t,
        min_outbound_trust_level: $trust,
        inbound_rate_limit_per_epoch: ($rate|tostring),
        outbound_rate_limit_per_epoch: ($rate|tostring),
        allow_reputation_queries: $allowq,
        accept_reputation_attestations: $accepta,
        require_review: false,
        blocked_identities: []
    }'
}

# ------------------------------------------------------------------
# One direction: register + policy + activate the peer `remote` on `side`.
# ------------------------------------------------------------------
setup_side() {  # <side> <remote-chain-id> <channel-on-this-side> <display>
    local side="$1" remote="$2" channel="$3" display="$4"
    echo "=== $(chain_for "$side"): peer $remote (channel $channel) ==="

    local status bound
    status="$(peer_status "$side" "$remote")"
    bound="$(peer_channel "$side" "$remote")"

    if ! peer_exists "$side" "$remote"; then
        echo "  registering..."
        tx "$side" "register $remote" register-peer \
            "$remote" "$display" "federation link via $channel" "$channel" \
            --type spark-dream
    elif [ "$bound" != "$channel" ]; then
        # A peer bound to the wrong channel -- or to none, which is what a
        # peer registered before bringup.sh ran looks like -- is inert in
        # both directions: findPeerByChannel matches incoming packets on
        # ibc_channel_id, and the send path rejects an empty one with
        # ErrIBCNotAvailable. Nothing downstream reports this, so activating
        # it would leave a peer that reads ACTIVE and moves nothing.
        #
        # There is no message to rebind a channel: MsgRegisterPeer refuses an
        # existing peer unless it is REMOVED, so the fix is remove and
        # re-register, and removal is a tombstone plus an EndBlocker cleanup
        # queue. That is destructive enough to be opt-in.
        echo "  MISMATCH: peer is registered with ibc_channel_id='${bound}'," >&2
        echo "            but this link needs '${channel}'." >&2
        if [ "${FIX_CHANNEL:-0}" != "1" ]; then
            echo "" >&2
            echo "  Re-run with FIX_CHANNEL=1 to remove and re-register it, or fix by hand:" >&2
            echo "    $(bin_for "$side") tx federation remove-peer $remote \"rebinding channel\" ..." >&2
            return 1
        fi
        echo "  FIX_CHANNEL=1: removing and re-registering..."
        tx "$side" "remove $remote" remove-peer "$remote" "rebinding to $channel"

        # RemovePeer sets status REMOVED in its own tx but also enqueues the
        # peer for EndBlocker cleanup, and MsgRegisterPeer refuses while that
        # queue entry exists (ErrPeerCleanupInProgress). REMOVED is therefore
        # reached long before re-registration is allowed, and the queue is not
        # exposed by any query -- so retry the registration rather than trying
        # to observe the drain.
        local i ok=0
        for i in $(seq 1 15); do
            if tx "$side" "re-register $remote" register-peer \
                "$remote" "$display" "federation link via $channel" "$channel" \
                --type spark-dream
            then ok=1; break; fi
            echo "    cleanup still in progress; retrying in 4s ($i/15)"
            sleep 4
        done
        [ "$ok" = "1" ] || {
            echo "  Re-registration never succeeded. Check whether the peer is" >&2
            echo "  still in the removal queue, or has bridge bindings blocking it." >&2
            return 1
        }
    else
        echo "  already registered on $channel (status=$status) - skipping registration"
    fi

    echo "  setting policy..."
    tx "$side" "policy $remote" update-peer-policy "$remote" \
        --policy "$(policy_json "$remote")"

    status="$(peer_status "$side" "$remote")"
    if [ "$status" = "PEER_STATUS_ACTIVE" ]; then
        echo "  already ACTIVE - skipping activation"
    elif [ "$DRY_RUN" = "1" ]; then
        echo "  [dry-run] would activate $remote via a Commons Operations"
        echo "  Committee proposal (submit + vote + execute)"
    else
        echo "  activating (committee vote)..."
        local rc=0
        activate_peer "$side" "$remote" || rc=$?
        # rc=2 means "submitted unsigned, the operator finishes it" --
        # the UNSIGNED TRANSACTIONS block below is what reports that.
        if [ "$rc" != "0" ] && [ "$rc" != "2" ]; then
            return 1
        fi
    fi
    echo ""
}

# SIDES lets one chain be configured on its own -- useful when the other
# chain's half is being done elsewhere (the web UI, another operator's
# machine) and emitting unsigned txs for it would just be noise. The link
# needs BOTH sides ACTIVE to carry anything, so a partial run says so.
for side in $SIDES; do
    case "$side" in
        dev)  setup_side dev  "$CHAIN_TEST" "$CHANNEL_DEV"  "Spark Dream Testnet" ;;
        test) setup_side test "$CHAIN_DEV"  "$CHANNEL_TEST" "Spark Dream Devnet" ;;
        *)    echo "ERROR: SIDES must contain only 'dev' and/or 'test' (got '$side')" >&2; exit 1 ;;
    esac
done

# ------------------------------------------------------------------
# Verify. A peer that is not ACTIVE on BOTH sides silently drops packets
# rather than erroring, so this check is the difference between "the link
# is up" and "the link looks up".
# ------------------------------------------------------------------
if [ "$DRY_RUN" = "1" ]; then
    echo "dry run complete - nothing was broadcast"
    exit 0
fi

# Only a side that actually RAN and could not sign has unsigned output. A
# side left out of SIDES produced nothing and must not trigger this.
emitted_unsigned=0
for side in $SIDES; do
    signs_locally "$side" || emitted_unsigned=1
done
if [ "$emitted_unsigned" = "1" ]; then
    cat <<EOF

==================================================
  UNSIGNED TRANSACTIONS WRITTEN
==================================================
  $UNSIGNED_DIR

  Sign and broadcast each one on the host that holds the key, in the order
  written (register -> policy -> activation proposal; each depends on the
  previous):

    sparkdreamd tx sign <file> --from <key> --chain-id <chain> \\
        --node <rpc> --output-document signed.json
    sparkdreamd tx broadcast signed.json --node <rpc>

  The *.proposal.json files are not transactions -- they are the proposal
  bodies for the activation vote. Activation is a committee decision now, so
  after the submit lands it still needs a yes vote and, once the committee's
  min_execution_period has elapsed, an execute.

  Or do all of it from the Federation page in the web UI, which signs with
  Keplr and never puts the key on disk.

  Re-run this script afterwards: it is idempotent and will verify both peers
  and fill in anything still missing.
EOF
    exit 0
fi

echo "=================================================="
echo "  RESULT"
echo "=================================================="
rc=0
# Both sides are always REPORTED: a peer that is ACTIVE on one chain and
# absent on the other moves nothing, and that is the state a partial run
# leaves behind, so it should be visible rather than implied.
for pair in "dev:$CHAIN_TEST" "test:$CHAIN_DEV"; do
    side="${pair%%:*}"; remote="${pair#*:}"
    status="$(peer_status "$side" "$remote")"
    bound="$(peer_channel "$side" "$remote")"
    printf '  %-22s peer %-22s %-22s channel=%s\n' \
        "$(chain_for "$side")" "$remote" "${status:-MISSING}" "${bound:-NONE}"
    [ "$status" = "PEER_STATUS_ACTIVE" ] || rc=1
    [ -n "$bound" ] || rc=1
done
echo ""

if [ "$rc" != "0" ]; then
    echo "  One or both peers are not ACTIVE. Content will not flow." >&2
    exit 1
fi

cat <<EOF
  Both peers ACTIVE. The link is live once hermes is relaying:

    $SCRIPT_DIR/../../deploy/relayer/hermes_config.toml
    hermes --config .../hermes_config.toml start

  To send content FROM testnet TO devnet, have a testnet member with trust
  level >= $MIN_TRUST run (MsgFederateContent is creator-signed -- a relayer
  cannot fabricate it):

    $BIN_TEST tx federation federate-content <content-type> <content-id> $CHAIN_DEV \\
        --from <member> --node $NODE_TEST --chain-id $CHAIN_TEST \\
        --keyring-backend $KEYRING --fees $FEES_TEST -y

  Then watch it arrive:

    $BIN_DEV query federation list-federated-content --node $NODE_DEV --output json
EOF
