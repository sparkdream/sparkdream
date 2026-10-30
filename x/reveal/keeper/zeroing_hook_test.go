package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/reveal/keeper"
	"sparkdream/x/reveal/types"
)

// A zeroed member's stake on a tranche still collecting is dropped and no
// longer counts toward the threshold (x/rep burned the DREAM behind it).
func TestZeroingHookDropsStakingTrancheStakes(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID)
	f.stakeOnTranche(t, contribID, 0, f.staker, 300)
	f.stakeOnTranche(t, contribID, 0, f.staker2, 200)

	hook := keeper.NewZeroingHook(&f.keeper)
	require.NoError(t, hook.AfterMemberZeroed(f.ctx, f.staker2, []string{f.staker2}))

	contrib, err := f.keeper.Contribution.Get(f.ctx, contribID)
	require.NoError(t, err)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_STAKING, contrib.Tranches[0].Status)
	require.Equal(t, math.NewInt(300), contrib.Tranches[0].DreamStaked)

	resp, err := f.queryServer.TrancheStakes(f.ctx, &types.QueryTrancheStakesRequest{ContributionId: contribID, TrancheId: 0})
	require.NoError(t, err)
	require.Len(t, resp.Stakes, 1)
	require.Equal(t, f.staker, resp.Stakes[0].Staker)
}
