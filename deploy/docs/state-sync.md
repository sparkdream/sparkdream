# Syncing a Node with State Sync

State sync allows a new node to join the SparkDream network in minutes
instead of hours or days by downloading a recent application state
snapshot from peers rather than replaying all historical blocks.

## How It Works

A state-synced node fetches a snapshot of the application state at a
recent height from a peer that has snapshot serving enabled. CometBFT
verifies the snapshot against a trusted block hash using light client
verification, then the node continues syncing normally from that height.

The trade-off: a state-synced node has no block history before the
snapshot height. It can serve current queries and participate in
consensus, but cannot answer historical queries for earlier blocks.
Use the `replay-from-archive` tool to fill in historical blocks if
needed.

## Prerequisites

- An initialized node with the correct `genesis.json`
- At least one peer with snapshot serving enabled
  (`snapshot-interval > 0` in their `app.toml`)
- That peer's RPC endpoint must be reachable
- A trusted block height and hash from a reliable source

## Step 1: Initialize the Node

```bash
sparkdreamd init my-node --chain-id sparkdream-1 --home ~/.sparkdream
cp genesis.json ~/.sparkdream/config/genesis.json
```

## Step 2: Get a Trust Height and Hash

You need a recent block height that is a multiple of the snapshot
interval (default 1000) and its corresponding block hash. Query a
trusted RPC endpoint — either the sentry's public RPC or another
trusted node.

```bash
# Set your trusted RPC endpoint
RPC="http://<sentry_provider>:<rpc_port>"

# Get the latest height
LATEST=$(curl -s "$RPC/block" | jq -r '.result.block.header.height')

# Round down to the nearest snapshot interval
# Adjust 1000 to match the snapshot-interval of the serving node
SNAP_INTERVAL=1000
SNAP_HEIGHT=$(( LATEST - (LATEST % SNAP_INTERVAL) ))

# Get the block hash at that height
TRUST_HASH=$(curl -s "$RPC/block?height=$SNAP_HEIGHT" \
  | jq -r '.result.block_id.hash')

echo "trust_height = $SNAP_HEIGHT"
echo "trust_hash = \"$TRUST_HASH\""
```

Verify the values look reasonable — the height should be recent
and the hash should be a 64-character hex string.

## Step 3: Configure State Sync

Edit `~/.sparkdream/config/config.toml` under the `[statesync]` section:

```bash
sed -i 's|^enable *=.*|enable = true|' ~/.sparkdream/config/config.toml

sed -i "s|^rpc_servers *=.*|rpc_servers = \"$RPC,$RPC\"|" \
  ~/.sparkdream/config/config.toml

sed -i "s|^trust_height *=.*|trust_height = $SNAP_HEIGHT|" \
  ~/.sparkdream/config/config.toml

sed -i "s|^trust_hash *=.*|trust_hash = \"$TRUST_HASH\"|" \
  ~/.sparkdream/config/config.toml
```

The `rpc_servers` field requires at least two entries for light client
verification. If you only have one trusted RPC, list it twice. For
production, using two independent RPC endpoints is more robust.

### Optional: Tune state sync performance

```bash
# Reduce chunk request timeout (default 60s, 10s is fine on fast networks)
sed -i 's|^chunk_request_timeout *=.*|chunk_request_timeout = "10s"|' \
  ~/.sparkdream/config/config.toml

# Increase concurrent chunk fetchers (default 1, 4 is faster)
sed -i 's|^chunk_fetchers *=.*|chunk_fetchers = "4"|' \
  ~/.sparkdream/config/config.toml
```

## Step 4: Configure Peers

The node needs at least one peer to discover snapshots from. Add the
snapshot-serving node as a persistent peer:

```bash
# Get the peer's node ID from the RPC
PEER_ID=$(curl -s "$RPC/status" | jq -r '.result.node_info.id')
PEER_ADDR="<peer_provider>:<peer_p2p_port>"

sed -i "s|^persistent_peers *=.*|persistent_peers = \"${PEER_ID}@${PEER_ADDR}\"|" \
  ~/.sparkdream/config/config.toml
```

If syncing via the Tailscale mesh, use the peer's Tailscale IP and
port 26656 directly:

```bash
sed -i "s|^persistent_peers *=.*|persistent_peers = \"${PEER_ID}@100.64.0.1:26656\"|" \
  ~/.sparkdream/config/config.toml
```

## Step 5: Reset and Start

If the node has any existing data, reset it first. State sync only
works on a node with no local state (`LastBlockHeight == 0`).

```bash
# WARNING: This deletes all local chain data
sparkdreamd tendermint unsafe-reset-all --home ~/.sparkdream --keep-addr-book
```

Start the node:

```bash
sparkdreamd start --home ~/.sparkdream
```

You should see log messages like:

```
INF Discovering snapshots for 15s  module=statesync
INF Discovered new snapshot        height=50000 module=statesync
INF Offering snapshot to ABCI app  height=50000 module=statesync
INF SnapshotChunk applied          chunk=0 module=statesync total=5
...
INF Snapshot restored              height=50000 module=statesync
INF Applied snapshot chunk to ABCI app  module=statesync
```

Once the snapshot is restored, the node switches to block sync and
catches up to the current height. This typically takes seconds to
minutes depending on how many blocks were produced since the snapshot.

## Step 6: Disable State Sync After Completion

After the node has synced, disable state sync so it doesn't attempt
to re-sync on the next restart:

```bash
sed -i 's|^enable *=.*|enable = false|' ~/.sparkdream/config/config.toml
```

This is important — leaving state sync enabled on a node that already
has state will cause errors on restart.

## State Sync on Akash

When deploying a new sentry on Akash, the workflow is:

1. Deploy with `WAIT_FOR_CONFIG=true`
2. SSH in and apply the state sync config as described above
3. Start the node manually: `sparkdreamd start --home /root/.sparkdream`
4. Wait for sync to complete
5. Disable state sync in config
6. Redeploy with `WAIT_FOR_CONFIG=false`

Alternatively, if the validator is serving snapshots over Tailscale,
use its Tailscale IP as the RPC server. This keeps the state sync
traffic private.

## Serving Snapshots

To enable a node to serve snapshots for others to state sync from,
set these values in `app.toml`:

```toml
[state-sync]
snapshot-interval = 1000
snapshot-keep-recent = 2
```

This takes a snapshot every 1000 blocks and keeps the 2 most recent.
The node's RPC must be accessible to peers requesting the snapshot.

The validator and sentry templates in this repo have snapshot serving
enabled by default.

## Troubleshooting

### "No snapshots found"

- Verify the snapshot-serving node has `snapshot-interval > 0` in `app.toml`
- Verify enough blocks have been produced since the node started
  (at least `snapshot-interval` blocks)
- Check that the RPC endpoint is reachable: `curl $RPC/status`
- Ensure the peer is connected: check logs for peer connection messages

### "Snapshot verification failed"

- The trust hash or height may be incorrect. Re-fetch them from the RPC
- The snapshot may be from a different chain. Verify `chain-id` matches

### "State sync not attempted, node has local state"

- Run `sparkdreamd tendermint unsafe-reset-all --home ~/.sparkdream`
  to clear existing data before attempting state sync

### Sync is very slow

- Increase `chunk_fetchers` to 4 or 8
- Decrease `chunk_request_timeout` to 10s
- Ensure the serving node has adequate bandwidth
- If using Tailscale, verify direct peer connection (not relayed):
  `tailscale status` should show "direct" not "relay"

## Combining State Sync with Block Archives

State sync gives you a fast-starting node but no block history.
To build a complete archive node efficiently:

1. State sync to the latest snapshot height (fast)
2. Download block archives from Arweave
3. Run `sparkdreamd replay-from-archive` to fill in block history

The replay tool auto-detects the node's current height and only
processes archive files containing blocks beyond that point. See
[archival-strategy.md](archival-strategy.md) for details.