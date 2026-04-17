package keeper

import (
	"context"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

func (k msgServer) RemoveItem(ctx context.Context, msg *types.MsgRemoveItem) (*types.MsgRemoveItemResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()

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

	// HasWriteAccess on parent collection
	hasAccess, err := k.HasWriteAccess(ctx, coll, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to check write access")
	}
	if !hasAccess {
		return nil, types.ErrUnauthorized
	}

	// If collaborator (not owner), must be member
	if coll.Owner != msg.Creator {
		if !k.isMember(ctx, msg.Creator) {
			return nil, types.ErrNotMember
		}
	}

	// Collection must not be immutable
	if coll.Immutable {
		return nil, types.ErrCollectionImmutable
	}

	// No pending sponsorship
	_, err = k.SponsorshipRequest.Get(ctx, coll.Id)
	if err == nil {
		return nil, types.ErrItemsLockedForSponsorship
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Refund per_item_deposit to owner if TTL
	if isTTLCollection(coll) {
		ownerAddr, err := k.addressCodec.StringToBytes(coll.Owner)
		if err != nil {
			return nil, errorsmod.Wrap(err, "invalid owner address")
		}
		if err := k.RefundSPARK(ctx, ownerAddr, params.PerItemDeposit); err != nil {
			return nil, errorsmod.Wrap(err, "failed to refund item deposit")
		}
		coll.ItemDepositTotal = coll.ItemDepositTotal.Sub(params.PerItemDeposit)
		if coll.ItemDepositTotal.IsNegative() {
			coll.ItemDepositTotal = math.ZeroInt()
		}
	}

	// Remove item from storage and indexes
	if err := k.Item.Remove(ctx, item.Id); err != nil {
		return nil, errorsmod.Wrap(err, "failed to remove item")
	}
	k.ItemsByCollection.Remove(ctx, collections.Join(coll.Id, item.Id)) //nolint:errcheck
	k.ItemsByOwner.Remove(ctx, collections.Join(coll.Owner, item.Id))   //nolint:errcheck
	if item.ReferenceType == types.ReferenceType_REFERENCE_TYPE_ON_CHAIN && item.OnChain != nil {
		k.ItemsByOnChainRef.Remove(ctx, collections.Join(onChainRefKey(item.OnChain), item.Id)) //nolint:errcheck
	}

	// Update collection
	coll.ItemCount--
	coll.UpdatedAt = blockHeight
	if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update collection")
	}

	// Positions are allowed to be sparse after removal (no auto-compaction).

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("item_removed",
		sdk.NewAttribute("id", strconv.FormatUint(item.Id, 10)),
		sdk.NewAttribute("collection_id", strconv.FormatUint(coll.Id, 10)),
		sdk.NewAttribute("removed_by", msg.Creator),
	))

	return &types.MsgRemoveItemResponse{}, nil
}
