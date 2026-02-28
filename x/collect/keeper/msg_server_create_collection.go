package keeper

import (
	"context"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"

	reptypes "sparkdream/x/rep/types"
)

func (k msgServer) CreateCollection(ctx context.Context, msg *types.MsgCreateCollection) (*types.MsgCreateCollectionResponse, error) {
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

	// Check tiered collection limit
	currentCount, err := k.countCollectionsByOwner(ctx, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to count collections")
	}
	maxCollections := k.getMaxCollections(ctx, msg.Creator, params)
	if currentCount >= maxCollections {
		return nil, types.ErrMaxCollections
	}

	// Validate visibility/encryption constraints
	if msg.Encrypted && msg.Visibility != types.Visibility_VISIBILITY_PRIVATE {
		return nil, types.ErrEncryptedRequiresPrivate
	}
	if msg.Visibility == types.Visibility_VISIBILITY_PRIVATE && !msg.Encrypted {
		return nil, types.ErrPrivateRequiresEncryption
	}

	// Validate content fields based on encrypted flag
	if msg.Encrypted {
		if uint32(len(msg.EncryptedData)) > params.MaxEncryptedDataSize {
			return nil, types.ErrEncryptedDataTooLarge
		}
		if msg.Name != "" || msg.Description != "" || msg.CoverUri != "" {
			return nil, types.ErrEncryptedFieldMismatch
		}
	} else {
		if msg.EncryptedData != nil && len(msg.EncryptedData) > 0 {
			return nil, types.ErrEncryptedFieldMismatch
		}
		if msg.Name == "" || uint32(len(msg.Name)) > params.MaxNameLength {
			return nil, types.ErrInvalidName
		}
		if uint32(len(msg.Description)) > params.MaxDescriptionLength {
			return nil, types.ErrInvalidDescription
		}
		if uint32(len(msg.CoverUri)) > params.MaxReferenceFieldLength {
			return nil, types.ErrReferenceFieldTooLong
		}
	}

	// Validate tags
	if uint32(len(msg.Tags)) > params.MaxTagsPerCollection {
		return nil, types.ErrMaxTags
	}
	for _, tag := range msg.Tags {
		if uint32(len(tag)) > params.MaxTagLength {
			return nil, types.ErrTagTooLong
		}
		// Validate against shared tag registry if available
		if k.forumKeeper != nil {
			exists, err := k.forumKeeper.TagExists(ctx, tag)
			if err != nil {
				return nil, errorsmod.Wrap(err, "failed to check tag registry")
			}
			if !exists {
				return nil, errorsmod.Wrapf(types.ErrTagTooLong, "tag %q not found in registry", tag)
			}
		}
	}

	member := k.isMember(ctx, msg.Creator)
	deposit := params.BaseCollectionDeposit

	var status types.CollectionStatus
	var depositBurned bool

	if member {
		// Members: status=ACTIVE
		status = types.CollectionStatus_COLLECTION_STATUS_ACTIVE

		if msg.ExpiresAt == 0 {
			// Permanent collection: burn deposit
			if err := k.BurnSPARKFromAccount(ctx, creatorAddr, deposit); err != nil {
				return nil, errorsmod.Wrap(types.ErrInsufficientFunds, err.Error())
			}
			depositBurned = true
		} else {
			// TTL collection: escrow deposit
			if msg.ExpiresAt <= blockHeight {
				return nil, types.ErrInvalidExpiry
			}
			if params.MaxTtlBlocks > 0 && (msg.ExpiresAt-blockHeight) > params.MaxTtlBlocks {
				return nil, types.ErrInvalidExpiry
			}
			if err := k.EscrowSPARK(ctx, creatorAddr, deposit); err != nil {
				return nil, errorsmod.Wrap(types.ErrInsufficientFunds, err.Error())
			}
			depositBurned = false
		}
	} else {
		// Non-members: status=PENDING, must be TTL
		status = types.CollectionStatus_COLLECTION_STATUS_PENDING

		if msg.ExpiresAt == 0 {
			return nil, types.ErrNonMemberPermanent
		}
		if msg.ExpiresAt <= blockHeight {
			return nil, types.ErrInvalidExpiry
		}
		if (msg.ExpiresAt - blockHeight) > params.MaxNonMemberTtlBlocks {
			return nil, types.ErrNonMemberTTLExceeded
		}

		// Escrow collection deposit
		if err := k.EscrowSPARK(ctx, creatorAddr, deposit); err != nil {
			return nil, errorsmod.Wrap(types.ErrInsufficientFunds, err.Error())
		}
		depositBurned = false

		// Escrow endorsement creation fee
		if err := k.EscrowSPARK(ctx, creatorAddr, params.EndorsementCreationFee); err != nil {
			// Refund the collection deposit we already escrowed
			k.RefundSPARK(ctx, creatorAddr, deposit) //nolint:errcheck
			return nil, errorsmod.Wrap(types.ErrInsufficientFunds, err.Error())
		}
	}

	// Validate initiative reference before creating the collection
	if msg.InitiativeId > 0 && k.repKeeper != nil {
		if err := k.repKeeper.ValidateInitiativeReference(ctx, msg.InitiativeId); err != nil {
			return nil, errorsmod.Wrapf(types.ErrInvalidInitiativeRef, "initiative %d: %s", msg.InitiativeId, err.Error())
		}
	}

	// Get next collection ID
	collID, err := k.CollectionSeq.Next(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get next collection ID")
	}

	coll := types.Collection{
		Id:                       collID,
		Owner:                    msg.Creator,
		Name:                     msg.Name,
		Description:              msg.Description,
		CoverUri:                 msg.CoverUri,
		Tags:                     msg.Tags,
		EncryptedData:            msg.EncryptedData,
		Type:                     msg.Type,
		Visibility:               msg.Visibility,
		Encrypted:                msg.Encrypted,
		ItemCount:                0,
		CollaboratorCount:        0,
		CreatedAt:                blockHeight,
		UpdatedAt:                blockHeight,
		ExpiresAt:                msg.ExpiresAt,
		DepositAmount:            deposit,
		ItemDepositTotal:         math.ZeroInt(),
		DepositBurned:            depositBurned,
		CommunityFeedbackEnabled: true,
		Status:                   status,
		SeekingEndorsement:       status == types.CollectionStatus_COLLECTION_STATUS_PENDING,
		InitiativeId:             msg.InitiativeId,
	}

	// Store collection
	if err := k.Collection.Set(ctx, collID, coll); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store collection")
	}

	// Set indexes
	if err := k.CollectionsByOwner.Set(ctx, collections.Join(msg.Creator, collID)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to set owner index")
	}
	if msg.ExpiresAt > 0 {
		if err := k.CollectionsByExpiry.Set(ctx, collections.Join(msg.ExpiresAt, collID)); err != nil {
			return nil, errorsmod.Wrap(err, "failed to set expiry index")
		}
	}
	if err := k.CollectionsByStatus.Set(ctx, collections.Join(int32(status), collID)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to set status index")
	}

	// Create author bond if requested (requires repKeeper)
	if msg.AuthorBond != nil && msg.AuthorBond.IsPositive() && k.repKeeper != nil {
		if _, err := k.repKeeper.CreateAuthorBond(ctx, creatorAddr, reptypes.StakeTargetType_STAKE_TARGET_COLLECTION_AUTHOR_BOND, collID, *msg.AuthorBond); err != nil {
			return nil, errorsmod.Wrap(err, "failed to create author bond")
		}
	}

	// Register initiative reference link for conviction propagation
	if msg.InitiativeId > 0 && k.repKeeper != nil {
		if err := k.repKeeper.RegisterContentInitiativeLink(ctx, msg.InitiativeId, int32(reptypes.StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT), collID); err != nil {
			return nil, errorsmod.Wrap(err, "failed to register content initiative link")
		}
	}

	// For PENDING: add EndorsementPending index entry
	if status == types.CollectionStatus_COLLECTION_STATUS_PENDING {
		expiryBlock := blockHeight + params.EndorsementExpiryBlocks
		if err := k.EndorsementPending.Set(ctx, collections.Join(expiryBlock, collID)); err != nil {
			return nil, errorsmod.Wrap(err, "failed to set endorsement pending index")
		}
	}

	endorsementFeeEscrowed := "0"
	if status == types.CollectionStatus_COLLECTION_STATUS_PENDING {
		endorsementFeeEscrowed = params.EndorsementCreationFee.String()
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("collection_created",
		sdk.NewAttribute("id", strconv.FormatUint(collID, 10)),
		sdk.NewAttribute("owner", msg.Creator),
		sdk.NewAttribute("type", msg.Type.String()),
		sdk.NewAttribute("visibility", msg.Visibility.String()),
		sdk.NewAttribute("encrypted", strconv.FormatBool(msg.Encrypted)),
		sdk.NewAttribute("expires_at", strconv.FormatInt(msg.ExpiresAt, 10)),
		sdk.NewAttribute("deposit_amount", deposit.String()),
		sdk.NewAttribute("endorsement_fee_escrowed", endorsementFeeEscrowed),
	))

	return &types.MsgCreateCollectionResponse{Id: collID}, nil
}
