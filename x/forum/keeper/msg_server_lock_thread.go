package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) LockThread(ctx context.Context, msg *types.MsgLockThread) (*types.MsgLockThreadResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Check forum_paused param
	params, err := k.Params.Get(ctx)
	if err != nil {
		params = types.DefaultParams()
	}
	if params.ForumPaused {
		return nil, types.ErrForumPaused
	}

	// Load post
	post, err := k.Post.Get(ctx, msg.RootId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d not found", msg.RootId))
	}

	// Check this is a root post
	if post.ParentId != 0 {
		return nil, types.ErrNotRootPost
	}

	// Check thread is not already locked
	if post.Locked {
		return nil, types.ErrThreadAlreadyLocked
	}

	// Check if sender is governance authority or sentinel
	isGovAuthority := k.IsGovAuthority(ctx, msg.Creator)

	if !isGovAuthority {
		// Check moderation_paused for sentinels
		if params.ModerationPaused {
			return nil, types.ErrModerationPaused
		}

		// Sentinel locking has higher requirements
		repTier := k.GetRepTier(ctx, msg.Creator)
		if repTier < types.DefaultMinRepTierThreadLock {
			return nil, errorsmod.Wrapf(types.ErrInsufficientReputation,
				"tier %d required for locking, have %d", types.DefaultMinRepTierThreadLock, repTier)
		}

		// Load sentinel activity
		sentinelActivity, err := k.SentinelActivity.Get(ctx, msg.Creator)
		if err != nil {
			return nil, errorsmod.Wrap(types.ErrNotSentinel, "not a registered sentinel")
		}

		// Check bond status
		if sentinelActivity.BondStatus == types.SentinelBondStatus_SENTINEL_BOND_STATUS_DEMOTED {
			return nil, types.ErrSentinelDemoted
		}

		// Check cooldown
		if sentinelActivity.OverturnCooldownUntil > now {
			return nil, errorsmod.Wrapf(types.ErrSentinelCooldown,
				"cooldown until %d", sentinelActivity.OverturnCooldownUntil)
		}

		// Check lock limit
		if sentinelActivity.EpochLocks >= types.DefaultMaxSentinelLocksPerEpoch {
			return nil, types.ErrLockLimitExceeded
		}

		// Check higher bond requirement for locking
		currentBond, _ := math.NewIntFromString(sentinelActivity.CurrentBond)
		if sentinelActivity.CurrentBond == "" {
			currentBond = math.ZeroInt()
		}
		minLockBond := math.NewInt(2000) // DefaultMinSentinelLockBond = 2x normal bond
		if currentBond.LT(minLockBond) {
			return nil, errorsmod.Wrapf(types.ErrInsufficientLockBond,
				"need %s DREAM bonded for locking, have %s", minLockBond.String(), currentBond.String())
		}

		// Check higher backing requirement
		backing := k.GetSentinelBacking(ctx, msg.Creator)
		minLockBacking := math.NewInt(20000) // DefaultMinSentinelLockBacking = 2x normal backing
		if backing.LT(minLockBacking) {
			return nil, errorsmod.Wrapf(types.ErrInsufficientLockBacking,
				"need %s DREAM backing for locking, have %s", minLockBacking.String(), backing.String())
		}

		// Reason required for sentinels
		if msg.Reason == "" {
			return nil, types.ErrLockReasonRequired
		}

		// Create lock record for appeal tracking
		lockRecord := types.ThreadLockRecord{
			RootId:                  msg.RootId,
			Sentinel:                msg.Creator,
			LockedAt:                now,
			SentinelBondSnapshot:    sentinelActivity.CurrentBond,
			SentinelBackingSnapshot: backing.String(),
			LockReason:              msg.Reason,
			AppealPending:           false,
			InitiativeId:            0,
		}

		if err := k.ThreadLockRecord.Set(ctx, msg.RootId, lockRecord); err != nil {
			return nil, errorsmod.Wrap(err, "failed to store lock record")
		}

		// Update sentinel activity
		sentinelActivity.TotalLocks++
		sentinelActivity.EpochLocks++

		if err := k.SentinelActivity.Set(ctx, msg.Creator, sentinelActivity); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update sentinel activity")
		}
	}

	// Update post
	post.Locked = true
	post.LockedBy = msg.Creator
	post.LockedAt = now
	post.LockReason = msg.Reason

	if err := k.Post.Set(ctx, msg.RootId, post); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update post")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"thread_locked",
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", msg.RootId)),
			sdk.NewAttribute("locked_by", msg.Creator),
			sdk.NewAttribute("reason", msg.Reason),
			sdk.NewAttribute("is_gov_authority", fmt.Sprintf("%t", isGovAuthority)),
		),
	)

	return &types.MsgLockThreadResponse{}, nil
}
