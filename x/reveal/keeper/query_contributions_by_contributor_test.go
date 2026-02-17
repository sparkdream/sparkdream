package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/reveal/types"
)

func TestQueryContributionsByContributor(t *testing.T) {
	f := initTestFixture(t)

	f.createDefaultProposal(t)
	f.createSingleTrancheProposal(t, 1000)

	resp, err := f.queryServer.ContributionsByContributor(f.ctx, &types.QueryContributionsByContributorRequest{
		Contributor: f.contributor,
	})
	require.NoError(t, err)
	require.Equal(t, 2, len(resp.Contributions))
	for _, c := range resp.Contributions {
		require.Equal(t, f.contributor, c.Contributor)
	}
}

func TestQueryContributionsByContributor_NilRequest(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.ContributionsByContributor(f.ctx, nil)
	require.Error(t, err)
}

func TestQueryContributionsByContributor_NoResults(t *testing.T) {
	f := initTestFixture(t)

	resp, err := f.queryServer.ContributionsByContributor(f.ctx, &types.QueryContributionsByContributorRequest{
		Contributor: f.staker, // hasn't contributed anything
	})
	require.NoError(t, err)
	require.Empty(t, resp.Contributions)
}
