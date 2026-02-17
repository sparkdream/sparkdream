package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/reveal/types"
)

func TestQueryContributions(t *testing.T) {
	f := initTestFixture(t)

	// Create multiple contributions
	f.createDefaultProposal(t)
	f.createSingleTrancheProposal(t, 1000)

	resp, err := f.queryServer.Contributions(f.ctx, &types.QueryContributionsRequest{})
	require.NoError(t, err)
	require.Equal(t, 2, len(resp.Contributions))
}

func TestQueryContributions_NilRequest(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.Contributions(f.ctx, nil)
	require.Error(t, err)
}
