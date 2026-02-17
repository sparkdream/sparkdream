package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/reveal/types"
)

func TestQueryTrancheStakes(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 10000)
	f.approveContribution(t, contribID)

	f.stakeOnTranche(t, contribID, 0, f.staker, 500)
	f.stakeOnTranche(t, contribID, 0, f.staker2, 300)

	resp, err := f.queryServer.TrancheStakes(f.ctx, &types.QueryTrancheStakesRequest{
		ContributionId: contribID,
		TrancheId:      0,
	})
	require.NoError(t, err)
	require.Equal(t, 2, len(resp.Stakes))
}

func TestQueryTrancheStakes_NilRequest(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.TrancheStakes(f.ctx, nil)
	require.Error(t, err)
}

func TestQueryTrancheStakes_NoStakes(t *testing.T) {
	f := initTestFixture(t)

	resp, err := f.queryServer.TrancheStakes(f.ctx, &types.QueryTrancheStakesRequest{
		ContributionId: 1,
		TrancheId:      0,
	})
	require.NoError(t, err)
	require.Empty(t, resp.Stakes)
}
