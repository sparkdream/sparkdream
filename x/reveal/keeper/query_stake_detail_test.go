package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/reveal/types"
)

func TestQueryStakeDetail(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 10000)
	f.approveContribution(t, contribID)

	stakeID := f.stakeOnTranche(t, contribID, 0, f.staker, 500)

	resp, err := f.queryServer.StakeDetail(f.ctx, &types.QueryStakeDetailRequest{
		StakeId: stakeID,
	})
	require.NoError(t, err)
	require.Equal(t, stakeID, resp.Stake.Id)
	require.Equal(t, f.staker, resp.Stake.Staker)
	require.Equal(t, math.NewInt(500), resp.Stake.Amount)
}

func TestQueryStakeDetail_NotFound(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.StakeDetail(f.ctx, &types.QueryStakeDetailRequest{
		StakeId: 9999,
	})
	require.Error(t, err)
}

func TestQueryStakeDetail_NilRequest(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.StakeDetail(f.ctx, nil)
	require.Error(t, err)
}
