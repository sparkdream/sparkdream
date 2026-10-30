package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) FollowThread(ctx context.Context, msg *types.MsgFollowThread) (*types.MsgFollowThreadResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	if err := k.requireForumNotPaused(ctx); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Verify thread exists (root post with parent_id = 0)
	rootPost, err := k.Post.Get(ctx, msg.ThreadId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("thread %d not found", msg.ThreadId))
	}
	if rootPost.ParentId != 0 {
		return nil, errorsmod.Wrap(types.ErrNotRootPost, "can only follow root posts (threads)")
	}
	if rootPost.Status != types.PostStatus_POST_STATUS_ACTIVE {
		return nil, errorsmod.Wrapf(types.ErrThreadNotActive, "thread %d is %s", msg.ThreadId, rootPost.Status)
	}

	// Create follow key (address:threadId)
	followKey := fmt.Sprintf("%s:%d", msg.Creator, msg.ThreadId)

	// Check if already following
	_, err = k.ThreadFollow.Get(ctx, followKey)
	if err == nil {
		return nil, types.ErrAlreadyFollowing
	}

	// Check follow rate limit
	if err := k.checkAndUpdateFollowLimit(ctx, msg.Creator, now); err != nil {
		return nil, err
	}

	// Create follow record
	follow := types.ThreadFollow{
		ThreadId:   msg.ThreadId,
		Follower:   msg.Creator,
		FollowedAt: now,
	}

	if err := k.ThreadFollow.Set(ctx, followKey, follow); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store follow record")
	}
	// FORUM-S2-8: maintain bidirectional indexes for paginated lookups by
	// thread (ThreadFollowers query) and by follower (UserFollowedThreads).
	if err := k.FollowersByThread.Set(ctx, collections.Join(msg.ThreadId, msg.Creator)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update followers index")
	}
	if err := k.ThreadsByFollower.Set(ctx, collections.Join(msg.Creator, msg.ThreadId)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update follower index")
	}

	// Update follow count
	followCount, err := k.ThreadFollowCount.Get(ctx, msg.ThreadId)
	if err != nil {
		followCount = types.ThreadFollowCount{
			ThreadId:      msg.ThreadId,
			FollowerCount: 0,
		}
	}
	followCount.FollowerCount++

	if err := k.ThreadFollowCount.Set(ctx, msg.ThreadId, followCount); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update follow count")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"thread_followed",
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", msg.ThreadId)),
			sdk.NewAttribute("follower", msg.Creator),
		),
	)

	return &types.MsgFollowThreadResponse{}, nil
}

// checkAndUpdateFollowLimit checks and updates the follow rate limit for a user.
func (k msgServer) checkAndUpdateFollowLimit(ctx context.Context, addr string, now int64) error {
	// Use a separate key for follow limit (prefix with "follow_")
	limitKey := types.UserReactionLimitStoreKey(types.ReactionLimitKind_REACTION_LIMIT_KIND_FOLLOW, addr)

	maxFollowsPerDay := types.DefaultMaxFollowsPerDay
	if params, perr := k.Params.Get(ctx); perr == nil && params.MaxFollowsPerDay > 0 {
		maxFollowsPerDay = params.MaxFollowsPerDay
	}

	reactionLimit, err := k.UserReactionLimit.Get(ctx, limitKey)
	if err != nil {
		// Create new follow limit record
		reactionLimit = types.UserReactionLimit{
			UserAddress:      addr,
			Kind:             types.ReactionLimitKind_REACTION_LIMIT_KIND_FOLLOW,
			CurrentDayCount:  0,
			PreviousDayCount: 0,
			CurrentDayStart:  now,
		}
	}

	// Day rotation (24h window)
	const dayDuration int64 = 86400
	if now-reactionLimit.CurrentDayStart >= dayDuration {
		reactionLimit.PreviousDayCount = reactionLimit.CurrentDayCount
		reactionLimit.CurrentDayCount = 0
		reactionLimit.CurrentDayStart = now
	}

	// Sliding-window count, evaluated in exact integer math.
	if slidingWindowAtLimit(uint64(reactionLimit.CurrentDayCount), uint64(reactionLimit.PreviousDayCount),
		now-reactionLimit.CurrentDayStart, dayDuration, uint64(maxFollowsPerDay)) {
		return types.ErrFollowLimitExceeded
	}

	// Update follow limit
	reactionLimit.CurrentDayCount++

	if err := k.UserReactionLimit.Set(ctx, limitKey, reactionLimit); err != nil {
		return errorsmod.Wrap(err, "failed to update follow limit")
	}

	return nil
}
