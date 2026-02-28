package keeper

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/collect/types"
)

func (k msgServer) ManageAnonymousCollection(ctx context.Context, msg *types.MsgManageAnonymousCollection) (*types.MsgManageAnonymousCollectionResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Submitter); err != nil {
		return nil, errorsmod.Wrap(err, "invalid submitter address")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()

	// Get collection
	coll, err := k.Collection.Get(ctx, msg.CollectionId)
	if err != nil {
		return nil, types.ErrCollectionNotFound
	}

	// Verify collection is anonymous (owner == module account)
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName).String()
	if coll.Owner != moduleAddr {
		return nil, types.ErrNotAnonymousCollection
	}

	// Collection must be active
	if coll.Status != types.CollectionStatus_COLLECTION_STATUS_ACTIVE {
		return nil, types.ErrNotPublicActive
	}

	// Collection not expired
	if coll.ExpiresAt > 0 && coll.ExpiresAt <= blockHeight {
		return nil, types.ErrCollectionExpired
	}

	// Get anonymous metadata
	meta, ok := k.GetAnonymousCollectionMeta(ctx, msg.CollectionId)
	if !ok {
		return nil, types.ErrNotAnonymousCollection
	}

	// Verify nonce is strictly increasing
	if msg.Nonce <= meta.Nonce {
		return nil, errorsmod.Wrapf(types.ErrInvalidNonce, "nonce %d <= last %d", msg.Nonce, meta.Nonce)
	}

	// Verify Ed25519 signature over canonical payload
	payload := buildManagementPayload(msg)
	if !ed25519.Verify(ed25519.PublicKey(meta.ManagementPublicKey), payload, msg.ManagementSignature) {
		return nil, types.ErrInvalidManagementSignature
	}

	// Update nonce
	meta.Nonce = msg.Nonce
	k.SetAnonymousCollectionMeta(ctx, msg.CollectionId, meta)

	// Dispatch by action
	switch msg.Action {
	case types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEM:
		return k.handleAnonAddItem(ctx, sdkCtx, msg, coll, params, blockHeight)

	case types.AnonymousManageAction_ANON_MANAGE_ACTION_ADD_ITEMS:
		return k.handleAnonAddItems(ctx, sdkCtx, msg, coll, params, blockHeight)

	case types.AnonymousManageAction_ANON_MANAGE_ACTION_REMOVE_ITEM:
		return k.handleAnonRemoveItem(ctx, sdkCtx, msg, coll, params, blockHeight)

	case types.AnonymousManageAction_ANON_MANAGE_ACTION_REMOVE_ITEMS:
		return k.handleAnonRemoveItems(ctx, sdkCtx, msg, coll, params, blockHeight)

	case types.AnonymousManageAction_ANON_MANAGE_ACTION_UPDATE_ITEM:
		return k.handleAnonUpdateItem(ctx, sdkCtx, msg, coll, params, blockHeight)

	case types.AnonymousManageAction_ANON_MANAGE_ACTION_REORDER_ITEM:
		return k.handleAnonReorderItem(ctx, sdkCtx, msg, coll, blockHeight)

	case types.AnonymousManageAction_ANON_MANAGE_ACTION_UPDATE_METADATA:
		return k.handleAnonUpdateMetadata(ctx, sdkCtx, msg, coll, params, blockHeight)

	default:
		return nil, errorsmod.Wrap(types.ErrUnauthorized, "unknown management action")
	}
}

func (k msgServer) handleAnonAddItem(ctx context.Context, sdkCtx sdk.Context, msg *types.MsgManageAnonymousCollection, coll types.Collection, params types.Params, blockHeight int64) (*types.MsgManageAnonymousCollectionResponse, error) {
	if len(msg.Items) == 0 {
		return nil, errorsmod.Wrap(types.ErrBatchTooLarge, "no items provided")
	}
	entry := msg.Items[0]
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName).String()

	if coll.ItemCount >= uint64(params.MaxItemsPerCollection) {
		return nil, types.ErrMaxItems
	}

	// Escrow per-item deposit from submitter (held in module account, burned on pin/expiry)
	submitterAddr, _ := k.addressCodec.StringToBytes(msg.Submitter)
	if params.PerItemDeposit.IsPositive() {
		if err := k.EscrowSPARK(ctx, submitterAddr, params.PerItemDeposit); err != nil {
			return nil, errorsmod.Wrap(types.ErrInsufficientFunds, err.Error())
		}
	}

	itemID, err := k.ItemSeq.Next(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get next item ID")
	}

	item := types.Item{
		Id:            itemID,
		CollectionId:  msg.CollectionId,
		Position:      coll.ItemCount,
		Title:         entry.Title,
		Description:   entry.Description,
		ImageUri:      entry.ImageUri,
		ReferenceType: entry.ReferenceType,
		Nft:           entry.Nft,
		Link:          entry.Link,
		OnChain:       entry.OnChain,
		Custom:        entry.Custom,
		Attributes:    entry.Attributes,
		AddedBy:       moduleAddr,
		AddedAt:       blockHeight,
		Status:        types.ItemStatus_ITEM_STATUS_ACTIVE,
	}

	if err := k.Item.Set(ctx, itemID, item); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store item")
	}

	k.ItemsByCollection.Set(ctx, collections.Join(msg.CollectionId, itemID)) //nolint:errcheck
	k.ItemsByOwner.Set(ctx, collections.Join(moduleAddr, itemID))            //nolint:errcheck

	coll.ItemCount++
	coll.ItemDepositTotal = coll.ItemDepositTotal.Add(params.PerItemDeposit)
	coll.UpdatedAt = blockHeight
	k.Collection.Set(ctx, coll.Id, coll) //nolint:errcheck

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("anonymous_item_added",
		sdk.NewAttribute("collection_id", strconv.FormatUint(msg.CollectionId, 10)),
		sdk.NewAttribute("item_id", strconv.FormatUint(itemID, 10)),
	))

	return &types.MsgManageAnonymousCollectionResponse{ItemIds: []uint64{itemID}}, nil
}

func (k msgServer) handleAnonAddItems(ctx context.Context, sdkCtx sdk.Context, msg *types.MsgManageAnonymousCollection, coll types.Collection, params types.Params, blockHeight int64) (*types.MsgManageAnonymousCollectionResponse, error) {
	if uint32(len(msg.Items)) > params.MaxBatchSize {
		return nil, types.ErrBatchTooLarge
	}
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName).String()
	submitterAddr, _ := k.addressCodec.StringToBytes(msg.Submitter)

	var itemIDs []uint64
	for i, entry := range msg.Items {
		if coll.ItemCount >= uint64(params.MaxItemsPerCollection) {
			return nil, types.ErrMaxItems
		}

		if params.PerItemDeposit.IsPositive() {
			if err := k.EscrowSPARK(ctx, submitterAddr, params.PerItemDeposit); err != nil {
				return nil, errorsmod.Wrap(types.ErrInsufficientFunds, err.Error())
			}
		}

		itemID, err := k.ItemSeq.Next(ctx)
		if err != nil {
			return nil, errorsmod.Wrap(err, "failed to get next item ID")
		}

		item := types.Item{
			Id:            itemID,
			CollectionId:  msg.CollectionId,
			Position:      coll.ItemCount,
			Title:         entry.Title,
			Description:   entry.Description,
			ImageUri:      entry.ImageUri,
			ReferenceType: entry.ReferenceType,
			Nft:           entry.Nft,
			Link:          entry.Link,
			OnChain:       entry.OnChain,
			Custom:        entry.Custom,
			Attributes:    entry.Attributes,
			AddedBy:       moduleAddr,
			AddedAt:       blockHeight,
			Status:        types.ItemStatus_ITEM_STATUS_ACTIVE,
		}

		if err := k.Item.Set(ctx, itemID, item); err != nil {
			return nil, errorsmod.Wrap(err, "failed to store item")
		}

		k.ItemsByCollection.Set(ctx, collections.Join(msg.CollectionId, itemID)) //nolint:errcheck
		k.ItemsByOwner.Set(ctx, collections.Join(moduleAddr, itemID))            //nolint:errcheck
		itemIDs = append(itemIDs, itemID)

		coll.ItemCount++
		coll.ItemDepositTotal = coll.ItemDepositTotal.Add(params.PerItemDeposit)
		_ = i
	}

	coll.UpdatedAt = blockHeight
	k.Collection.Set(ctx, coll.Id, coll) //nolint:errcheck

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("anonymous_items_added",
		sdk.NewAttribute("collection_id", strconv.FormatUint(msg.CollectionId, 10)),
		sdk.NewAttribute("count", strconv.Itoa(len(itemIDs))),
	))

	return &types.MsgManageAnonymousCollectionResponse{ItemIds: itemIDs}, nil
}

func (k msgServer) handleAnonRemoveItem(ctx context.Context, sdkCtx sdk.Context, msg *types.MsgManageAnonymousCollection, coll types.Collection, params types.Params, blockHeight int64) (*types.MsgManageAnonymousCollectionResponse, error) {
	if msg.TargetItemId == 0 {
		return nil, errorsmod.Wrap(types.ErrItemNotFound, "target_item_id is required")
	}

	item, err := k.Item.Get(ctx, msg.TargetItemId)
	if err != nil {
		return nil, types.ErrItemNotFound
	}
	if item.CollectionId != msg.CollectionId {
		return nil, types.ErrItemNotFound
	}

	// Remove item and indexes
	k.Item.Remove(ctx, item.Id)                                                    //nolint:errcheck
	k.ItemsByCollection.Remove(ctx, collections.Join(msg.CollectionId, item.Id))   //nolint:errcheck
	k.ItemsByOwner.Remove(ctx, collections.Join(coll.Owner, item.Id))              //nolint:errcheck

	if coll.ItemCount > 0 {
		coll.ItemCount--
	}
	coll.ItemDepositTotal = coll.ItemDepositTotal.Sub(params.PerItemDeposit)
	if coll.ItemDepositTotal.IsNegative() {
		coll.ItemDepositTotal = math.ZeroInt()
	}
	coll.UpdatedAt = blockHeight
	k.Collection.Set(ctx, coll.Id, coll) //nolint:errcheck

	// Refund per-item deposit to submitter
	submitterAddr, _ := k.addressCodec.StringToBytes(msg.Submitter)
	if params.PerItemDeposit.IsPositive() {
		k.RefundSPARK(ctx, submitterAddr, params.PerItemDeposit) //nolint:errcheck
	}

	k.CompactPositions(ctx, msg.CollectionId) //nolint:errcheck

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("anonymous_item_removed",
		sdk.NewAttribute("collection_id", strconv.FormatUint(msg.CollectionId, 10)),
		sdk.NewAttribute("item_id", strconv.FormatUint(msg.TargetItemId, 10)),
	))

	return &types.MsgManageAnonymousCollectionResponse{}, nil
}

func (k msgServer) handleAnonRemoveItems(ctx context.Context, sdkCtx sdk.Context, msg *types.MsgManageAnonymousCollection, coll types.Collection, params types.Params, blockHeight int64) (*types.MsgManageAnonymousCollectionResponse, error) {
	if uint32(len(msg.ItemIds)) > params.MaxBatchSize {
		return nil, types.ErrBatchTooLarge
	}

	submitterAddr, _ := k.addressCodec.StringToBytes(msg.Submitter)
	for _, itemID := range msg.ItemIds {
		item, err := k.Item.Get(ctx, itemID)
		if err != nil {
			return nil, errorsmod.Wrapf(types.ErrItemNotFound, "item %d", itemID)
		}
		if item.CollectionId != msg.CollectionId {
			return nil, errorsmod.Wrapf(types.ErrItemNotFound, "item %d not in collection", itemID)
		}

		k.Item.Remove(ctx, itemID)                                                     //nolint:errcheck
		k.ItemsByCollection.Remove(ctx, collections.Join(msg.CollectionId, itemID))    //nolint:errcheck
		k.ItemsByOwner.Remove(ctx, collections.Join(coll.Owner, itemID))               //nolint:errcheck

		if coll.ItemCount > 0 {
			coll.ItemCount--
		}
		coll.ItemDepositTotal = coll.ItemDepositTotal.Sub(params.PerItemDeposit)
		if coll.ItemDepositTotal.IsNegative() {
			coll.ItemDepositTotal = math.ZeroInt()
		}

		if params.PerItemDeposit.IsPositive() {
			k.RefundSPARK(ctx, submitterAddr, params.PerItemDeposit) //nolint:errcheck
		}
	}

	coll.UpdatedAt = blockHeight
	k.Collection.Set(ctx, coll.Id, coll) //nolint:errcheck
	k.CompactPositions(ctx, msg.CollectionId) //nolint:errcheck

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("anonymous_items_removed",
		sdk.NewAttribute("collection_id", strconv.FormatUint(msg.CollectionId, 10)),
		sdk.NewAttribute("count", strconv.Itoa(len(msg.ItemIds))),
	))

	return &types.MsgManageAnonymousCollectionResponse{}, nil
}

func (k msgServer) handleAnonUpdateItem(ctx context.Context, sdkCtx sdk.Context, msg *types.MsgManageAnonymousCollection, coll types.Collection, params types.Params, blockHeight int64) (*types.MsgManageAnonymousCollectionResponse, error) {
	item, err := k.Item.Get(ctx, msg.TargetItemId)
	if err != nil {
		return nil, types.ErrItemNotFound
	}
	if item.CollectionId != msg.CollectionId {
		return nil, types.ErrItemNotFound
	}

	// Update fields if provided
	if msg.Title != "" {
		if uint32(len(msg.Title)) > params.MaxTitleLength {
			return nil, types.ErrInvalidTitle
		}
		item.Title = msg.Title
	}
	if msg.Description != "" {
		if uint32(len(msg.Description)) > params.MaxDescriptionLength {
			return nil, types.ErrInvalidDescription
		}
		item.Description = msg.Description
	}
	if msg.ImageUri != "" {
		item.ImageUri = msg.ImageUri
	}
	if msg.ReferenceType != types.ReferenceType_REFERENCE_TYPE_UNSPECIFIED {
		item.ReferenceType = msg.ReferenceType
		item.Nft = msg.Nft
		item.Link = msg.Link
		item.OnChain = msg.OnChain
		item.Custom = msg.Custom
	}
	if len(msg.Attributes) > 0 {
		if uint32(len(msg.Attributes)) > params.MaxAttributesPerItem {
			return nil, types.ErrMaxAttributes
		}
		item.Attributes = msg.Attributes
	}

	if err := k.Item.Set(ctx, item.Id, item); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update item")
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("anonymous_item_updated",
		sdk.NewAttribute("collection_id", strconv.FormatUint(msg.CollectionId, 10)),
		sdk.NewAttribute("item_id", strconv.FormatUint(msg.TargetItemId, 10)),
	))

	return &types.MsgManageAnonymousCollectionResponse{}, nil
}

func (k msgServer) handleAnonReorderItem(ctx context.Context, sdkCtx sdk.Context, msg *types.MsgManageAnonymousCollection, coll types.Collection, blockHeight int64) (*types.MsgManageAnonymousCollectionResponse, error) {
	item, err := k.Item.Get(ctx, msg.TargetItemId)
	if err != nil {
		return nil, types.ErrItemNotFound
	}
	if item.CollectionId != msg.CollectionId {
		return nil, types.ErrItemNotFound
	}

	if msg.NewPosition >= coll.ItemCount {
		return nil, types.ErrInvalidPosition
	}

	oldPosition := item.Position
	item.Position = msg.NewPosition
	if err := k.Item.Set(ctx, item.Id, item); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update item position")
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("anonymous_item_reordered",
		sdk.NewAttribute("collection_id", strconv.FormatUint(msg.CollectionId, 10)),
		sdk.NewAttribute("item_id", strconv.FormatUint(msg.TargetItemId, 10)),
		sdk.NewAttribute("old_position", strconv.FormatUint(oldPosition, 10)),
		sdk.NewAttribute("new_position", strconv.FormatUint(msg.NewPosition, 10)),
	))

	return &types.MsgManageAnonymousCollectionResponse{}, nil
}

func (k msgServer) handleAnonUpdateMetadata(ctx context.Context, sdkCtx sdk.Context, msg *types.MsgManageAnonymousCollection, coll types.Collection, params types.Params, blockHeight int64) (*types.MsgManageAnonymousCollectionResponse, error) {
	if msg.CollectionName != "" {
		if uint32(len(msg.CollectionName)) > params.MaxNameLength {
			return nil, types.ErrInvalidName
		}
		coll.Name = msg.CollectionName
	}
	if msg.CollectionDescription != "" {
		if uint32(len(msg.CollectionDescription)) > params.MaxDescriptionLength {
			return nil, types.ErrInvalidDescription
		}
		coll.Description = msg.CollectionDescription
	}
	if msg.CollectionCoverUri != "" {
		coll.CoverUri = msg.CollectionCoverUri
	}
	if len(msg.MetadataTags) > 0 {
		if uint32(len(msg.MetadataTags)) > params.MaxTagsPerCollection {
			return nil, types.ErrMaxTags
		}
		for _, tag := range msg.MetadataTags {
			if uint32(len(tag)) > params.MaxTagLength {
				return nil, types.ErrTagTooLong
			}
		}
		coll.Tags = msg.MetadataTags
	}

	coll.UpdatedAt = blockHeight
	if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update collection metadata")
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("anonymous_collection_updated",
		sdk.NewAttribute("collection_id", strconv.FormatUint(msg.CollectionId, 10)),
	))

	return &types.MsgManageAnonymousCollectionResponse{}, nil
}

// buildManagementPayload constructs the canonical payload for signature verification.
// Format: SHA256(collection_id || nonce || action)
func buildManagementPayload(msg *types.MsgManageAnonymousCollection) []byte {
	buf := make([]byte, 8+8+4)
	binary.BigEndian.PutUint64(buf[0:8], msg.CollectionId)
	binary.BigEndian.PutUint64(buf[8:16], msg.Nonce)
	binary.BigEndian.PutUint32(buf[16:20], uint32(msg.Action))
	hash := sha256.Sum256(buf)
	return hash[:]
}
