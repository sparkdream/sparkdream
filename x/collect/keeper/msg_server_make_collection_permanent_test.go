package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
	reptypes "sparkdream/x/rep/types"
)

func TestMakeCollectionPermanent_Success(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createTTLCollection(t, f.owner, 500)

	var burnCalled bool
	f.bankKeeper.burnCoinsFn = func(_ context.Context, _ string, _ sdk.Coins) error {
		burnCalled = true
		return nil
	}

	resp, err := f.msgServer.MakeCollectionPermanent(f.ctx, &types.MsgMakeCollectionPermanent{
		Creator: f.owner, CollectionId: collID,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	require.Equal(t, int64(0), coll.ExpiresAt)
	require.True(t, coll.DepositBurned)
	require.False(t, coll.Pinned, "MakePermanent must not flip the pinned marker")
	require.True(t, burnCalled, "deposits should be burned on lifecycle promotion")
}

func TestMakeCollectionPermanent_BurnsDepositsIncludingItems(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createTTLCollection(t, f.owner, 500)
	f.addItem(t, collID, f.owner)
	f.addItem(t, collID, f.owner)

	var burnedAmount sdk.Coins
	f.bankKeeper.burnCoinsFn = func(_ context.Context, _ string, amt sdk.Coins) error {
		burnedAmount = burnedAmount.Add(amt...)
		return nil
	}

	_, err := f.msgServer.MakeCollectionPermanent(f.ctx, &types.MsgMakeCollectionPermanent{
		Creator: f.owner, CollectionId: collID,
	})
	require.NoError(t, err)

	params, _ := f.keeper.Params.Get(f.ctx)
	expected := params.BaseCollectionDeposit.Add(params.PerItemDeposit.MulRaw(2))
	require.Equal(t, expected, burnedAmount.AmountOf("uspark"))
}

func TestMakeCollectionPermanent_IdempotentOnPermanent(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner) // permanent

	var burnCalled bool
	f.bankKeeper.burnCoinsFn = func(_ context.Context, _ string, _ sdk.Coins) error {
		burnCalled = true
		return nil
	}

	resp, err := f.msgServer.MakeCollectionPermanent(f.ctx, &types.MsgMakeCollectionPermanent{
		Creator: f.owner, CollectionId: collID,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.False(t, burnCalled, "already-permanent collection must not re-burn")
}

func TestMakeCollectionPermanent_ZeroDepositsNoBurn(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createTTLCollection(t, f.owner, 500)
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	coll.DepositAmount = math.ZeroInt()
	coll.ItemDepositTotal = math.ZeroInt()
	f.keeper.Collection.Set(f.ctx, collID, coll) //nolint:errcheck

	var burnCalled bool
	f.bankKeeper.burnCoinsFn = func(_ context.Context, _ string, _ sdk.Coins) error {
		burnCalled = true
		return nil
	}

	_, err := f.msgServer.MakeCollectionPermanent(f.ctx, &types.MsgMakeCollectionPermanent{
		Creator: f.owner, CollectionId: collID,
	})
	require.NoError(t, err)
	require.False(t, burnCalled)
}

func TestMakeCollectionPermanent_NotFound(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	_, err := f.msgServer.MakeCollectionPermanent(f.ctx, &types.MsgMakeCollectionPermanent{
		Creator: f.owner, CollectionId: 9999,
	})
	require.ErrorIs(t, err, types.ErrCollectionNotFound)
}

func TestMakeCollectionPermanent_Expired(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createTTLCollection(t, f.owner, 150)
	f.setBlockHeight(200)

	_, err := f.msgServer.MakeCollectionPermanent(f.ctx, &types.MsgMakeCollectionPermanent{
		Creator: f.owner, CollectionId: collID,
	})
	require.ErrorIs(t, err, types.ErrCollectionExpired)
}

func TestMakeCollectionPermanent_TrustLevelTooLow(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// Raise the gate above ESTABLISHED so the default mock falls under.
	params, _ := f.keeper.Params.Get(f.ctx)
	params.MakePermanentMinTrustLevel = 3 // TRUSTED
	f.keeper.Params.Set(f.ctx, params)    //nolint:errcheck

	collID := f.createTTLCollection(t, f.owner, 500)

	_, err := f.msgServer.MakeCollectionPermanent(f.ctx, &types.MsgMakeCollectionPermanent{
		Creator: f.owner, CollectionId: collID,
	})
	require.ErrorIs(t, err, types.ErrMakePermanentTrustLevelTooLow)

	// Bumping the caller to CORE lets it through.
	f.repKeeper.getTrustLevelFn = func(_ context.Context, _ sdk.AccAddress) (reptypes.TrustLevel, error) {
		return reptypes.TrustLevel_TRUST_LEVEL_CORE, nil
	}
	_, err = f.msgServer.MakeCollectionPermanent(f.ctx, &types.MsgMakeCollectionPermanent{
		Creator: f.owner, CollectionId: collID,
	})
	require.NoError(t, err)
}

func TestMakeCollectionPermanent_NotAMember(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createTTLCollection(t, f.owner, 500)
	_, err := f.msgServer.MakeCollectionPermanent(f.ctx, &types.MsgMakeCollectionPermanent{
		Creator: f.nonMember, CollectionId: collID,
	})
	require.ErrorIs(t, err, types.ErrMakePermanentTrustLevelTooLow)
}

func TestMakeCollectionPermanent_RateLimitIndependentOfPin(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.MaxPinsPerDay = 1
	params.MaxMakePermanentPerDay = 2
	f.keeper.Params.Set(f.ctx, params) //nolint:errcheck

	permCollID := f.createCollection(t, f.owner)
	ttlCollID1 := f.createTTLCollection(t, f.owner, 500)
	ttlCollID2 := f.createTTLCollection(t, f.owner, 500)

	// Pin consumes the one allowed pin slot.
	_, err := f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator: f.owner, CollectionId: permCollID,
	})
	require.NoError(t, err)

	// MakePermanent draws from its own counter and is unaffected by the
	// exhausted pin quota.
	_, err = f.msgServer.MakeCollectionPermanent(f.ctx, &types.MsgMakeCollectionPermanent{
		Creator: f.owner, CollectionId: ttlCollID1,
	})
	require.NoError(t, err, "MakePermanent counter is independent of MaxPinsPerDay")

	_, err = f.msgServer.MakeCollectionPermanent(f.ctx, &types.MsgMakeCollectionPermanent{
		Creator: f.owner, CollectionId: ttlCollID2,
	})
	require.NoError(t, err, "second MakePermanent still within MaxMakePermanentPerDay=2")
}

func TestMakeCollectionPermanent_RateLimitOwnCap(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.MaxMakePermanentPerDay = 1
	f.keeper.Params.Set(f.ctx, params) //nolint:errcheck

	id1 := f.createTTLCollection(t, f.owner, 500)
	id2 := f.createTTLCollection(t, f.owner, 500)

	_, err := f.msgServer.MakeCollectionPermanent(f.ctx, &types.MsgMakeCollectionPermanent{
		Creator: f.owner, CollectionId: id1,
	})
	require.NoError(t, err)

	_, err = f.msgServer.MakeCollectionPermanent(f.ctx, &types.MsgMakeCollectionPermanent{
		Creator: f.owner, CollectionId: id2,
	})
	require.ErrorIs(t, err, types.ErrMaxDailyReactions,
		"second MakePermanent in the same day must hit MaxMakePermanentPerDay")
}

func TestMakeCollectionPermanent_RoundTripWithPin(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createTTLCollection(t, f.owner, 500)

	// Step 1: MakePermanent flips lifecycle without setting pinned.
	_, err := f.msgServer.MakeCollectionPermanent(f.ctx, &types.MsgMakeCollectionPermanent{
		Creator: f.owner, CollectionId: collID,
	})
	require.NoError(t, err)
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	require.Equal(t, int64(0), coll.ExpiresAt)
	require.False(t, coll.Pinned)

	// Step 2: Pin now succeeds because the target is permanent.
	_, err = f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator: f.owner, CollectionId: collID,
	})
	require.NoError(t, err)
	coll, _ = f.keeper.Collection.Get(f.ctx, collID)
	require.True(t, coll.Pinned)
	require.Equal(t, int64(0), coll.ExpiresAt)
}
