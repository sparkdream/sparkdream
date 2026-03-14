package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

func TestGetNextPendingOpIDSequence(t *testing.T) {
	f := initFixture(t)

	// IDs should increment sequentially
	id1 := f.keeper.GetNextPendingOpID(f.ctx)
	id2 := f.keeper.GetNextPendingOpID(f.ctx)
	id3 := f.keeper.GetNextPendingOpID(f.ctx)

	require.Equal(t, id1+1, id2)
	require.Equal(t, id2+1, id3)
}

func TestPendingOpSetDeleteIdempotent(t *testing.T) {
	f := initFixture(t)

	op := types.PendingShieldedOp{
		Id:          42,
		TargetEpoch: 5,
	}
	require.NoError(t, f.keeper.SetPendingOp(f.ctx, op))
	require.Equal(t, uint64(1), f.keeper.GetPendingOpCountVal(f.ctx))

	// Deleting should reduce count
	require.NoError(t, f.keeper.DeletePendingOp(f.ctx, 42))
	require.Equal(t, uint64(0), f.keeper.GetPendingOpCountVal(f.ctx))

	// Deleting again should not error
	require.NoError(t, f.keeper.DeletePendingOp(f.ctx, 42))
	require.Equal(t, uint64(0), f.keeper.GetPendingOpCountVal(f.ctx))
}

func TestGetPendingOpsForEpochMultiple(t *testing.T) {
	f := initFixture(t)

	// Add ops across multiple epochs
	for i := uint64(1); i <= 5; i++ {
		require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{
			Id:          i,
			TargetEpoch: i % 3, // epochs 1, 2, 0, 1, 2
		}))
	}

	require.Len(t, f.keeper.GetPendingOpsForEpoch(f.ctx, 0), 1) // op 3
	require.Len(t, f.keeper.GetPendingOpsForEpoch(f.ctx, 1), 2) // ops 1, 4
	require.Len(t, f.keeper.GetPendingOpsForEpoch(f.ctx, 2), 2) // ops 2, 5
}

func TestGetPendingOpsBeforeEpochEmpty(t *testing.T) {
	f := initFixture(t)

	ops := f.keeper.GetPendingOpsBeforeEpoch(f.ctx, 10)
	require.Len(t, ops, 0)
}

func TestGetPendingOpsBeforeEpochFiltering(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{
		Id: 1, TargetEpoch: 10, SubmittedAtEpoch: 1,
	}))
	require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{
		Id: 2, TargetEpoch: 10, SubmittedAtEpoch: 5,
	}))
	require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{
		Id: 3, TargetEpoch: 10, SubmittedAtEpoch: 10,
	}))

	// cutoff=5 means SubmittedAtEpoch < 5, so only op 1 (epoch 1)
	ops := f.keeper.GetPendingOpsBeforeEpoch(f.ctx, 5)
	require.Len(t, ops, 1)
	require.Equal(t, uint64(1), ops[0].Id)

	// cutoff=10 means SubmittedAtEpoch < 10, so ops 1 and 2
	ops = f.keeper.GetPendingOpsBeforeEpoch(f.ctx, 10)
	require.Len(t, ops, 2)
}

func TestPendingOpOverwrite(t *testing.T) {
	f := initFixture(t)

	// Set op, then overwrite with different data
	require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{
		Id:               1,
		TargetEpoch:      5,
		EncryptedPayload: []byte("original"),
	}))

	require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{
		Id:               1,
		TargetEpoch:      7,
		EncryptedPayload: []byte("updated"),
	}))

	// Should still be count 1 (same key)
	require.Equal(t, uint64(1), f.keeper.GetPendingOpCountVal(f.ctx))

	ops := f.keeper.GetPendingOpsForEpoch(f.ctx, 7)
	require.Len(t, ops, 1)
	require.Equal(t, []byte("updated"), ops[0].EncryptedPayload)
}
