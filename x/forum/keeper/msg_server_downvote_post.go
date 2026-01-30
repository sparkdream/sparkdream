package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) DownvotePost(ctx context.Context, msg *types.MsgDownvotePost) (*types.MsgDownvotePostResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// TODO: Check reactions_enabled param

	// Load post
	post, err := k.Post.Get(ctx, msg.PostId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d not found", msg.PostId))
	}

	// Check post status - cannot vote on hidden/deleted/archived posts
	switch post.Status {
	case types.PostStatus_POST_STATUS_HIDDEN:
		return nil, types.ErrPostAlreadyHidden
	case types.PostStatus_POST_STATUS_DELETED:
		return nil, types.ErrPostDeleted
	case types.PostStatus_POST_STATUS_ARCHIVED:
		return nil, types.ErrPostArchived
	}

	// Check downvote rate limit (separate from upvote limit)
	if err := k.checkAndUpdateDownvoteLimit(ctx, msg.Creator, now); err != nil {
		return nil, err
	}

	// TODO: Burn downvote_deposit from creator
	// Downvotes require a SPARK deposit that is burned immediately (no refund)

	// Increment downvote count (counter-only system - no individual vote tracking)
	post.DownvoteCount++

	// Store updated post
	if err := k.Post.Set(ctx, msg.PostId, post); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update post")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"post_downvoted",
			sdk.NewAttribute("post_id", fmt.Sprintf("%d", msg.PostId)),
			sdk.NewAttribute("voter", msg.Creator),
		),
	)

	return &types.MsgDownvotePostResponse{}, nil
}

// checkAndUpdateDownvoteLimit checks and updates the downvote rate limit for a user.
func (k msgServer) checkAndUpdateDownvoteLimit(ctx context.Context, addr string, now int64) error {
	// Use a separate key for downvote limit (prefix with "downvote_")
	limitKey := "downvote_" + addr

	reactionLimit, err := k.UserReactionLimit.Get(ctx, limitKey)
	if err != nil {
		// Create new downvote limit record
		reactionLimit = types.UserReactionLimit{
			UserAddress:       addr,
			CurrentDayCount:   0,
			PreviousDayCount:  0,
			CurrentDayStart:   now,
		}
	}

	// Day rotation (24h window)
	const dayDuration int64 = 86400
	if now-reactionLimit.CurrentDayStart >= dayDuration {
		reactionLimit.PreviousDayCount = reactionLimit.CurrentDayCount
		reactionLimit.CurrentDayCount = 0
		reactionLimit.CurrentDayStart = now
	}

	// Calculate effective count using sliding window approximation
	var overlapRatio float64
	elapsed := now - reactionLimit.CurrentDayStart
	if elapsed < dayDuration {
		overlapRatio = float64(dayDuration-elapsed) / float64(dayDuration)
	}
	effectiveCount := float64(reactionLimit.CurrentDayCount) + float64(reactionLimit.PreviousDayCount)*overlapRatio

	if effectiveCount >= float64(types.DefaultMaxDownvotesPerDay) {
		return types.ErrDownvoteLimitExceeded
	}

	// Update downvote limit
	reactionLimit.CurrentDayCount++

	if err := k.UserReactionLimit.Set(ctx, limitKey, reactionLimit); err != nil {
		return errorsmod.Wrap(err, "failed to update downvote limit")
	}

	return nil
}
