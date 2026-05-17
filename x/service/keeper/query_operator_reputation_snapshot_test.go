package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/types"
)

func TestQueryOperatorReputationSnapshot(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)

	// No live records → both totals zero.
	resp, err := f.queryServer.OperatorReputationSnapshot(f.ctx, &types.QueryOperatorReputationSnapshotRequest{
		Address: testOperator1,
	})
	require.NoError(t, err)
	require.True(t, resp.TotalBondBlocks.IsZero())
	require.True(t, resp.EffectiveBondBlocks.IsZero())

	// Seed one ACTIVE operator and advance the block height — settleBondBlocks
	// runs in-memory at query time and accrues elapsed * bond.
	op := f.seedActiveOperator(t, testOperator1, testController, math.NewInt(1_000))
	startHeight := op.LastBondBlockUpdateAt
	f.withBlockHeight(startHeight + 5)

	resp, err = f.queryServer.OperatorReputationSnapshot(f.ctx, &types.QueryOperatorReputationSnapshotRequest{
		Address: testOperator1,
	})
	require.NoError(t, err)
	// 5 blocks * 1000 bond = 5000 bond-blocks.
	require.True(t, resp.TotalBondBlocks.Equal(math.NewInt(5_000)))
	require.True(t, resp.EffectiveBondBlocks.Equal(math.NewInt(5_000)))
}

func TestQueryOperatorReputationSnapshot_InvalidAddress(t *testing.T) {
	f := initFixture(t)

	_, err := f.queryServer.OperatorReputationSnapshot(f.ctx, &types.QueryOperatorReputationSnapshotRequest{
		Address: "not-bech32",
	})
	require.Error(t, err)
}

func TestQueryOperatorReputationSnapshot_NilRequest(t *testing.T) {
	f := initFixture(t)
	_, err := f.queryServer.OperatorReputationSnapshot(f.ctx, nil)
	require.Error(t, err)
}
