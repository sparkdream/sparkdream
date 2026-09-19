#!/bin/bash
# ------------------------------------------------------------------
# Bring up the IBC federation link between sparkdream-dev-1 and
# sparkdream-test-1: clients -> connection -> federation channel.
#
# This is the deployed-network counterpart to
# test/federation/multichain/setup_ibc.sh. It does NOT create or fund the
# relayer keys -- on a live network those are real funded accounts, so key
# handling stays manual (see README.md "Relayer keys").
#
# This script does not start the Hermes daemon either. Channels alone do not
# relay packets; run `hermes --config hermes_config.toml start` afterwards.
#
# Every step is idempotent: `hermes create ...` is not, so each one is guarded
# by a query and skipped when the object already exists. Re-running after a
# transient failure reuses what is already on chain instead of stacking a
# second client/connection/channel.
#
# Prerequisites:
#   - hermes >= 1.13.3 on PATH (the chains run ibc-go/v10)
#   - gRPC for both chains reachable at the addresses in hermes_config.toml
#   - relayer keys imported into hermes and funded on both chains
# ------------------------------------------------------------------
set -euo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
HERMES="${HERMES:-hermes}"
HERMES_CONFIG="${HERMES_CONFIG:-$SCRIPT_DIR/hermes_config.toml}"

CHAIN_DEV="${CHAIN_DEV:-sparkdream-dev-1}"
CHAIN_TEST="${CHAIN_TEST:-sparkdream-test-1}"
PORT="federation"
VERSION="federation-1"

OUT_ENV="$SCRIPT_DIR/.ibc_channels"

command -v "$HERMES" >/dev/null 2>&1 || {
    echo "ERROR: hermes not on PATH. Set HERMES=/path/to/hermes." >&2
    exit 1
}

echo "=================================================="
echo "  FEDERATION IBC BRINGUP"
echo "=================================================="
echo "  hermes:  $($HERMES version 2>&1 | head -1)"
echo "  config:  $HERMES_CONFIG"
echo "  chains:  $CHAIN_DEV  <->  $CHAIN_TEST"
echo ""

hc() { "$HERMES" --config "$HERMES_CONFIG" "$@"; }

# ------------------------------------------------------------------
# 0. Fail early if gRPC is not actually reachable. Hermes needs it for
#    account queries and tx simulation, and the sentry template binds it
#    to localhost, so this is the failure everyone hits first.
#
#    It does NOT report that failure: with an unreachable grpc_addr hermes
#    prints its startup banner and then hangs indefinitely, with no error
#    and no timeout. Verified against a dead port. So wrap health-check in
#    a hard timeout and treat expiry as "the tunnel is down" -- otherwise
#    this script just appears to freeze.
# ------------------------------------------------------------------
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-60}"
echo "=== Health check (timeout ${HEALTH_TIMEOUT}s) ==="
HEALTH_LOG=$(mktemp)
if timeout "$HEALTH_TIMEOUT" "$HERMES" --config "$HERMES_CONFIG" health-check > "$HEALTH_LOG" 2>&1; then
    tail -20 "$HEALTH_LOG"
    rm -f "$HEALTH_LOG"
else
    RC=$?
    tail -20 "$HEALTH_LOG"
    rm -f "$HEALTH_LOG"
    echo "" >&2
    if [ "$RC" -eq 124 ]; then
        echo "ERROR: health-check timed out with no error message. That is what an" >&2
        echo "       unreachable grpc_addr looks like -- hermes dials it forever." >&2
        echo "       Check your tunnels are up:" >&2
        echo "         ss -ltn | grep -E '9090|9091'" >&2
        echo "       See README.md 'Reaching gRPC'." >&2
    else
        echo "ERROR: health-check failed (exit $RC). See output above." >&2
    fi
    exit 1
fi
echo ""

# ------------------------------------------------------------------
# 1. Clients. One per direction, each tracking the counterparty.
# ------------------------------------------------------------------
client_count() {  # <host chain> <reference chain id>
    hc query clients --host-chain "$1" 2>&1 | grep -c "$2" || true
}

echo "=== Clients ==="
for pair in "$CHAIN_DEV $CHAIN_TEST" "$CHAIN_TEST $CHAIN_DEV"; do
    set -- $pair
    if [ "$(client_count "$1" "$2")" -eq 0 ]; then
        echo "  creating client on $1 tracking $2"
        hc create client --host-chain "$1" --reference-chain "$2" 2>&1 | tail -3
        sleep 3
    else
        echo "  client on $1 tracking $2 already exists - skipping"
    fi
done
echo ""

# ------------------------------------------------------------------
# 2. Connection. Only create when neither side has one, so a re-run
#    cannot silently open a second handshake.
# ------------------------------------------------------------------
connection_ids() { hc query connections --chain "$1" 2>&1 | grep -oE 'connection-[0-9]+' || true; }

echo "=== Connection ==="
CONNS_DEV=$(connection_ids "$CHAIN_DEV" | wc -l)
CONNS_TEST=$(connection_ids "$CHAIN_TEST" | wc -l)
if [ "$CONNS_DEV" -eq 0 ] && [ "$CONNS_TEST" -eq 0 ]; then
    hc create connection --a-chain "$CHAIN_DEV" --b-chain "$CHAIN_TEST" 2>&1 | tail -10
    sleep 3
else
    echo "  connection already exists (dev=$CONNS_DEV, test=$CONNS_TEST) - skipping"
fi

CONN_DEV=$(connection_ids "$CHAIN_DEV" | tail -1)
CONN_TEST=$(connection_ids "$CHAIN_TEST" | tail -1)
if [ -z "$CONN_DEV" ] || [ -z "$CONN_TEST" ]; then
    echo "ERROR: could not discover connections on both chains." >&2
    hc query connections --chain "$CHAIN_DEV" 2>&1 | head -20 >&2
    hc query connections --chain "$CHAIN_TEST" 2>&1 | head -20 >&2
    exit 1
fi
echo "  dev:  $CONN_DEV"
echo "  test: $CONN_TEST"
echo ""

# ------------------------------------------------------------------
# 3. The federation channel itself. Unordered, version federation-1,
#    port federation on both ends -- these must match x/federation's
#    params (ibc_port, ibc_channel_version) or the handshake is refused
#    by the module's OnChanOpenInit/Try.
# ------------------------------------------------------------------
# Lowest-numbered OPEN channel on $PORT, via --json.
#
# The human-readable `hermes query channels` output cannot be grepped for
# this: it prints `channel_id: ChannelId(` / `"channel-0",` / `port_id:
# PortId(` / `"federation",` across separate lines, so the port name is five
# lines AFTER the channel id it belongs to. A `grep -B1 federation` finds
# `port_id: PortId(` and no channel id at all, the function returns empty,
# and the caller concludes no channel exists -- which is how a re-run opened
# a SECOND federation channel on both chains instead of skipping.
#
# Sorted so repeated runs pick the same channel every time; a duplicate from
# an earlier run stays open (IBC channels cannot be deleted) but is ignored.
federation_channel() {
    "$HERMES" --json --config "$HERMES_CONFIG" query channels --chain "$1" 2>/dev/null \
        | python3 -c '
import json,sys
port=sys.argv[1]
ids=[]
for line in sys.stdin:
    line=line.strip()
    if not line.startswith("{"): continue
    try: d=json.loads(line)
    except Exception: continue
    for e in d.get("result") or []:
        if e.get("port_id")==port and e.get("channel_id"):
            ids.append(e["channel_id"])
ids.sort(key=lambda c:int(c.rsplit("-",1)[-1]) if c.rsplit("-",1)[-1].isdigit() else 0)
print(ids[0] if ids else "")' "$PORT" 2>/dev/null || true
}

echo "=== Federation channel ==="
if [ -z "$(federation_channel "$CHAIN_DEV")" ] || [ -z "$(federation_channel "$CHAIN_TEST")" ]; then
    hc create channel \
        --a-chain "$CHAIN_DEV" \
        --a-connection "$CONN_DEV" \
        --a-port "$PORT" \
        --b-port "$PORT" \
        --channel-version "$VERSION" \
        --order unordered 2>&1 | tail -10
    sleep 5
else
    echo "  federation channel already open on both chains - skipping"
fi

CHANNEL_DEV=$(federation_channel "$CHAIN_DEV")
CHANNEL_TEST=$(federation_channel "$CHAIN_TEST")
if [ -z "$CHANNEL_DEV" ] || [ -z "$CHANNEL_TEST" ]; then
    echo "ERROR: could not discover the federation channel on both chains." >&2
    hc query channels --chain "$CHAIN_DEV" 2>&1 | head -20 >&2
    hc query channels --chain "$CHAIN_TEST" 2>&1 | head -20 >&2
    exit 1
fi

cat > "$OUT_ENV" <<EOF
# Written by bringup.sh. These are the channel ids to put in MsgRegisterPeer.
CHANNEL_DEV=$CHANNEL_DEV
CHANNEL_TEST=$CHANNEL_TEST
CONN_DEV=$CONN_DEV
CONN_TEST=$CONN_TEST
EOF

echo ""
echo "=================================================="
echo "  DONE"
echo "=================================================="
echo "  On $CHAIN_DEV,  register peer $CHAIN_TEST with ibc_channel_id = $CHANNEL_DEV"
echo "  On $CHAIN_TEST, register peer $CHAIN_DEV  with ibc_channel_id = $CHANNEL_TEST"
echo ""
echo "  Saved to $OUT_ENV"
echo ""
echo "  The channel does not relay anything until the daemon runs:"
echo "    $HERMES --config $HERMES_CONFIG start"
