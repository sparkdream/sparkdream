package collect_test

import (
	"encoding/json"
	"math/rand"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/module"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/keeper"
	collect "sparkdream/x/collect/module"
	"sparkdream/x/collect/types"
)

func TestGenerateGenesisState(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(collect.AppModule{})
	simState := &module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
		Rand:      rand.New(rand.NewSource(1)),
		GenState:  make(map[string]json.RawMessage),
		Accounts:  simtypes.RandomAccounts(rand.New(rand.NewSource(1)), 3),
	}

	collect.AppModule{}.GenerateGenesisState(simState)

	raw, ok := simState.GenState[types.ModuleName]
	require.True(t, ok)

	var genesis types.GenesisState
	require.NoError(t, encCfg.Codec.UnmarshalJSON(raw, &genesis))

	// Validate the genesis state is structurally valid.
	require.NoError(t, genesis.Validate())

	// Spot-check stable scalar fields against DefaultParams.
	defaults := types.DefaultParams()
	p := genesis.Params
	require.Equal(t, defaults.MaxCollectionsBase, p.MaxCollectionsBase)
	require.Equal(t, defaults.MaxItemsPerCollection, p.MaxItemsPerCollection)
	require.Equal(t, defaults.MaxTitleLength, p.MaxTitleLength)
	require.Equal(t, defaults.MaxDescriptionLength, p.MaxDescriptionLength)
	require.Equal(t, defaults.BaseCollectionDeposit, p.BaseCollectionDeposit)
	require.Equal(t, defaults.AnonymousPostingEnabled, p.AnonymousPostingEnabled)
	require.Equal(t, defaults.PinMinTrustLevel, p.PinMinTrustLevel)
	// AnonSubsidyRelayAddresses: DefaultParams returns nil; proto3 JSON round-trip
	// produces an empty (non-nil) slice. Both are semantically empty.
	require.Empty(t, p.AnonSubsidyRelayAddresses)
}

func TestWeightedOperations(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(collect.AppModule{})
	am := collect.NewAppModule(encCfg.Codec, keeper.Keeper{}, nil, nil)
	simState := module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
		TxConfig:  encCfg.TxConfig,
	}

	ops := am.WeightedOperations(simState)
	// 30 operations:
	// CreateCollection, UpdateCollection, DeleteCollection,
	// AddItem, AddItems, UpdateItem, RemoveItem, RemoveItems, ReorderItem,
	// AddCollaborator, RemoveCollaborator, UpdateCollaboratorRole,
	// RegisterCurator, UnregisterCurator, RateCollection, ChallengeReview,
	// RequestSponsorship, CancelSponsorshipRequest, SponsorCollection,
	// UpvoteContent, DownvoteContent, FlagContent, HideContent, AppealHide,
	// EndorseCollection, SetSeekingEndorsement,
	// PinCollection, CreateAnonymousCollection, ManageAnonymousCollection, AnonymousReact
	require.Len(t, ops, 30)
}

func TestProposalMsgs(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(collect.AppModule{})
	am := collect.NewAppModule(encCfg.Codec, keeper.Keeper{}, nil, nil)
	simState := module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
	}

	msgs := am.ProposalMsgs(simState)
	require.Empty(t, msgs)
}
