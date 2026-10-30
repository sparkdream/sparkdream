package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// A zeroed member's nomination stakes are dropped and the nomination's total
// is recomputed without them.
func TestZeroingHookDropsNominationStakes(t *testing.T) {
	f := initBeginBlockFixture(t)
	nom := types.Nomination{
		Id: 4, Nominator: "cosmos1nominator", Season: 1,
		TotalStaked: math.LegacyNewDec(300), Conviction: math.LegacyZeroDec(), RewardAmount: math.LegacyZeroDec(),
	}
	require.NoError(t, f.keeper.SetNomination(f.ctx, nom))
	require.NoError(t, f.keeper.NominationStake.Set(f.ctx, "4/cosmos1zeroed", types.NominationStake{
		NominationId: 4, Staker: "cosmos1zeroed", Amount: math.LegacyNewDec(100),
	}))
	require.NoError(t, f.keeper.NominationStake.Set(f.ctx, "4/cosmos1kept", types.NominationStake{
		NominationId: 4, Staker: "cosmos1kept", Amount: math.LegacyNewDec(200),
	}))

	hook := keeper.NewZeroingHook(&f.keeper)
	require.NoError(t, hook.AfterMemberZeroed(f.ctx, "cosmos1zeroed", []string{"cosmos1zeroed"}))

	_, err := f.keeper.NominationStake.Get(f.ctx, "4/cosmos1zeroed")
	require.Error(t, err)
	_, err = f.keeper.NominationStake.Get(f.ctx, "4/cosmos1kept")
	require.NoError(t, err)
	got, err := f.keeper.Nomination.Get(f.ctx, 4)
	require.NoError(t, err)
	require.Equal(t, math.LegacyNewDec(200).String(), got.TotalStaked.String())
}

// SEA-06: the reputation reset walks x/rep's members, so a member with no
// season profile is reset too.
func TestResetReputationCoversMembersWithoutProfile(t *testing.T) {
	f := initBeginBlockFixture(t)
	require.NoError(t, f.keeper.Season.Set(f.ctx, types.Season{
		Number: 1, Name: "Phoenix", EndBlock: 100, Status: types.SeasonStatus_SEASON_STATUS_ENDING,
	}))
	f.repKeeper.SetMember("cosmos1noprofile", 10, map[string]string{"tag": "40.0"}, 0)
	require.NoError(t, f.keeper.SeasonTransitionState.Set(f.ctx, types.SeasonTransitionState{
		Phase: types.TransitionPhase_TRANSITION_PHASE_RESET_REPUTATION,
	}))
	f.repKeeper.ArchiveCallCount = 0
	require.NoError(t, f.keeper.BeginBlocker(f.ctx))
	require.Contains(t, f.repKeeper.LastArchivedAddresses, "cosmos1noprofile")
}
