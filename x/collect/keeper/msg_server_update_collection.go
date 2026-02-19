package keeper

import (
	"context"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

func (k msgServer) UpdateCollection(ctx context.Context, msg *types.MsgUpdateCollection) (*types.MsgUpdateCollectionResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()

	// Get collection
	coll, err := k.Collection.Get(ctx, msg.Id)
	if err != nil {
		return nil, types.ErrCollectionNotFound
	}

	// Must be owner
	if coll.Owner != msg.Creator {
		return nil, types.ErrUnauthorized
	}

	// Collection must not be immutable
	if coll.Immutable {
		return nil, types.ErrCollectionImmutable
	}

	// Status must be ACTIVE or PENDING (not HIDDEN)
	if coll.Status != types.CollectionStatus_COLLECTION_STATUS_ACTIVE &&
		coll.Status != types.CollectionStatus_COLLECTION_STATUS_PENDING {
		return nil, errorsmod.Wrapf(types.ErrUnauthorized, "cannot update collection in status %s", coll.Status.String())
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Validate content fields
	if coll.Encrypted {
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
	}

	member := k.isMember(ctx, msg.Creator)

	// Handle TTL/permanent conversion
	if msg.ExpiresAt == 0 && coll.ExpiresAt > 0 {
		// TTL->permanent conversion: only for members
		if !member {
			return nil, types.ErrNonMemberPermanent
		}

		// Burn held deposits (collection deposit + item deposits)
		if !coll.DepositBurned {
			totalHeld := coll.DepositAmount.Add(coll.ItemDepositTotal)
			if totalHeld.IsPositive() {
				if err := k.BurnSPARK(ctx, totalHeld); err != nil {
					return nil, errorsmod.Wrap(err, "failed to burn held deposits")
				}
			}
			coll.DepositBurned = true
		}

		// Remove from expiry index
		k.CollectionsByExpiry.Remove(ctx, collections.Join(coll.ExpiresAt, coll.Id)) //nolint:errcheck

		// Cancel pending sponsorship request if any
		req, err := k.SponsorshipRequest.Get(ctx, coll.Id)
		if err == nil {
			// Refund sponsorship request deposits
			requesterAddr, err := k.addressCodec.StringToBytes(req.Requester)
			if err == nil {
				refundAmt := req.CollectionDeposit.Add(req.ItemDepositTotal)
				if refundAmt.IsPositive() {
					k.RefundSPARK(ctx, requesterAddr, refundAmt) //nolint:errcheck
				}
			}
			k.SponsorshipRequestsByExpiry.Remove(ctx, collections.Join(req.ExpiresAt, coll.Id)) //nolint:errcheck
			k.SponsorshipRequest.Remove(ctx, coll.Id)                                           //nolint:errcheck
		}

		coll.ExpiresAt = 0
	} else if msg.ExpiresAt > 0 {
		if coll.ExpiresAt == 0 {
			// Permanent cannot set expires_at > 0
			return nil, types.ErrCannotRevertPermanent
		}

		// Non-members: TTL must remain ≤ max_non_member_ttl_blocks from original creation block
		if !member && (msg.ExpiresAt-coll.CreatedAt) > params.MaxNonMemberTtlBlocks {
			return nil, types.ErrNonMemberTTLExceeded
		}

		// Validate TTL constraints
		if msg.ExpiresAt <= blockHeight {
			return nil, types.ErrInvalidExpiry
		}
		if params.MaxTtlBlocks > 0 && (msg.ExpiresAt-blockHeight) > params.MaxTtlBlocks {
			return nil, types.ErrInvalidExpiry
		}

		// Update expiry index
		if coll.ExpiresAt > 0 {
			k.CollectionsByExpiry.Remove(ctx, collections.Join(coll.ExpiresAt, coll.Id)) //nolint:errcheck
		}
		if err := k.CollectionsByExpiry.Set(ctx, collections.Join(msg.ExpiresAt, coll.Id)); err != nil {
			return nil, errorsmod.Wrap(err, "failed to set expiry index")
		}
		coll.ExpiresAt = msg.ExpiresAt
	}

	// Update fields
	coll.Type = msg.Type
	coll.Name = msg.Name
	coll.Description = msg.Description
	coll.CoverUri = msg.CoverUri
	coll.Tags = msg.Tags
	coll.EncryptedData = msg.EncryptedData
	coll.UpdatedAt = blockHeight

	// Only update community_feedback_enabled if update_community_feedback is true
	if msg.UpdateCommunityFeedback {
		coll.CommunityFeedbackEnabled = msg.CommunityFeedbackEnabled
	}

	// Store updated collection
	if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update collection")
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("collection_updated",
		sdk.NewAttribute("id", strconv.FormatUint(coll.Id, 10)),
		sdk.NewAttribute("owner", coll.Owner),
		sdk.NewAttribute("expires_at", strconv.FormatInt(coll.ExpiresAt, 10)),
		sdk.NewAttribute("deposit_burned", strconv.FormatBool(coll.DepositBurned)),
	))

	return &types.MsgUpdateCollectionResponse{}, nil
}
