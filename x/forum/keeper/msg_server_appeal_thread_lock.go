package keeper

import (
	"context"
	"encoding/json"
	"fmt"

	"sparkdream/x/forum/types"

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
	cooldownEnd := lockRecord.LockedAt + types.DefaultLockAppealCooldown
	if now < cooldownEnd {
		return nil, errorsmod.Wrapf(types.ErrAppealCooldown,
			"must wait until %d to appeal", cooldownEnd)
	}

	// Charge lock_appeal_fee to appellant and escrow it
	if params.LockAppealFee.IsPositive() {
		creatorAddr, _ := sdk.AccAddressFromBech32(msg.Creator)
		if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, creatorAddr, types.ModuleName, sdk.NewCoins(params.LockAppealFee)); err != nil {
			return nil, errorsmod.Wrap(err, "failed to charge lock appeal fee")
		}
	}

	// Create appeal initiative in x/rep (stub)
	payload, _ := json.Marshal(map[string]interface{}{
		"thread_id":      msg.RootId,
		"sentinel_addr":  lockRecord.Sentinel,
		"appellant_addr": msg.Creator,
		"lock_reason":    lockRecord.LockReason,
	})

	deadline := now + types.DefaultLockAppealDeadline
	initiativeID, err := k.CreateAppealInitiative(ctx, "THREAD_LOCK_APPEAL", payload, deadline)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to create appeal initiative")
	}

	// Update lock record with appeal info
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
			sdk.NewAttribute("deadline", fmt.Sprintf("%d", deadline)),
		),
	)

	return &types.MsgAppealThreadLockResponse{}, nil
}
