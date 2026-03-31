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

Archive format includes both `/block` and `/block_results` data
for each height, providing everything needed for replay.

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

### Layer 5: IPFS Pinning on Pinata (optional)

Archive files can be pinned on IPFS via Pinata, a managed pinning
service. Unlike self-hosted IPFS nodes, Pinata guarantees your pins
stay available without maintaining infrastructure. Files are tagged
with block range metadata for easy discovery.

Pinata only requires a JWT token (no CLI install) and works with
plain `curl`, making it a good fit for the Alpine container. The
free tier covers 1 GiB. Files are retrievable from Pinata's gateway
or any public IPFS gateway.

### Layer 6: S3-Compatible IPFS on Filebase (optional)

Archive files can be uploaded to Filebase, which provides an
S3-compatible API that automatically pins uploads to IPFS. This
means you can use the standard `aws` CLI (or any S3 client) and
get IPFS CIDs back in the response metadata.

Filebase is a good fit if you already use S3 tooling or want a
familiar API. The free tier covers 5 GiB. Requires the `aws` CLI
and Filebase access keys.

### Layer 7: Cosmos-Native Storage on Jackal (optional)

Archive files can be uploaded to Jackal Protocol, a Cosmos SDK chain
with proof-of-persistence storage. Jackal providers must regularly
submit Merkle proofs that they still hold the data, so archives are
actively maintained rather than passively stored.

Jackal uses a recurring payment model (JKL tokens per duration per
byte), so it complements Arweave (permanent, pay-once) rather than
replacing it. Files are encrypted client-side and replicated across
3+ providers automatically.

Requires a running `jackalapi` instance and an active storage plan.
See `deploy/scripts/jackal-upload.sh` for details.

## Archival Workflow

### Automated (recommended)

The Docker image includes `dcron` (Dillon's cron daemon) and the
`setup-archive-cron` helper script. Run once after deploying the
sentry to start automated archival:

```bash
ssh -p <port> root@<sentry_provider> setup-archive-cron
```

This installs a dcron job that runs `block-archiver` every 6 hours,
saving archives to `/root/.sparkdream/archives` on the persistent
volume. Customize the schedule:

```bash
# Archive every hour instead
ssh -p <port> root@<sentry_provider> \
  'ARCHIVE_INTERVAL="0 * * * *" setup-archive-cron'
```

Manage the automation:

```bash
# Check if archival is active
ssh -p <port> root@<sentry_provider> setup-archive-cron --status

# Disable archival and stop dcron
ssh -p <port> root@<sentry_provider> setup-archive-cron --disable
```

### Manual workflow

For upload to permanent/redundant storage, periodically download
archives from the sentry and upload from your local machine:

```bash
# 1. Download new archives to local machine
scp -O -P <port> -i ~/.ssh/key \
  root@<sentry_provider>:/root/.sparkdream/archives/*.jsonl.gz \
  ./archives/

# 2. Upload to Arweave (permanent)
./arweave-upload.sh -w ~/arweave-wallet.json ./archives/

# 3. Optionally upload to Storacha (IPFS redundancy)
./storacha-upload.sh ./archives/

# 4. Optionally upload to Pinata (IPFS pinning)
PINATA_JWT="your-jwt-token" ./pinata-upload.sh ./archives/

# 5. Optionally upload to Filebase (S3-compatible IPFS)
FILEBASE_BUCKET="sparkdream-archives" ./filebase-upload.sh ./archives/

# 6. Optionally upload to Jackal (Cosmos-native, proof-of-persistence)
./jackal-upload.sh ./archives/
```

All upload scripts track which files have already been uploaded
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
- Pinata: free tier covers 1 GiB, paid plans from $20/month
- Filebase: free tier covers 5 GiB, paid plans from $5.99/month
- Jackal: ~8-10 JKL per TB/month (recurring, check current params)

These estimates assume low transaction volume. Costs scale with
chain activity.

## Recommended Storage Per Network

Not every network needs every storage layer. Choose based on how
critical the data is and whether you're willing to pay ongoing fees.

| Network | Primary         | Secondary        | Notes                              |
|---------|-----------------|------------------|------------------------------------|
| Devnet  | Pinata          | —                | Free tier (1 GiB), disposable data |
| Testnet | Storacha        | Pinata           | Free tiers, two IPFS providers     |
| Mainnet | Arweave         | Filebase + Jackal| Permanent + IPFS + active proofs   |

**Devnet** — throwaway by nature, frequent resets. A single free
IPFS pin is enough in case you need to inspect historical state.

**Testnet** — longer-lived and useful for debugging production-like
issues, but not critical if lost. Two free-tier IPFS providers
give redundancy at zero cost.

**Mainnet** — real chain history that must never be lost.

- Arweave is **required** as the immutable baseline. It is the only
  option where data survives if you stop paying or the company
  disappears. Pay once, available forever.
- Filebase provides IPFS redundancy for faster retrieval (S3-
  compatible, 5 GiB free tier).
- Jackal adds Cosmos-native proof-of-persistence — providers must
  regularly prove they still hold the data, giving active
  verification that IPFS pinning and Arweave do not.

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
  FIRST=$(zcat "$f" | head -1 | jq -r '.block.header.height')
  LAST=$(zcat "$f" | tail -1 | jq -r '.block.header.height')
  echo "$f: blocks $FIRST to $LAST"
done
```

The `replay-from-archive` tool validates block structure and app
hashes during replay by default, catching any corruption or
tampering before it affects state. It also detects gaps in archive
coverage before replay begins.
