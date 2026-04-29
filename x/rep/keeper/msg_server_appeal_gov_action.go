package keeper

import (
	"context"
	"encoding/json"
	"fmt"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AppealGovAction allows members to appeal governance actions.
func (k msgServer) AppealGovAction(ctx context.Context, msg *types.MsgAppealGovAction) (*types.MsgAppealGovActionResponse, error) {
	creatorBytes, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	creatorAddr := sdk.AccAddress(creatorBytes)
	if !k.IsMember(ctx, creatorAddr) {
		return nil, errorsmod.Wrap(types.ErrNotMember, "only members can appeal governance actions")
	}

	actionType := types.GovActionType(msg.ActionType)
	if actionType == types.GovActionType_GOV_ACTION_TYPE_UNSPECIFIED {
		return nil, errorsmod.Wrap(types.ErrInvalidReasonCode, "invalid action type")
	}

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

	// Charge the appellant an SPARK bond. Held at the appeal-bond escrow
	// sub-address until the appeal is resolved; returned / burned per verdict.
	bondAmount := math.NewInt(types.DefaultAppealBondAmount)
	bondCoins := sdk.NewCoins(sdk.NewCoin(types.RewardDenom, bondAmount))
	if err := k.bankKeeper.SendCoins(ctx, creatorAddr, AppealBondEscrowAddress(), bondCoins); err != nil {
		return nil, errorsmod.Wrap(types.ErrInsufficientBalance, fmt.Sprintf("failed to charge appeal bond: %s", err.Error()))
	}

	payload := map[string]interface{}{
		"type":          "gov_action_appeal",
		"action_type":   actionType.String(),
		"action_target": msg.ActionTarget,
		"appellant":     msg.Creator,
		"reason":        msg.AppealReason,
	}
	payloadBytes, _ := json.Marshal(payload)

	deadline := now + types.DefaultAppealDeadline

	initiativeID, err := k.CreateAppealInitiative(ctx, "gov_action_appeal", payloadBytes, deadline)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to create appeal initiative")
	}

	appealID, err := k.GovActionAppealSeq.Next(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to generate appeal ID")
	}

	appeal := types.GovActionAppeal{
		Id:           appealID,
		ActionType:   actionType,
		ActionTarget: msg.ActionTarget,
		Appellant:    msg.Creator,
		AppealReason: msg.AppealReason,
		AppealBond:   bondAmount.String(),
		CreatedAt:    now,
		Deadline:     deadline,
		Status:       types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING,
		InitiativeId: initiativeID,
	}

	if err := k.GovActionAppeal.Set(ctx, appealID, appeal); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store appeal")
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"gov_action_appealed",
			sdk.NewAttribute("appeal_id", fmt.Sprintf("%d", appealID)),
			sdk.NewAttribute("action_type", actionType.String()),
			sdk.NewAttribute("action_target", msg.ActionTarget),
			sdk.NewAttribute("appellant", msg.Creator),
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
			sdk.NewAttribute("appeal_bond", bondAmount.String()),
			sdk.NewAttribute("deadline", fmt.Sprintf("%d", deadline)),
		),
	)

	return &types.MsgAppealGovActionResponse{}, nil
}
