#!/bin/bash
# ------------------------------------------------------------------
# Stand up local full nodes for sparkdream-dev-1 and sparkdream-test-1 so
# Hermes has a gRPC endpoint to talk to.
#
# Why this exists: the deployed sentries expose only 2222 (ssh), 26656 (p2p)
# and 26657 (rpc) -- see deploy/config/network/*/sentry.sdl.yaml -- and their
# app.toml binds gRPC to localhost. Hermes requires gRPC. Rather than change
# the deployment or depend on holding the sentry's SSH key, sync a local full
# node per chain: a local node serves gRPC on localhost natively, and both
# chains are small enough that this is quick.
#
# IMPORTANT: each chain needs its OWN binary, built with that network's build
# tag. x/federation, x/shield and x/identity all carry //go:build devnet |
# testnet | mainnet genesis defaults, so an untagged binary computes a
# different app hash and the node dies at block 1 with:
#
#   Error in validation  err="wrong Block.Header.AppHash. Expected ..., got ..."
#
# Verified the hard way: an untagged build stalls at height 1 against the
# devnet; the same source built with -tags devnet syncs cleanly. Build both:
#
#   cd <chain repo>
#   CGO_ENABLED=0 go build -tags devnet  -o ~/.local/bin/sparkdreamd-devnet  ./cmd/sparkdreamd/main.go
#   CGO_ENABLED=0 go build -tags testnet -o ~/.local/bin/sparkdreamd-testnet ./cmd/sparkdreamd/main.go
#
# The version must also match what the network runs -- check
# `curl -s https://rpc-dev.sparkdream.io/abci_info`.
#
# Ports (dev / test), chosen to avoid the defaults being taken twice:
#   RPC   26657 / 36657
#   P2P   26656 / 36656
#   gRPC   9090 /  9091   <- what hermes_config.toml points at
#
# Usage:
#   ./local_nodes.sh setup     # init both homes, fetch genesis, write config
#   ./local_nodes.sh start     # start both in the background
#   ./local_nodes.sh status    # sync progress
#   ./local_nodes.sh stop
# ------------------------------------------------------------------
set -euo pipefail

BIN_DEV="${BIN_DEV:-$HOME/.local/bin/sparkdreamd-devnet}"
BIN_TEST="${BIN_TEST:-$HOME/.local/bin/sparkdreamd-testnet}"
HOME_DEV="${HOME_DEV:-$HOME/.sparkdream-relay-dev}"
HOME_TEST="${HOME_TEST:-$HOME/.sparkdream-relay-test}"
LOG_DIR="${LOG_DIR:-$HOME/.sparkdream-relay-logs}"

# chain_id | rpc host | node_id@p2p | rpc port | p2p port | grpc port | home
#
# The persistent-peer strings below are FALLBACKS. Akash reassigns a lease's
# external ports on every redeploy, so a hardcoded p2p port goes stale exactly
# when you need it -- the node then sits at height 0 logging "i/o timeout" and
# looks like a sync problem rather than a config one. (devnet moved 32704 ->
# 30370 on the 2026-09-19 redeploy.)
#
# resolve_peer asks the chain instead: /status reports the sentry's own node id
# and the address it is reachable on, which is precisely the persistent-peer
# string. Falls back to the literal below when the RPC is down or reports
# something unusable like 0.0.0.0.
resolve_peer() {  # <rpc> <fallback>
    local out id addr
    out=$(curl -sf -m 10 "$1/status" 2>/dev/null | python3 -c '
import json, sys
try:
    n = json.load(sys.stdin)["result"]["node_info"]
    print(n["id"], n.get("listen_addr", ""))
except Exception:
    pass' 2>/dev/null || true)
    id=$(echo "$out" | awk '{print $1}')
    addr=$(echo "$out" | awk '{print $2}')
    addr="${addr#tcp://}"
    case "$addr" in
        ""|0.0.0.0:*|127.0.0.1:*|localhost:*) echo "$2"; return 0 ;;
    esac
    case "$addr" in
        *:*) [ -n "$id" ] && { echo "$id@$addr"; return 0; } ;;
    esac
    echo "$2"
}

DEV_ID="sparkdream-dev-1"
DEV_RPC="https://rpc-dev.sparkdream.io"
DEV_PEER="$(resolve_peer "$DEV_RPC" "cd2437306c3b74945ed6935628a805350e81ae9b@provider.akash-palmito.org:30370")"
DEV_PORTS="26657 26656 9090"

TEST_ID="sparkdream-test-1"
TEST_RPC="https://rpc-test.sparkdream.io"
TEST_PEER="$(resolve_peer "$TEST_RPC" "2d34d550572226799c0ac7281318be7520853f38@provider.zencloud.eu:30845")"
TEST_PORTS="36657 36656 9091"

# min-gas-prices differ per chain; a local full node only needs a value that
# parses, but matching the network avoids surprises if you ever sign from it.
DEV_GAS="0.025usparz.sparkdreamdev"
TEST_GAS="0.025uspark.sparkdreamtest"

setup_one() {
    local NAME=$1 CHAIN_ID=$2 RPC=$3 PEER=$4 PORTS=$5 HOME_DIR=$6 GAS=$7 BINARY=$8
    read -r RPC_PORT P2P_PORT GRPC_PORT <<< "$PORTS"
    [ -x "$BINARY" ] || { echo "ERROR: $BINARY missing. See the build note at the top." >&2; exit 1; }

    echo "=== $CHAIN_ID -> $HOME_DIR ==="

    # The deployed chain is the source of truth for genesis, never the copy in
    # deploy/config/network -- that one drifts (it preserves genesis_time
    # across regenerations, and the launcher rewrites parts of it), so a node
    # built from the repo can hold a document the running chain never started
    # from.
    #
    # Existence alone is NOT a reason to skip. A devnet reset writes a new
    # genesis under the SAME chain-id, and CometBFT's p2p handshake checks
    # only the chain-id -- so a node left on the previous genesis is accepted
    # as a peer, advertises the dead chain's height, and poisons the sentry's
    # blocksync until someone sets max_num_inbound_peers = 0. That happened on
    # 2026-09-17. Compare against the live document instead and re-init when
    # it has moved.
    if [ -f "$HOME_DIR/config/genesis.json" ]; then
        local LIVE_SUM LOCAL_SUM
        LIVE_SUM=$(curl -sf -m 60 "$RPC/genesis" 2>/dev/null \
            | python3 -c 'import json,sys,hashlib
try:
    g=json.load(sys.stdin)["result"]["genesis"]
except Exception:
    sys.exit(1)
print(hashlib.sha256(json.dumps(g,sort_keys=True,separators=(",",":")).encode()).hexdigest())' 2>/dev/null || true)
        LOCAL_SUM=$(python3 -c 'import json,sys,hashlib
g=json.load(open(sys.argv[1]))
print(hashlib.sha256(json.dumps(g,sort_keys=True,separators=(",",":")).encode()).hexdigest())' \
            "$HOME_DIR/config/genesis.json" 2>/dev/null || true)

        if [ -z "$LIVE_SUM" ]; then
            echo "  WARNING: could not reach $RPC to check genesis; leaving $HOME_DIR alone." >&2
            echo "           If the chain was reset, this node is stale and must not be started." >&2
            return 0
        fi
        # FORCE_REINIT is checked BEFORE the genesis comparison, not inside
        # the changed-genesis branch. A reset that reuses the same genesis
        # file produces an identical hash, so "genesis matches" does not mean
        # "same chain" -- and an operator who has just reset the chain and
        # passed FORCE_REINIT=1 explicitly should never be told "skipping".
        # Getting this backwards is what left a node holding 2059 blocks from
        # a dead instance against a chain that was at 876.
        if [ "${FORCE_REINIT:-0}" = "1" ]; then
            if pgrep -f "start --home $HOME_DIR" >/dev/null 2>&1; then
                echo "  ERROR: a node is still running against $HOME_DIR. Stop it first:" >&2
                echo "         $0 stop" >&2
                return 1
            fi
            echo "  FORCE_REINIT=1: clearing $HOME_DIR and re-initialising"
            rm -rf "${HOME_DIR:?}/data" "${HOME_DIR:?}/config"
        elif [ "$LIVE_SUM" = "$LOCAL_SUM" ]; then
            echo "  genesis matches the live chain - skipping"
            echo "    NOTE: a reset that reuses the same genesis is invisible here."
            echo "    ./local_nodes.sh start verifies block 1 and will refuse if the"
            echo "    instance differs; FORCE_REINIT=1 re-initialises regardless."
            return 0
        else

            echo "  GENESIS CHANGED - the chain was reset under the same chain-id."
            echo "    local: ${LOCAL_SUM:0:16}"
            echo "    live:  ${LIVE_SUM:0:16}"
            echo "" >&2
            echo "  Refusing to touch $HOME_DIR automatically: re-initialising deletes" >&2
            echo "  its chain data. Stop the node, then re-run with FORCE_REINIT=1." >&2
            echo "  Starting it as-is would wedge the sentry it peers with." >&2
            return 1
        fi
    fi

    "$BINARY" init "$NAME" --chain-id "$CHAIN_ID" --home "$HOME_DIR" >/dev/null 2>&1
    # `init` writes this, but it is inside data/ and trivially lost if the
    # data dir is ever cleared by hand -- the node then refuses to start with
    # "priv_validator_state.json: no such file or directory".
    mkdir -p "$HOME_DIR/data"
    [ -f "$HOME_DIR/data/priv_validator_state.json" ] || \
        echo '{"height":"0","round":0,"step":0}' > "$HOME_DIR/data/priv_validator_state.json"
    echo "  initialised"

    # The /genesis RPC endpoint wraps the document in a JSON-RPC envelope.
    # These chains' genesis files are small enough to come back in one piece;
    # a chunked response would need /genesis_chunked instead.
    curl -sf -m 60 "$RPC/genesis" \
        | python3 -c 'import json,sys; json.dump(json.load(sys.stdin)["result"]["genesis"], sys.stdout)' \
        > "$HOME_DIR/config/genesis.json"
    echo "  genesis: $(wc -c < "$HOME_DIR/config/genesis.json") bytes"

    local CFG="$HOME_DIR/config/config.toml"
    local APP="$HOME_DIR/config/app.toml"

    sed -i "s|^persistent_peers = .*|persistent_peers = \"$PEER\"|" "$CFG"
    sed -i "s|^laddr = \"tcp://127.0.0.1:26657\"|laddr = \"tcp://127.0.0.1:$RPC_PORT\"|" "$CFG"
    sed -i "s|^laddr = \"tcp://0.0.0.0:26656\"|laddr = \"tcp://0.0.0.0:$P2P_PORT\"|" "$CFG"
    # A relayer node is not serving anyone else; don't advertise for inbound.
    sed -i "s|^addr_book_strict = .*|addr_book_strict = false|" "$CFG"

    # gRPC is the entire point of this node.
    python3 - "$APP" "$GRPC_PORT" "$GAS" <<'PY'
import re, sys
path, grpc_port, gas = sys.argv[1], sys.argv[2], sys.argv[3]
s = open(path).read()
s = re.sub(r'^minimum-gas-prices = .*$', f'minimum-gas-prices = "{gas}"', s, count=1, flags=re.M)
# Only the [grpc] block's address, not [grpc-web]'s.
s = re.sub(r'(\[grpc\]\n(?:.*\n)*?address = )"[^"]*"', rf'\g<1>"localhost:{grpc_port}"', s, count=1)
s = re.sub(r'(\[grpc\]\n(?:.*\n)*?enable = )\w+', r'\g<1>true', s, count=1)
# Keep the disk footprint small; a relayer needs recent state, not history.
s = re.sub(r'^pruning = .*$', 'pruning = "everything"', s, count=1, flags=re.M)
open(path, 'w').write(s)
PY
    echo "  rpc=$RPC_PORT p2p=$P2P_PORT grpc=$GRPC_PORT gas=$GAS"
}

# sha256 of a genesis document in canonical form (sorted keys, no spaces), so
# a node's on-disk file and the same document served over RPC compare equal
# despite being serialised differently. Raw sha256sum of the two files does
# NOT match even when they are the same genesis.
genesis_sum_local() {  # <home dir>
    python3 -c 'import json,sys,hashlib
print(hashlib.sha256(json.dumps(json.load(open(sys.argv[1])),sort_keys=True,separators=(",",":")).encode()).hexdigest())' \
        "$1/config/genesis.json" 2>/dev/null || true
}

# Hash of block 1, which fingerprints a chain INSTANCE rather than its
# genesis file.
#
# The genesis comparison above is necessary but not sufficient: a reset that
# reuses the same genesis.json produces an identical hash, because the commons
# governance bootstrap runs at InitGenesis and never appears in the file. That
# is not hypothetical -- on 2026-09-19 the devnet was reset with a byte-
# identical genesis, the genesis guard said "already initialised", and the
# local node came up holding 2059 blocks from the previous instance against a
# chain that was at 876.
#
# Block 1 differs between instances even from the same genesis: it carries the
# proposer's timestamp and signatures. Comparing it catches every reset the
# genesis hash cannot.
block1_hash_live() {  # <rpc>
    curl -sf -m 10 "$1/block?height=1" 2>/dev/null \
        | python3 -c 'import json,sys; print(json.load(sys.stdin)["result"]["block_id"]["hash"])' 2>/dev/null || true
}

block1_hash_local() {  # <port>
    curl -sf -m 5 "http://127.0.0.1:$1/block?height=1" 2>/dev/null \
        | python3 -c 'import json,sys; print(json.load(sys.stdin)["result"]["block_id"]["hash"])' 2>/dev/null || true
}

genesis_sum_live() {  # <rpc>
    curl -sf -m 60 "$1/genesis" 2>/dev/null \
        | python3 -c 'import json,sys,hashlib
try: g=json.load(sys.stdin)["result"]["genesis"]
except Exception: sys.exit(1)
print(hashlib.sha256(json.dumps(g,sort_keys=True,separators=(",",":")).encode()).hexdigest())' 2>/dev/null || true
}

# verify_instance <chain-id> <rpc> <port> [pid-to-kill-on-mismatch]
#
# Compares block 1 with the live chain. Genesis matching is necessary but not
# sufficient -- a reset that reuses the same genesis.json is invisible to a
# genesis hash comparison, because the commons governance bootstrap runs at
# InitGenesis and never lands in the file. Block 1 carries the proposer's
# timestamp and signatures, so it differs between instances built from the
# same genesis.
#
# Returns 0 when the instance matches or cannot be determined (a missing
# answer is not evidence of a mismatch); 1 only on a confirmed mismatch.
verify_instance() {
    local CHAIN_ID=$1 RPC=$2 PORT=$3 PID=${4:-}
    [ -n "$PORT" ] || return 0

    # Wait for the RPC to answer at all.
    local i h
    for i in $(seq 1 30); do
        h=$(curl -sf -m 5 "http://127.0.0.1:$PORT/status" 2>/dev/null \
            | python3 -c 'import json,sys; print(json.load(sys.stdin)["result"]["sync_info"]["latest_block_height"])' 2>/dev/null || true)
        [ -n "$h" ] && break
        sleep 2
    done
    if [ -z "$h" ]; then
        echo "  WARNING: $CHAIN_ID did not answer on :$PORT -- chain instance unverified." >&2
        return 0
    fi

    # A node with no blocks cannot be holding stale ones, and /block?height=1
    # errors until it has synced that far. Nothing to check, and saying so
    # beats a warning that looks like a failure on every fresh init.
    if [ "$h" = "0" ]; then
        echo "  $CHAIN_ID freshly initialised (height 0) - nothing to verify yet"
        return 0
    fi

    local lh rh
    lh=$(block1_hash_local "$PORT")
    if [ -z "$lh" ]; then
        echo "  WARNING: $CHAIN_ID at height $h but block 1 unavailable -- unverified." >&2
        return 0
    fi
    rh=$(block1_hash_live "$RPC")
    if [ -z "$rh" ]; then
        echo "  WARNING: could not reach $RPC -- chain instance unverified." >&2
        return 0
    fi
    if [ "$lh" != "$rh" ]; then
        [ -n "$PID" ] && kill "$PID" 2>/dev/null || true
        echo "  $CHAIN_ID: same genesis, DIFFERENT chain instance." >&2
        echo "    local block 1:  ${lh:0:16}" >&2
        echo "    live  block 1:  ${rh:0:16}" >&2
        echo "  The chain was reset and reused its genesis file, so the genesis" >&2
        echo "  check cannot see it. This node holds blocks from the previous" >&2
        echo "  instance and will wedge the sentry it peers with." >&2
        echo "  Stop it and re-initialise:  $0 stop && FORCE_REINIT=1 $0 setup" >&2
        return 1
    fi
    echo "  $CHAIN_ID chain instance verified (block 1 ${lh:0:12})"
    return 0
}

start_one() {
    local CHAIN_ID=$1 HOME_DIR=$2 BINARY=$3 RPC=$4 PORT=${5:-}
    mkdir -p "$LOG_DIR"
    local LOG="$LOG_DIR/$CHAIN_ID.log"
    if pgrep -f "start --home $HOME_DIR" >/dev/null; then
        echo "  $CHAIN_ID already running"
        # Still verify it. A node that was already up when the chain was reset
        # is exactly the dangerous case, and returning early here would skip
        # every check below -- which is how a stale node stays up unnoticed.
        verify_instance "$CHAIN_ID" "$RPC" "${PORT:-}" "" && return 0
        return 1
    fi

    # Refuse to start a node whose genesis has moved. `setup` already checks
    # this, but setup is not what gets run after a chain reset -- `start` is,
    # and starting a stale node is precisely what wedges the sentry it peers
    # with: CometBFT authenticates the chain-id and nothing else, so the node
    # is accepted as a peer and then advertises a height from a chain that no
    # longer exists. This exact sequence took the devnet sentry down three
    # times (2026-09-17, and twice on 09-18) before the check moved here.
    local live local_sum
    live=$(genesis_sum_live "$RPC")
    local_sum=$(genesis_sum_local "$HOME_DIR")
    if [ -z "$live" ]; then
        echo "  WARNING: $CHAIN_ID -- could not reach $RPC to verify genesis; starting anyway." >&2
        echo "           If the chain was reset while this node was down, stop it and re-run setup." >&2
    elif [ "$live" != "$local_sum" ]; then
        echo "  REFUSING to start $CHAIN_ID: its genesis does not match the live chain." >&2
        echo "    local: ${local_sum:0:16}" >&2
        echo "    live:  ${live:0:16}" >&2
        echo "  The chain was reset. Starting this node would wedge the sentry's blocksync." >&2
        echo "  Re-initialise first:  FORCE_REINIT=1 $0 setup" >&2
        return 1
    fi

    nohup "$BINARY" start --home "$HOME_DIR" > "$LOG" 2>&1 &
    local PID=$!
    echo "  $CHAIN_ID started (pid $PID), log: $LOG"

    # Genesis matching is not proof the node is on the same chain instance;
    # see verify_instance.
    verify_instance "$CHAIN_ID" "$RPC" "${PORT:-}" "$PID" || return 1
}

status_one() {
    local CHAIN_ID=$1 PORT=$2 REMOTE=$3
    local LOCAL REMOTE_H CATCHING
    LOCAL=$(curl -sf -m 5 "http://127.0.0.1:$PORT/status" 2>/dev/null \
        | python3 -c 'import json,sys; d=json.load(sys.stdin)["result"]["sync_info"]; print(d["latest_block_height"], d["catching_up"])' 2>/dev/null) || LOCAL=""
    REMOTE_H=$(curl -sf -m 10 "$REMOTE/status" 2>/dev/null \
        | python3 -c 'import json,sys; print(json.load(sys.stdin)["result"]["sync_info"]["latest_block_height"])' 2>/dev/null) || REMOTE_H="?"
    if [ -z "$LOCAL" ]; then
        echo "  $CHAIN_ID: not responding on :$PORT (network height $REMOTE_H)"
    else
        read -r H C <<< "$LOCAL"
        echo "  $CHAIN_ID: local $H / network $REMOTE_H  (catching_up=$C)"
    fi
}

case "${1:-}" in
    setup)
        echo "devnet binary:  $BIN_DEV  ($("$BIN_DEV" version 2>&1 | head -1))"
        echo "testnet binary: $BIN_TEST ($("$BIN_TEST" version 2>&1 | head -1))"
        echo ""
        setup_one relay-dev  "$DEV_ID"  "$DEV_RPC"  "$DEV_PEER"  "$DEV_PORTS"  "$HOME_DEV"  "$DEV_GAS"  "$BIN_DEV"
        setup_one relay-test "$TEST_ID" "$TEST_RPC" "$TEST_PEER" "$TEST_PORTS" "$HOME_TEST" "$TEST_GAS" "$BIN_TEST"
        echo ""
        echo "Now: ./local_nodes.sh start   then watch ./local_nodes.sh status"
        ;;
    start)
        rc=0
        start_one "$DEV_ID"  "$HOME_DEV"  "$BIN_DEV"  "$DEV_RPC"  26657 || rc=1
        start_one "$TEST_ID" "$HOME_TEST" "$BIN_TEST" "$TEST_RPC" 36657 || rc=1
        exit $rc
        ;;
    status)
        status_one "$DEV_ID"  26657 "$DEV_RPC"
        status_one "$TEST_ID" 36657 "$TEST_RPC"
        ;;
    stop)
        for pair in "dev:$HOME_DEV" "test:$HOME_TEST"; do
            label="${pair%%:*}"; home="${pair#*:}"
            if pkill -f "start --home $home"; then
                # Wait for it to actually exit: `setup --force` deletes this
                # home, and racing a still-running process there corrupts it.
                for _ in $(seq 1 30); do
                    pgrep -f "start --home $home" >/dev/null || break
                    sleep 1
                done
                pgrep -f "start --home $home" >/dev/null \
                    && echo "  $label STILL RUNNING after 30s" >&2 \
                    || echo "  stopped $label"
            else
                echo "  $label not running"
            fi
        done
        ;;
    *)
        echo "usage: $0 {setup|start|status|stop}" >&2
        exit 1
        ;;
esac
