package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) FollowThread(ctx context.Context, msg *types.MsgFollowThread) (*types.MsgFollowThreadResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
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

	// Create follow key (address:threadId)
	followKey := fmt.Sprintf("%s:%d", msg.Creator, msg.ThreadId)

	// Check if already following
	_, err = k.ThreadFollow.Get(ctx, followKey)
	if err == nil {
		return nil, types.ErrAlreadyFollowing
	}

	// TODO: Check follow rate limit when params are fully implemented

	// Create follow record
	follow := types.ThreadFollow{
		ThreadId:   msg.ThreadId,
		Follower:   msg.Creator,
		FollowedAt: now,
	}

	if err := k.ThreadFollow.Set(ctx, followKey, follow); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store follow record")
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
