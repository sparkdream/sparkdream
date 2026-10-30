package keeper_test

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/keeper"
	"sparkdream/x/shield/types"
)

// TestShieldInvariants (SHD-18): each invariant holds on a consistent store
// and reports the corruption it guards against.
func TestShieldInvariants(t *testing.T) {
	f := initFixture(t)
	run := func(name string) bool {
		switch name {
		case "count":
			_, b := keeper.PendingOpCountInvariant(f.keeper)(f.ctx)
			return b
		case "pending":
			_, b := keeper.PendingNullifiersInvariant(f.keeper)(f.ctx)
			return b
		default:
			_, b := keeper.CanonicalNullifiersInvariant(f.keeper)(f.ctx)
			return b
		}
	}
	for _, n := range []string{"count", "pending", "canonical"} {
		require.False(t, run(n), n)
	}

	// A pending op with its nullifier in the dedup set: consistent.
	sub := make([]byte, 32)
	sub[31] = 9
	require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{Id: 1, TargetEpoch: 1, SubmissionNullifier: sub}))
	require.NoError(t, f.keeper.RecordPendingNullifier(f.ctx, hex.EncodeToString(sub)))
	require.False(t, run("count"))
	require.False(t, run("pending"))

	// A leaked pending nullifier, then a drifted counter.
	require.NoError(t, f.keeper.RecordPendingNullifier(f.ctx, hex.EncodeToString(make([]byte, 32))))
	require.True(t, run("pending"))
	require.NoError(t, f.keeper.PendingOpCount.Set(f.ctx, 5))
	require.True(t, run("count"))

	// A used nullifier at or above the field modulus.
	alias := new(big.Int).Add(big.NewInt(1), fr.Modulus()).FillBytes(make([]byte, 32))
	require.NoError(t, f.keeper.RecordNullifier(f.ctx, 2, 7, hex.EncodeToString(alias), 1))
	require.True(t, run("canonical"))
}
