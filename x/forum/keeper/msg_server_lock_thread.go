package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
)

func (k msgServer) LockThread(ctx context.Context, msg *types.MsgLockThread) (*types.MsgLockThreadResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	params, err := k.Params.Get(ctx)
	if err != nil {
		params = types.DefaultParams()
	}
	if params.ForumPaused {
		return nil, types.ErrForumPaused
	}

	post, err := k.Post.Get(ctx, msg.RootId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("post %d not found", msg.RootId))
	}

	if post.ParentId != 0 {
		return nil, types.ErrNotRootPost
	}

	if post.Locked {
		return nil, types.ErrThreadAlreadyLocked
	}

	isGovAuthority := k.isCouncilAuthorized(ctx, msg.Creator, "commons", "operations")

	var bondSnapshot string
	if !isGovAuthority {
		if params.ModerationPaused {
			return nil, types.ErrModerationPaused
		}

		repTier := k.GetRepTier(ctx, msg.Creator)
		if repTier < types.DefaultMinRepTierThreadLock {
			return nil, errorsmod.Wrapf(types.ErrInsufficientReputation,
				"tier %d required for locking, have %d", types.DefaultMinRepTierThreadLock, repTier)
		}

		if k.repKeeper == nil {
			return nil, errorsmod.Wrap(types.ErrNotSentinel, "rep keeper not wired")
		}
		br, err := k.repKeeper.GetBondedRole(ctx, reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, msg.Creator)
		if err != nil {
			return nil, errorsmod.Wrap(types.ErrNotSentinel, "not a registered sentinel")
		}
		bondSnapshot = br.CurrentBond

		if br.BondStatus == reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED {
			return nil, types.ErrSentinelDemoted
		}

		local, err := k.SentinelActivity.Get(ctx, msg.Creator)
		if err != nil {
			local = types.SentinelActivity{Address: msg.Creator}
		}
		if local.OverturnCooldownUntil > now {
			return nil, errorsmod.Wrapf(types.ErrSentinelCooldown,
				"cooldown until %d", local.OverturnCooldownUntil)
		}
		if local.EpochLocks >= types.DefaultMaxSentinelLocksPerEpoch {
			return nil, types.ErrLockLimitExceeded
		}

		// Higher bond requirement for locking (2x normal bond).
		currentBond := parseIntOrZero(br.CurrentBond)
		minLockBond := math.NewInt(2000)
		if currentBond.LT(minLockBond) {
			return nil, errorsmod.Wrapf(types.ErrInsufficientLockBond,
				"need %s DREAM bonded for locking, have %s", minLockBond.String(), currentBond.String())
		}

		backing := k.GetSentinelBacking(ctx, msg.Creator)
		minLockBacking := math.NewInt(20000)
		if backing.LT(minLockBacking) {
			return nil, errorsmod.Wrapf(types.ErrInsufficientLockBacking,
				"need %s DREAM backing for locking, have %s", minLockBacking.String(), backing.String())
		}

		if msg.Reason == "" {
			return nil, types.ErrLockReasonRequired
		}

		lockRecord := types.ThreadLockRecord{
			RootId:                  msg.RootId,
			Sentinel:                msg.Creator,
			LockedAt:                now,
			SentinelBondSnapshot:    bondSnapshot,
			SentinelBackingSnapshot: backing.String(),
			LockReason:              msg.Reason,
			AppealPending:           false,
			InitiativeId:            0,
		}
		if err := k.ThreadLockRecord.Set(ctx, msg.RootId, lockRecord); err != nil {
			return nil, errorsmod.Wrap(err, "failed to store lock record")
		}

		local.TotalLocks++
		local.EpochLocks++
		if err := k.SentinelActivity.Set(ctx, msg.Creator, local); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update sentinel activity")
		}

		_ = k.repKeeper.RecordActivity(ctx, reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, msg.Creator)
	}

	post.Locked = true
	post.LockedBy = msg.Creator
	post.LockedAt = now
	post.LockReason = msg.Reason

	if err := k.Post.Set(ctx, msg.RootId, post); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update post")
	}

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

// parseIntOrZero is defined in msg_server_hide_post.go-adjacent helpers.
// Implemented inline here to avoid importing rep keeper internals.
func parseIntOrZero(s string) math.Int {
	if s == "" {
		return math.ZeroInt()
	}
	v, ok := math.NewIntFromString(s)
	if !ok {
		return math.ZeroInt()
	}
	return v
}
