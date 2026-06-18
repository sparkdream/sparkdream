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

	// Resolve which moderation authority this lock invokes. An account can be
	// BOTH a bonded sentinel and a Commons Operations Committee member, so the
	// choice must be explicit rather than an implicit upgrade to the council
	// path (which writes no lock record and is unlockable only by the council).
	// See docs/HANDOFF_HIDE_AUTHORITY_DISAMBIGUATION.md.
	//
	// Lock eligibility = bonded sentinel in NORMAL/RECOVERY with a valid bond,
	// meeting the lock rep-tier and the 2x (2000 DREAM) bond floor. The backing
	// floor, cooldown, and epoch limit are operational gates applied once the
	// sentinel path is taken, not part of the authority decision.
	// Lock-eligibility floors are governance-tunable + bounded (Params 56-58),
	// derived from the base sentinel bond/tier so there is a single source of
	// truth. Read directly — Validate guarantees the bond/multiplier are
	// positive; lock_min_rep_tier == 0 means "no rep-tier floor".
	minLockBond := params.LockMinBondInt()
	lockMinRepTier := params.LockMinRepTier
	var (
		bondSnapshot     string
		sentinelEligible bool
		sentinelErr      error
	)
	if k.repKeeper == nil {
		sentinelErr = errorsmod.Wrap(types.ErrNotSentinel, "rep keeper not wired")
	} else if repTier := k.GetRepTier(ctx, msg.Creator); repTier < lockMinRepTier {
		sentinelErr = errorsmod.Wrapf(types.ErrInsufficientReputation,
			"tier %d required for locking, have %d", lockMinRepTier, repTier)
	} else if br, berr := k.repKeeper.GetBondedRole(ctx, reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, msg.Creator); berr != nil {
		sentinelErr = errorsmod.Wrap(types.ErrNotSentinel, "not a registered sentinel")
	} else if _, ok := math.NewIntFromString(br.CurrentBond); !ok || br.CurrentBond == "" {
		sentinelErr = errorsmod.Wrapf(types.ErrInvalidAmount, "invalid bonded role current_bond: %q", br.CurrentBond)
	} else if br.BondStatus != reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL &&
		br.BondStatus != reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_RECOVERY {
		sentinelErr = types.ErrSentinelDemoted
	} else if currentBond := parseIntOrZero(br.CurrentBond); currentBond.LT(minLockBond) {
		// Higher bond requirement for locking (2x normal bond, in DREAM micro-units).
		sentinelErr = errorsmod.Wrapf(types.ErrInsufficientLockBond,
			"need %s udream bonded for locking, have %s", minLockBond.String(), currentBond.String())
	} else {
		bondSnapshot = br.CurrentBond
		sentinelEligible = true
	}

	isCouncil := k.isCouncilAuthorized(ctx, msg.Creator, "commons", "operations")

	isGovAuthority, err := resolveModerationAuthority(msg.Authority, sentinelEligible, sentinelErr, isCouncil)
	if err != nil {
		return nil, err
	}

	if !isGovAuthority {
		if params.ModerationPaused {
			return nil, types.ErrModerationPaused
		}

		local, err := k.SentinelActivity.Get(ctx, msg.Creator)
		if err != nil {
			local = types.SentinelActivity{Address: msg.Creator}
		}
		if local.OverturnCooldownUntil > now {
			return nil, errorsmod.Wrapf(types.ErrSentinelCooldown,
				"cooldown until %d", local.OverturnCooldownUntil)
		}
		if local.EpochLocks >= params.MaxSentinelLocksPerEpoch {
			return nil, types.ErrLockLimitExceeded
		}

		backing := k.GetSentinelBacking(ctx, msg.Creator)
		minLockBacking := params.LockBackingAmountInt()
		if backing.LT(minLockBacking) {
			return nil, errorsmod.Wrapf(types.ErrInsufficientLockBacking,
				"need %s udream backing for locking, have %s", minLockBacking.String(), backing.String())
		}

		if msg.Reason == "" {
			return nil, types.ErrLockReasonRequired
		}

		// Reserve slash amount against the sentinel's bond so overturned
		// appeals have funds to slash. Mirrors the HidePost reservation path.
		slashAmount := params.SentinelSlashAmountInt()
		if err := k.repKeeper.ReserveBond(ctx, reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, msg.Creator, slashAmount); err != nil {
			return nil, errorsmod.Wrap(err, "insufficient bond to lock")
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
			CommittedAmount:         slashAmount.String(),
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
