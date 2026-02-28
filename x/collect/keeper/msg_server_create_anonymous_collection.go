package keeper

import (
	"context"
	"encoding/hex"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/collect/types"

	reptypes "sparkdream/x/rep/types"
)

func (k msgServer) CreateAnonymousCollection(ctx context.Context, msg *types.MsgCreateAnonymousCollection) (*types.MsgCreateAnonymousCollectionResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Submitter); err != nil {
		return nil, errorsmod.Wrap(err, "invalid submitter address")
	}

	// VoteKeeper must be available
	if k.voteKeeper == nil {
		return nil, types.ErrAnonymousPostingUnavailable
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Anonymous posting must be enabled
	if !params.AnonymousPostingEnabled {
		return nil, types.ErrAnonymousPostingDisabled
	}

	// Trust level must meet minimum
	if msg.MinTrustLevel < params.AnonymousMinTrustLevel {
		return nil, errorsmod.Wrapf(types.ErrInsufficientAnonTrustLevel,
			"proven %d < required %d", msg.MinTrustLevel, params.AnonymousMinTrustLevel)
	}

	// Management key must be exactly 32 bytes
	if len(msg.ManagementPublicKey) != 32 {
		return nil, types.ErrInvalidManagementKey
	}

	// Anonymous collections must have TTL (expires_at > 0)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()
	if msg.ExpiresAt <= 0 {
		return nil, types.ErrAnonymousRequiresTTL
	}
	if msg.ExpiresAt <= blockHeight {
		return nil, errorsmod.Wrap(types.ErrInvalidExpiry, "expires_at must be in the future")
	}
	if params.MaxTtlBlocks > 0 && (msg.ExpiresAt-blockHeight) > params.MaxTtlBlocks {
		return nil, errorsmod.Wrapf(types.ErrInvalidExpiry, "TTL exceeds max_ttl_blocks %d", params.MaxTtlBlocks)
	}

	// Management key collection count check
	keyCount := k.GetManagementKeyCollectionCount(ctx, msg.ManagementPublicKey)
	if keyCount >= params.MaxAnonymousCollectionsPerKey {
		return nil, errorsmod.Wrapf(types.ErrMaxAnonymousCollections,
			"key has %d collections, max %d", keyCount, params.MaxAnonymousCollectionsPerKey)
	}

	// Validate content
	if uint32(len(msg.Name)) > params.MaxNameLength || len(msg.Name) == 0 {
		return nil, types.ErrInvalidName
	}
	if uint32(len(msg.Description)) > params.MaxDescriptionLength {
		return nil, types.ErrInvalidDescription
	}
	if uint32(len(msg.Tags)) > params.MaxTagsPerCollection {
		return nil, types.ErrMaxTags
	}
	for _, tag := range msg.Tags {
		if uint32(len(tag)) > params.MaxTagLength {
			return nil, types.ErrTagTooLong
		}
	}
	if uint32(len(msg.InitialItems)) > params.MaxBatchSize {
		return nil, types.ErrBatchTooLarge
	}

	// Compute nullifier scope (epoch-based: domain=6, scope=epoch)
	epochDuration := DefaultEpochDuration
	if k.seasonKeeper != nil {
		epochDuration = k.seasonKeeper.GetEpochDuration(ctx)
	}
	epoch := uint64(sdkCtx.BlockTime().Unix()) / uint64(epochDuration)

	// Check nullifier not used (domain=6, scope=epoch)
	nullifierHex := hex.EncodeToString(msg.Nullifier)
	if k.IsNullifierUsed(ctx, 6, epoch, nullifierHex) {
		return nil, types.ErrNullifierUsed
	}

	// Verify ZK proof
	if err := k.voteKeeper.VerifyAnonymousActionProof(ctx, msg.Proof, msg.Nullifier, msg.MerkleRoot, msg.MinTrustLevel); err != nil {
		return nil, errorsmod.Wrap(types.ErrInvalidZKProof, err.Error())
	}

	// Validate initiative reference
	if msg.InitiativeId > 0 && k.repKeeper != nil {
		if err := k.repKeeper.ValidateInitiativeReference(ctx, msg.InitiativeId); err != nil {
			return nil, errorsmod.Wrapf(types.ErrInvalidInitiativeRef, "initiative %d: %s", msg.InitiativeId, err.Error())
		}
	}

	// Escrow deposit from submitter (held in module account, burned on pin/expiry)
	submitterAddr, _ := k.addressCodec.StringToBytes(msg.Submitter)
	if params.BaseCollectionDeposit.IsPositive() {
		if err := k.EscrowSPARK(ctx, submitterAddr, params.BaseCollectionDeposit); err != nil {
			return nil, errorsmod.Wrap(types.ErrInsufficientFunds, err.Error())
		}
	}

	// Create the collection with module account as owner
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName).String()

	collID, err := k.CollectionSeq.Next(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get next collection ID")
	}

	coll := types.Collection{
		Id:                       collID,
		Owner:                    moduleAddr,
		Name:                     msg.Name,
		Description:              msg.Description,
		CoverUri:                 msg.CoverUri,
		Tags:                     msg.Tags,
		Type:                     msg.Type,
		Visibility:               types.Visibility_VISIBILITY_PUBLIC,
		Encrypted:                false,
		ItemCount:                0,
		CollaboratorCount:        0,
		CreatedAt:                blockHeight,
		UpdatedAt:                blockHeight,
		ExpiresAt:                msg.ExpiresAt,
		DepositAmount:            params.BaseCollectionDeposit,
		ItemDepositTotal:         math.ZeroInt(),
		DepositBurned:            false,
		CommunityFeedbackEnabled: true,
		Status:                   types.CollectionStatus_COLLECTION_STATUS_ACTIVE,
		InitiativeId:             msg.InitiativeId,
	}

	if err := k.Collection.Set(ctx, collID, coll); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store collection")
	}

	// Set indexes
	k.CollectionsByOwner.Set(ctx, collections.Join(moduleAddr, collID))                                             //nolint:errcheck
	k.CollectionsByExpiry.Set(ctx, collections.Join(msg.ExpiresAt, collID))                                         //nolint:errcheck
	k.CollectionsByStatus.Set(ctx, collections.Join(int32(types.CollectionStatus_COLLECTION_STATUS_ACTIVE), collID)) //nolint:errcheck

	// Store anonymous metadata
	meta := types.AnonymousCollectionMeta{
		CollectionId:       collID,
		ManagementPublicKey: msg.ManagementPublicKey,
		Nullifier:          msg.Nullifier,
		MerkleRoot:         msg.MerkleRoot,
		Nonce:              0,
		ProvenTrustLevel:   msg.MinTrustLevel,
	}
	k.SetAnonymousCollectionMeta(ctx, collID, meta)

	// Record nullifier used
	k.SetNullifierUsed(ctx, 6, epoch, nullifierHex, types.AnonNullifierEntry{
		UsedAt: blockHeight,
		Domain: 6,
		Scope:  epoch,
	})

	// Increment management key collection count
	k.IncrementManagementKeyCollectionCount(ctx, msg.ManagementPublicKey)

	// Register initiative reference link for conviction propagation
	if msg.InitiativeId > 0 && k.repKeeper != nil {
		if err := k.repKeeper.RegisterContentInitiativeLink(ctx, msg.InitiativeId, int32(reptypes.StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT), collID); err != nil {
			return nil, errorsmod.Wrap(err, "failed to register content initiative link")
		}
	}

	// Create author bond if requested
	if !msg.AuthorBondAmount.IsNil() && msg.AuthorBondAmount.IsPositive() && k.repKeeper != nil {
		if _, err := k.repKeeper.CreateAuthorBond(ctx, submitterAddr, 3, collID, msg.AuthorBondAmount); err != nil {
			sdkCtx.Logger().Debug("author bond creation skipped", "error", err)
		}
	}

	// Add initial items (if any)
	var itemIDs []uint64
	for i, itemEntry := range msg.InitialItems {
		_ = i
		itemID, err := k.ItemSeq.Next(ctx)
		if err != nil {
			return nil, errorsmod.Wrap(err, "failed to get next item ID")
		}

		item := types.Item{
			Id:            itemID,
			CollectionId:  collID,
			Position:      uint64(i),
			Title:         itemEntry.Title,
			Description:   itemEntry.Description,
			ImageUri:      itemEntry.ImageUri,
			ReferenceType: itemEntry.ReferenceType,
			Nft:           itemEntry.Nft,
			Link:          itemEntry.Link,
			OnChain:       itemEntry.OnChain,
			Custom:        itemEntry.Custom,
			Attributes:    itemEntry.Attributes,
			AddedBy:       moduleAddr,
			AddedAt:       blockHeight,
			Status:        types.ItemStatus_ITEM_STATUS_ACTIVE,
		}

		if err := k.Item.Set(ctx, itemID, item); err != nil {
			return nil, errorsmod.Wrap(err, "failed to store item")
		}

		k.ItemsByCollection.Set(ctx, collections.Join(collID, itemID)) //nolint:errcheck
		k.ItemsByOwner.Set(ctx, collections.Join(moduleAddr, itemID))  //nolint:errcheck
		itemIDs = append(itemIDs, itemID)

		// Escrow per-item deposit (held in module account, burned on pin/expiry)
		if params.PerItemDeposit.IsPositive() {
			if err := k.EscrowSPARK(ctx, submitterAddr, params.PerItemDeposit); err != nil {
				return nil, errorsmod.Wrap(types.ErrInsufficientFunds, "insufficient funds for item deposit")
			}
			coll.ItemDepositTotal = coll.ItemDepositTotal.Add(params.PerItemDeposit)
		}
		coll.ItemCount++
	}

	// Update collection with final item count and deposit total
	if len(msg.InitialItems) > 0 {
		if err := k.Collection.Set(ctx, collID, coll); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update collection with items")
		}
	}

	// Emit event (no submitter address for privacy)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("anonymous_collection_created",
		sdk.NewAttribute("collection_id", strconv.FormatUint(collID, 10)),
		sdk.NewAttribute("proven_trust_level", strconv.FormatUint(uint64(msg.MinTrustLevel), 10)),
		sdk.NewAttribute("expires_at", strconv.FormatInt(msg.ExpiresAt, 10)),
		sdk.NewAttribute("item_count", strconv.FormatUint(coll.ItemCount, 10)),
	))

	return &types.MsgCreateAnonymousCollectionResponse{
		Id:      collID,
		ItemIds: itemIDs,
	}, nil
}
