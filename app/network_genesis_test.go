package app

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"cosmossdk.io/log"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/stretchr/testify/require"
)

// TestCheckNetworkBuildGenesisValidates runs `sparkdreamd genesis validate`
// over the built network's checked-in genesis file. Several module bounds
// compare genesis values against compiled per-tag constants (x/forum's
// hidden-post expiration, x/vault's TTLs, x/artifact's hard bounds), so the
// check only means something under the network's own tag: the testparams
// build rejects every production genesis and an untagged run says nothing.
// The name shares the TestCheckNetworkBuild prefix so `make test-production`
// runs it for each of devnet, testnet and mainnet.
func TestCheckNetworkBuildGenesisValidates(t *testing.T) {
	if builtForNetwork == "" {
		t.Skip("network genesis bounds are per build tag; run with -tags devnet|testnet|mainnet")
	}
	genFile := filepath.Join("..", "deploy", "config", "network", builtForNetwork, "genesis.json")

	gen, err := genutiltypes.AppGenesisFromFile(genFile)
	require.NoError(t, err, genFile)
	require.NoError(t, gen.ValidateAndComplete(), genFile)
	require.Equal(t, builtForNetwork, canonicalNetworkChainIDs[gen.ChainID],
		"%s: chain_id %q is not the canonical id of the %s build", genFile, gen.ChainID, builtForNetwork)

	var genState map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(gen.AppState, &genState), genFile)

	appOptions := make(simtestutil.AppOptionsMap, 0)
	appOptions[flags.FlagHome] = t.TempDir()
	app := New(log.NewNopLogger(), dbm.NewMemDB(), nil, true, appOptions, fauxMerkleModeOpt, baseapp.SetChainID(gen.ChainID))

	// Same walk as the CLI: ModuleManager holds the depinject and IBC
	// modules alike, and every module with genesis basics validates its
	// own section. genutil needs the gentx validator AppConfig supplies,
	// or a genesis with gentxs dereferences a nil function.
	mbm := module.NewBasicManagerFromManager(app.ModuleManager, map[string]module.AppModuleBasic{
		genutiltypes.ModuleName: genutil.NewAppModuleBasic(genutiltypes.DefaultMessageValidator),
	})
	require.NoError(t, mbm.ValidateGenesis(app.AppCodec(), app.TxConfig(), genState), genFile)
}
