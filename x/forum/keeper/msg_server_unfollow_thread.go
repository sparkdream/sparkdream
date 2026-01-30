package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) UnfollowThread(ctx context.Context, msg *types.MsgUnfollowThread) (*types.MsgUnfollowThreadResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Create follow key (address:threadId)
	followKey := fmt.Sprintf("%s:%d", msg.Creator, msg.ThreadId)

	// Check if following
	_, err := k.ThreadFollow.Get(ctx, followKey)
	if err != nil {
		return nil, types.ErrNotFollowing
	}

	// Remove follow record
	if err := k.ThreadFollow.Remove(ctx, followKey); err != nil {
		return nil, errorsmod.Wrap(err, "failed to remove follow record")
	}

	// Update follow count
	followCount, err := k.ThreadFollowCount.Get(ctx, msg.ThreadId)
	if err == nil && followCount.FollowerCount > 0 {
		followCount.FollowerCount--
		if err := k.ThreadFollowCount.Set(ctx, msg.ThreadId, followCount); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update follow count")
		}
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"thread_unfollowed",
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", msg.ThreadId)),
			sdk.NewAttribute("follower", msg.Creator),
		),
	)

	return &types.MsgUnfollowThreadResponse{}, nil
}
