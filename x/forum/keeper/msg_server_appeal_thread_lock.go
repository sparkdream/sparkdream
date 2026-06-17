package keeper

import (
	"context"
	"fmt"

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
	if post.Author != msg.Creator {
		return nil, errorsmod.Wrap(types.ErrNotThreadAuthor, "only the thread author can appeal a lock")
	}

	// Check lock record exists (sentinel lock vs gov lock)
	lockRecord, err := k.ThreadLockRecord.Get(ctx, msg.RootId)
	if err != nil {
		// No lock record means governance authority locked this thread
		return nil, errorsmod.Wrap(types.ErrGovLockNotAppealable,
			"governance authority locks must be appealed via governance action appeal")
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
	lockRecord.AppealPending = true
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
