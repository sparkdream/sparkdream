package keeper

import (
	"context"
	"encoding/json"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AppealGovAction allows members to appeal governance actions.
func (k msgServer) AppealGovAction(ctx context.Context, msg *types.MsgAppealGovAction) (*types.MsgAppealGovActionResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Verify creator is a member
	if !k.IsMember(ctx, msg.Creator) {
		return nil, errorsmod.Wrap(types.ErrNotMember, "only members can appeal governance actions")
	}

	actionType := types.GovActionType(msg.ActionType)
	if actionType == types.GovActionType_GOV_ACTION_TYPE_UNSPECIFIED {
		return nil, errorsmod.Wrap(types.ErrInvalidReasonCode, "invalid action type")
	}

	// Check appeal doesn't already exist
	appealIter, err := k.GovActionAppeal.Iterate(ctx, nil)
	if err == nil {
		defer appealIter.Close()
		for ; appealIter.Valid(); appealIter.Next() {
			appeal, _ := appealIter.Value()
			if appeal.ActionTarget == msg.ActionTarget && appeal.ActionType == actionType &&
				appeal.Status == types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING {
				return nil, errorsmod.Wrap(types.ErrAppealAlreadyFiled, "appeal already exists for this action")
			}
		}
	}

	// Create appeal initiative payload
	payload := map[string]interface{}{
		"type":          "gov_action_appeal",
		"action_type":   actionType.String(),
		"action_target": msg.ActionTarget,
		"appellant":     msg.Creator,
		"reason":        msg.AppealReason,
	}
	payloadBytes, _ := json.Marshal(payload)

	// Create appeal initiative
	initiativeID, err := k.CreateAppealInitiative(ctx, "gov_action_appeal", payloadBytes, now+types.DefaultAppealDeadline)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to create appeal initiative")
	}

	// Generate appeal ID
	appealID, err := k.GovActionAppealSeq.Next(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to generate appeal ID")
	}

	// Create appeal record
	appeal := types.GovActionAppeal{
		Id:           appealID,
		ActionType:   actionType,
		ActionTarget: msg.ActionTarget,
		Appellant:    msg.Creator,
		AppealReason: msg.AppealReason,
		CreatedAt:    now,
		Status:       types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING,
		InitiativeId: initiativeID,
	}

	if err := k.GovActionAppeal.Set(ctx, appealID, appeal); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store appeal")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"gov_action_appealed",
			sdk.NewAttribute("appeal_id", fmt.Sprintf("%d", appealID)),
			sdk.NewAttribute("action_type", actionType.String()),
			sdk.NewAttribute("action_target", msg.ActionTarget),
			sdk.NewAttribute("appellant", msg.Creator),
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
		),
	)

	return &types.MsgAppealGovActionResponse{}, nil
}
