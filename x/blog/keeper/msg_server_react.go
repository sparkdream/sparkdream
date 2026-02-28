package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/blog/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// adjustReactionCount modifies a specific reaction count field by delta.
// For decrement (delta < 0), it checks for underflow and clamps to 0.
func adjustReactionCount(counts *types.ReactionCounts, rt types.ReactionType, delta int64) {
	switch rt {
	case types.ReactionType_REACTION_TYPE_LIKE:
		if delta < 0 && counts.LikeCount == 0 {
			return
		}
		counts.LikeCount = uint64(int64(counts.LikeCount) + delta)
	case types.ReactionType_REACTION_TYPE_INSIGHTFUL:
		if delta < 0 && counts.InsightfulCount == 0 {
			return
		}
		counts.InsightfulCount = uint64(int64(counts.InsightfulCount) + delta)
	case types.ReactionType_REACTION_TYPE_DISAGREE:
		if delta < 0 && counts.DisagreeCount == 0 {
			return
		}
		counts.DisagreeCount = uint64(int64(counts.DisagreeCount) + delta)
	case types.ReactionType_REACTION_TYPE_FUNNY:
		if delta < 0 && counts.FunnyCount == 0 {
			return
		}
		counts.FunnyCount = uint64(int64(counts.FunnyCount) + delta)
	}
}

func (k msgServer) React(ctx context.Context, msg *types.MsgReact) (*types.MsgReactResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	creatorAddr, _ := sdk.AccAddressFromBech32(msg.Creator)

	// Must be active member
	if !k.isActiveMember(ctx, creatorAddr) {
		return nil, errorsmod.Wrap(types.ErrNotMember, msg.Creator)
	}

	// Validate reaction type
	if msg.ReactionType == types.ReactionType_REACTION_TYPE_UNSPECIFIED {
		return nil, errorsmod.Wrap(types.ErrInvalidReactionType, "reaction type must be specified")
	}

	// Get post, must exist and be active
	post, found := k.GetPost(ctx, msg.PostId)
	if !found {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d not found", msg.PostId))
	}
	if post.Status == types.PostStatus_POST_STATUS_DELETED {
		return nil, errorsmod.Wrap(types.ErrPostDeleted, fmt.Sprintf("post %d has been deleted", msg.PostId))
	}
	if post.Status == types.PostStatus_POST_STATUS_HIDDEN {
		return nil, errorsmod.Wrap(types.ErrPostHidden, fmt.Sprintf("post %d is hidden", msg.PostId))
	}

	// If reacting to a reply, validate it
	if msg.ReplyId > 0 {
		reply, found := k.GetReply(ctx, msg.ReplyId)
		if !found {
			return nil, errorsmod.Wrap(types.ErrReplyNotFound, fmt.Sprintf("reply %d not found", msg.ReplyId))
		}
		if reply.PostId != msg.PostId {
			return nil, errorsmod.Wrap(types.ErrReplyNotFound, "reply does not belong to this post")
		}
		if reply.Status == types.ReplyStatus_REPLY_STATUS_DELETED {
			return nil, errorsmod.Wrap(types.ErrReplyDeleted, fmt.Sprintf("reply %d has been deleted", msg.ReplyId))
		}
		if reply.Status == types.ReplyStatus_REPLY_STATUS_HIDDEN {
			return nil, errorsmod.Wrap(types.ErrReplyHidden, fmt.Sprintf("reply %d is hidden", msg.ReplyId))
		}
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Check existing reaction
	existing, found := k.GetReaction(ctx, msg.PostId, msg.ReplyId, msg.Creator)
	if found {
		if existing.ReactionType == msg.ReactionType {
			// Same type - no-op
			return &types.MsgReactResponse{}, nil
		}

		// Different type - change reaction
		oldType := existing.ReactionType
		counts := k.GetReactionCounts(ctx, msg.PostId, msg.ReplyId)
		adjustReactionCount(&counts, oldType, -1)
		adjustReactionCount(&counts, msg.ReactionType, 1)
		k.SetReactionCounts(ctx, msg.PostId, msg.ReplyId, counts)

		existing.ReactionType = msg.ReactionType
		k.SetReaction(ctx, existing)

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("blog.reaction.changed",
			sdk.NewAttribute("creator", msg.Creator),
			sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.PostId)),
			sdk.NewAttribute("reply_id", fmt.Sprintf("%d", msg.ReplyId)),
			sdk.NewAttribute("old_type", oldType.String()),
			sdk.NewAttribute("new_type", msg.ReactionType.String()),
		))

		return &types.MsgReactResponse{}, nil
	}

	// New reaction
	if err := k.checkRateLimit(ctx, "reaction", creatorAddr, params.MaxReactionsPerDay); err != nil {
		return nil, err
	}

	// Charge reaction fee
	if !params.ReactionFeeExempt && params.ReactionFee.IsPositive() {
		if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, creatorAddr, types.ModuleName, sdk.NewCoins(params.ReactionFee)); err != nil {
			return nil, err
		}
		if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, sdk.NewCoins(params.ReactionFee)); err != nil {
			return nil, err
		}
	}

	// Store reaction
	k.SetReaction(ctx, types.Reaction{
		Creator:      msg.Creator,
		ReactionType: msg.ReactionType,
		PostId:       msg.PostId,
		ReplyId:      msg.ReplyId,
	})

	// Increment count
	counts := k.GetReactionCounts(ctx, msg.PostId, msg.ReplyId)
	adjustReactionCount(&counts, msg.ReactionType, 1)
	k.SetReactionCounts(ctx, msg.PostId, msg.ReplyId, counts)

	// Increment rate limit
	k.incrementRateLimit(ctx, "reaction", creatorAddr)

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("blog.reaction.added",
		sdk.NewAttribute("creator", msg.Creator),
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.PostId)),
		sdk.NewAttribute("reply_id", fmt.Sprintf("%d", msg.ReplyId)),
		sdk.NewAttribute("reaction_type", msg.ReactionType.String()),
	))

	return &types.MsgReactResponse{}, nil
}
