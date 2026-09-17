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
    if [ -f "$HOME_DIR/config/genesis.json" ]; then
        echo "  already initialised - skipping (delete the home dir to redo)"
        return 0
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

start_one() {
    local CHAIN_ID=$1 HOME_DIR=$2 BINARY=$3
    mkdir -p "$LOG_DIR"
    local LOG="$LOG_DIR/$CHAIN_ID.log"
    if pgrep -f "start --home $HOME_DIR" >/dev/null; then
        echo "  $CHAIN_ID already running"
        return 0
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
        start_one "$DEV_ID"  "$HOME_DEV"  "$BIN_DEV"
        start_one "$TEST_ID" "$HOME_TEST" "$BIN_TEST"
        ;;
    status)
        status_one "$DEV_ID"  26657 "$DEV_RPC"
        status_one "$TEST_ID" 36657 "$TEST_RPC"
        ;;
    stop)
        pkill -f "start --home $HOME_DEV"  && echo "  stopped dev"  || echo "  dev not running"
        pkill -f "start --home $HOME_TEST" && echo "  stopped test" || echo "  test not running"
        ;;
    *)
        echo "usage: $0 {setup|start|status|stop}" >&2
        exit 1
        ;;
esac
