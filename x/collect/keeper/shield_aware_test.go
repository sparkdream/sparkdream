package keeper_test

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
	shieldtypes "sparkdream/x/shield/types"
)

// ownerCtx is the context x/shield runs an ownership-mode exec in: the proven
// trust level and the owner tag the proof reproduced.
func ownerCtx(f *testFixture, tag []byte) sdk.Context {
	return shieldtypes.WithOwnershipTag(shieldtypes.WithProvenTrustLevel(f.sdkCtx, 1), tag)
}

// An anonymous create stores the creation nullifier as the owner claim.
func TestCreateCollection_AnonymousStoresOwnerClaim(t *testing.T) {
	f := initTestFixture(t)
	id := createAnonCollection(t, f, 7, f.sdkCtx.BlockHeight()+100)

	coll, err := f.keeper.Collection.Get(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, uint32(21), coll.AnonOwnerDomain)
	require.Equal(t, uint64(7), coll.AnonOwnerScope)
	require.Len(t, coll.AnonOwnerTag, 32)
	require.Equal(t, byte(7), coll.AnonOwnerTag[31])
	require.Zero(t, coll.AnonOwnerSequence)

	// Without the creation nullifier (not a shielded exec) there is no owner.
	_, err = f.msgServer.CreateCollection(shieldtypes.WithProvenTrustLevel(f.sdkCtx, 1), &types.MsgCreateCollection{
		Creator:    authtypes.NewModuleAddress("shield").String(),
		Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
		Visibility: types.Visibility_VISIBILITY_PUBLIC,
		Name:       "aurora",
		ExpiresAt:  f.sdkCtx.BlockHeight() + 100,
	})
	require.ErrorIs(t, err, types.ErrAnonymousNoOwnerClaim)
}

// Only an exec carrying this collection's owner tag can manage it as the
// shield address; another anonymous collection's tag, or none, cannot.
func TestAnonymousCollection_OwnerManages(t *testing.T) {
	f := initTestFixture(t)
	shield := authtypes.NewModuleAddress("shield").String()
	expires := f.sdkCtx.BlockHeight() + 100
	mine := createAnonCollection(t, f, 1, expires)
	other := createAnonCollection(t, f, 2, expires)

	mineColl, err := f.keeper.Collection.Get(f.ctx, mine)
	require.NoError(t, err)
	otherColl, err := f.keeper.Collection.Get(f.ctx, other)
	require.NoError(t, err)
	addItem := &types.MsgAddItem{Creator: shield, CollectionId: mine, Title: "zenith"}

	_, err = f.msgServer.AddItem(shieldtypes.WithProvenTrustLevel(f.sdkCtx, 1), addItem)
	require.ErrorIs(t, err, types.ErrUnauthorized, "no ownership proof")
	_, err = f.msgServer.AddItem(shieldtypes.WithOwnershipTag(f.sdkCtx, otherColl.AnonOwnerTag), addItem)
	require.ErrorIs(t, err, types.ErrUnauthorized, "another collection's owner")

	ownCtx := ownerCtx(f, mineColl.AnonOwnerTag)
	itemResp, err := f.msgServer.AddItem(ownCtx, addItem)
	require.NoError(t, err)

	_, err = f.msgServer.UpdateItem(ownCtx, &types.MsgUpdateItem{Creator: shield, Id: itemResp.Id, Title: "zenith two"})
	require.NoError(t, err)

	update := &types.MsgUpdateCollection{
		Creator:   shield,
		Id:        mine,
		Type:      types.CollectionType_COLLECTION_TYPE_MIXED,
		Name:      "phoenix renamed",
		ExpiresAt: expires,
	}
	_, err = f.msgServer.UpdateCollection(f.sdkCtx, update)
	require.ErrorIs(t, err, types.ErrUnauthorized, "no ownership proof")
	_, err = f.msgServer.UpdateCollection(ownCtx, update)
	require.NoError(t, err)

	// The anonymous owner can't change the lifetime or turn off feedback.
	update.ExpiresAt = 0
	_, err = f.msgServer.UpdateCollection(ownCtx, update)
	require.ErrorIs(t, err, types.ErrAnonymousFixedField)
	update.ExpiresAt = expires + 50
	_, err = f.msgServer.UpdateCollection(ownCtx, update)
	require.ErrorIs(t, err, types.ErrAnonymousFixedField)
	update.ExpiresAt = expires
	update.UpdateCommunityFeedback = true
	update.CommunityFeedbackEnabled = false
	_, err = f.msgServer.UpdateCollection(ownCtx, update)
	require.ErrorIs(t, err, types.ErrAnonymousFixedField)

	_, err = f.msgServer.RemoveItem(ownCtx, &types.MsgRemoveItem{Creator: shield, Id: itemResp.Id})
	require.NoError(t, err)

	_, err = f.msgServer.DeleteCollection(shieldtypes.WithOwnershipTag(f.sdkCtx, otherColl.AnonOwnerTag), &types.MsgDeleteCollection{Creator: shield, Id: mine})
	require.ErrorIs(t, err, types.ErrUnauthorized)
	_, err = f.msgServer.DeleteCollection(ownCtx, &types.MsgDeleteCollection{Creator: shield, Id: mine})
	require.NoError(t, err)
	_, err = f.keeper.Collection.Get(f.ctx, mine)
	require.Error(t, err)
}

// x/shield resolves which owner claim a management message acts under and
// advances that claim's sequence after accepting a proof.
func TestResolveAndAdvanceOwnership(t *testing.T) {
	f := initTestFixture(t)
	shield := authtypes.NewModuleAddress("shield").String()
	expires := f.sdkCtx.BlockHeight() + 100
	a := createAnonCollection(t, f, 1, expires)
	b := createAnonCollection(t, f, 2, expires)

	aColl, err := f.keeper.Collection.Get(f.ctx, a)
	require.NoError(t, err)
	bColl, err := f.keeper.Collection.Get(f.ctx, b)
	require.NoError(t, err)
	ownA := ownerCtx(f, aColl.AnonOwnerTag)
	ownB := ownerCtx(f, bColl.AnonOwnerTag)
	itemA, err := f.msgServer.AddItem(ownA, &types.MsgAddItem{Creator: shield, CollectionId: a, Title: "phoenix"})
	require.NoError(t, err)
	itemB, err := f.msgServer.AddItem(ownB, &types.MsgAddItem{Creator: shield, CollectionId: b, Title: "aurora"})
	require.NoError(t, err)

	for _, msg := range []sdk.Msg{
		&types.MsgUpdateCollection{Creator: shield, Id: a},
		&types.MsgDeleteCollection{Creator: shield, Id: a},
		&types.MsgAddItem{Creator: shield, CollectionId: a},
		&types.MsgAddItems{Creator: shield, CollectionId: a},
		&types.MsgUpdateItem{Creator: shield, Id: itemA.Id},
		&types.MsgRemoveItem{Creator: shield, Id: itemA.Id},
		&types.MsgReorderItem{Creator: shield, Id: itemA.Id},
		&types.MsgRemoveItems{Creator: shield, Ids: []uint64{itemA.Id}},
	} {
		claim, err := f.keeper.ResolveOwnership(f.ctx, msg)
		require.NoError(t, err, "%T", msg)
		require.Equal(t, aColl.AnonOwnerTag, claim.Tag, "%T", msg)
		require.Equal(t, aColl.AnonOwnerDomain, claim.Domain)
		require.Equal(t, aColl.AnonOwnerScope, claim.Scope)
	}

	// Items from two collections can't be removed under one claim.
	_, err = f.keeper.ResolveOwnership(f.ctx, &types.MsgRemoveItems{Creator: shield, Ids: []uint64{itemA.Id, itemB.Id}})
	require.ErrorIs(t, err, types.ErrUnauthorized)

	// An identified member's collection has no anonymous owner claim.
	identified := f.createCollection(t, f.owner)
	_, err = f.keeper.ResolveOwnership(f.ctx, &types.MsgUpdateCollection{Creator: shield, Id: identified})
	require.ErrorIs(t, err, types.ErrAnonymousNoOwnerClaim)

	// Advancing bumps the sequence the next proof must bind.
	before, err := f.keeper.ResolveOwnership(f.ctx, &types.MsgAddItem{Creator: shield, CollectionId: a})
	require.NoError(t, err)
	require.NoError(t, f.keeper.AdvanceOwnership(f.ctx, &types.MsgAddItem{Creator: shield, CollectionId: a}))
	after, err := f.keeper.ResolveOwnership(f.ctx, &types.MsgAddItem{Creator: shield, CollectionId: a})
	require.NoError(t, err)
	require.Equal(t, before.Sequence+1, after.Sequence)
}

// x/shield advances the owner sequence and runs the management action in the
// same tx. If the action fails the tx rolls back, so the sequence the proof
// bound stays current and the owner can retry with it; a successful action
// retires it by advancing the sequence exactly once.
func TestAnonymousCollection_FailedActionKeepsOwnerSequence(t *testing.T) {
	f := initTestFixture(t)
	shield := authtypes.NewModuleAddress("shield").String()
	expires := f.sdkCtx.BlockHeight() + 100
	id := createAnonCollection(t, f, 5, expires)

	coll, err := f.keeper.Collection.Get(f.ctx, id)
	require.NoError(t, err)
	startSeq := coll.AnonOwnerSequence

	// runOwned mimics x/shield's ownership-mode exec in a tx-scoped cache:
	// advance the claim, then run the inner action under the owner tag. The
	// cache is written only when the whole exec succeeds.
	runOwned := func(update *types.MsgUpdateCollection) error {
		cacheCtx, write := f.sdkCtx.CacheContext()
		if err := f.keeper.AdvanceOwnership(cacheCtx, update); err != nil {
			return err
		}
		inCache, err := f.keeper.ResolveOwnership(cacheCtx, update)
		require.NoError(t, err)
		require.Equal(t, startSeq+1, inCache.Sequence, "advance is visible inside the tx")
		if _, err := f.msgServer.UpdateCollection(ownerCtx(f, coll.AnonOwnerTag).WithMultiStore(cacheCtx.MultiStore()), update); err != nil {
			return err
		}
		write()
		return nil
	}

	// An anonymous owner can't change the lifetime: the action fails.
	bad := &types.MsgUpdateCollection{
		Creator:   shield,
		Id:        id,
		Type:      types.CollectionType_COLLECTION_TYPE_MIXED,
		Name:      "phoenix renamed",
		ExpiresAt: expires + 50,
	}
	require.ErrorIs(t, runOwned(bad), types.ErrAnonymousFixedField)

	claim, err := f.keeper.ResolveOwnership(f.ctx, bad)
	require.NoError(t, err)
	require.Equal(t, startSeq, claim.Sequence, "rolled-back action must not consume the sequence")

	// Retrying under the same sequence with a valid action succeeds and
	// advances the sequence by exactly one.
	good := &types.MsgUpdateCollection{
		Creator:   shield,
		Id:        id,
		Type:      types.CollectionType_COLLECTION_TYPE_MIXED,
		Name:      "phoenix renamed",
		ExpiresAt: expires,
	}
	require.NoError(t, runOwned(good))

	claim, err = f.keeper.ResolveOwnership(f.ctx, good)
	require.NoError(t, err)
	require.Equal(t, startSeq+1, claim.Sequence)
	coll, err = f.keeper.Collection.Get(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, "phoenix renamed", coll.Name)
	require.Equal(t, startSeq+1, coll.AnonOwnerSequence)
}

// bankMoves records every non-empty SPARK movement through the mock bank
// keeper so tests can assert that a flow moved nothing.
type bankMoves struct {
	calls []string
}

func trackBank(f *testFixture) *bankMoves {
	m := &bankMoves{}
	f.bankKeeper.sendCoinsFromAccountToModuleFn = func(_ context.Context, from sdk.AccAddress, _ string, amt sdk.Coins) error {
		if !amt.IsZero() {
			m.calls = append(m.calls, "escrow "+amt.String()+" from "+from.String())
		}
		return nil
	}
	f.bankKeeper.sendCoinsFromModuleToAccountFn = func(_ context.Context, _ string, to sdk.AccAddress, amt sdk.Coins) error {
		if !amt.IsZero() {
			m.calls = append(m.calls, "refund "+amt.String()+" to "+to.String())
		}
		return nil
	}
	f.bankKeeper.burnCoinsFn = func(_ context.Context, _ string, amt sdk.Coins) error {
		if !amt.IsZero() {
			m.calls = append(m.calls, "burn "+amt.String())
		}
		return nil
	}
	return m
}

// Anonymous collections are signed by the shield module account, whose
// balance is the communal gas reserve: no step of their lifecycle charges,
// refunds or burns SPARK, and no deposit is ever recorded.
func TestAnonymousCollection_NoSPARKMoves(t *testing.T) {
	shield := authtypes.NewModuleAddress("shield").String()

	tests := []struct {
		name string
		run  func(t *testing.T, f *testFixture, collID uint64, ownCtx sdk.Context)
	}{
		{
			name: "create",
			run:  func(*testing.T, *testFixture, uint64, sdk.Context) {},
		},
		{
			name: "add item then remove it",
			run: func(t *testing.T, f *testFixture, collID uint64, ownCtx sdk.Context) {
				resp, err := f.msgServer.AddItem(ownCtx, &types.MsgAddItem{Creator: shield, CollectionId: collID, Title: "zenith"})
				require.NoError(t, err)
				requireNoDeposit(t, f, collID, 1)
				_, err = f.msgServer.RemoveItem(ownCtx, &types.MsgRemoveItem{Creator: shield, Id: resp.Id})
				require.NoError(t, err)
			},
		},
		{
			name: "add items then remove them",
			run: func(t *testing.T, f *testFixture, collID uint64, ownCtx sdk.Context) {
				resp, err := f.msgServer.AddItems(ownCtx, &types.MsgAddItems{
					Creator:      shield,
					CollectionId: collID,
					Items:        []types.AddItemEntry{{Title: "aurora"}, {Title: "zenith"}},
				})
				require.NoError(t, err)
				requireNoDeposit(t, f, collID, 2)
				_, err = f.msgServer.RemoveItems(ownCtx, &types.MsgRemoveItems{Creator: shield, Ids: resp.Ids})
				require.NoError(t, err)
			},
		},
		{
			name: "made permanent by a member",
			run: func(t *testing.T, f *testFixture, collID uint64, ownCtx sdk.Context) {
				_, err := f.msgServer.AddItem(ownCtx, &types.MsgAddItem{Creator: shield, CollectionId: collID, Title: "zenith"})
				require.NoError(t, err)
				_, err = f.msgServer.MakeCollectionPermanent(f.ctx, &types.MsgMakeCollectionPermanent{Creator: f.owner, CollectionId: collID})
				require.NoError(t, err)
				coll, err := f.keeper.Collection.Get(f.ctx, collID)
				require.NoError(t, err)
				require.Zero(t, coll.ExpiresAt)
			},
		},
		{
			name: "deleted by its owner",
			run: func(t *testing.T, f *testFixture, collID uint64, ownCtx sdk.Context) {
				_, err := f.msgServer.AddItem(ownCtx, &types.MsgAddItem{Creator: shield, CollectionId: collID, Title: "zenith"})
				require.NoError(t, err)
				_, err = f.msgServer.DeleteCollection(ownCtx, &types.MsgDeleteCollection{Creator: shield, Id: collID})
				require.NoError(t, err)
			},
		},
		{
			name: "expires",
			run: func(t *testing.T, f *testFixture, collID uint64, ownCtx sdk.Context) {
				_, err := f.msgServer.AddItem(ownCtx, &types.MsgAddItem{Creator: shield, CollectionId: collID, Title: "zenith"})
				require.NoError(t, err)
				coll, err := f.keeper.Collection.Get(f.ctx, collID)
				require.NoError(t, err)
				f.setBlockHeight(coll.ExpiresAt + 1)
				require.NoError(t, f.keeper.PruneExpired(f.ctx))
				_, err = f.keeper.Collection.Get(f.ctx, collID)
				require.Error(t, err, "expired anonymous collection is pruned")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			moves := trackBank(f)

			collID := createAnonCollection(t, f, 1, f.sdkCtx.BlockHeight()+100)
			requireNoDeposit(t, f, collID, 0)
			coll, err := f.keeper.Collection.Get(f.ctx, collID)
			require.NoError(t, err)
			require.False(t, coll.DepositBurned)

			tc.run(t, f, collID, ownerCtx(f, coll.AnonOwnerTag))
			require.Empty(t, moves.calls, "anonymous collection lifecycle must move no SPARK")
		})
	}
}

// requireNoDeposit asserts the collection records no held deposit.
func requireNoDeposit(t *testing.T, f *testFixture, collID uint64, itemCount uint64) {
	t.Helper()
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.True(t, coll.DepositAmount.IsZero(), "deposit amount %s", coll.DepositAmount)
	require.True(t, coll.ItemDepositTotal.IsZero(), "item deposit total %s", coll.ItemDepositTotal)
	require.Equal(t, itemCount, coll.ItemCount)
}

// An identified member's TTL collection still escrows the base and per-item
// deposits and refunds exactly what it holds.
func TestIdentifiedCollection_DepositsUnchanged(t *testing.T) {
	f := initTestFixture(t)
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)

	moves := trackBank(f)
	collID := f.createTTLCollection(t, f.owner, f.sdkCtx.BlockHeight()+100)
	itemID := f.addItem(t, collID, f.owner)

	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, params.BaseCollectionDeposit, coll.DepositAmount)
	require.Equal(t, params.PerItemDeposit, coll.ItemDepositTotal)
	require.Len(t, moves.calls, 2, "base deposit and item deposit escrowed")

	_, err = f.msgServer.RemoveItem(f.ctx, &types.MsgRemoveItem{Creator: f.owner, Id: itemID})
	require.NoError(t, err)
	require.Len(t, moves.calls, 3, "item deposit refunded")
	coll, err = f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.True(t, coll.ItemDepositTotal.IsZero())
}
