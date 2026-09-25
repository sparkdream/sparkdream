#!/bin/bash
# ------------------------------------------------------------------
# Relay the two test chains with the Hermes relayer IMAGE
# (deploy/docker/Dockerfile-hermes) instead of a host hermes: the same
# container, scripts and contract the chain launcher deploys, driven the
# way the launcher drives it. Replaces setup_ibc.sh + start_relayer.sh when
# the suite runs with --relayer=container.
#
# Usage:
#   relayer_container.sh [setup]   build image, fund key, open both paths, start
#   relayer_container.sh stop      remove container, volume and gRPC bridges
#   relayer_container.sh clear <chain> <port> <channel>
#                                  hermes clear packets, inside the container
#
# What setup proves, in the order the launcher relies on it:
#   1. one mnemonic keys both chains (relayer-bringup --keys-only imports it
#      and deletes the plaintext)
#   2. a plain bank send is enough funding: relayer-fundcheck reports the
#      auth account present with no self-send (x/bank creates it on receive)
#   3. two paths to the same chain -- ICS-20 transfer AND federation -- share
#      one client pair and one connection, each on its own channel
#   4. bringup is idempotent: a second run opens nothing and reports the
#      same channels
#   5. the ready marker survives a container restart, and hermes comes back
#      by itself on the re-pinned config
#
# Writes .ibc_channels with CHANNEL_A/B (federation, as setup_ibc.sh does)
# plus TRANSFER_CHANNEL_A/B, and .relayer_mode = container for the helpers.
#
# Environment:
#   RELAYER_IMAGE   use this image instead of building one
#                   (default: build sparkdream-hermes:e2e from the repo)
#   DOCKER_HOST_ADDR  address the container reaches the host chains on
#                   (default: host.docker.internal, mapped to the host gateway)
# ------------------------------------------------------------------
set -euo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
REPO_ROOT="$( cd "$SCRIPT_DIR/../../.." && pwd )"
source "$SCRIPT_DIR/lib_multichain.sh"

CONTAINER="sd-relayer-e2e"
VOLUME="sd-relayer-e2e"
BRIDGE_PIDS="$SCRIPT_DIR/.relayer_bridge_pids"
MODE_FILE="$SCRIPT_DIR/.relayer_mode"
WORK="$SCRIPT_DIR/data/relayer-container"
HOST_ADDR="${DOCKER_HOST_ADDR:-host.docker.internal}"
IMAGE="${RELAYER_IMAGE:-sparkdream-hermes:e2e}"

# gRPC is bound to localhost on both test chains, as on a deployed sentry;
# these bridges play the part of the launcher's mesh tunnel to sentry-0
GRPC_A=9390; GRPC_B=9190
BRIDGE_A=19390; BRIDGE_B=19190
LCD_A=1617; LCD_B=1417

stop_all() {
    docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
    docker volume rm "$VOLUME" >/dev/null 2>&1 || true
    if [ -f "$BRIDGE_PIDS" ]; then
        # by PID only: a pattern match can hit the calling shell
        while read -r pid; do
            [ -n "$pid" ] && kill "$pid" 2>/dev/null || true
        done < "$BRIDGE_PIDS"
        rm -f "$BRIDGE_PIDS"
    fi
    rm -f "$MODE_FILE"
}

dx() { docker exec "$CONTAINER" "$@"; }

case "${1:-setup}" in
    stop)
        stop_all
        echo "  relayer container stopped"
        exit 0
        ;;
    clear)
        dx hermes --config /data/relayer/config.toml clear packets --chain "$2" --port "$3" --channel "$4"
        exit $?
        ;;
    setup) ;;
    *)
        echo "unknown action: $1" >&2
        exit 2
        ;;
esac

command -v docker >/dev/null 2>&1 || { echo "ERROR: docker is required for --relayer=container" >&2; exit 1; }
command -v socat >/dev/null 2>&1 || { echo "ERROR: socat is required for --relayer=container" >&2; exit 1; }

echo "=================================================="
echo "  IBC SETUP: RELAYER CONTAINER"
echo "=================================================="
stop_all
mkdir -p "$WORK"

# ------------------------------------------------------------------
# 0. Image
# ------------------------------------------------------------------
if [ -z "${RELAYER_IMAGE:-}" ]; then
    echo "=== Building $IMAGE from deploy/docker/Dockerfile-hermes ==="
    docker build -q -f "$REPO_ROOT/deploy/docker/Dockerfile-hermes" -t "$IMAGE" "$REPO_ROOT" >/dev/null
fi
echo "  image: $IMAGE"

# ------------------------------------------------------------------
# 1. gRPC bridges (host 0.0.0.0:<bridge> -> 127.0.0.1:<grpc>)
# ------------------------------------------------------------------
: > "$BRIDGE_PIDS"
for pair in "$BRIDGE_A:$GRPC_A" "$BRIDGE_B:$GRPC_B"; do
    socat "TCP-LISTEN:${pair%%:*},bind=0.0.0.0,fork,reuseaddr" "TCP:127.0.0.1:${pair##*:}" >/dev/null 2>&1 &
    echo $! >> "$BRIDGE_PIDS"
done

# ------------------------------------------------------------------
# 2. One mnemonic, funded on both chains by a plain bank send
# ------------------------------------------------------------------
echo "=== Relayer key ==="
MNEMONIC_FILE="$WORK/relayer.mnemonic"
keys_a delete relayer-e2e -y >/dev/null 2>&1 || true
keys_b delete relayer-e2e -y >/dev/null 2>&1 || true
keys_a add relayer-e2e --output json 2>/dev/null | jq -r .mnemonic > "$MNEMONIC_FILE"
keys_b add relayer-e2e --recover < "$MNEMONIC_FILE" >/dev/null 2>&1
RELAYER_A=$(keys_a show relayer-e2e -a)
RELAYER_B=$(keys_b show relayer-e2e -a)
[ "$RELAYER_A" = "$RELAYER_B" ] || { echo "ERROR: one mnemonic gave two addresses ($RELAYER_A / $RELAYER_B)" >&2; exit 1; }
echo "  relayer: $RELAYER_A (both chains)"

for c in a b; do
    TX=$(cli_$c tx bank send alice "$RELAYER_A" 1000000000${BOND_DENOM} --from alice -y --fees 5000${BOND_DENOM} --output json)
    submit_and_wait_$c "$TX" "fund relayer on chain-$c"
done

# ------------------------------------------------------------------
# 3. Config + path manifest, rendered as the launcher renders them
# ------------------------------------------------------------------
chain_block() {  # <id> <rpc port> <bridge port> <filter>
    cat <<EOF
[[chains]]
id = '$1'
type = 'CosmosSdk'
rpc_addr = 'http://$HOST_ADDR:$2'
grpc_addr = 'http://$HOST_ADDR:$3'
event_source = { mode = 'push', url = 'ws://$HOST_ADDR:$2/websocket', batch_delay = '500ms' }
rpc_timeout = '20s'
account_prefix = 'sprkdrm'
key_name = 'relayer'
key_store_folder = '/data/relayer/keys'
address_type = { derivation = 'cosmos' }
store_prefix = 'ibc'
default_gas = 200000
max_gas = 4000000
gas_price = { price = 0.0025, denom = '$BOND_DENOM' }
gas_multiplier = 3.0
max_msg_num = 30
max_tx_size = 180000
clock_drift = '10s'
max_block_time = '30s'
trust_threshold = { numerator = '2', denominator = '3' }
memo_prefix = 'sparkdream-e2e'

[chains.packet_filter]
policy = 'allow'
list = [$4]

EOF
}
render_config() {  # <filter a> <filter b>
    cat <<'EOF'
[global]
log_level = 'info'

[mode.clients]
enabled = true
refresh = true
misbehaviour = true

[mode.connections]
enabled = true

[mode.channels]
enabled = true

[mode.packets]
enabled = true
clear_interval = 100
clear_on_start = true
tx_confirmation = true

[rest]
enabled = false
host = '127.0.0.1'
port = 3000

[telemetry]
enabled = false
host = '127.0.0.1'
port = 3001

EOF
    chain_block "$CHAIN_A_ID" 56657 "$BRIDGE_A" "$1"
    chain_block "$CHAIN_B_ID" 36657 "$BRIDGE_B" "$2"
}
WILD="['transfer', '*'], ['federation', '*']"
render_config "$WILD" "$WILD" > "$WORK/config.toml"
cat > "$WORK/relayer.json" <<EOF
{
  "chains": [
    { "id": "$CHAIN_A_ID", "hd_path": "m/44'/118'/0'/0/0", "lcd": "http://$HOST_ADDR:$LCD_A" },
    { "id": "$CHAIN_B_ID", "hd_path": "m/44'/118'/0'/0/0", "lcd": "http://$HOST_ADDR:$LCD_B" }
  ],
  "paths": [
    { "id": "transfer", "a": "$CHAIN_A_ID", "b": "$CHAIN_B_ID", "port": "transfer", "version": "ics20-1", "order": "unordered" },
    { "id": "federation", "a": "$CHAIN_A_ID", "b": "$CHAIN_B_ID", "port": "federation", "version": "federation-1", "order": "unordered" }
  ]
}
EOF

# ------------------------------------------------------------------
# 4. Container: waits for the ready marker, sshd/tailscale unused here
# ------------------------------------------------------------------
echo "=== Starting $CONTAINER ==="
docker volume create "$VOLUME" >/dev/null
docker run -d --name "$CONTAINER" \
    --add-host "host.docker.internal:host-gateway" \
    -v "$VOLUME:/data" "$IMAGE" >/dev/null
echo "container" > "$MODE_FILE"

dx mkdir -p /data/relayer/mnemonics
docker cp -q "$WORK/config.toml" "$CONTAINER:/data/relayer/config.toml"
docker cp -q "$WORK/relayer.json" "$CONTAINER:/data/relayer/relayer.json"
for id in "$CHAIN_A_ID" "$CHAIN_B_ID"; do
    docker cp -q "$MNEMONIC_FILE" "$CONTAINER:/data/relayer/mnemonics/$id.mnemonic"
done
rm -f "$MNEMONIC_FILE"

echo "=== relayer-bringup --keys-only ==="
dx relayer-bringup --keys-only >/dev/null
LEFT=$(dx sh -c 'ls /data/relayer/mnemonics | wc -l')
[ "$LEFT" -eq 0 ] || { echo "ERROR: $LEFT mnemonic file(s) left behind after import" >&2; exit 1; }

echo "=== relayer-fundcheck ==="
if ! FUND=$(dx relayer-fundcheck); then
    echo "$FUND" >&2
    echo "ERROR: relayer not ready after a plain bank send" >&2
    exit 1
fi
echo "$FUND" | jq -c '.[] | {chain, balance, account, ready}'
echo "$FUND" | jq -e 'all(.account == true)' >/dev/null \
    || { echo "ERROR: fundcheck found no auth account (bank send should create it)" >&2; exit 1; }

# ------------------------------------------------------------------
# 5. Bringup, twice: the second run must open nothing
# ------------------------------------------------------------------
echo "=== relayer-bringup (both paths) ==="
dx relayer-bringup > "$WORK/channels.json" 2> "$WORK/bringup.log" \
    || { tail -30 "$WORK/bringup.log" >&2; exit 1; }
jq -c '.[] | {id, a: .a.channel, b: .b.channel, conn: .a.connection}' "$WORK/channels.json"

CONNS=$(jq -r '[.[].a.connection] | unique | length' "$WORK/channels.json")
[ "$CONNS" -eq 1 ] || { echo "ERROR: the two paths opened $CONNS connections, expected one shared" >&2; exit 1; }

echo "=== relayer-bringup again (idempotency) ==="
dx relayer-bringup > "$WORK/channels2.json" 2> "$WORK/bringup2.log"
if grep -q "creating" "$WORK/bringup2.log"; then
    grep "creating" "$WORK/bringup2.log" >&2
    echo "ERROR: a re-run created IBC objects" >&2
    exit 1
fi
cmp -s <(jq -S . "$WORK/channels.json") <(jq -S . "$WORK/channels2.json") \
    || { echo "ERROR: a re-run reported different channels" >&2; exit 1; }

ch() { jq -r --arg p "$1" --arg s "$2" '.[] | select(.port == $p) | .[$s].channel' "$WORK/channels.json"; }
TRANSFER_CHANNEL_A=$(ch transfer a); TRANSFER_CHANNEL_B=$(ch transfer b)
CHANNEL_A=$(ch federation a); CHANNEL_B=$(ch federation b)
cat > "$IBC_ENV" <<EOF
# Written by relayer_container.sh
CHANNEL_A=$CHANNEL_A
CHANNEL_B=$CHANNEL_B
TRANSFER_CHANNEL_A=$TRANSFER_CHANNEL_A
TRANSFER_CHANNEL_B=$TRANSFER_CHANNEL_B
CONN_A=$(jq -r '.[0].a.connection' "$WORK/channels.json")
CONN_B=$(jq -r '.[0].b.connection' "$WORK/channels.json")
EOF

# ------------------------------------------------------------------
# 6. Pin the filter to exactly these channels, start, then restart: the
#    ready marker is on the volume, so hermes must come back by itself
# ------------------------------------------------------------------
render_config \
    "['transfer', '$TRANSFER_CHANNEL_A'], ['federation', '$CHANNEL_A']" \
    "['transfer', '$TRANSFER_CHANNEL_B'], ['federation', '$CHANNEL_B']" > "$WORK/config.toml"
docker cp -q "$WORK/config.toml" "$CONTAINER:/data/relayer/config.toml"
dx touch /data/relayer/ready

wait_started() {  # [since: docker timestamp; whole log when omitted]
    local since=()
    [ -n "${1:-}" ] && since=(--since "$1")
    for _ in $(seq 1 60); do
        if docker logs "${since[@]}" "$CONTAINER" 2>&1 | grep -q "Hermes has started"; then return 0; fi
        sleep 2
    done
    docker logs --tail 40 "$CONTAINER" >&2
    return 1
}
echo "=== Waiting for hermes ==="
wait_started || { echo "ERROR: hermes did not start on the ready marker" >&2; exit 1; }
SINCE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
docker restart "$CONTAINER" >/dev/null
wait_started "$SINCE" || { echo "ERROR: hermes did not come back after a container restart" >&2; exit 1; }

echo ""
echo "  transfer:   $CHAIN_A_ID/$TRANSFER_CHANNEL_A <-> $CHAIN_B_ID/$TRANSFER_CHANNEL_B"
echo "  federation: $CHAIN_A_ID/$CHANNEL_A <-> $CHAIN_B_ID/$CHANNEL_B"
echo "  relayer container up; saved to $IBC_ENV"
