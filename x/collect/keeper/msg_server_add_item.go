package keeper

import (
	"context"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

func (k msgServer) AddItem(ctx context.Context, msg *types.MsgAddItem) (*types.MsgAddItemResponse, error) {
	creatorAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()

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

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Check max_items_per_collection
	if coll.ItemCount >= uint64(params.MaxItemsPerCollection) {
		return nil, types.ErrMaxItems
	}

	// Validate item fields - convert []*KeyValuePair to []KeyValuePair for validation
	attrs := make([]types.KeyValuePair, len(msg.Attributes))
	for i, a := range msg.Attributes {
		if a != nil {
			attrs[i] = *a
		}
	}
	if err := k.validateItemFields(coll.Encrypted, msg.Title, msg.Description, msg.ImageUri,
		msg.ReferenceType, msg.Nft, msg.Link, msg.OnChain, msg.Custom, attrs, msg.EncryptedData, params); err != nil {
		return nil, err
	}

	// Determine position
	position := msg.Position
	if position >= coll.ItemCount {
		// Append at end
		position = coll.ItemCount
	} else {
		// Insert at position: shift items right
		if err := k.InsertAtPosition(ctx, coll.Id, position, 0); err != nil {
			return nil, errorsmod.Wrap(err, "failed to insert at position")
		}
	}

	// Fee handling
	depositAmt := params.PerItemDeposit
	if coll.ExpiresAt > 0 && !coll.DepositBurned {
		// TTL collection: hold deposit
		if err := k.EscrowSPARK(ctx, creatorAddr, depositAmt); err != nil {
			return nil, errorsmod.Wrap(types.ErrInsufficientFunds, err.Error())
		}
	} else {
		// Permanent collection: burn deposit
		if err := k.BurnSPARKFromAccount(ctx, creatorAddr, depositAmt); err != nil {
			return nil, errorsmod.Wrap(types.ErrInsufficientFunds, err.Error())
		}
	}

	// Non-member creator pays per_item_spam_tax (burned)
	if !k.isMember(ctx, msg.Creator) && params.PerItemSpamTax.IsPositive() {
		if err := k.BurnSPARKFromAccount(ctx, creatorAddr, params.PerItemSpamTax); err != nil {
			return nil, errorsmod.Wrap(types.ErrInsufficientFunds, err.Error())
		}
	}

	// Get next item ID
	itemID, err := k.ItemSeq.Next(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get next item ID")
	}

	item := types.Item{
		Id:            itemID,
		CollectionId:  coll.Id,
		AddedBy:       msg.Creator,
		Title:         msg.Title,
		Description:   msg.Description,
		ImageUri:      msg.ImageUri,
		ReferenceType: msg.ReferenceType,
		Nft:           msg.Nft,
		Link:          msg.Link,
		OnChain:       msg.OnChain,
		Custom:        msg.Custom,
		Attributes:    msg.Attributes,
		EncryptedData: msg.EncryptedData,
		Position:      position,
		AddedAt:       blockHeight,
		Status:        types.ItemStatus_ITEM_STATUS_ACTIVE,
	}

	// Store item
	if err := k.Item.Set(ctx, itemID, item); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store item")
	}

	// Update collection
	coll.ItemCount++
	if coll.ExpiresAt > 0 && !coll.DepositBurned {
		coll.ItemDepositTotal = coll.ItemDepositTotal.Add(depositAmt)
	}
	coll.UpdatedAt = blockHeight
	if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update collection")
	}

	// Set indexes
	if err := k.ItemsByCollection.Set(ctx, collections.Join(coll.Id, itemID)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to set collection index")
	}
	if err := k.ItemsByOwner.Set(ctx, collections.Join(coll.Owner, itemID)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to set owner index")
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("item_added",
		sdk.NewAttribute("id", strconv.FormatUint(itemID, 10)),
		sdk.NewAttribute("collection_id", strconv.FormatUint(coll.Id, 10)),
		sdk.NewAttribute("added_by", msg.Creator),
		sdk.NewAttribute("position", strconv.FormatUint(position, 10)),
	))

	return &types.MsgAddItemResponse{Id: itemID}, nil
}
