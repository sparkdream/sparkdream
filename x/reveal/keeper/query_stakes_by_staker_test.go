package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/reveal/types"
)

func TestQueryStakesByStaker(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createSingleTrancheProposal(t, 10000)
	f.approveContribution(t, contribID)

	f.stakeOnTranche(t, contribID, 0, f.staker, 500)
	f.stakeOnTranche(t, contribID, 0, f.staker2, 300)

	resp, err := f.queryServer.StakesByStaker(f.ctx, &types.QueryStakesByStakerRequest{
		Staker: f.staker,
	})
	require.NoError(t, err)
	require.Equal(t, 1, len(resp.Stakes))
	require.Equal(t, f.staker, resp.Stakes[0].Staker)
}

func TestQueryStakesByStaker_NilRequest(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.StakesByStaker(f.ctx, nil)
	require.Error(t, err)
}
