package keeper

import (
	"context"
	"strconv"
	"strings"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

func (k msgServer) AddItems(ctx context.Context, msg *types.MsgAddItems) (*types.MsgAddItemsResponse, error) {
	creatorAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Batch size check
	batchSize := uint32(len(msg.Items))
	if batchSize == 0 {
		return nil, errorsmod.Wrap(types.ErrBatchTooLarge, "empty batch")
	}
	if batchSize > params.MaxBatchSize {
		return nil, types.ErrBatchTooLarge
	}

	// Get collection
	coll, err := k.Collection.Get(ctx, msg.CollectionId)
	if err != nil {
		return nil, types.ErrCollectionNotFound
	}

	// HasWriteAccess check
	hasAccess, err := k.HasWriteAccess(ctx, coll, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to check write access")
	}
	if !hasAccess {
		return nil, types.ErrUnauthorized
	}

	// If collaborator (not owner), must be x/rep member
	if coll.Owner != msg.Creator {
		if !k.isMember(ctx, msg.Creator) {
			return nil, types.ErrNotMember
		}
	}

	// Collection must not be immutable
	if coll.Immutable {
		return nil, types.ErrCollectionImmutable
	}

	// Collection must not have pending sponsorship
	_, err = k.SponsorshipRequest.Get(ctx, coll.Id)
	if err == nil {
		return nil, types.ErrItemsLockedForSponsorship
	}

	// Total items after add <= max_items_per_collection
	newTotal := coll.ItemCount + uint64(batchSize)
	if newTotal > uint64(params.MaxItemsPerCollection) {
		return nil, types.ErrMaxItems
	}

	// Validate all items before any state changes
	for i := range msg.Items {
		entry := &msg.Items[i]
		attrs := attrsToValues(entry.Attributes)
		if err := k.validateItemFields(coll.Encrypted, entry.Title, entry.Description, entry.ImageUri,
			entry.ReferenceType, entry.Nft, entry.Link, entry.OnChain, entry.Custom, attrs, entry.EncryptedData, params); err != nil {
			return nil, errorsmod.Wrapf(err, "item[%d]", i)
		}
		if entry.ReferenceType == types.ReferenceType_REFERENCE_TYPE_ON_CHAIN && entry.OnChain != nil {
			if err := k.validateOnChainReference(ctx, entry.OnChain); err != nil {
				return nil, errorsmod.Wrapf(err, "item[%d]", i)
			}
		}
	}

	// Single deposit transfer for all items
	depositPerItem := params.PerItemDeposit
	totalDeposit := depositPerItem.MulRaw(int64(batchSize))
	isMemberCreator := k.isMember(ctx, msg.Creator)

	if isTTLCollection(coll) {
		// TTL: escrow total deposit
		if err := k.EscrowSPARK(ctx, creatorAddr, totalDeposit); err != nil {
			return nil, errorsmod.Wrap(types.ErrInsufficientFunds, err.Error())
		}
	} else {
		// Permanent: burn total deposit
		if err := k.BurnSPARKFromAccount(ctx, creatorAddr, totalDeposit); err != nil {
			return nil, errorsmod.Wrap(types.ErrInsufficientFunds, err.Error())
		}
	}

	// Non-member creator pays per_item_spam_tax for all items (burned)
	if !isMemberCreator && params.PerItemSpamTax.IsPositive() {
		totalSpamTax := params.PerItemSpamTax.MulRaw(int64(batchSize))
		if err := k.BurnSPARKFromAccount(ctx, creatorAddr, totalSpamTax); err != nil {
			return nil, errorsmod.Wrap(types.ErrInsufficientFunds, err.Error())
		}
	}

	// Create items: all appended at end
	ids := make([]uint64, 0, batchSize)
	for i := range msg.Items {
		entry := &msg.Items[i]
		position := coll.ItemCount + uint64(i)

		itemID, err := k.ItemSeq.Next(ctx)
		if err != nil {
			return nil, errorsmod.Wrap(err, "failed to get next item ID")
		}

		item := types.Item{
			Id:            itemID,
			CollectionId:  coll.Id,
			AddedBy:       msg.Creator,
			Title:         entry.Title,
			Description:   entry.Description,
			ImageUri:      entry.ImageUri,
			ReferenceType: entry.ReferenceType,
			Nft:           entry.Nft,
			Link:          entry.Link,
			OnChain:       entry.OnChain,
			Custom:        entry.Custom,
			Attributes:    entry.Attributes,
			EncryptedData: entry.EncryptedData,
			Position:      position,
			AddedAt:       blockHeight,
			Status:        types.ItemStatus_ITEM_STATUS_ACTIVE,
		}

		if err := k.Item.Set(ctx, itemID, item); err != nil {
			return nil, errorsmod.Wrap(err, "failed to store item")
		}

		if err := k.ItemsByCollection.Set(ctx, collections.Join(coll.Id, itemID)); err != nil {
			return nil, errorsmod.Wrap(err, "failed to set collection index")
		}
		if err := k.ItemsByOwner.Set(ctx, collections.Join(coll.Owner, itemID)); err != nil {
			return nil, errorsmod.Wrap(err, "failed to set owner index")
		}
		if item.ReferenceType == types.ReferenceType_REFERENCE_TYPE_ON_CHAIN && item.OnChain != nil {
			if err := k.ItemsByOnChainRef.Set(ctx, collections.Join(onChainRefKey(item.OnChain), itemID)); err != nil {
				return nil, errorsmod.Wrap(err, "failed to set on-chain ref index")
			}
		}

		ids = append(ids, itemID)
	}

	// Update collection
	coll.ItemCount = newTotal
	if isTTLCollection(coll) {
		coll.ItemDepositTotal = coll.ItemDepositTotal.Add(totalDeposit)
	}
	coll.UpdatedAt = blockHeight
	if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update collection")
	}

	// Build IDs string for event
	idStrs := make([]string, len(ids))
	for i, id := range ids {
		idStrs[i] = strconv.FormatUint(id, 10)
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("items_added",
		sdk.NewAttribute("collection_id", strconv.FormatUint(coll.Id, 10)),
		sdk.NewAttribute("added_by", msg.Creator),
		sdk.NewAttribute("count", strconv.FormatUint(uint64(batchSize), 10)),
		sdk.NewAttribute("ids", strings.Join(idStrs, ",")),
	))

	return &types.MsgAddItemsResponse{Ids: ids}, nil
}
