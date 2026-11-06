package rep_test

import (
	"encoding/json"

	"math/rand"
	"testing"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/types/module"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/stretchr/testify/require"

	commons "sparkdream/x/commons/module"
	commonstypes "sparkdream/x/commons/types"
	"sparkdream/x/rep/keeper"
	rep "sparkdream/x/rep/module"
	"sparkdream/x/rep/types"
)

func TestGenerateGenesisState(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(rep.AppModule{})
	simState := &module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
		Rand:      rand.New(rand.NewSource(1)),
		GenState:  make(map[string]json.RawMessage),
		Accounts:  simtypes.RandomAccounts(rand.New(rand.NewSource(1)), 3),
	}
	rep.AppModule{}.GenerateGenesisState(simState)

	raw, ok := simState.GenState[types.ModuleName]
	require.True(t, ok, "expected GenState to contain entry for module %q", types.ModuleName)

	var genesis types.GenesisState
	require.NoError(t, encCfg.Codec.UnmarshalJSON(raw, &genesis))

	// The defaults, with the simulator-clock adjustments of simGenesisParams:
	// short epochs and migration delays no run reaches.
	require.NoError(t, genesis.Params.Validate())
	want := types.DefaultParams()
	want.EpochBlocks = genesis.Params.EpochBlocks
	want.ZkKeyActivationDelay = genesis.Params.ZkKeyActivationDelay
	want.DefaultReviewPeriodEpochs = genesis.Params.DefaultReviewPeriodEpochs
	want.ChallengeResponseDeadlineEpochs = genesis.Params.ChallengeResponseDeadlineEpochs
	want.DefaultChallengePeriodEpochs = genesis.Params.DefaultChallengePeriodEpochs
	want.InitiativeBountyReclaimDelay = genesis.Params.InitiativeBountyReclaimDelay
	want.KeyMigrationDelay = genesis.Params.KeyMigrationDelay
	want.RecoveryMigrationDelay = genesis.Params.RecoveryMigrationDelay
	want.CouncilMigrationDelay = genesis.Params.CouncilMigrationDelay
	want.RecoveryChangeDelay = genesis.Params.RecoveryChangeDelay
	want.MigrationAcceptWindow = genesis.Params.MigrationAcceptWindow
	want.RecoveryCommitTtl = genesis.Params.RecoveryCommitTtl
	require.Equal(t, want, genesis.Params)
	const year = int64(365 * 24 * 3600)
	require.Greater(t, genesis.Params.KeyMigrationDelay, 10*year)
	require.Greater(t, genesis.Params.RecoveryMigrationDelay, 10*year)
	require.Greater(t, genesis.Params.CouncilMigrationDelay, 10*year)
	require.Less(t, genesis.Params.EpochBlocks, types.DefaultParams().EpochBlocks)
}

// TestGenerateGenesisStateMembers checks the simulated membership against
// the commons seats generated before it: every seat-holder is a TRUSTED or
// CORE member, several other accounts are ESTABLISHED, each member's
// reputation and DREAM fit its trust level, and the genesis validates.
func TestGenerateGenesisStateMembers(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(rep.AppModule{}, commons.AppModule{})
	simState := &module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
		Rand:      rand.New(rand.NewSource(1)),
		GenState:  make(map[string]json.RawMessage),
		Accounts:  simtypes.RandomAccounts(rand.New(rand.NewSource(1)), 40),
	}
	commons.AppModule{}.GenerateGenesisState(simState)
	rep.AppModule{}.GenerateGenesisState(simState)

	var cg commonstypes.GenesisState
	require.NoError(t, encCfg.Codec.UnmarshalJSON(simState.GenState[commonstypes.ModuleName], &cg))
	var genesis types.GenesisState
	require.NoError(t, encCfg.Codec.UnmarshalJSON(simState.GenState[types.ModuleName], &genesis))
	require.NoError(t, genesis.Validate())

	members := map[string]types.Member{}
	for _, m := range genesis.MemberMap {
		members[m.Address] = m
	}
	for _, f := range cg.FoundingMembers {
		m, ok := members[f.Address]
		require.True(t, ok, "seat-holder %s is not a member", f.Address)
		require.GreaterOrEqual(t, m.TrustLevel, types.TrustLevel_TRUST_LEVEL_TRUSTED)
		if f.Founder {
			require.Equal(t, types.TrustLevel_TRUST_LEVEL_CORE, m.TrustLevel)
		}
	}
	require.Less(t, len(members), len(simState.Accounts), "some accounts stay non-members")

	cfg := genesis.Params.TrustLevelConfig
	established := 0
	for _, m := range genesis.MemberMap {
		require.Equal(t, types.MemberStatus_MEMBER_STATUS_ACTIVE, m.Status)
		require.True(t, m.StakedDream.IsZero())
		require.True(t, m.DreamBalance.Equal(*m.LifetimeEarned))
		total := math.LegacyZeroDec()
		for _, v := range m.ReputationScores {
			total = total.Add(math.LegacyMustNewDecFromStr(v))
		}
		switch m.TrustLevel {
		case types.TrustLevel_TRUST_LEVEL_ESTABLISHED:
			established++
			require.True(t, total.GTE(cfg.EstablishedMinRep) && total.LT(cfg.TrustedMinRep))
			require.True(t, m.DreamBalance.GTE(math.NewInt(2000_000_000)))
		case types.TrustLevel_TRUST_LEVEL_TRUSTED:
			require.True(t, total.GTE(cfg.TrustedMinRep) && total.LT(cfg.CoreMinRep))
		case types.TrustLevel_TRUST_LEVEL_CORE:
			require.True(t, total.GTE(cfg.CoreMinRep))
		}
	}
	require.GreaterOrEqual(t, established, 3)
}

func TestWeightedOperations(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(rep.AppModule{})
	am := rep.NewAppModule(encCfg.Codec, keeper.Keeper{}, nil, nil)
	simState := module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
		TxConfig:  encCfg.TxConfig,
	}
	ops := am.WeightedOperations(simState)
	require.Len(t, ops, 56) // +MsgRevokeInvitation, +9 member key migration ops (ForceMigration is authority-only and not simulated), +MsgClearZkPublicKey, +MsgCompleteProject (NoOp: policy-signed), +MsgInviteExpertWitness, +MsgEndorseExpertWitness
}

func TestProposalMsgs(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(rep.AppModule{})
	am := rep.NewAppModule(encCfg.Codec, keeper.Keeper{}, nil, nil)
	simState := module.SimulationState{
		AppParams: make(simtypes.AppParams),
		Cdc:       encCfg.Codec,
	}
	msgs := am.ProposalMsgs(simState)
	require.Len(t, msgs, 0)
}
