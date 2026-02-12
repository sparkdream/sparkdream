package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) UnlockThread(ctx context.Context, msg *types.MsgUnlockThread) (*types.MsgUnlockThreadResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Load post
	post, err := k.Post.Get(ctx, msg.RootId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d not found", msg.RootId))
	}

	// Check thread is locked
	if !post.Locked {
		return nil, types.ErrThreadNotLocked
	}

	// Check if sender is operations committee
	isGovAuthority := k.IsCouncilAuthorized(ctx, msg.Creator, "commons", "operations")

	if !isGovAuthority {
		// Sentinel can only unlock threads they locked
		if post.LockedBy != msg.Creator {
			return nil, errorsmod.Wrap(types.ErrUnauthorized, "sentinels can only unlock threads they locked")
		}

		// Check lock record exists (sentinel lock)
		lockRecord, err := k.ThreadLockRecord.Get(ctx, msg.RootId)
		if err != nil {
			return nil, errorsmod.Wrap(types.ErrUnauthorized, "no lock record found - this was an governance authority lock")
		}

		// Check no appeal is pending
		if lockRecord.AppealPending {
			return nil, types.ErrAppealPending
		}

		// Check lock appeal window hasn't expired
		lockAppealDeadline := lockRecord.LockedAt + types.DefaultLockAppealDeadline
		if now >= lockAppealDeadline {
			return nil, errorsmod.Wrapf(types.ErrLockAppealExpired,
				"lock appeal window expired at %d", lockAppealDeadline)
		}

		// Remove lock record (atomic coordination point)
		if err := k.ThreadLockRecord.Remove(ctx, msg.RootId); err != nil {
			return nil, errorsmod.Wrap(err, "failed to remove lock record")
		}
	}

	// Update post
	post.Locked = false
	post.LockedBy = ""
	post.LockedAt = 0
	post.LockReason = ""

	if err := k.Post.Set(ctx, msg.RootId, post); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update post")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"thread_unlocked",
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", msg.RootId)),
			sdk.NewAttribute("unlocked_by", msg.Creator),
			sdk.NewAttribute("is_gov_authority", fmt.Sprintf("%t", isGovAuthority)),
		),
	)

	return &types.MsgUnlockThreadResponse{}, nil
}
