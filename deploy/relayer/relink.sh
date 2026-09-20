#!/bin/bash
# ------------------------------------------------------------------
# Rebuild the whole devnet<->testnet federation link after a chain reset.
#
# Resetting a chain destroys every piece of state this link is built on --
# IBC clients, connections, channels, peer records, relayer balances -- and
# invalidates the local full nodes that hermes talks to. Each of the pieces
# is already a script; what kept going wrong was the ORDER and the handful
# of steps that have no script at all. This is that order, with those steps.
#
# Run it after the networks are back up on the new release:
#
#   ./relink.sh
#
# It is idempotent: every step is guarded by a query and skipped when it is
# already in the desired state, so a re-run after a partial failure resumes
# rather than duplicating.
#
# WHAT IT DOES NOT DO: the testnet half of the peer setup. That needs a key
# this machine deliberately does not hold (see "The testnet half" at the end
# of the run, and the README). Everything else is automated.
#
# Flags / env:
#   SKIP_BUILD=1      don't rebuild the tagged binaries (only when you are
#                     certain they already match the deployed release)
#   SKIP_FUND=1       don't top up the hermes relayer accounts
#   FUND_DEV=<amt>    override the devnet relayer top-up (bare integer)
#   FUND_TEST=<amt>   override the testnet relayer top-up (bare integer)
#   SIDES=dev|test|"dev test"   passed through to setup_peers.sh
# ------------------------------------------------------------------
set -euo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
REPO_DIR="$( cd "$SCRIPT_DIR/../.." && pwd )"
NETWORK_DIR="$SCRIPT_DIR/../config/network"

BIN_DEV="${BIN_DEV:-$HOME/.local/bin/sparkdreamd-devnet}"
BIN_TEST="${BIN_TEST:-$HOME/.local/bin/sparkdreamd-testnet}"
HOME_DEV="${HOME_DEV:-$HOME/.sparkdream-relay-dev}"
HOME_TEST="${HOME_TEST:-$HOME/.sparkdream-relay-test}"
NODE_DEV="${NODE_DEV:-tcp://127.0.0.1:26657}"
NODE_TEST="${NODE_TEST:-tcp://127.0.0.1:36657}"
RPC_DEV="https://rpc-dev.sparkdream.io"
RPC_TEST="https://rpc-test.sparkdream.io"

# Funder keys. These live in the per-chain relay keyrings, which survive a
# local node re-init (local_nodes.sh clears data/ and config/, not
# keyring-test/). They are NOT the hermes keys -- they are the accounts that
# pay the hermes keys.
FUND_KEY_DEV="${FUND_KEY_DEV:-alice}"
FUND_KEY_TEST="${FUND_KEY_TEST:-bob}"

# Top-up amounts, bare integers in each chain's own micro-denom. The
# handshake plus a long stretch of relaying costs far less than this; the
# point is to not come back for a while.
FUND_DEV="${FUND_DEV:-100000000}"
FUND_TEST="${FUND_TEST:-50000000}"
# Below this, top up. Above it, leave alone -- so a re-run does not keep
# draining the funder.
FUND_FLOOR="${FUND_FLOOR:-10000000}"

SIDES="${SIDES:-dev}"
SKIP_BUILD="${SKIP_BUILD:-0}"
SKIP_FUND="${SKIP_FUND:-0}"

step() { echo ""; echo "=== $* ==="; }
die()  { echo "ERROR: $*" >&2; exit 1; }

read_env() {  # <network> <key>
    local f="$NETWORK_DIR/$1/chain.env"
    [ -f "$f" ] || die "$f not found"
    grep -E "^$2=" "$f" | head -1 | cut -d= -f2- | tr -d '"'"'"
}

DENOM_DEV="$(read_env devnet DENOM)"
DENOM_TEST="$(read_env testnet DENOM)"

echo "=================================================="
echo "  FEDERATION RELINK"
echo "=================================================="
echo "  repo:  $REPO_DIR"
echo "  sides: $SIDES"

# ------------------------------------------------------------------
# 1. Binaries.
#
# The single most expensive mistake available here. x/federation, x/shield
# and x/identity carry //go:build devnet|testnet genesis defaults, so a
# binary built from the wrong commit computes a different app hash and the
# local node dies at block 1 with "wrong Block.Header.AppHash". That reads
# like a corrupt genesis and no amount of re-downloading fixes it.
#
# Rebuilding is cheap and unconditional for exactly that reason: there is
# no reliable way to check a stripped binary's commit, and being wrong
# costs an hour of confused debugging.
# ------------------------------------------------------------------
if [ "$SKIP_BUILD" = "1" ]; then
    step "1. Binaries - SKIPPED (SKIP_BUILD=1)"
else
    step "1. Rebuilding network-tagged binaries"
    ( cd "$REPO_DIR" && CGO_ENABLED=0 go build -tags devnet  -o "$BIN_DEV"  ./cmd/sparkdreamd/main.go ) \
        || die "devnet build failed"
    echo "  built $BIN_DEV"
    ( cd "$REPO_DIR" && CGO_ENABLED=0 go build -tags testnet -o "$BIN_TEST" ./cmd/sparkdreamd/main.go ) \
        || die "testnet build failed"
    echo "  built $BIN_TEST"

    # Compare against what the networks actually run. A mismatch here is not
    # fatal -- you may be staging a release -- but it is worth saying out loud.
    for pair in "devnet:$RPC_DEV" "testnet:$RPC_TEST"; do
        n="${pair%%:*}"; u="${pair#*:}"
        live=$(curl -s -m 10 "$u/abci_info" 2>/dev/null | jq -r '.result.response.version // empty')
        echo "  $n deployed version: ${live:-unreachable}"
    done
    echo "  (binaries are built from the working tree; make sure it matches)"
fi

# ------------------------------------------------------------------
# 2. Local full nodes.
#
# Stop first, ALWAYS. A node still running against the pre-reset chain and
# peering with the sentry is what wedged the devnet sentry repeatedly: the
# p2p handshake authenticates chain-id only, never the genesis hash, so a
# stale node is accepted as a peer and then serves garbage.
# ------------------------------------------------------------------
step "2. Local full nodes"
"$SCRIPT_DIR/local_nodes.sh" stop || true

# FORCE_REINIT is required whenever genesis changed, which is the whole
# point of a reset. local_nodes.sh refuses without it and says so.
FORCE_REINIT=1 "$SCRIPT_DIR/local_nodes.sh" setup || die "local node setup failed"
"$SCRIPT_DIR/local_nodes.sh" start || die "local nodes failed to start"

echo "  waiting for both nodes to catch up (this is the slow part)..."
SYNC_DEADLINE=$(( $(date +%s) + ${SYNC_TIMEOUT:-1800} ))
while :; do
    synced=$("$SCRIPT_DIR/local_nodes.sh" status 2>/dev/null | grep -c "catching_up=False" || true)
    [ "${synced:-0}" -ge 2 ] && break
    if [ "$(date +%s)" -ge "$SYNC_DEADLINE" ]; then
        "$SCRIPT_DIR/local_nodes.sh" status || true
        die "nodes did not sync within ${SYNC_TIMEOUT:-1800}s -- check ~/.sparkdream-relay-logs/"
    fi
    sleep 15
done
"$SCRIPT_DIR/local_nodes.sh" status

# Verify again now that both nodes actually hold blocks. The check inside
# local_nodes.sh start cannot run on a freshly initialised node -- it has no
# block 1 yet -- so this is the pass that would catch a node re-synced onto
# the wrong instance.
for pair in "sparkdream-dev-1:26657:$RPC_DEV" "sparkdream-test-1:36657:$RPC_TEST"; do
    cid="${pair%%:*}"; rest="${pair#*:}"; port="${rest%%:*}"; rpc="${rest#*:}"
    lb=$(curl -sf -m 5 "http://127.0.0.1:$port/block?height=1" 2>/dev/null \
        | python3 -c 'import json,sys; print(json.load(sys.stdin)["result"]["block_id"]["hash"])' 2>/dev/null || true)
    rb=$(curl -sf -m 10 "$rpc/block?height=1" 2>/dev/null \
        | python3 -c 'import json,sys; print(json.load(sys.stdin)["result"]["block_id"]["hash"])' 2>/dev/null || true)
    if [ -n "$lb" ] && [ -n "$rb" ] && [ "$lb" != "$rb" ]; then
        die "$cid synced onto a DIFFERENT chain instance (block 1 ${lb:0:12} vs ${rb:0:12}) -- stop it and re-run with FORCE_REINIT=1"
    fi
    [ -n "$lb" ] && echo "  $cid instance ok (block 1 ${lb:0:12})"
done

# ------------------------------------------------------------------
# 3. Hermes relayer funding.
#
# The relayer keys survive a reset -- same mnemonic, same address -- but
# their BALANCES do not, because the new genesis has never heard of them.
# ------------------------------------------------------------------
hermes_addr() {  # <chain-id>
    jq -r '.account // empty' "$HOME/.hermes/keys/$1/keyring-test/"*.json 2>/dev/null | head -1
}

balance_of() {  # <bin> <node> <addr> <denom>
    "$1" query bank balances "$3" --node "$2" -o json 2>/dev/null \
        | jq -r --arg d "$4" '.balances[]? | select(.denom==$d) | .amount' 2>/dev/null | head -1
}

fund_one() {  # <label> <bin> <node> <chain-id> <funder-key> <keyring-home> <addr> <amount> <denom>
    local label="$1" bin="$2" node="$3" chain="$4" key="$5" home="$6" addr="$7" amt="$8" denom="$9"
    local bal; bal=$(balance_of "$bin" "$node" "$addr" "$denom")
    bal="${bal:-0}"
    if [ "$bal" -ge "$FUND_FLOOR" ] 2>/dev/null; then
        echo "  $label: $bal$denom - above floor, skipping"
        return 0
    fi
    echo "  $label: $bal$denom - topping up $amt"
    # --gas is fixed rather than auto: simulation under-estimates this send
    # and the tx dies with "out of gas in location: ReadFlat".
    "$bin" tx bank send "$key" "$addr" "${amt}${denom}" \
        --from "$key" --keyring-backend test --keyring-dir "$home" \
        --chain-id "$chain" --node "$node" \
        --gas 300000 --gas-prices "0.025${denom}" -y -o json >/dev/null 2>&1 \
        || die "funding $label failed -- is $key funded on $chain?"
    sleep 8
    echo "  $label now: $(balance_of "$bin" "$node" "$addr" "$denom")$denom"
}

if [ "$SKIP_FUND" = "1" ]; then
    step "3. Relayer funding - SKIPPED (SKIP_FUND=1)"
else
    step "3. Funding hermes relayers"
    AD=$(hermes_addr sparkdream-dev-1)
    AT=$(hermes_addr sparkdream-test-1)
    [ -n "$AD" ] || die "no hermes key for sparkdream-dev-1 -- run ./keys.sh"
    [ -n "$AT" ] || die "no hermes key for sparkdream-test-1 -- run ./keys.sh"
    fund_one "relayer-dev"  "$BIN_DEV"  "$NODE_DEV"  sparkdream-dev-1 \
        "$FUND_KEY_DEV"  "$HOME_DEV"  "$AD" "$FUND_DEV"  "$DENOM_DEV"
    fund_one "relayer-test" "$BIN_TEST" "$NODE_TEST" sparkdream-test-1 \
        "$FUND_KEY_TEST" "$HOME_TEST" "$AT" "$FUND_TEST" "$DENOM_TEST"
fi

# fund_check also verifies the auth BaseAccount exists, which a bank send
# does not always create and which hermes needs for its sequence number.
step "4. Relayer readiness"
"$SCRIPT_DIR/fund_check.sh" || die "relayers not ready -- see above"

# ------------------------------------------------------------------
# 5. IBC. Clients, connection, channel. Writes .ibc_channels.
# ------------------------------------------------------------------
step "5. IBC client / connection / channel"
"$SCRIPT_DIR/bringup.sh" || die "bringup failed"

# ------------------------------------------------------------------
# 6. Peers. Register, policy, activate.
# ------------------------------------------------------------------
step "6. Federation peers (SIDES=$SIDES)"
# setup_peers.sh always VERIFIES both directions and exits non-zero when
# either peer is not ACTIVE -- deliberately, because a link that is up on one
# side only silently drops packets instead of erroring. On a one-sided run
# that non-zero is the expected outcome, not a failure, so only treat it as
# fatal when this run was supposed to do both halves.
PEERS_RC=0
SIDES="$SIDES" "$SCRIPT_DIR/setup_peers.sh" || PEERS_RC=$?
if [ "$PEERS_RC" != "0" ]; then
    if [ "$SIDES" = "dev test" ] || [ "$SIDES" = "test dev" ]; then
        echo "" >&2
        echo "  setup_peers.sh did not complete. It is idempotent -- fix the cause" >&2
        echo "  and re-run either it or this script." >&2
        exit 1
    fi
    echo ""
    echo "  (setup_peers.sh reported the link incomplete, which is expected on a"
    echo "   SIDES=$SIDES run -- the other half has not been done yet.)"
fi

# ------------------------------------------------------------------
# Done. What is left is the half that needs a key we do not hold.
# ------------------------------------------------------------------
# shellcheck disable=SC2016
cat <<'EOF'

==================================================
  MACHINE-SIDE DONE
==================================================
EOF

if [ "$SIDES" = "dev" ]; then
    CHAN_TEST=$(grep -E '^CHANNEL_TEST=' "$SCRIPT_DIR/.ibc_channels" 2>/dev/null | cut -d= -f2)
    cat <<EOF
  The testnet half still needs kingofbitchain, whose key is deliberately not
  on this machine. Do it from the UI's federation page:

    1. Register peer   sparkdream-dev-1
                       type SPARK_DREAM, ibc_channel_id = ${CHAN_TEST:-<see .ibc_channels>}
    2. Edit policy     same content types both directions; reputation flags on
    3. ACTIVATE        via a COMMITTEE PROPOSAL, not direct signing.

  Step 3 is the one that changed. MsgResumePeer no longer accepts an
  individual committee member's signature -- submit it to the Operations
  Committee policy, vote yes, then execute once the 10-minute
  min_execution_period has elapsed. Steps 1 and 2 can still be signed
  directly.

EOF
fi

cat <<EOF
  Start relaying:
    hermes --config $SCRIPT_DIR/hermes_config.toml start

  Verify both sides are ACTIVE before expecting anything to flow:
    ./setup_peers.sh    # re-run; it reports both peers and exits non-zero if not
EOF
