package keeper_test

import (
	"context"
	"errors"
	"testing"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// COL-104 regression: an EndBlocker collection delete that fails after the
// deposit refund must leave no trace, or the retry next block pays the
// refund again out of other owners' escrow.
func TestEndBlockerCollectionDeleteIsAtomic(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	collID := f.createPendingCollection(t) // escrows a deposit + the endorsement fee
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)

	// The mock bank is stateless; record each payout in the module store
	// through the ctx it receives, so a discarded cache context discards the
	// payout too, as the real bank's does.
	refunds := func() int {
		n := 0
		require.NoError(t, f.keeper.CollectionsByOwner.Walk(f.ctx,
			collections.NewPrefixedPairRange[string, uint64]("zz-refund-ledger"),
			func(collections.Pair[string, uint64]) (bool, error) { n++; return false, nil }))
		return n
	}
	var seq uint64
	f.bankKeeper.sendCoinsFromModuleToAccountFn = func(ctx context.Context, _ string, _ sdk.AccAddress, _ sdk.Coins) error {
		seq++
		return f.keeper.CollectionsByOwner.Set(ctx, collections.Join("zz-refund-ledger", seq))
	}
	f.bankKeeper.burnCoinsFn = func(context.Context, string, sdk.Coins) error { return errors.New("module account short") }

	// Past both the endorsement deadline and the TTL: both pruners try.
	f.setBlockHeight(coll.ExpiresAt + 1)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	f.setBlockHeight(coll.ExpiresAt + 2)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	require.Zero(t, refunds(), "a failed delete pays nothing")
	_, err = f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err, "the collection survives a failed delete")

	f.bankKeeper.burnCoinsFn = nil
	f.setBlockHeight(coll.ExpiresAt + 3)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))
	_, err = f.keeper.Collection.Get(f.ctx, collID)
	require.Error(t, err, "the retry deletes it")
	require.Equal(t, 2, refunds(), "one deposit refund and one fee refund, once")
}
