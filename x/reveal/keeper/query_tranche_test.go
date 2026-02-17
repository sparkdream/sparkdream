package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/reveal/types"
)

func TestQueryTranche(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createDefaultProposal(t)

	resp, err := f.queryServer.Tranche(f.ctx, &types.QueryTrancheRequest{
		ContributionId: contribID,
		TrancheId:      0,
	})
	require.NoError(t, err)
	require.Equal(t, "phase-1", resp.Tranche.Name)
}

func TestQueryTranche_NotFound(t *testing.T) {
	f := initTestFixture(t)
	contribID := f.createDefaultProposal(t)

	_, err := f.queryServer.Tranche(f.ctx, &types.QueryTrancheRequest{
		ContributionId: contribID,
		TrancheId:      99, // doesn't exist
	})
	require.Error(t, err)
}

func TestQueryTranche_ContributionNotFound(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.Tranche(f.ctx, &types.QueryTrancheRequest{
		ContributionId: 9999,
		TrancheId:      0,
	})
	require.Error(t, err)
}

func TestQueryTranche_NilRequest(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.queryServer.Tranche(f.ctx, nil)
	require.Error(t, err)
}
