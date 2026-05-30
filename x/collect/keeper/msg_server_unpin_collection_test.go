package keeper_test

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
	reptypes "sparkdream/x/rep/types"
)

func TestUnpinCollection_Success(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)
	_, err := f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator: f.owner, CollectionId: collID,
	})
	require.NoError(t, err)

	resp, err := f.msgServer.UnpinCollection(f.ctx, &types.MsgUnpinCollection{
		Creator: f.owner, CollectionId: collID,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	require.False(t, coll.Pinned)
}

func TestUnpinCollection_NotPinned(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)

	_, err := f.msgServer.UnpinCollection(f.ctx, &types.MsgUnpinCollection{
		Creator: f.owner, CollectionId: collID,
	})
	require.ErrorIs(t, err, types.ErrCollectionNotPinned)
}

func TestUnpinCollection_NotFound(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	_, err := f.msgServer.UnpinCollection(f.ctx, &types.MsgUnpinCollection{
		Creator: f.owner, CollectionId: 9999,
	})
	require.ErrorIs(t, err, types.ErrCollectionNotFound)
}

func TestUnpinCollection_TrustLevelTooLow(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)
	_, err := f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator: f.owner, CollectionId: collID,
	})
	require.NoError(t, err)

	f.repKeeper.getTrustLevelFn = func(_ context.Context, _ sdk.AccAddress) (reptypes.TrustLevel, error) {
		return reptypes.TrustLevel_TRUST_LEVEL_PROVISIONAL, nil
	}

	_, err = f.msgServer.UnpinCollection(f.ctx, &types.MsgUnpinCollection{
		Creator: f.owner, CollectionId: collID,
	})
	require.ErrorIs(t, err, types.ErrPinTrustLevelTooLow)
}

func TestUnpinCollection_RateLimitSharedWithPin(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.MaxPinsPerDay = 1
	f.keeper.Params.Set(f.ctx, params) //nolint:errcheck

	collID := f.createCollection(t, f.owner)
	_, err := f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator: f.owner, CollectionId: collID,
	})
	require.NoError(t, err) // consumes the one allowed slot

	_, err = f.msgServer.UnpinCollection(f.ctx, &types.MsgUnpinCollection{
		Creator: f.owner, CollectionId: collID,
	})
	require.ErrorIs(t, err, types.ErrMaxDailyReactions,
		"Pin and Unpin share the daily counter so an alternation can't bypass the cap")
}
