# Block Archival and Recovery Strategy

SparkDream uses a layered archival approach that ensures no chain data
is ever permanently lost, while minimizing storage costs and avoiding
data duplication.

## Core Principle

Blocks are incremental state diffs. The genesis file defines the
initial state, and each subsequent block transforms that state
deterministically. If you have genesis + every block, you can
reconstruct the complete chain state at any height by replaying
from the beginning. No snapshots, no duplication — each archive
file is purely additive.

## Archival Layers

### Layer 1: Live Nodes

Running nodes (validator, sentry, archive) maintain the current
block store and application state. This is the primary data source
for day-to-day operations and queries.

- Validator: `pruning = "default"`, keeps recent state, all blocks
- Sentry: `pruning = "everything"`, minimal state, all blocks
- Archive node (home LAN): `pruning = "nothing"`, full state, all blocks

### Layer 2: Incremental Block Archives

The `block-archiver.sh` script periodically fetches new blocks from
the sentry's RPC and saves them as gzipped JSONL files:

```
blocks_1_to_10000.jsonl.gz
blocks_10001_to_20000.jsonl.gz
blocks_20001_to_30000.jsonl.gz
```

Each file contains only new blocks since the previous archive. A
state file (`.last_archived_height`) tracks progress, ensuring zero
overlap between runs.

Archive format (v2) includes both `/block` and `/block_results`
data for each height, providing everything needed for replay.

### Layer 3: Permanent Storage on Arweave

Archive files are uploaded to Arweave for permanent, immutable
storage. Each upload is tagged with `App-Name`, `Chain-ID`, and
`Block-Range-From/To` for discoverability. A manifest CSV maps
block ranges to Arweave transaction IDs.

Arweave storage is pay-once, permanent. Once uploaded, the data
is available forever without ongoing fees.

### Layer 4: Redundant Storage on Storacha/IPFS (optional)

Archive files can also be uploaded to Storacha (formerly web3.storage)
for IPFS availability. This provides a second retrieval path and
faster access via IPFS gateways. The free tier covers 5 GiB.

## Archival Workflow

Run periodically (e.g., daily or weekly):

```bash
# 1. Archive new blocks from the sentry RPC
ssh -p <port> root@<sentry_provider> \
  'RPC_URL=http://localhost:26657 OUTPUT_DIR=/root/.sparkdream/archives ./block-archiver.sh'

# 2. Download new archives to local machine
scp -O -P <port> -i ~/.ssh/key \
  root@<sentry_provider>:/root/.sparkdream/archives/*.jsonl.gz \
  ./archives/

# 3. Upload to Arweave (permanent)
./arweave-upload.sh -w ~/arweave-wallet.json ./archives/

# 4. Optionally upload to Storacha (IPFS redundancy)
./storacha-upload.sh ./archives/
```

Both upload scripts track which files have already been uploaded
and skip duplicates automatically.

## Recovery Scenarios

### Scenario 1: Sentry goes down

Deploy a new sentry on Akash. State sync from the validator over
Tailscale. The sentry is operational in minutes.

### Scenario 2: Validator goes down

Deploy a new validator on Akash. State sync from the sentry (or
restore from the archive node on your home LAN). Reconnect TMKMS.

### Scenario 3: All Akash nodes are lost

1. Start from your home archive node (if running)
2. Or state sync a new node from any surviving peer
3. Redeploy validator and sentry on Akash
4. Reconnect the mesh via Headscale

### Scenario 4: Complete network loss (all nodes down)

This is the worst case — no running peers to sync from. Recovery
relies entirely on the archived data:

```bash
# 1. Initialize a fresh node
sparkdreamd init recovery --chain-id sparkdream-1

# 2. Copy genesis
cp genesis.json ~/.sparkdream/config/genesis.json

# 3. Download all block archives from Arweave
#    Use the manifest to find transaction IDs
#    Fetch from https://arweave.net/<TX_ID>

# 4. Replay all blocks from genesis
sparkdreamd replay-from-archive \
  --home ~/.sparkdream \
  --archive-dir ./archives \
  --validate true

# 5. Start the node
sparkdreamd start --home ~/.sparkdream
```

### Scenario 5: Fast recovery with state sync + archive fill

For a quicker restore that also has full block history:

```bash
# 1. State sync to a recent height (minutes)
#    Configure statesync in config.toml, start, wait, stop

# 2. Download only the archive files beyond the sync height
#    No need to download the full history initially

# 3. Replay from the sync point
sparkdreamd replay-from-archive \
  --home ~/.sparkdream \
  --archive-dir ./archives

# 4. Optionally, replay older archives later to fill
#    complete block history for a true archive node
```

### Scenario 6: Resume interrupted replay

The replay tool auto-detects the last committed height. If a
replay is interrupted (crash, power loss), simply run it again:

```bash
sparkdreamd replay-from-archive \
  --home ~/.sparkdream \
  --archive-dir ./archives
# Automatically resumes from where it left off
```

## Discovering Archives on Arweave

All archives are tagged for GraphQL discovery:

```graphql
{
  transactions(
    tags: [
      { name: "App-Name", values: ["sparkdream-block-archive"] }
      { name: "Chain-ID", values: ["sparkdream-1"] }
    ]
    sort: HEIGHT_ASC
  ) {
    edges {
      node {
        id
        tags { name value }
      }
    }
  }
}
```

Query this at `https://arweave.net/graphql` to enumerate all
archive files with their block ranges and transaction IDs.

## Storage Cost Estimates

At current chain activity (~90MB/day total node growth, blocks
are approximately 1.5KB each):

- 10,000 blocks ≈ 15MB compressed archive
- 1 year of blocks ≈ 5-10GB on Arweave
- Arweave cost: one-time payment, roughly $0.50-1.00/GB at
  current AR prices
- Storacha: free tier covers 5 GiB, paid plans from $3/month

These estimates assume low transaction volume. Costs scale with
chain activity.

## Verification

After downloading archives from Arweave, verify integrity before
replaying:

```bash
# Check each file decompresses without errors
for f in archives/blocks_*.jsonl.gz; do
  gzip -t "$f" && echo "OK: $f" || echo "CORRUPT: $f"
done

# Verify block continuity (no gaps)
# Each file's first block should be the previous file's last + 1
for f in $(ls archives/blocks_*.jsonl.gz | sort -t_ -k2 -n); do
  FIRST=$(zcat "$f" | head -1 | jq -r '.result.block.header.height // .height')
  LAST=$(zcat "$f" | tail -1 | jq -r '.result.block.header.height // .height')
  echo "$f: blocks $FIRST to $LAST"
done
```

The `replay-from-archive` tool also validates block hashes during
replay when the `--validate` flag is set, catching any corruption
or tampering before it affects state.
