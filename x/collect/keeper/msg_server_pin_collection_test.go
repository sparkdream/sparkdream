package keeper_test

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
	reptypes "sparkdream/x/rep/types"
)

// Pin is now a display-only marker: it requires the collection to already be
// permanent and only flips the Pinned flag. Deposit-burn + lifecycle change
// live in MsgMakeCollectionPermanent (see msg_server_make_collection_permanent_test.go).

func TestPinCollection_Success(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner) // permanent (no TTL)

	var burnCalled bool
	f.bankKeeper.burnCoinsFn = func(_ context.Context, _ string, _ sdk.Coins) error {
		burnCalled = true
		return nil
	}

	resp, err := f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator:      f.owner,
		CollectionId: collID,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	require.True(t, coll.Pinned, "Pin should set the pinned marker")
	require.Equal(t, int64(0), coll.ExpiresAt, "Pin must not change lifecycle")
	require.False(t, burnCalled, "Pin must not burn deposits — that's MakePermanent's job")
}

func TestPinCollection_RejectsEphemeral(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createTTLCollection(t, f.owner, 500)

	_, err := f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator:      f.owner,
		CollectionId: collID,
	})
	require.ErrorIs(t, err, types.ErrCannotPinEphemeral)
}

func TestPinCollection_AlreadyPinned(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)

	_, err := f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator: f.owner, CollectionId: collID,
	})
	require.NoError(t, err)

	// Second pin on the same collection rejects with ErrCollectionAlreadyPinned.
	_, err = f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator: f.owner, CollectionId: collID,
	})
	require.ErrorIs(t, err, types.ErrCollectionAlreadyPinned)
}

func TestPinCollection_CollectionNotFound(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	_, err := f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator:      f.owner,
		CollectionId: 9999,
	})
	require.ErrorIs(t, err, types.ErrCollectionNotFound)
}

func TestPinCollection_NotAMember(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)

	_, err := f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator:      f.nonMember,
		CollectionId: collID,
	})
	require.ErrorIs(t, err, types.ErrPinTrustLevelTooLow)
}

func TestPinCollection_TrustLevelTooLow(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)

	f.repKeeper.getTrustLevelFn = func(_ context.Context, _ sdk.AccAddress) (reptypes.TrustLevel, error) {
		return reptypes.TrustLevel_TRUST_LEVEL_PROVISIONAL, nil // level 1, need 2
	}

	_, err := f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator:      f.owner,
		CollectionId: collID,
	})
	require.ErrorIs(t, err, types.ErrPinTrustLevelTooLow)
}

func TestPinCollection_RateLimit(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.MaxPinsPerDay = 1
	f.keeper.Params.Set(f.ctx, params) //nolint:errcheck

	collID1 := f.createCollection(t, f.owner)
	collID2 := f.createCollection(t, f.owner)

	_, err := f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator:      f.owner,
		CollectionId: collID1,
	})
	require.NoError(t, err)

	// Second pin on same day hits the shared per-day cap.
	_, err = f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator:      f.owner,
		CollectionId: collID2,
	})
	require.ErrorIs(t, err, types.ErrMaxDailyReactions)
}

func TestPinCollection_HiddenNotActive(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	coll.Status = types.CollectionStatus_COLLECTION_STATUS_HIDDEN
	f.keeper.Collection.Set(f.ctx, collID, coll) //nolint:errcheck

	_, err := f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator:      f.owner,
		CollectionId: collID,
	})
	require.Error(t, err)
}

func TestPinCollection_HighTrustLevel(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.PinMinTrustLevel = 3
	f.keeper.Params.Set(f.ctx, params) //nolint:errcheck

	collID := f.createCollection(t, f.owner)

	// Default mock returns ESTABLISHED (2) — should fail.
	_, err := f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator:      f.owner,
		CollectionId: collID,
	})
	require.ErrorIs(t, err, types.ErrPinTrustLevelTooLow)

	f.repKeeper.getTrustLevelFn = func(_ context.Context, _ sdk.AccAddress) (reptypes.TrustLevel, error) {
		return reptypes.TrustLevel_TRUST_LEVEL_CORE, nil
	}

	_, err = f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator:      f.owner,
		CollectionId: collID,
	})
	require.NoError(t, err)
}

// Verify the fixture still wires the msg server entrypoint.
var _ = keeper.NewMsgServerImpl
