package keeper

import (
	"context"
	"strconv"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

func (k msgServer) UpdateItem(ctx context.Context, msg *types.MsgUpdateItem) (*types.MsgUpdateItemResponse, error) {
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

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Validate item fields (no deposit, no spam tax)
	attrs := attrsToValues(msg.Attributes)
	if err := k.validateItemFields(coll.Encrypted, msg.Title, msg.Description, msg.ImageUri,
		msg.ReferenceType, msg.Nft, msg.Link, msg.OnChain, msg.Custom, attrs, msg.EncryptedData, params); err != nil {
		return nil, err
	}

	// Update item fields
	item.Title = msg.Title
	item.Description = msg.Description
	item.ImageUri = msg.ImageUri
	item.ReferenceType = msg.ReferenceType
	item.Nft = msg.Nft
	item.Link = msg.Link
	item.OnChain = msg.OnChain
	item.Custom = msg.Custom
	item.Attributes = msg.Attributes
	item.EncryptedData = msg.EncryptedData

	// Store updated item
	if err := k.Item.Set(ctx, item.Id, item); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update item")
	}

	// Update collection timestamp
	coll.UpdatedAt = blockHeight
	if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update collection")
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("item_updated",
		sdk.NewAttribute("id", strconv.FormatUint(item.Id, 10)),
		sdk.NewAttribute("collection_id", strconv.FormatUint(coll.Id, 10)),
		sdk.NewAttribute("updated_by", msg.Creator),
	))

	return &types.MsgUpdateItemResponse{}, nil
}
