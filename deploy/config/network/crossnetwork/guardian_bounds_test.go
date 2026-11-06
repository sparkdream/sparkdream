// Guardian bounds audit for the committed network geneses (APP-201).
//
// x/guardian compares every gov-routed SDK params update against compiled
// floors and ceilings, not against the current value, and every SDK
// MsgUpdateParams replaces the whole Params. A genesis that ships a value
// outside a bound therefore ships a param gov can never touch again without
// first moving that value inside the bound as a side effect (devnet's 1 h
// voting period under the old 6 h floor). This runs each network's shipped
// auth, bank, consensus, distribution, gov, mint, slashing and staking params
// through guardian's real filters, with that network's bounds table, as a
// no-op update: each one must be accepted and routed.
package crossnetwork_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govtypesv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"

	guardiankeeper "sparkdream/x/guardian/keeper"
	guardiantypes "sparkdream/x/guardian/types"
	identitytypes "sparkdream/x/identity/types"
)

type genesisIdentity struct {
	identity identitytypes.ChainIdentity
}

func (g genesisIdentity) IsIdentityKeeper() {}
func (g genesisIdentity) GetSealedIdentity(context.Context) (identitytypes.ChainIdentity, error) {
	return g.identity, nil
}

type genesisMint struct{ params minttypes.Params }

func (g genesisMint) GetParams(context.Context) (minttypes.Params, error) { return g.params, nil }

type genesisStaking struct{ params stakingtypes.Params }

func (g genesisStaking) GetParams(context.Context) (stakingtypes.Params, error) {
	return g.params, nil
}

// countingRouter accepts every routed msg, so an accepted filter is told
// apart from a rejected one.
type countingRouter struct{ routed int }

func (r *countingRouter) Handler(sdk.Msg) baseapp.MsgServiceHandler {
	return func(sdk.Context, sdk.Msg) (*sdk.Result, error) {
		r.routed++
		return &sdk.Result{}, nil
	}
}

func (r *countingRouter) HandlerByTypeURL(string) baseapp.MsgServiceHandler { return r.Handler(nil) }

// genesisParams decodes app_state.<module>.params as proto JSON into dst.
func genesisParams(t *testing.T, cdc codec.Codec, appState map[string]json.RawMessage, module string, dst gogoproto.Message) {
	t.Helper()
	var mod struct {
		Params json.RawMessage `json:"params"`
	}
	require.NoError(t, json.Unmarshal(appState[module], &mod), "%s section", module)
	require.NotEmpty(t, mod.Params, "%s.params missing", module)
	require.NoError(t, cdc.UnmarshalJSON(mod.Params, dst), "%s.params", module)
}

func TestNetworkGenesisParamsInsideGuardianBounds(t *testing.T) {
	require.ElementsMatch(t, networks, guardiankeeper.NetworkNames(),
		"guardian's bounds table and the shipped networks must list the same networks")

	registry := codectypes.NewInterfaceRegistry()
	for _, reg := range []func(codectypes.InterfaceRegistry){
		authtypes.RegisterInterfaces, banktypes.RegisterInterfaces, consensustypes.RegisterInterfaces,
		distrtypes.RegisterInterfaces, govtypesv1.RegisterInterfaces, minttypes.RegisterInterfaces,
		slashingtypes.RegisterInterfaces, stakingtypes.RegisterInterfaces, guardiantypes.RegisterInterfaces,
	} {
		reg(registry)
	}
	cdc := codec.NewProtoCodec(registry)
	govAuthority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	for _, network := range networks {
		t.Run(network, func(t *testing.T) {
			genFile := filepath.Join(baseDir, network, "genesis.json")
			gen, err := genutiltypes.AppGenesisFromFile(genFile)
			if err != nil {
				t.Skipf("%s not yet present or unreadable: %v", genFile, err)
			}
			var appState map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(gen.AppState, &appState))
			require.NotNil(t, gen.Consensus)
			require.NotNil(t, gen.Consensus.Params, "%s: consensus params missing", genFile)
			consensus := gen.Consensus.Params.ToProto()

			var (
				auth     authtypes.Params
				bank     banktypes.Params
				distr    distrtypes.Params
				gov      govtypesv1.Params
				mint     minttypes.Params
				slashing slashingtypes.Params
				staking  stakingtypes.Params
			)
			genesisParams(t, cdc, appState, authtypes.ModuleName, &auth)
			genesisParams(t, cdc, appState, banktypes.ModuleName, &bank)
			genesisParams(t, cdc, appState, distrtypes.ModuleName, &distr)
			genesisParams(t, cdc, appState, govtypes.ModuleName, &gov)
			genesisParams(t, cdc, appState, minttypes.ModuleName, &mint)
			genesisParams(t, cdc, appState, slashingtypes.ModuleName, &slashing)
			genesisParams(t, cdc, appState, stakingtypes.ModuleName, &staking)

			var identityGenesis struct {
				Identity json.RawMessage `json:"identity"`
			}
			require.NoError(t, json.Unmarshal(appState[identitytypes.ModuleName], &identityGenesis))
			var identity identitytypes.ChainIdentity
			require.NoError(t, cdc.UnmarshalJSON(identityGenesis.Identity, &identity), "identity.identity")
			require.Equal(t, staking.BondDenom, identity.BondDenom, "staking.bond_denom is the sealed bond denom")

			storeKey := storetypes.NewKVStoreKey(guardiantypes.StoreKey)
			ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx.
				WithConsensusParams(consensus).
				WithBlockTime(time.Unix(1_800_000_000, 0))
			router := &countingRouter{}
			k := guardiankeeper.NewKeeper(runtime.NewKVStoreService(storeKey), cdc,
				addresscodec.NewBech32Codec("sprkdrm"), router, govAuthority)
			require.NoError(t, k.UseNetworkBounds(network))
			k.SetIdentityKeeper(genesisIdentity{identity: identity})
			k.SetMintKeeper(genesisMint{params: mint})
			k.SetStakingKeeper(genesisStaking{params: staking})
			ms := guardiankeeper.NewMsgServerImpl(&k)

			updates := []struct {
				name string
				msg  sdk.Msg
			}{
				{"auth", &authtypes.MsgUpdateParams{Params: auth}},
				{"bank", &banktypes.MsgUpdateParams{Params: bank}},
				{"consensus", &consensustypes.MsgUpdateParams{
					Block: consensus.Block, Evidence: consensus.Evidence,
					Validator: consensus.Validator, Abci: consensus.Abci,
				}},
				{"distribution", &distrtypes.MsgUpdateParams{Params: distr}},
				{"gov", &govtypesv1.MsgUpdateParams{Params: gov}},
				{"mint", &minttypes.MsgUpdateParams{Params: mint}},
				{"slashing", &slashingtypes.MsgUpdateParams{Params: slashing}},
				{"staking", &stakingtypes.MsgUpdateParams{Params: staking}},
			}
			for _, u := range updates {
				t.Run(u.name, func(t *testing.T) {
					before := router.routed
					inner, err := codectypes.NewAnyWithValue(u.msg)
					require.NoError(t, err)
					_, err = ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuthority, Inner: inner})
					require.NoError(t, err, "%s: shipped %s params are outside guardian's %s bounds; gov could never re-submit them", genFile, u.name, network)
					require.Equal(t, before+1, router.routed, "%s update was not routed", u.name)
				})
			}
		})
	}
}
