package cmd

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"

	"cosmossdk.io/log"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cs "github.com/cometbft/cometbft/consensus"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	cmtlog "github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/mempool"
	cmtproxy "github.com/cometbft/cometbft/proxy"
	sm "github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/store"
	cmttypes "github.com/cometbft/cometbft/types"

	cmtdbm "github.com/cometbft/cometbft-db"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"

	"sparkdream/app"
)

const (
	flagArchiveDir = "archive-dir"
	flagEndHeight  = "end-height"
	flagValidate   = "validate"

	// BlockPartSizeBytes is the standard CometBFT block part size.
	BlockPartSizeBytes uint32 = 65536
)

// archiveFile represents a parsed archive filename with its block range.
type archiveFile struct {
	path      string
	fromBlock int64
	toBlock   int64
}

// archiveBlockEntry represents a single line in the archive JSONL file.
// Format: {"block_id": ..., "block": ..., "block_results": ..., "commit": ...}
type archiveBlockEntry struct {
	BlockID      *cmttypes.BlockID `json:"block_id"`
	Block        *cmttypes.Block   `json:"block"`
	BlockResults json.RawMessage   `json:"block_results"` // preserved for future use
	Commit       *cmttypes.Commit  `json:"commit"`        // commit for THIS block (from /commit RPC)
}

var archiveFilePattern = regexp.MustCompile(`^blocks_(\d+)_to_(\d+)\.jsonl\.gz$`)

// ReplayFromArchiveCmd returns the cobra command for replaying blocks from archive files.
func ReplayFromArchiveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "replay-from-archive",
		Short: "Replay blocks from archive files to reconstruct chain state",
		Long: `Reads incremental block archive files (gzipped JSONL) and replays them
through the ABCI application to reconstruct the full chain state.

The command auto-detects the node's current height and begins replay from
the next block. This allows it to work with any starting state: fresh
genesis, state sync snapshot, genesis export, or a previously interrupted
replay.

Archive files that fall entirely below the current height are skipped
automatically. Partially overlapping files are read but already-applied
blocks within them are skipped.

After replay completes, the node can be started normally with:
  sparkdreamd start`,
		RunE: replayFromArchive,
	}

	cmd.Flags().String(flagArchiveDir, "", "Directory containing blocks_*.jsonl.gz archive files (required)")
	cmd.Flags().Int64(flagEndHeight, 0, "Stop replay at this height (0 = replay all available)")
	cmd.Flags().Bool(flagValidate, true, "Verify app hash after each block (disable with --validate=false for speed)")
	_ = cmd.MarkFlagRequired(flagArchiveDir)

	return cmd
}

func replayFromArchive(cmd *cobra.Command, _ []string) error {
	archiveDir, _ := cmd.Flags().GetString(flagArchiveDir)
	endHeight, _ := cmd.Flags().GetInt64(flagEndHeight)
	validate, _ := cmd.Flags().GetBool(flagValidate)

	serverCtx := server.GetServerContextFromCmd(cmd)
	cmtCfg := serverCtx.Config
	homeDir := cmtCfg.RootDir

	logger := log.NewLogger(os.Stdout, log.ColorOption(false))
	cmtLogger := newCmtLogAdapter(logger)

	// -----------------------------------------------------------------------
	// 1. Discover and sort archive files
	// -----------------------------------------------------------------------
	archives, err := discoverArchives(archiveDir)
	if err != nil {
		return fmt.Errorf("failed to discover archive files: %w", err)
	}
	if len(archives) == 0 {
		return fmt.Errorf("no archive files (blocks_*_to_*.jsonl.gz) found in %s", archiveDir)
	}
	logger.Info("Discovered archive files", "count", len(archives),
		"first", filepath.Base(archives[0].path),
		"last", filepath.Base(archives[len(archives)-1].path))

	// -----------------------------------------------------------------------
	// 2. Open CometBFT databases (blockstore + state)
	// -----------------------------------------------------------------------
	dataDir := filepath.Join(homeDir, "data")
	dbBackend := cmtdbm.BackendType(cmtCfg.DBBackend)

	blockStoreDB, err := cmtdbm.NewDB("blockstore", dbBackend, dataDir)
	if err != nil {
		return fmt.Errorf("failed to open blockstore DB: %w", err)
	}
	defer blockStoreDB.Close()
	blockStore := store.NewBlockStore(blockStoreDB)

	stateDB, err := cmtdbm.NewDB("state", dbBackend, dataDir)
	if err != nil {
		return fmt.Errorf("failed to open state DB: %w", err)
	}
	defer stateDB.Close()
	stateStore := sm.NewStore(stateDB, sm.StoreOptions{
		DiscardABCIResponses: false,
	})

	// -----------------------------------------------------------------------
	// 3. Load or initialize state
	// -----------------------------------------------------------------------
	// Load genesis via the SDK's standard JSON (not CometBFT's strict
	// type-registered JSON) to handle integer fields like initial_height
	// that the SDK writes as numbers rather than strings.
	genesisDoc, err := loadGenesisDoc(cmtCfg.GenesisFile())
	if err != nil {
		return fmt.Errorf("failed to load genesis: %w", err)
	}
	state, err := stateStore.LoadFromDBOrGenesisDoc(genesisDoc)
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	lastHeight := state.LastBlockHeight
	startFrom := lastHeight + 1

	logger.Info("Node state loaded",
		"chain_id", state.ChainID,
		"last_block_height", lastHeight,
		"replay_starts_from", startFrom)

	// -----------------------------------------------------------------------
	// 4. Open application database and create the SDK app
	// -----------------------------------------------------------------------
	appDB, err := dbm.NewDB("application", dbm.BackendType(cmtCfg.DBBackend), dataDir)
	if err != nil {
		return fmt.Errorf("failed to open application DB: %w", err)
	}
	defer appDB.Close()

	viperOpts := viper.New()
	viperOpts.Set(flags.FlagHome, homeDir)

	sdkApp := app.New(
		logger,
		appDB,
		nil,   // traceStore
		true,  // loadLatest
		viperOpts,
		baseapp.SetChainID(genesisDoc.ChainID),
	)

	// -----------------------------------------------------------------------
	// 5. Create proxy app connection and block executor
	// -----------------------------------------------------------------------
	cmtApp := server.NewCometABCIWrapper(sdkApp)
	clientCreator := cmtproxy.NewLocalClientCreator(cmtApp)
	proxyApp := cmtproxy.NewAppConns(clientCreator, cmtproxy.NopMetrics())
	proxyApp.SetLogger(cmtLogger.With("module", "proxy"))
	if err := proxyApp.Start(); err != nil {
		return fmt.Errorf("failed to start proxy app: %w", err)
	}
	defer func() {
		if err := proxyApp.Stop(); err != nil {
			logger.Error("failed to stop proxy app", "err", err)
		}
	}()

	blockExec := sm.NewBlockExecutor(
		stateStore,
		cmtLogger.With("module", "state"),
		proxyApp.Consensus(),
		&mempool.NopMempool{},
		sm.EmptyEvidencePool{},
		blockStore,
	)

	// -----------------------------------------------------------------------
	// 6. Run the ABCI handshake (handles InitChain for fresh state, or
	//    syncs the app to the last committed height for resumed replays)
	// -----------------------------------------------------------------------
	handshaker := cs.NewHandshaker(stateStore, state, blockStore, genesisDoc)
	handshaker.SetLogger(cmtLogger.With("module", "consensus"))
	if err := handshaker.Handshake(proxyApp); err != nil {
		return fmt.Errorf("ABCI handshake failed: %w", err)
	}

	// Reload state after handshake — it may have been updated by InitChain
	state, err = stateStore.Load()
	if err != nil {
		return fmt.Errorf("failed to reload state after handshake: %w", err)
	}
	lastHeight = state.LastBlockHeight
	startFrom = lastHeight + 1
	logger.Info("State after handshake",
		"last_block_height", lastHeight,
		"app_hash", fmt.Sprintf("%X", state.AppHash))

	// -----------------------------------------------------------------------
	// 7. Pre-flight: detect gaps in archive coverage
	// -----------------------------------------------------------------------
	if err := detectArchiveGaps(archives, startFrom, endHeight); err != nil {
		return err
	}

	// -----------------------------------------------------------------------
	// 8. Replay blocks from archives
	// -----------------------------------------------------------------------
	totalBlocks := int64(0)
	startTime := time.Now()
	lastLogTime := startTime

	// Buffer the previous block so we can save it to the block store when
	// the next block arrives (using nextBlock.LastCommit as the seenCommit).
	var prevBlock *cmttypes.Block
	var prevPartSet *cmttypes.PartSet
	var lastCommit *cmttypes.Commit // commit for the most recently processed block

	for _, af := range archives {
		// Skip archive files entirely below our start height
		if af.toBlock < startFrom {
			logger.Debug("Skipping archive (below start height)",
				"file", filepath.Base(af.path),
				"range", fmt.Sprintf("%d-%d", af.fromBlock, af.toBlock))
			continue
		}

		// Check if we've reached end height
		if endHeight > 0 && af.fromBlock > endHeight {
			break
		}

		logger.Info("Processing archive",
			"file", filepath.Base(af.path),
			"range", fmt.Sprintf("%d-%d", af.fromBlock, af.toBlock))

		err := processArchiveFile(af.path, startFrom, endHeight, func(block *cmttypes.Block, blockID cmttypes.BlockID, commit *cmttypes.Commit) error {
			height := block.Height

			// Verify sequential
			expectedHeight := state.LastBlockHeight + 1
			if height != expectedHeight {
				return fmt.Errorf("block height %d does not match expected %d (gap or duplicate in archives)", height, expectedHeight)
			}

			// Save the previous block to the block store now that we have
			// this block's LastCommit as the seenCommit for it.
			if prevBlock != nil && block.LastCommit != nil {
				blockStore.SaveBlock(prevBlock, prevPartSet, block.LastCommit)
			}

			// Create part set for block store
			partSet, err := block.MakePartSet(BlockPartSizeBytes)
			if err != nil {
				return fmt.Errorf("failed to make part set for block %d: %w", height, err)
			}

			// Construct correct BlockID with PartSetHeader
			blockID = cmttypes.BlockID{
				Hash:          block.Hash(),
				PartSetHeader: partSet.Header(),
			}

			// Apply the block through the ABCI app (with full validation —
			// verifies block structure, evidence, and header consistency)
			newState, err := blockExec.ApplyBlock(state, blockID, block)
			if err != nil {
				return fmt.Errorf("failed to apply block %d: %w", height, err)
			}

			// Validate app hash if requested. The block's AppHash records
			// the state root BEFORE this block's execution (i.e., the result
			// of executing the previous block). So we compare it against the
			// state's AppHash from before we applied this block.
			if validate && height > state.InitialHeight {
				if len(block.AppHash) > 0 && len(state.AppHash) > 0 {
					blockAppHash := fmt.Sprintf("%X", block.AppHash)
					prevStateAppHash := fmt.Sprintf("%X", state.AppHash)
					if blockAppHash != prevStateAppHash {
						return fmt.Errorf(
							"APP HASH MISMATCH at height %d: block expects %s, but state has %s (possible archive corruption or non-determinism)",
							height, blockAppHash, prevStateAppHash,
						)
					}
				}
			}

			// Buffer this block for saving when the next block arrives
			prevBlock = block
			prevPartSet = partSet
			lastCommit = commit

			state = newState
			totalBlocks++

			// Progress logging every 5 seconds or every 100 blocks
			now := time.Now()
			if now.Sub(lastLogTime) > 5*time.Second || totalBlocks%100 == 0 {
				elapsed := now.Sub(startTime)
				bps := float64(totalBlocks) / elapsed.Seconds()
				logger.Info("Replay progress",
					"height", height,
					"blocks_replayed", totalBlocks,
					"blocks_per_sec", fmt.Sprintf("%.1f", bps),
					"app_hash", fmt.Sprintf("%X", state.AppHash),
				)
				lastLogTime = now
			}

			return nil
		})
		if err != nil {
			return err
		}
	}

	// Save the last block to the block store using the commit from the
	// archive (fetched from the /commit RPC endpoint during archival).
	// This ensures the block store height matches the app height so the
	// node can restart cleanly after replay.
	if prevBlock != nil {
		if lastCommit == nil {
			return fmt.Errorf("archive is missing commit for block %d — re-archive with updated block-archiver.sh", prevBlock.Height)
		}
		blockStore.SaveBlock(prevBlock, prevPartSet, lastCommit)
		logger.Info("Saved last block to block store",
			"height", prevBlock.Height)
	}

	// -----------------------------------------------------------------------
	// 8. Summary
	// -----------------------------------------------------------------------
	elapsed := time.Since(startTime)
	logger.Info("Replay complete",
		"start_height", startFrom,
		"end_height", state.LastBlockHeight,
		"blocks_replayed", totalBlocks,
		"elapsed", elapsed.Round(time.Second),
		"final_app_hash", fmt.Sprintf("%X", state.AppHash),
	)

	if totalBlocks == 0 {
		logger.Info("No new blocks to replay. Node is already at the latest archived height.")
	} else {
		logger.Info("Node is ready. Start with: sparkdreamd start --home " + homeDir)
	}

	return nil
}

// detectArchiveGaps checks that archive files provide continuous coverage
// from startFrom through endHeight (or the last archive if endHeight is 0).
func detectArchiveGaps(archives []archiveFile, startFrom, endHeight int64) error {
	nextExpected := startFrom
	for _, af := range archives {
		if af.toBlock < startFrom {
			continue // skip files entirely below start
		}
		if endHeight > 0 && af.fromBlock > endHeight {
			break
		}
		// The file must start at or before the next expected block
		if af.fromBlock > nextExpected {
			return fmt.Errorf(
				"gap in archives: need block %d but next file starts at %d (%s)",
				nextExpected, af.fromBlock, filepath.Base(af.path),
			)
		}
		if af.toBlock >= nextExpected {
			nextExpected = af.toBlock + 1
		}
	}
	return nil
}

// discoverArchives finds and sorts all archive files in the given directory.
func discoverArchives(dir string) ([]archiveFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var archives []archiveFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := archiveFilePattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			continue
		}
		from, err := strconv.ParseInt(matches[1], 10, 64)
		if err != nil {
			continue
		}
		to, err := strconv.ParseInt(matches[2], 10, 64)
		if err != nil {
			continue
		}
		archives = append(archives, archiveFile{
			path:      filepath.Join(dir, entry.Name()),
			fromBlock: from,
			toBlock:   to,
		})
	}

	// Sort by fromBlock ascending
	sort.Slice(archives, func(i, j int) bool {
		return archives[i].fromBlock < archives[j].fromBlock
	})

	return archives, nil
}

// processArchiveFile reads a gzipped JSONL archive file and calls the handler
// for each block within the specified range.
func processArchiveFile(
	path string,
	startFrom int64,
	endHeight int64,
	handler func(block *cmttypes.Block, blockID cmttypes.BlockID, commit *cmttypes.Commit) error,
) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open archive %s: %w", path, err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader for %s: %w", path, err)
	}
	defer gz.Close()

	scanner := bufio.NewScanner(gz)
	// Increase buffer size for large blocks (default 64KB may be too small)
	scanner.Buffer(make([]byte, 0, 4*1024*1024), 64*1024*1024) // 64MB max

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()

		if len(line) == 0 {
			continue
		}

		block, blockID, commit, err := parseBlockLine(line)
		if err != nil {
			return fmt.Errorf("failed to parse line %d of %s: %w", lineNum, filepath.Base(path), err)
		}

		height := block.Height

		// Skip blocks below start height
		if height < startFrom {
			continue
		}

		// Stop if we've passed end height
		if endHeight > 0 && height > endHeight {
			break
		}

		if err := handler(block, blockID, commit); err != nil {
			return err
		}
	}

	return scanner.Err()
}

// parseBlockLine parses a single JSONL line from an archive file.
// Expected format: {"block_id": ..., "block": ..., "block_results": ..., "commit": ...}
func parseBlockLine(line []byte) (*cmttypes.Block, cmttypes.BlockID, *cmttypes.Commit, error) {
	var entry archiveBlockEntry
	if err := cmtjson.Unmarshal(line, &entry); err != nil {
		if err2 := json.Unmarshal(line, &entry); err2 != nil {
			return nil, cmttypes.BlockID{}, nil, fmt.Errorf("failed to unmarshal block JSON: %w (also tried: %v)", err, err2)
		}
	}

	if entry.Block == nil {
		return nil, cmttypes.BlockID{}, nil, fmt.Errorf("no block data found in JSON line")
	}

	blockID := cmttypes.BlockID{}
	if entry.BlockID != nil {
		blockID = *entry.BlockID
	}

	return entry.Block, blockID, entry.Commit, nil
}

// loadGenesisDoc loads a genesis file, trying CometBFT's type-registered
// JSON first, then falling back to patching integer fields (like
// initial_height) that the Cosmos SDK writes as numbers rather than strings
// which CometBFT's strict decoder rejects.
func loadGenesisDoc(path string) (*cmttypes.GenesisDoc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("couldn't read genesis file: %w", err)
	}

	// Try CometBFT's native decoder first (handles HexBytes correctly)
	genDoc, err := cmttypes.GenesisDocFromJSON(data)
	if err == nil {
		return genDoc, nil
	}

	// If that fails (typically because initial_height is a number not a
	// string), patch the JSON and retry. Parse as generic JSON, fix the
	// known integer fields, re-serialize, and decode again.
	var raw map[string]json.RawMessage
	if jsonErr := json.Unmarshal(data, &raw); jsonErr != nil {
		return nil, fmt.Errorf("couldn't parse genesis JSON: %w (original error: %v)", jsonErr, err)
	}

	// Patch initial_height: convert number to string
	if ih, ok := raw["initial_height"]; ok {
		var num json.Number
		if json.Unmarshal(ih, &num) == nil {
			// If it's a bare number, wrap it as a string
			if _, intErr := num.Int64(); intErr == nil {
				patched := fmt.Sprintf(`"%s"`, num.String())
				raw["initial_height"] = json.RawMessage(patched)
			}
		}
	}

	patched, jsonErr := json.Marshal(raw)
	if jsonErr != nil {
		return nil, fmt.Errorf("failed to re-encode patched genesis: %w", jsonErr)
	}

	genDoc, err = cmttypes.GenesisDocFromJSON(patched)
	if err != nil {
		return nil, fmt.Errorf("failed to parse patched genesis: %w", err)
	}
	return genDoc, nil
}

// cmtLogAdapter adapts cosmossdk.io/log.Logger to CometBFT's log.Logger interface.
type cmtLogAdapter struct {
	logger log.Logger
}

func newCmtLogAdapter(logger log.Logger) cmtlog.Logger {
	return &cmtLogAdapter{logger: logger}
}

func (l *cmtLogAdapter) Debug(msg string, keyvals ...interface{}) { l.logger.Debug(msg, keyvals...) }
func (l *cmtLogAdapter) Info(msg string, keyvals ...interface{})  { l.logger.Info(msg, keyvals...) }
func (l *cmtLogAdapter) Error(msg string, keyvals ...interface{}) { l.logger.Error(msg, keyvals...) }

func (l *cmtLogAdapter) With(keyvals ...interface{}) cmtlog.Logger {
	return &cmtLogAdapter{logger: l.logger.With(keyvals...)}
}
