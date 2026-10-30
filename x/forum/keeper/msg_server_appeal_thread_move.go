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
	if !k.canActAs(ctx, msg.Creator, post.Author) {
		return nil, errorsmod.Wrap(types.ErrNotThreadAuthor, "only the thread author can appeal a move")
	}

	// Every move (sentinel, committee or council) writes a move record; the
	// record is dropped once the appeal window closes or an appeal settles,
	// so a missing record means no appealable move.
	moveRecord, err := k.ThreadMoveRecord.Get(ctx, msg.RootId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrGovLockNotAppealable,
			"no appealable move record: the thread was not moved, the appeal window closed or the move was already appealed")
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
	if deadline := moveExpiryAt(moveRecord); now >= deadline {
		return nil, errorsmod.Wrapf(types.ErrMoveAppealExpired, "move appeal window closed at %d", deadline)
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
	// Mark the record BEFORE opening the rep appeal (see AppealThreadLock).
	moveRecord.AppealPending = true
	if err := k.ThreadMoveRecord.Set(ctx, msg.RootId, moveRecord); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update move record")
	}
	if err := k.MoveRecordExpiry.Remove(ctx, collections.Join(moveExpiryAt(moveRecord), msg.RootId)); err != nil {
		return nil, err
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
