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

	creatorAddr := sdk.AccAddress(creatorBytes)
	if !k.IsMember(ctx, creatorAddr) {
		return nil, errorsmod.Wrap(types.ErrNotMember, "only members can appeal governance actions")
	}

	actionType := types.GovActionType(msg.ActionType)
	if actionType == types.GovActionType_GOV_ACTION_TYPE_UNSPECIFIED {
		return nil, errorsmod.Wrap(types.ErrInvalidReasonCode, "invalid action type")
	}

	if _, _, err := k.CreateGovActionAppeal(ctx, actionType, msg.ActionTarget, creatorAddr, msg.AppealReason); err != nil {
		return nil, err
	}

	return &types.MsgAppealGovActionResponse{}, nil
}

// CreateGovActionAppeal charges the appellant the standard SPARK appeal bond,
// opens a jury initiative, and records a pending GovActionAppeal. Shared by
// MsgAppealGovAction (lock/move/hide/etc.) and forum's pin-dispute path
// (ActionType REPLY_PIN) so every appeal resolves through the single audited
// ResolveGovActionAppeal machinery. Returns (appealID, initiativeID).
func (k Keeper) CreateGovActionAppeal(ctx context.Context, actionType types.GovActionType, actionTarget string, appellant sdk.AccAddress, reason string) (uint64, uint64, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// One pending appeal per (actionType, actionTarget).
	appealIter, err := k.GovActionAppeal.Iterate(ctx, nil)
	if err == nil {
		defer appealIter.Close()
		for ; appealIter.Valid(); appealIter.Next() {
			appeal, _ := appealIter.Value()
			if appeal.ActionTarget == actionTarget && appeal.ActionType == actionType &&
				appeal.Status == types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING {
				return 0, 0, errorsmod.Wrap(types.ErrAppealAlreadyFiled, "appeal already exists for this action")
			}
		}
	}

	// Charge the appellant an SPARK bond. Held at the appeal-bond escrow
	// sub-address until the appeal is resolved; returned / burned per verdict.
	bondAmount := math.NewInt(types.DefaultAppealBondAmount)
	bondCoins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), bondAmount))
	if err := k.bankKeeper.SendCoins(ctx, appellant, AppealBondEscrowAddress(), bondCoins); err != nil {
		return 0, 0, errorsmod.Wrap(types.ErrInsufficientBalance, fmt.Sprintf("failed to charge appeal bond: %s", err.Error()))
	}

	payload := map[string]interface{}{
		"type":          govActionAppealInitiativeType,
		"action_type":   actionType.String(),
		"action_target": actionTarget,
		"appellant":     appellant.String(),
		"reason":        reason,
	}
	payloadBytes, _ := json.Marshal(payload)

	deadline := now + types.DefaultAppealDeadline

	initiativeID, err := k.CreateAppealInitiative(ctx, govActionAppealInitiativeType, payloadBytes, deadline)
	if err != nil {
		return 0, 0, errorsmod.Wrap(err, "failed to create appeal initiative")
	}

	// Seat a jury on the appeal so it can be resolved by jury verdict (via
	// TallyJuryVotes) rather than only by manual committee override. Exclude the
	// parties to the dispute: the appellant, the action target, and (for forum
	// moderation actions) the accused sentinel. Best-effort — if no jury can be
	// seated the appeal still stands and falls back to committee resolution or
	// timeout.
	excludes := []string{appellant.String(), actionTarget}
	if fk := k.late.forumKeeper; fk != nil {
		if sentinel, sErr := fk.GetActionSentinel(ctx, actionType, actionTarget); sErr == nil && sentinel != "" {
			excludes = append(excludes, sentinel)
		}
	}
	if jurors, jErr := k.selectModerationAppealJury(ctx, initiativeID, excludes); jErr != nil {
		sdkCtx.Logger().Warn("failed to seat appeal jury", "initiative_id", initiativeID, "error", jErr)
	} else if len(jurors) > 0 {
		if review, rErr := k.GetJuryReview(ctx, initiativeID); rErr == nil {
			review.Jurors = jurors
			review.RequiredVotes = uint32(k.appealRequiredVotes(ctx, len(jurors)))
			if err := k.JuryReview.Set(ctx, initiativeID, review); err != nil {
				sdkCtx.Logger().Warn("failed to attach appeal jury", "initiative_id", initiativeID, "error", err)
			} else if err := k.AddJuryReviewToJurorIndex(ctx, review); err != nil {
				sdkCtx.Logger().Warn("failed to index appeal jury seating", "initiative_id", initiativeID, "error", err)
			} else if err := k.RecordJurySeating(ctx, jurors); err != nil {
				// Best-effort, matching the rest of this block: a bookkeeping
				// failure must not block the appeal from being filed.
				sdkCtx.Logger().Warn("failed to record appeal jury seating", "initiative_id", initiativeID, "error", err)
			}
		}
	}

	appealID, err := k.GovActionAppealSeq.Next(ctx)
	if err != nil {
		return 0, 0, errorsmod.Wrap(err, "failed to generate appeal ID")
	}

	appeal := types.GovActionAppeal{
		Id:           appealID,
		ActionType:   actionType,
		ActionTarget: actionTarget,
		Appellant:    appellant.String(),
		AppealReason: reason,
		AppealBond:   bondAmount.String(),
		CreatedAt:    now,
		Deadline:     deadline,
		Status:       types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING,
		InitiativeId: initiativeID,
	}

	if err := k.GovActionAppeal.Set(ctx, appealID, appeal); err != nil {
		return 0, 0, errorsmod.Wrap(err, "failed to store appeal")
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"gov_action_appealed",
			sdk.NewAttribute("appeal_id", fmt.Sprintf("%d", appealID)),
			sdk.NewAttribute("action_type", actionType.String()),
			sdk.NewAttribute("action_target", actionTarget),
			sdk.NewAttribute("appellant", appellant.String()),
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
			sdk.NewAttribute("appeal_bond", bondAmount.String()),
			sdk.NewAttribute("deadline", fmt.Sprintf("%d", deadline)),
		),
	)

	return appealID, initiativeID, nil
}
