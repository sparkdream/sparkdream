package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/reveal/types"
)

func TestQueryContributionsByStatus(t *testing.T) {
	f := initTestFixture(t)

	f.createDefaultProposal(t)
	contribID2 := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, contribID2)

	// Query PROPOSED
	resp, err := f.queryServer.ContributionsByStatus(f.ctx, &types.QueryContributionsByStatusRequest{
		Status: types.ContributionStatus_CONTRIBUTION_STATUS_PROPOSED,
	})
	require.NoError(t, err)
	require.Equal(t, 1, len(resp.Contributions))

	// Query IN_PROGRESS
	resp, err = f.queryServer.ContributionsByStatus(f.ctx, &types.QueryContributionsByStatusRequest{
		Status: types.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS,
	})
	require.NoError(t, err)
	require.Equal(t, 1, len(resp.Contributions))
}

func TestQueryContributionsByStatus_NilRequest(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.ContributionsByStatus(f.ctx, nil)
	require.Error(t, err)
}
