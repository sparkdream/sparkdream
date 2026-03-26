# SparkDream Block Archive Replay Tool — Design Document

## Goal

Enable chain state reconstruction from incremental block archive files,
without relying on the network. Replay can start from any existing node
state — a fresh genesis, a state sync snapshot, a genesis export at an
arbitrary height, or a previously replayed node. Each archive file
contains only new blocks (zero duplication). Replaying the appropriate
range of archives extends the node's state and block history to any
desired height.

## Archive Format

The block archiver (sparkdream-block-archiver.sh) fetches blocks via RPC
and stores them as gzipped JSONL files:

```
blocks_1_to_10000.jsonl.gz
blocks_10001_to_20000.jsonl.gz
blocks_20001_to_30000.jsonl.gz
...
```

Each line is a JSON object from the `/block?height=N` RPC endpoint, which
returns the full block including header, data (transactions), evidence,
and last commit.

### Problem with RPC JSON format

The `/block` RPC response wraps the block in a `result` envelope and
uses Amino/JSON encoding. For reliable replay we need the actual
protobuf-serialized block that CometBFT uses internally. The archiver
should be updated to also capture `/block_results` for each height,
which contains the ABCI response data (events, validator updates, etc.)
that the node needs during replay.

### Improved Archive Format (v2)

Each line in the JSONL file should contain:

```json
{
  "height": 12345,
  "block": { ... },          // from /block
  "block_results": { ... },  // from /block_results
  "commit": { ... }          // included in block, but also in next block's last_commit
}
```

This ensures each archive file is fully self-contained for replay.

## CLI Command

```
sparkdreamd replay-from-archive \
  --home /root/.sparkdream \
  --archive-dir /path/to/archives \
  [--end-height 50000] \
  [--validate true]
```

### Flags

- `--home`: Node home directory (must have config/ with genesis.json)
- `--archive-dir`: Directory containing blocks_*.jsonl.gz files
- `--end-height`: Stop replay at this height (default: replay all available)
- `--validate`: Verify app hash after each block (slower but safer)

### Auto-detection of start height

The command automatically reads the node's last committed block height
from the application database. Replay begins at `last_committed + 1`.
This means:

- **Fresh genesis node (height 0)**: Runs InitChain, replays from block 1
- **State synced node (height N)**: Replays from block N+1
- **Genesis export at height N**: Replays from block N+1
- **Previously replayed node (height M)**: Resumes from block M+1
- **Node synced via peers (height P)**: Fills in from P+1 onward

No manual `--start-height` flag is needed. The tool reads state and
picks up where the node left off.

### Starting from a state base other than genesis

The tool supports several "base state" scenarios:

**Scenario A — Full replay from genesis:**
```bash
sparkdreamd init node --chain-id sparkdream-1
cp genesis.json ~/.sparkdream/config/genesis.json
sparkdreamd replay-from-archive --archive-dir ./archives
```

**Scenario B — State sync + archive replay (fast + complete):**
```bash
# First, state sync to a recent height (fast, minutes)
# This gives you app state at height N but no block history
sparkdreamd start  # with statesync enabled, let it sync, then stop

# Then replay archives from N+1 onward (extends block history)
sparkdreamd replay-from-archive --archive-dir ./archives
```

**Scenario C — Genesis export + archive replay:**
```bash
# Start from an exported genesis at height N
sparkdreamd init node --chain-id sparkdream-1
cp exported_genesis_height_50000.json ~/.sparkdream/config/genesis.json
sparkdreamd replay-from-archive --archive-dir ./archives
# Automatically starts from block 50001
```

**Scenario D — Resume interrupted replay:**
```bash
# Previous replay was interrupted at height 35000
# Just run again — it detects height 35000 and continues
sparkdreamd replay-from-archive --archive-dir ./archives
```

## Implementation Outline

### Phase 1: Block Store Population

```go
// cmd/sparkdreamd/cmd/replay_archive.go

func ReplayFromArchiveCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "replay-from-archive",
        Short: "Replay blocks from archive files to reconstruct state",
        Long: `Reads incremental block archive files (JSONL format) and
replays them through the ABCI application to reconstruct the full
chain state.

The command auto-detects the node's current height and begins replay
from the next block. This allows it to work with any starting state:
fresh genesis, state sync snapshot, genesis export, or a previously
interrupted replay.

Archive files that fall entirely below the current height are skipped
automatically. Partially overlapping files are read but already-applied
blocks within them are skipped.`,
        RunE: replayFromArchive,
    }
}
```

### Core Replay Logic

```go
func replayFromArchive(cmd *cobra.Command, args []string) error {
    // 1. Open the node's databases
    //    - blockstore DB (CometBFT block storage)
    //    - state DB (CometBFT consensus state)
    //    - app DB (Cosmos SDK application state)

    // 2. Detect current app height
    //    lastHeight := app.LastBlockHeight()
    //    If lastHeight == 0 and no state exists:
    //      Run InitChain with genesis.json
    //    startFrom := lastHeight + 1

    // 3. Discover and sort archive files by block range
    //    Sort by: blocks_1_to_10000, blocks_10001_to_20000, ...
    //    Skip files where file.endHeight < startFrom
    //    For partially overlapping files, skip individual blocks
    //    below startFrom

    // 4. For each relevant archive file, for each block >= startFrom:
    //    a. Deserialize the block JSON into CometBFT types.Block
    //    b. Verify block.Height == expected next height (sequential)
    //    c. Create the PartSet from the block
    //    d. Call app.FinalizeBlock() with the block's transactions
    //    e. Call app.Commit()
    //    f. Save the block to the block store
    //    g. Update the consensus state DB
    //    h. If --validate: compare resulting app hash with block header
    //    i. If mismatch: abort with detailed error (possible archive
    //       corruption or non-determinism)

    // 5. Print progress every N blocks
    //    "Replayed block 15000/50000 (30%) — app_hash: ABC123..."

    // 6. On completion (or --end-height reached):
    //    Print summary: start height, end height, time elapsed,
    //    final app hash
    //    The node can now be started with `sparkdreamd start`
}
```

### Key CometBFT/SDK Integration Points

```go
import (
    "github.com/cometbft/cometbft/store"
    cmttypes "github.com/cometbft/cometbft/types"
    dbm "github.com/cosmos/cosmos-db"
)

// Opening the block store
blockStoreDB, _ := dbm.NewDB("blockstore", dbm.GoLevelDBBackend, dataDir)
blockStore := store.NewBlockStore(blockStoreDB)

// Saving a block
blockStore.SaveBlock(block, partSet, seenCommit)

// Running the ABCI app
app := sparkdreamApp.New(...)  // your Cosmos SDK app
responseInitChain := app.InitChain(requestInitChain)
responseFinalizeBlock := app.FinalizeBlock(requestFinalizeBlock)
responseCommit := app.Commit()
```

### Block Deserialization

The RPC JSON format needs to be converted to CometBFT's internal types.
CometBFT provides JSON unmarshaling for its types:

```go
import (
    cmttypes "github.com/cometbft/cometbft/types"
    cmtjson  "github.com/cometbft/cometbft/libs/json"
)

// RPC response structure
type RPCBlockResponse struct {
    Result struct {
        BlockID cmttypes.BlockID `json:"block_id"`
        Block   *cmttypes.Block `json:"block"`
    } `json:"result"`
}

var resp RPCBlockResponse
cmtjson.Unmarshal(jsonLine, &resp)
block := resp.Result.Block
```

## Replay Flow Diagram

```
    Base State (any of these)          Archive Files (Arweave/IPFS)
    ┌─────────────────────┐            ┌──────────────────────────┐
    │ Option A: Genesis   │            │ blocks_1_to_10000.jsonl.gz│
    │ Option B: State Sync│            │ blocks_10001_to_20000... │
    │ Option C: Export    │            │ blocks_20001_to_30000... │
    │ Option D: Prior run │            │ ...                      │
    └─────────┬───────────┘            └──────────┬───────────────┘
              │                                   │
              │    ┌──────────────────────┐        │
              └───►│  replay-from-archive │◄───────┘
                   │                      │
                   │  1. Detect last      │
                   │     committed height │
                   │  2. Skip archives    │
                   │     below that height│
                   │  3. Replay remaining │
                   │     blocks through   │
                   │     ABCI app         │
                   └──────────┬───────────┘
                              │
             ┌────────────────┼────────────────┐
             │                │                │
             ▼                ▼                ▼
   ┌─────────────┐  ┌──────────────┐  ┌──────────────┐
   │  Block Store │  │  State DB    │  │  App State   │
   │  (LevelDB)  │  │  (LevelDB)   │  │  (IAVL)      │
   └─────────────┘  └──────────────┘  └──────────────┘
             │                │                │
             └────────────────┼────────────────┘
                              │
                              ▼
                   ┌──────────────────────┐
                   │  sparkdreamd start   │
                   │  (fully functional   │
                   │   archive node)      │
                   └──────────────────────┘
```

## Recovery Scenarios

### Scenario 1: Full restore from scratch (complete archive node)
```bash
# 1. Initialize empty node
sparkdreamd init archive-node --chain-id sparkdream-1 --home /root/.sparkdream

# 2. Copy genesis.json (from Arweave or repo)
cp genesis.json /root/.sparkdream/config/genesis.json

# 3. Download all archive files from Arweave
# (using manifest to get CIDs/TX IDs)

# 4. Replay all archives (auto-detects height 0, starts from block 1)
sparkdreamd replay-from-archive \
  --home /root/.sparkdream \
  --archive-dir ./archives \
  --validate true

# 5. Start the node — it's a full archive node
sparkdreamd start --home /root/.sparkdream
```

### Scenario 2: Fast bootstrap via state sync + fill block history
```bash
# 1. State sync to height 50000 (takes minutes)
sparkdreamd start --home /root/.sparkdream
# (with [statesync] enable = true in config.toml)
# Wait for sync, then stop the node

# 2. Download archive files covering blocks 50001+
# (archives before 50001 can be skipped for now)

# 3. Replay to extend from state sync point
#    Auto-detects height 50000, starts from 50001
sparkdreamd replay-from-archive \
  --home /root/.sparkdream \
  --archive-dir ./archives

# 4. Optionally, replay earlier archives (1-50000) to fill
#    the complete block history for a true archive node
```

### Scenario 3: Restore from periodic genesis export
```bash
# 1. Initialize from an exported genesis at height 100000
sparkdreamd init node --chain-id sparkdream-1 --home /root/.sparkdream
cp exported_genesis_at_100000.json /root/.sparkdream/config/genesis.json

# 2. Download archives from block 100001 onward

# 3. Replay (auto-detects height 100000, starts from 100001)
sparkdreamd replay-from-archive \
  --home /root/.sparkdream \
  --archive-dir ./archives
```

### Scenario 4: Resume interrupted replay
```bash
# Previous replay was interrupted at height 35000
# Just run again — auto-detects height 35000 and continues from 35001
sparkdreamd replay-from-archive \
  --home /root/.sparkdream \
  --archive-dir ./archives
```

### Scenario 5: Periodic catch-up from new archive files
```bash
# Node was replayed up to height 80000 last week
# New archive files blocks_80001_to_90000.jsonl.gz downloaded

# Run replay — auto-detects height 80000, replays new file
sparkdreamd replay-from-archive \
  --home /root/.sparkdream \
  --archive-dir ./archives
```

## Archiver Updates Required

The current block archiver needs to capture additional data for
reliable replay. Update the archiver to fetch both `/block` and
`/block_results` for each height:

```sh
BLOCK=$(curl -s "${RPC_URL}/block?height=${h}")
RESULTS=$(curl -s "${RPC_URL}/block_results?height=${h}")

# Combine into single JSON line
jq -c --argjson block "$BLOCK" --argjson results "$RESULTS" \
  -n '{height: $block.result.block.header.height,
       block: $block.result,
       block_results: $results.result}' >> "$BATCH_FILE"
```

## Verification

After replay, verify integrity by comparing the app hash at the
final replayed height with a known-good value (from the sentry RPC
or from the next block's header):

```bash
# Query the replayed node
sparkdreamd query block <height> --home /root/.sparkdream

# Compare app_hash with the archive's recorded value
```

## Future Enhancements

- **Parallel deserialization**: Read and decompress archive files
  in a background goroutine while the main goroutine replays blocks
- **Checkpoint support**: Periodically save a "checkpoint" file
  recording the last successfully replayed height, enabling clean
  resume after crashes during long replays
- **Archive verification**: Validate block hashes chain correctly
  (each block's last_commit_hash matches the previous block's commit)
  before starting replay, to catch corrupted archives early
- **Streaming from Arweave**: Fetch and replay archive files directly
  from Arweave gateway URLs without downloading everything first
- **Gap detection**: Before replay, scan all archive files and report
  any missing block ranges so the operator can download them before
  starting a long replay
- **Backwards fill**: After state syncing to height N and replaying
  forward, optionally replay archives from 1 to N to populate the
  block store with full history (blocks only, no state re-execution
  needed since app state at N is already correct)
- **Archive format migration**: Tool to convert v1 archives
  (block only) to v2 (block + block_results) by re-fetching
  block_results from a running node