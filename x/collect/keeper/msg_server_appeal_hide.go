package keeper

import (
	"context"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

func (k msgServer) AppealHide(ctx context.Context, msg *types.MsgAppealHide) (*types.MsgAppealHideResponse, error) {
	creatorAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()

	// HideRecord must exist
	hideRecord, err := k.HideRecord.Get(ctx, msg.HideRecordId)
	if err != nil {
		return nil, types.ErrHideRecordNotFound
	}

	// HideRecord must not already be resolved
	if hideRecord.Resolved {
		return nil, types.ErrHideRecordResolved
	}

	// HideRecord must not already be appealed
	if hideRecord.Appealed {
		return nil, types.ErrAppealAlreadyFiled
	}

	// Creator must be owner of the hidden content (resolve target from HideRecord)
	coll, err := k.GetCollectionForTarget(ctx, hideRecord.TargetType, hideRecord.TargetId)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to resolve target collection")
	}
	if coll.Owner != msg.Creator {
		return nil, types.ErrNotContentOwner
	}

	// Get params
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Must wait appeal_cooldown_blocks after hide
	if blockHeight < hideRecord.HiddenAt+params.AppealCooldownBlocks {
		return nil, types.ErrAppealCooldown
	}

	// Appeal deadline must not have passed
	if blockHeight >= hideRecord.AppealDeadline {
		return nil, types.ErrHideRecordResolved
	}

	// Escrow appeal_fee SPARK from creator to module
	if err := k.EscrowSPARK(ctx, creatorAddr, params.AppealFee); err != nil {
		return nil, errorsmod.Wrap(types.ErrInsufficientFunds, err.Error())
	}

	// Remove old HideRecordExpiry entry
	k.HideRecordExpiry.Remove(ctx, collections.Join(hideRecord.AppealDeadline, hideRecord.Id)) //nolint:errcheck

	// Set appealed=true and update appeal_deadline
	hideRecord.Appealed = true
	newDeadline := blockHeight + params.AppealDeadlineBlocks
	hideRecord.AppealDeadline = newDeadline

	if err := k.HideRecord.Set(ctx, hideRecord.Id, hideRecord); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update hide record")
	}

	// Re-index in HideRecordExpiry with new deadline
	if err := k.HideRecordExpiry.Set(ctx, collections.Join(newDeadline, hideRecord.Id)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to set hide record expiry")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("hide_appealed",
		sdk.NewAttribute("hide_record_id", strconv.FormatUint(hideRecord.Id, 10)),
		sdk.NewAttribute("appellant", msg.Creator),
		sdk.NewAttribute("target_id", strconv.FormatUint(hideRecord.TargetId, 10)),
		sdk.NewAttribute("target_type", hideRecord.TargetType.String()),
		sdk.NewAttribute("new_appeal_deadline", strconv.FormatInt(newDeadline, 10)),
	))

	return &types.MsgAppealHideResponse{}, nil
}
