package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
	reptypes "sparkdream/x/rep/types"
)

func TestPinCollection_Success(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// owner has TRUST_LEVEL_ESTABLISHED (default mock), pin_min_trust_level=2 (default)
	collID := f.createTTLCollection(t, f.owner, 500)

	// Track deposit burn
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

	// Verify collection is now permanent
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	require.Equal(t, int64(0), coll.ExpiresAt)
	require.True(t, coll.DepositBurned)
	require.True(t, burnCalled, "deposits should be burned on pin")
}

func TestPinCollection_AlreadyPermanent(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// Create a permanent collection (no TTL)
	collID := f.createCollection(t, f.owner) // no TTL = permanent

	_, err := f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator:      f.owner,
		CollectionId: collID,
	})
	require.ErrorIs(t, err, types.ErrCannotPinActive)
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

func TestPinCollection_Expired(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createTTLCollection(t, f.owner, 150)

	// Advance past expiry
	f.setBlockHeight(200)

	_, err := f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator:      f.owner,
		CollectionId: collID,
	})
	require.ErrorIs(t, err, types.ErrCollectionExpired)
}

func TestPinCollection_NotAMember(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createTTLCollection(t, f.owner, 500)

	// nonMember is not a member
	_, err := f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator:      f.nonMember,
		CollectionId: collID,
	})
	require.ErrorIs(t, err, types.ErrPinTrustLevelTooLow)
}

func TestPinCollection_TrustLevelTooLow(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createTTLCollection(t, f.owner, 500)

	// Set member trust level below pin requirement
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

	// Set max pins per day to 1
	params, _ := f.keeper.Params.Get(f.ctx)
	params.MaxPinsPerDay = 1
	f.keeper.Params.Set(f.ctx, params) //nolint:errcheck

	// Create two TTL collections
	collID1 := f.createTTLCollection(t, f.owner, 500)
	collID2 := f.createTTLCollection(t, f.owner, 600)

	// First pin should succeed
	_, err := f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator:      f.owner,
		CollectionId: collID1,
	})
	require.NoError(t, err)

	// Second pin on same day should fail (rate limit)
	_, err = f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator:      f.owner,
		CollectionId: collID2,
	})
	require.ErrorIs(t, err, types.ErrMaxDailyReactions)
}

func TestPinCollection_HiddenNotActive(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createTTLCollection(t, f.owner, 500)

	// Set collection to HIDDEN
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	coll.Status = types.CollectionStatus_COLLECTION_STATUS_HIDDEN
	f.keeper.Collection.Set(f.ctx, collID, coll) //nolint:errcheck

	_, err := f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator:      f.owner,
		CollectionId: collID,
	})
	require.Error(t, err) // collection not active
}

func TestPinCollection_BurnsDepositsIncludingItems(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// Create TTL collection and add items
	collID := f.createTTLCollection(t, f.owner, 500)
	f.addItem(t, collID, f.owner)
	f.addItem(t, collID, f.owner)

	var burnedAmount sdk.Coins
	f.bankKeeper.burnCoinsFn = func(_ context.Context, _ string, amt sdk.Coins) error {
		burnedAmount = burnedAmount.Add(amt...)
		return nil
	}

	_, err := f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator:      f.owner,
		CollectionId: collID,
	})
	require.NoError(t, err)

	// Should have burned base deposit + 2 * per-item deposit
	params, _ := f.keeper.Params.Get(f.ctx)
	expectedBurn := params.BaseCollectionDeposit.Add(params.PerItemDeposit.MulRaw(2))
	require.Equal(t, expectedBurn, burnedAmount.AmountOf("uspark"))
}

func TestPinCollection_HighTrustLevel(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	// Set pin trust level to CORE (3)
	params, _ := f.keeper.Params.Get(f.ctx)
	params.PinMinTrustLevel = 3
	f.keeper.Params.Set(f.ctx, params) //nolint:errcheck

	collID := f.createTTLCollection(t, f.owner, 500)

	// Default mock returns ESTABLISHED (2) - should fail
	_, err := f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator:      f.owner,
		CollectionId: collID,
	})
	require.ErrorIs(t, err, types.ErrPinTrustLevelTooLow)

	// Set trust level to CORE
	f.repKeeper.getTrustLevelFn = func(_ context.Context, _ sdk.AccAddress) (reptypes.TrustLevel, error) {
		return reptypes.TrustLevel_TRUST_LEVEL_CORE, nil
	}

	_, err = f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator:      f.owner,
		CollectionId: collID,
	})
	require.NoError(t, err)
}

func TestPinCollection_ZeroDepositsNoBurn(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createTTLCollection(t, f.owner, 500)

	// Manually set deposits to zero
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	coll.DepositAmount = math.ZeroInt()
	coll.ItemDepositTotal = math.ZeroInt()
	f.keeper.Collection.Set(f.ctx, collID, coll) //nolint:errcheck

	var burnCalled bool
	f.bankKeeper.burnCoinsFn = func(_ context.Context, _ string, _ sdk.Coins) error {
		burnCalled = true
		return nil
	}

	_, err := f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator:      f.owner,
		CollectionId: collID,
	})
	require.NoError(t, err)
	require.False(t, burnCalled, "zero deposits should not trigger burn")
}

// Verify we can use the non-anon initTestFixture for pin tests (no voteKeeper needed)
func TestPinCollection_UsesInitTestFixture(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	collID := f.createTTLCollection(t, f.owner, 500)

	resp, err := f.msgServer.PinCollection(f.ctx, &types.MsgPinCollection{
		Creator:      f.owner,
		CollectionId: collID,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// Suppress unused import warnings - these are used in the test file but
// the compiler may complain about unused specific imports.
var _ = keeper.NewMsgServerImpl
