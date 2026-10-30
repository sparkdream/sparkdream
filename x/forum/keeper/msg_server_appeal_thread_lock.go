package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) AppealThreadLock(ctx context.Context, msg *types.MsgAppealThreadLock) (*types.MsgAppealThreadLockResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Check appeals_paused param
	params, err := k.Params.Get(ctx)
	if err != nil {
		params = types.DefaultParams()
	}
	if params.AppealsPaused {
		return nil, types.ErrAppealsPaused
	}

	// Load post
	post, err := k.Post.Get(ctx, msg.RootId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("thread %d not found", msg.RootId))
	}

	// Check thread is locked
	if !post.Locked {
		return nil, types.ErrThreadNotLocked
	}

	// Verify appellant is the thread author
	if !k.canActAs(ctx, msg.Creator, post.Author) {
		return nil, errorsmod.Wrap(types.ErrNotThreadAuthor, "only the thread author can appeal a lock")
	}

	// Every lock (sentinel, committee or council) writes a lock record; the
	// record is dropped once the appeal window closes or an appeal settles,
	// so a missing record means the lock is no longer appealable.
	lockRecord, err := k.ThreadLockRecord.Get(ctx, msg.RootId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrGovLockNotAppealable,
			"no appealable lock record: the appeal window closed or the lock was already appealed")
	}

	// Check appeal not already filed
	if lockRecord.AppealPending {
		return nil, types.ErrLockAppealAlreadyFiled
	}

	// Check appeal cooldown
	cooldownEnd := lockRecord.LockedAt + params.LockAppealCooldown
	if now < cooldownEnd {
		return nil, errorsmod.Wrapf(types.ErrAppealCooldown,
			"must wait until %d to appeal", cooldownEnd)
	}
	// The appeal window closes at the lock-appeal deadline; past it the
	// record is finalized by the EndBlocker (the lock stood unchallenged).
	if deadline := lockExpiryAt(lockRecord); now >= deadline {
		return nil, errorsmod.Wrapf(types.ErrLockAppealExpired, "lock appeal window closed at %d", deadline)
	}

	// Open a moderation appeal through the unified x/rep GovActionAppeal path
	// (ActionType THREAD_LOCK, ActionTarget = root id). Charges the rep appeal
	// bond and creates the resolvable GovActionAppeal record + jury initiative.
	// The legacy per-action forum lock-appeal fee is superseded by the rep bond.
	if k.repKeeper == nil {
		return nil, errorsmod.Wrap(types.ErrGovLockNotAppealable, "rep keeper not wired")
	}
	creatorAddr, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	// Mark the record BEFORE opening the rep appeal: the appeal-target
	// adapter only honors THREAD_LOCK appeals whose record carries the flag
	// (see forumAppealRecord). Appealed records leave the expiry index; the
	// verdict settles them. A failure below reverts the whole message.
	lockRecord.AppealPending = true
	if err := k.ThreadLockRecord.Set(ctx, msg.RootId, lockRecord); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update lock record")
	}
	if err := k.LockRecordExpiry.Remove(ctx, collections.Join(lockExpiryAt(lockRecord), msg.RootId)); err != nil {
		return nil, err
	}

	_, initiativeID, err := k.repKeeper.CreateGovActionAppeal(
		ctx,
		reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_LOCK,
		fmt.Sprintf("%d", msg.RootId),
		creatorAddr,
		fmt.Sprintf("lock appeal: %s", lockRecord.LockReason),
	)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to create lock appeal")
	}

	// Update lock record with appeal info (blocks sentinel self-unlock while
	// the appeal is pending; see MsgUnlockThread).
	lockRecord.InitiativeId = initiativeID

	if err := k.ThreadLockRecord.Set(ctx, msg.RootId, lockRecord); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update lock record")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"thread_lock_appeal_filed",
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", msg.RootId)),
			sdk.NewAttribute("appellant", msg.Creator),
			sdk.NewAttribute("sentinel", lockRecord.Sentinel),
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
		),
	)

	return &types.MsgAppealThreadLockResponse{}, nil
}
