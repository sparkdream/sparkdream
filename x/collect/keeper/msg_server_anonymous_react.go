package keeper

import (
	"context"
	"encoding/hex"
	"strconv"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

func (k msgServer) AnonymousReact(ctx context.Context, msg *types.MsgAnonymousReact) (*types.MsgAnonymousReactResponse, error) {
	submitterAddr, err := k.addressCodec.StringToBytes(msg.Submitter)
	if err != nil {
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

	// Reaction type must be 1 (upvote) or 2 (downvote)
	if msg.ReactionType != 1 && msg.ReactionType != 2 {
		return nil, types.ErrInvalidReactionType
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()

	// Validate target exists and is public/active
	switch msg.TargetType {
	case types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION:
		coll, err := k.Collection.Get(ctx, msg.TargetId)
		if err != nil {
			return nil, types.ErrCollectionNotFound
		}
		if coll.Visibility != types.Visibility_VISIBILITY_PUBLIC {
			return nil, types.ErrCannotRatePrivate
		}
		if coll.Status != types.CollectionStatus_COLLECTION_STATUS_ACTIVE {
			return nil, types.ErrNotPublicActive
		}
		if !coll.CommunityFeedbackEnabled {
			return nil, types.ErrNotPublicActive
		}

	case types.FlagTargetType_FLAG_TARGET_TYPE_ITEM:
		item, err := k.Item.Get(ctx, msg.TargetId)
		if err != nil {
			return nil, types.ErrItemNotFound
		}
		if item.Status != types.ItemStatus_ITEM_STATUS_ACTIVE {
			return nil, types.ErrNotPublicActive
		}
		// Check parent collection
		coll, err := k.Collection.Get(ctx, item.CollectionId)
		if err != nil {
			return nil, types.ErrCollectionNotFound
		}
		if coll.Visibility != types.Visibility_VISIBILITY_PUBLIC {
			return nil, types.ErrCannotRatePrivate
		}
		if !coll.CommunityFeedbackEnabled {
			return nil, types.ErrNotPublicActive
		}

	default:
		return nil, errorsmod.Wrap(types.ErrUnauthorized, "invalid target type")
	}

	// Determine nullifier domain and scope
	var domain, scope uint64
	switch msg.TargetType {
	case types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION:
		domain = 10
		scope = msg.TargetId
	case types.FlagTargetType_FLAG_TARGET_TYPE_ITEM:
		domain = 11
		scope = msg.TargetId
	}

	// Check nullifier not used
	nullifierHex := hex.EncodeToString(msg.Nullifier)
	if k.IsNullifierUsed(ctx, domain, scope, nullifierHex) {
		return nil, types.ErrNullifierUsed
	}

	// Verify ZK proof
	if err := k.voteKeeper.VerifyAnonymousActionProof(ctx, msg.Proof, msg.Nullifier, msg.MerkleRoot, msg.MinTrustLevel); err != nil {
		return nil, errorsmod.Wrap(types.ErrInvalidZKProof, err.Error())
	}

	// For downvotes, burn downvote_cost from submitter
	if msg.ReactionType == 2 && params.DownvoteCost.IsPositive() {
		if err := k.BurnSPARKFromAccount(ctx, submitterAddr, params.DownvoteCost); err != nil {
			return nil, errorsmod.Wrap(types.ErrInsufficientFunds, err.Error())
		}
	}

	// Update vote counts on target
	switch msg.TargetType {
	case types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION:
		coll, _ := k.Collection.Get(ctx, msg.TargetId)
		if msg.ReactionType == 1 {
			coll.UpvoteCount++
		} else {
			coll.DownvoteCount++
		}
		k.Collection.Set(ctx, coll.Id, coll) //nolint:errcheck

	case types.FlagTargetType_FLAG_TARGET_TYPE_ITEM:
		item, _ := k.Item.Get(ctx, msg.TargetId)
		if msg.ReactionType == 1 {
			item.UpvoteCount++
		} else {
			item.DownvoteCount++
		}
		k.Item.Set(ctx, item.Id, item) //nolint:errcheck
	}

	// Record nullifier used
	k.SetNullifierUsed(ctx, domain, scope, nullifierHex, types.AnonNullifierEntry{
		UsedAt: blockHeight,
		Domain: domain,
		Scope:  scope,
	})

	// Emit event (no submitter for privacy)
	eventType := "anonymous_content_upvoted"
	if msg.ReactionType == 2 {
		eventType = "anonymous_content_downvoted"
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(eventType,
		sdk.NewAttribute("target_id", strconv.FormatUint(msg.TargetId, 10)),
		sdk.NewAttribute("target_type", msg.TargetType.String()),
		sdk.NewAttribute("proven_trust_level", strconv.FormatUint(uint64(msg.MinTrustLevel), 10)),
	))

	return &types.MsgAnonymousReactResponse{}, nil
}
