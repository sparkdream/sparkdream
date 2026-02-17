package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/reveal/types"
)

func TestQueryContribution_Success(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createDefaultProposal(t)

	resp, err := f.queryServer.Contribution(f.ctx, &types.QueryContributionRequest{
		ContributionId: contribID,
	})
	require.NoError(t, err)
	require.Equal(t, contribID, resp.Contribution.Id)
	require.Equal(t, "zenith-core", resp.Contribution.ProjectName)
}

func TestQueryContribution_NotFound(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.Contribution(f.ctx, &types.QueryContributionRequest{
		ContributionId: 9999,
	})
	require.Error(t, err)
}

func TestQueryContribution_NilRequest(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.Contribution(f.ctx, nil)
	require.Error(t, err)
}
