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
DEV_ID="sparkdream-dev-1"
DEV_RPC="https://rpc-dev.sparkdream.io"
DEV_PEER="cd2437306c3b74945ed6935628a805350e81ae9b@provider.akash-palmito.org:32704"
DEV_PORTS="26657 26656 9090"

TEST_ID="sparkdream-test-1"
TEST_RPC="https://rpc-test.sparkdream.io"
TEST_PEER="2d34d550572226799c0ac7281318be7520853f38@provider.zencloud.eu:30845"
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
        if [ "$LIVE_SUM" = "$LOCAL_SUM" ]; then
            echo "  already initialised on the live genesis - skipping"
            return 0
        fi

        echo "  GENESIS CHANGED - the chain was reset under the same chain-id."
        echo "    local: ${LOCAL_SUM:0:16}"
        echo "    live:  ${LIVE_SUM:0:16}"
        if [ "${FORCE_REINIT:-0}" != "1" ]; then
            echo "" >&2
            echo "  Refusing to touch $HOME_DIR automatically: re-initialising deletes" >&2
            echo "  its chain data. Stop the node, then re-run with FORCE_REINIT=1." >&2
            echo "  Starting it as-is would wedge the sentry it peers with." >&2
            return 1
        fi
        if pgrep -f "start --home $HOME_DIR" >/dev/null 2>&1; then
            echo "  ERROR: a node is still running against $HOME_DIR. Stop it first:" >&2
            echo "         $0 stop" >&2
            return 1
        fi
        echo "  FORCE_REINIT=1: clearing $HOME_DIR and re-initialising"
        rm -rf "${HOME_DIR:?}/data" "${HOME_DIR:?}/config"
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

genesis_sum_live() {  # <rpc>
    curl -sf -m 60 "$1/genesis" 2>/dev/null \
        | python3 -c 'import json,sys,hashlib
try: g=json.load(sys.stdin)["result"]["genesis"]
except Exception: sys.exit(1)
print(hashlib.sha256(json.dumps(g,sort_keys=True,separators=(",",":")).encode()).hexdigest())' 2>/dev/null || true
}

start_one() {
    local CHAIN_ID=$1 HOME_DIR=$2 BINARY=$3 RPC=$4
    mkdir -p "$LOG_DIR"
    local LOG="$LOG_DIR/$CHAIN_ID.log"
    if pgrep -f "start --home $HOME_DIR" >/dev/null; then
        echo "  $CHAIN_ID already running"
        return 0
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
    echo "  $CHAIN_ID started (pid $!), log: $LOG"
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
        start_one "$DEV_ID"  "$HOME_DEV"  "$BIN_DEV"  "$DEV_RPC"  || rc=1
        start_one "$TEST_ID" "$HOME_TEST" "$BIN_TEST" "$TEST_RPC" || rc=1
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
