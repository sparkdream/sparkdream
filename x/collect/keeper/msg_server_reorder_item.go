package keeper

import (
	"context"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

func (k msgServer) ReorderItem(ctx context.Context, msg *types.MsgReorderItem) (*types.MsgReorderItemResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Item must exist
	item, err := k.Item.Get(ctx, msg.Id)
	if err != nil {
		return nil, types.ErrItemNotFound
	}

	// Get parent collection
	coll, err := k.Collection.Get(ctx, item.CollectionId)
	if err != nil {
		return nil, types.ErrCollectionNotFound
	}

	// HasWriteAccess
	hasAccess, err := k.HasWriteAccess(ctx, coll, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to check write access")
	}
	if !hasAccess {
		return nil, types.ErrUnauthorized
	}

	// Collection must not be immutable
	if coll.Immutable {
		return nil, types.ErrCollectionImmutable
	}

	// new_position must be in range [0, item_count-1]
	if coll.ItemCount == 0 {
		return nil, types.ErrInvalidPosition
	}
	maxPos := coll.ItemCount - 1
	if msg.NewPosition > maxPos {
		return nil, types.ErrInvalidPosition
	}

	oldPosition := item.Position
	newPosition := msg.NewPosition

	if oldPosition == newPosition {
		// No-op
		return &types.MsgReorderItemResponse{}, nil
	}

	// Collect all items in this collection with their positions
	type posEntry struct {
		id       uint64
		position uint64
	}
	var entries []posEntry

	err = k.ItemsByCollection.Walk(ctx,
		collections.NewPrefixedPairRange[uint64, uint64](coll.Id),
		func(key collections.Pair[uint64, uint64]) (bool, error) {
			itemID := key.K2()
			it, err := k.Item.Get(ctx, itemID)
			if err != nil {
				return true, err
			}
			entries = append(entries, posEntry{id: itemID, position: it.Position})
			return false, nil
		},
	)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to walk items")
	}

	// Shift items between old and new position
	for _, e := range entries {
		if e.id == item.Id {
			continue // We'll set this one to newPosition below
		}
		var newPos uint64
		var shouldUpdate bool
		if oldPosition < newPosition {
			// Moving forward: shift items in (oldPosition, newPosition] left by 1
			if e.position > oldPosition && e.position <= newPosition {
				newPos = e.position - 1
				shouldUpdate = true
			}
		} else {
			// Moving backward: shift items in [newPosition, oldPosition) right by 1
			if e.position >= newPosition && e.position < oldPosition {
				newPos = e.position + 1
				shouldUpdate = true
			}
		}
		if shouldUpdate {
			it, err := k.Item.Get(ctx, e.id)
			if err != nil {
				return nil, errorsmod.Wrap(err, "failed to get item for reorder")
			}
			it.Position = newPos
			if err := k.Item.Set(ctx, e.id, it); err != nil {
				return nil, errorsmod.Wrap(err, "failed to update item position")
			}
		}
	}

	// Set the moved item to newPosition
	item.Position = newPosition
	if err := k.Item.Set(ctx, item.Id, item); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update moved item")
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("item_reordered",
		sdk.NewAttribute("id", strconv.FormatUint(item.Id, 10)),
		sdk.NewAttribute("collection_id", strconv.FormatUint(coll.Id, 10)),
		sdk.NewAttribute("old_position", strconv.FormatUint(oldPosition, 10)),
		sdk.NewAttribute("new_position", strconv.FormatUint(newPosition, 10)),
	))

	return &types.MsgReorderItemResponse{}, nil
}
