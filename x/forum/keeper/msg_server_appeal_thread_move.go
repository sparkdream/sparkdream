package keeper

import (
	"context"
	"encoding/json"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) AppealThreadMove(ctx context.Context, msg *types.MsgAppealThreadMove) (*types.MsgAppealThreadMoveResponse, error) {
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

	// Verify appellant is the thread author
	if post.Author != msg.Creator {
		return nil, errorsmod.Wrap(types.ErrNotThreadAuthor, "only the thread author can appeal a move")
	}

	// Check move record exists (sentinel move vs gov move)
	moveRecord, err := k.ThreadMoveRecord.Get(ctx, msg.RootId)
	if err != nil {
		// No move record means governance authority moved this thread or no move occurred
		return nil, errorsmod.Wrap(types.ErrGovLockNotAppealable,
			"governance authority moves must be appealed via governance action appeal")
	}

	// Check appeal not already filed
	if moveRecord.AppealPending {
		return nil, types.ErrMoveAppealAlreadyFiled
	}

	// Check appeal cooldown
	cooldownEnd := moveRecord.MovedAt + types.DefaultMoveAppealCooldown
	if now < cooldownEnd {
		return nil, errorsmod.Wrapf(types.ErrAppealCooldown,
			"must wait until %d to appeal", cooldownEnd)
	}

	// Charge move_appeal_fee to appellant and escrow it
	if params.MoveAppealFee.IsPositive() {
		creatorAddr, _ := sdk.AccAddressFromBech32(msg.Creator)
		if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, creatorAddr, types.ModuleName, sdk.NewCoins(params.MoveAppealFee)); err != nil {
			return nil, errorsmod.Wrap(err, "failed to charge move appeal fee")
		}
	}

	// Create appeal initiative in x/rep (stub)
	payload, _ := json.Marshal(map[string]interface{}{
		"thread_id":            msg.RootId,
		"sentinel_addr":        moveRecord.Sentinel,
		"appellant_addr":       msg.Creator,
		"original_category_id": moveRecord.OriginalCategoryId,
		"new_category_id":      moveRecord.NewCategoryId,
		"move_reason":          moveRecord.MoveReason,
	})

	deadline := now + types.DefaultMoveAppealDeadline
	initiativeID, err := k.CreateAppealInitiative(ctx, "THREAD_MOVE_APPEAL", payload, deadline)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to create appeal initiative")
	}

	// Update move record with appeal info
	moveRecord.AppealPending = true
	moveRecord.InitiativeId = initiativeID

	if err := k.ThreadMoveRecord.Set(ctx, msg.RootId, moveRecord); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update move record")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"thread_move_appeal_filed",
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", msg.RootId)),
			sdk.NewAttribute("appellant", msg.Creator),
			sdk.NewAttribute("sentinel", moveRecord.Sentinel),
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
			sdk.NewAttribute("deadline", fmt.Sprintf("%d", deadline)),
		),
	)

	return &types.MsgAppealThreadMoveResponse{}, nil
}
