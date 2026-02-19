package keeper

import (
	"context"
	"strconv"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

func (k msgServer) UpvoteContent(ctx context.Context, msg *types.MsgUpvoteContent) (*types.MsgUpvoteContentResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()

	// Creator must be an active x/rep member
	if !k.isMember(ctx, msg.Creator) {
		return nil, types.ErrNotMember
	}

	// Target must be PUBLIC, ACTIVE, with community_feedback_enabled
	coll, err := k.ValidatePublicActiveFeedbackTarget(ctx, msg.TargetType, msg.TargetId)
	if err != nil {
		return nil, err
	}

	// Creator must not be collection owner or collaborator
	if coll.Owner == msg.Creator {
		return nil, types.ErrCannotVoteOwnContent
	}
	isCollab, _, err := k.IsCollaborator(ctx, coll.Id, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to check collaborator")
	}
	if isCollab {
		return nil, types.ErrCannotVoteOwnContent
	}

	// Check ReactionDedup - creator must not have already voted on this target
	dedupKey := ReactionDedupCompositeKey(msg.Creator, msg.TargetType, msg.TargetId)
	_, err = k.ReactionDedup.Get(ctx, dedupKey)
	if err == nil {
		return nil, types.ErrAlreadyVoted
	}

	// Check daily limit
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}
	if err := k.checkDailyLimit(ctx, msg.Creator, blockHeight, "upvote", params.MaxUpvotesPerDay); err != nil {
		return nil, err
	}

	// Increment upvote_count on target
	switch msg.TargetType {
	case types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION:
		coll.UpvoteCount++
		if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update collection")
		}
	case types.FlagTargetType_FLAG_TARGET_TYPE_ITEM:
		item, err := k.Item.Get(ctx, msg.TargetId)
		if err != nil {
			return nil, types.ErrItemNotFound
		}
		item.UpvoteCount++
		if err := k.Item.Set(ctx, item.Id, item); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update item")
		}
	}

	// Store ReactionDedup key with value 1 (upvote)
	if err := k.ReactionDedup.Set(ctx, dedupKey, 1); err != nil {
		return nil, errorsmod.Wrap(err, "failed to set reaction dedup")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("content_upvoted",
		sdk.NewAttribute("creator", msg.Creator),
		sdk.NewAttribute("target_id", strconv.FormatUint(msg.TargetId, 10)),
		sdk.NewAttribute("target_type", msg.TargetType.String()),
	))

	return &types.MsgUpvoteContentResponse{}, nil
}
