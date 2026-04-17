package keeper

import (
	"context"
	"strconv"
	"strings"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

func (k msgServer) RemoveItems(ctx context.Context, msg *types.MsgRemoveItems) (*types.MsgRemoveItemsResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Batch size check
	batchSize := uint32(len(msg.Ids))
	if batchSize == 0 {
		return nil, errorsmod.Wrap(types.ErrBatchTooLarge, "empty batch")
	}
	if batchSize > params.MaxBatchSize {
		return nil, types.ErrBatchTooLarge
	}

	// Check for duplicate IDs
	seen := make(map[uint64]bool, batchSize)
	for _, id := range msg.Ids {
		if seen[id] {
			return nil, types.ErrDuplicateIDs
		}
		seen[id] = true
	}

	// All items must exist and belong to same collection
	items := make([]types.Item, 0, batchSize)
	var collectionID uint64
	for i, id := range msg.Ids {
		item, err := k.Item.Get(ctx, id)
		if err != nil {
			return nil, errorsmod.Wrapf(types.ErrItemNotFound, "item %d", id)
		}
		if i == 0 {
			collectionID = item.CollectionId
		} else if item.CollectionId != collectionID {
			return nil, types.ErrBatchItemsMixedCollections
		}
		items = append(items, item)
	}

	// Get collection
	coll, err := k.Collection.Get(ctx, collectionID)
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

	// If collaborator (not owner), must be member
	if coll.Owner != msg.Creator {
		if !k.isMember(ctx, msg.Creator) {
			return nil, types.ErrNotMember
		}
	}

	// No immutable
	if coll.Immutable {
		return nil, types.ErrCollectionImmutable
	}

	// No pending sponsorship
	_, err = k.SponsorshipRequest.Get(ctx, coll.Id)
	if err == nil {
		return nil, types.ErrItemsLockedForSponsorship
	}

	// Refund deposits if TTL
	if isTTLCollection(coll) {
		ownerAddr, err := k.addressCodec.StringToBytes(coll.Owner)
		if err != nil {
			return nil, errorsmod.Wrap(err, "invalid owner address")
		}
		totalRefund := params.PerItemDeposit.MulRaw(int64(batchSize))
		if err := k.RefundSPARK(ctx, ownerAddr, totalRefund); err != nil {
			return nil, errorsmod.Wrap(err, "failed to refund item deposits")
		}
		coll.ItemDepositTotal = coll.ItemDepositTotal.Sub(totalRefund)
		if coll.ItemDepositTotal.IsNegative() {
			coll.ItemDepositTotal = math.ZeroInt()
		}
	}

	// Remove all items
	for _, item := range items {
		k.Item.Remove(ctx, item.Id)                                         //nolint:errcheck
		k.ItemsByCollection.Remove(ctx, collections.Join(coll.Id, item.Id)) //nolint:errcheck
		k.ItemsByOwner.Remove(ctx, collections.Join(coll.Owner, item.Id))   //nolint:errcheck
		if item.ReferenceType == types.ReferenceType_REFERENCE_TYPE_ON_CHAIN && item.OnChain != nil {
			k.ItemsByOnChainRef.Remove(ctx, collections.Join(onChainRefKey(item.OnChain), item.Id)) //nolint:errcheck
		}
	}

	// Update collection (guard against unsigned underflow)
	if coll.ItemCount < uint64(batchSize) {
		coll.ItemCount = 0
	} else {
		coll.ItemCount -= uint64(batchSize)
	}
	coll.UpdatedAt = blockHeight
	if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update collection")
	}

	// Skip auto-compaction: positions are allowed to be sparse after removal.
	// Compaction can be triggered explicitly if sequential positions are needed.

	// Build IDs string for event
	idStrs := make([]string, len(msg.Ids))
	for i, id := range msg.Ids {
		idStrs[i] = strconv.FormatUint(id, 10)
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("items_removed",
		sdk.NewAttribute("collection_id", strconv.FormatUint(coll.Id, 10)),
		sdk.NewAttribute("removed_by", msg.Creator),
		sdk.NewAttribute("count", strconv.FormatUint(uint64(batchSize), 10)),
		sdk.NewAttribute("ids", strings.Join(idStrs, ",")),
	))

	return &types.MsgRemoveItemsResponse{}, nil
}
