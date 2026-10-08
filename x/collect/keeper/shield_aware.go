package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
	shieldtypes "sparkdream/x/shield/types"
)

// IsShieldCompatible implements the x/shield ShieldAware interface.
// Returns true if the message type is designed for anonymous execution
// through the shield module.
func (k Keeper) IsShieldCompatible(_ context.Context, msg sdk.Msg) bool {
	switch msg.(type) {
	case *types.MsgCreateCollection,
		*types.MsgUpvoteContent,
		*types.MsgDownvoteContent:
		return true
	case *types.MsgUpdateCollection,
		*types.MsgDeleteCollection,
		*types.MsgAddItem,
		*types.MsgAddItems,
		*types.MsgUpdateItem,
		*types.MsgRemoveItem,
		*types.MsgRemoveItems,
		*types.MsgReorderItem:
		// Managing an anonymous collection: x/shield runs these in ownership
		// mode, proving the anonymous creator against the collection's tag.
		return true
	default:
		return false
	}
}

// ResolveOwnership implements shieldtypes.ShieldOwnershipResolver: the owner
// claim of the anonymous collection msg manages.
func (k Keeper) ResolveOwnership(ctx context.Context, msg sdk.Msg) (shieldtypes.OwnershipClaim, error) {
	coll, err := k.ownedCollection(ctx, msg)
	if err != nil {
		return shieldtypes.OwnershipClaim{}, err
	}
	return shieldtypes.OwnershipClaim{
		Domain:   coll.AnonOwnerDomain,
		Scope:    coll.AnonOwnerScope,
		Tag:      coll.AnonOwnerTag,
		Sequence: coll.AnonOwnerSequence,
	}, nil
}

// AdvanceOwnership implements shieldtypes.ShieldOwnershipResolver: it bumps the
// collection's owner sequence, retiring the proof x/shield just accepted.
func (k Keeper) AdvanceOwnership(ctx context.Context, msg sdk.Msg) error {
	coll, err := k.ownedCollection(ctx, msg)
	if err != nil {
		return err
	}
	coll.AnonOwnerSequence++
	return k.Collection.Set(ctx, coll.Id, coll)
}

// ownedCollection returns the anonymously owned collection a management
// message targets.
func (k Keeper) ownedCollection(ctx context.Context, msg sdk.Msg) (types.Collection, error) {
	var collID uint64
	switch m := msg.(type) {
	case *types.MsgUpdateCollection:
		collID = m.Id
	case *types.MsgDeleteCollection:
		collID = m.Id
	case *types.MsgAddItem:
		collID = m.CollectionId
	case *types.MsgAddItems:
		collID = m.CollectionId
	case *types.MsgUpdateItem:
		return k.ownedCollectionOfItems(ctx, []uint64{m.Id})
	case *types.MsgRemoveItem:
		return k.ownedCollectionOfItems(ctx, []uint64{m.Id})
	case *types.MsgReorderItem:
		return k.ownedCollectionOfItems(ctx, []uint64{m.Id})
	case *types.MsgRemoveItems:
		return k.ownedCollectionOfItems(ctx, m.Ids)
	default:
		return types.Collection{}, fmt.Errorf("%T does not manage a collection", msg)
	}
	return k.anonOwnedCollection(ctx, collID)
}

// ownedCollectionOfItems resolves the single collection that all ids belong to.
func (k Keeper) ownedCollectionOfItems(ctx context.Context, ids []uint64) (types.Collection, error) {
	if len(ids) == 0 {
		return types.Collection{}, types.ErrItemNotFound
	}
	var collID uint64
	for i, id := range ids {
		item, err := k.Item.Get(ctx, id)
		if errors.Is(err, collections.ErrNotFound) {
			return types.Collection{}, errorsmod.Wrapf(types.ErrItemNotFound, "item %d", id)
		}
		if err != nil {
			return types.Collection{}, err
		}
		if i > 0 && item.CollectionId != collID {
			return types.Collection{}, errorsmod.Wrap(types.ErrUnauthorized, "items belong to different collections")
		}
		collID = item.CollectionId
	}
	return k.anonOwnedCollection(ctx, collID)
}

// anonOwnedCollection loads a collection and requires an anonymous owner claim.
func (k Keeper) anonOwnedCollection(ctx context.Context, collID uint64) (types.Collection, error) {
	coll, err := k.Collection.Get(ctx, collID)
	if errors.Is(err, collections.ErrNotFound) {
		return types.Collection{}, types.ErrCollectionNotFound
	}
	if err != nil {
		return types.Collection{}, err
	}
	if !k.isAnonymous(coll.Owner) || len(coll.AnonOwnerTag) == 0 {
		return types.Collection{}, types.ErrAnonymousNoOwnerClaim
	}
	return coll, nil
}

var _ shieldtypes.ShieldOwnershipResolver = Keeper{}
