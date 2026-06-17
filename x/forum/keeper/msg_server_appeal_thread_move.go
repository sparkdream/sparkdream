package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"

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
	cooldownEnd := moveRecord.MovedAt + params.MoveAppealCooldown
	if now < cooldownEnd {
		return nil, errorsmod.Wrapf(types.ErrAppealCooldown,
			"must wait until %d to appeal", cooldownEnd)
	}

	// Open a moderation appeal through the unified x/rep GovActionAppeal path
	// (ActionType THREAD_MOVE, ActionTarget = root id). Charges the rep appeal
	// bond and creates the resolvable GovActionAppeal record + jury initiative.
	// The legacy per-action forum move-appeal fee is superseded by the rep bond.
	if k.repKeeper == nil {
		return nil, errorsmod.Wrap(types.ErrGovLockNotAppealable, "rep keeper not wired")
	}
	creatorAddr, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	_, initiativeID, err := k.repKeeper.CreateGovActionAppeal(
		ctx,
		reptypes.GovActionType_GOV_ACTION_TYPE_THREAD_MOVE,
		fmt.Sprintf("%d", msg.RootId),
		creatorAddr,
		fmt.Sprintf("move appeal: %s", moveRecord.MoveReason),
	)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to create move appeal")
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
		),
	)

	return &types.MsgAppealThreadMoveResponse{}, nil
}
