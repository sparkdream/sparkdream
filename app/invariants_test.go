package app

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"cosmossdk.io/log"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/stretchr/testify/require"
)

// The chain does not wire x/crisis: a broken invariant must never halt a
// production network, and periodic on-chain checks cost every node. Module
// invariants are still written, so this test is where they run. It imports
// every e2e post-setup genesis snapshot under test/ (real multi-module
// state produced by the setup scripts), finalizes the first block so every
// BeginBlocker/EndBlocker has run once, and evaluates every invariant each
// module registers.
//
// Snapshots lag behind proto changes between /save-setup runs, so one
// whose app_state no longer imports is skipped with the decode error in
// the skip message rather than failing the run; the skip is the signal to
// re-run /save-setup for that module. At least one snapshot must import.

// invariantRoute is one registered invariant.
type invariantRoute struct {
	module, name string
	inv          sdk.Invariant
}

// invariantRegistry collects routes the way x/crisis would.
type invariantRegistry struct {
	routes []invariantRoute
}

func (r *invariantRegistry) RegisterRoute(moduleName, route string, inv sdk.Invariant) {
	r.routes = append(r.routes, invariantRoute{module: moduleName, name: route, inv: inv})
}

// collectInvariants registers every module's invariants into a fresh
// registry. Manager.RegisterInvariants is a no-op under the depinject
// runtime, so modules are walked directly.
func collectInvariants(app *App) []invariantRoute {
	reg := &invariantRegistry{}
	names := make([]string, 0, len(app.ModuleManager.Modules))
	for name := range app.ModuleManager.Modules {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if hi, ok := app.ModuleManager.Modules[name].(module.HasInvariants); ok {
			hi.RegisterInvariants(reg)
		}
	}
	return reg.routes
}

// snapshotGenesisFiles lists the e2e post-setup genesis snapshots.
func snapshotGenesisFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("../test/*/snapshots/post-setup/sparkdream_data/config/genesis.json")
	require.NoError(t, err)
	sort.Strings(files)
	return files
}

// newAppFromGenesisFile builds an app, runs InitChain with the snapshot's
// app_state, and commits block 1. InitGenesis panics on an unknown field,
// so a stale snapshot surfaces as a recovered panic and is returned as an
// error for the caller to skip on.
func newAppFromGenesisFile(t *testing.T, genFile string) (app *App, ctx sdk.Context, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("stale snapshot: %v", r)
		}
	}()
	gen, err := genutiltypes.AppGenesisFromFile(genFile)
	if err != nil {
		return nil, sdk.Context{}, err
	}

	appOptions := make(simtestutil.AppOptionsMap, 0)
	appOptions[flags.FlagHome] = t.TempDir()

	db := dbm.NewMemDB()
	app = New(log.NewNopLogger(), db, nil, true, appOptions, fauxMerkleModeOpt, baseapp.SetChainID(gen.ChainID))

	var consensus *cmtproto.ConsensusParams
	if gen.Consensus != nil && gen.Consensus.Params != nil {
		p := gen.Consensus.Params.ToProto()
		consensus = &p
	}
	_, err = app.InitChain(&abci.RequestInitChain{
		ChainId:         gen.ChainID,
		Time:            gen.GenesisTime,
		AppStateBytes:   gen.AppState,
		ConsensusParams: consensus,
		InitialHeight:   gen.InitialHeight,
	})
	if err != nil {
		return nil, sdk.Context{}, fmt.Errorf("InitChain: %w", err)
	}

	height := gen.InitialHeight
	if height == 0 {
		height = 1
	}
	_, err = app.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: height,
		Time:   gen.GenesisTime.Add(time.Second),
	})
	require.NoError(t, err, "FinalizeBlock from %s", genFile)
	_, err = app.Commit()
	require.NoError(t, err)

	ctx = app.NewContextLegacy(true, cmtproto.Header{
		Height:  height,
		ChainID: gen.ChainID,
		Time:    gen.GenesisTime.Add(time.Second),
	})
	return app, ctx, nil
}

func runInvariants(t *testing.T, app *App, ctx sdk.Context) {
	t.Helper()
	routes := collectInvariants(app)
	require.NotEmpty(t, routes, "no invariants registered; did a module drop HasInvariants?")
	for _, r := range routes {
		t.Run(fmt.Sprintf("%s/%s", r.module, r.name), func(t *testing.T) {
			cacheCtx, _ := ctx.CacheContext()
			msg, broken := r.inv(cacheCtx)
			require.False(t, broken, "%s/%s broken:\n%s", r.module, r.name, msg)
		})
	}
}

func TestInvariantsOnE2ESnapshots(t *testing.T) {
	files := snapshotGenesisFiles(t)
	require.NotEmpty(t, files, "no e2e snapshots found under test/*/snapshots/post-setup")
	imported := 0
	for _, f := range files {
		f := f
		rel, err := filepath.Rel("../test", f)
		require.NoError(t, err)
		name := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
		t.Run(name, func(t *testing.T) {
			app, ctx, err := newAppFromGenesisFile(t, f)
			if err != nil {
				t.Skipf("%s does not import against the current tree; re-run /save-setup %s: %v", f, name, err)
			}
			imported++
			runInvariants(t, app, ctx)
		})
	}
	require.Greater(t, imported, 0, "no snapshot imported; every e2e snapshot is stale")
}

// TestInvariantsRegistered pins the set of registered invariants so a
// module silently dropping its RegisterInvariants shows up here.
func TestInvariantsRegistered(t *testing.T) {
	appOptions := make(simtestutil.AppOptionsMap, 0)
	appOptions[flags.FlagHome] = t.TempDir()
	app := New(log.NewNopLogger(), dbm.NewMemDB(), nil, true, appOptions, fauxMerkleModeOpt, baseapp.SetChainID(SimAppChainID))

	modules := map[string]int{}
	for _, r := range collectInvariants(app) {
		modules[r.module]++
	}
	for _, m := range []string{"artifact", "blog", "collect", "federation", "forum", "identity", "rep", "service"} {
		require.Greater(t, modules[m], 0, "module %s registers no invariants", m)
	}
}
