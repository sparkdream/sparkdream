package keeper

import (
	"context"
	"encoding/json"
	"fmt"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AppealGovAction lets a member appeal a member action applied to them:
// a warning, demotion or zeroing from MsgResolveMemberReport. Only the
// affected member (or its canonical successor) may file, within
// DefaultAppealDeadline of the resolution, once per resolution.
//
// Moderation actions on content are appealed through the owning module
// (x/forum MsgAppealPost / MsgAppealThreadLock / MsgAppealThreadMove /
// MsgDisputePin, x/collect and x/artifact MsgAppealHide), which checks who
// may appeal and marks its own record appealed. Accepting them here let any
// member file an appeal the forum ignored but that still blocked the
// affected author's own appeal while pending (FRM-02, REP-R-14).
func (k msgServer) AppealGovAction(ctx context.Context, msg *types.MsgAppealGovAction) (*types.MsgAppealGovActionResponse, error) {
	creatorBytes, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	creatorAddr := sdk.AccAddress(creatorBytes)
	if !k.IsMember(ctx, creatorAddr) {
		return nil, errorsmod.Wrap(types.ErrNotMember, "only members can appeal governance actions")
	}

	actionType := commontypes.Saturate[types.GovActionType](msg.ActionType)
	if actionType == types.GovActionType_GOV_ACTION_TYPE_UNSPECIFIED {
		return nil, errorsmod.Wrap(types.ErrInvalidReasonCode, "invalid action type")
	}
	if types.ModuleOwnedAppealTypes[actionType] {
		return nil, errorsmod.Wrapf(types.ErrInvalidReasonCode,
			"%s appeals must be filed through the owning module", actionType)
	}
	if !types.MemberActionAppealTypes[actionType] {
		return nil, errorsmod.Wrapf(types.ErrAppealNotAllowed,
			"%s has no on-chain action record to appeal", actionType)
	}

	// The target is the member the action was applied to; only that member
	// may appeal it. CanActAs refuses a tombstoned signer and lets a
	// migrated member appeal an action recorded against the old address.
	if _, err := k.addressCodec.StringToBytes(msg.ActionTarget); err != nil {
		return nil, errorsmod.Wrap(types.ErrAppealNotAllowed, "action_target must be the member's address")
	}
	if !k.CanActAs(ctx, msg.Creator, msg.ActionTarget) {
		return nil, errorsmod.Wrap(types.ErrAppealNotAllowed, "only the affected member may appeal a member action")
	}
	member := k.Canonical(ctx, msg.ActionTarget)

	// Filing window: the member's report must have been resolved with this
	// action within DefaultAppealDeadline.
	now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	report, err := k.MemberReport.Get(ctx, member)
	if err != nil || report.Status != types.MemberReportStatus_MEMBER_REPORT_STATUS_RESOLVED ||
		report.ResolvedAction != actionType || report.ResolvedAt == 0 {
		return nil, errorsmod.Wrapf(types.ErrAppealNotAllowed, "no %s was applied to %s", actionType, member)
	}
	if now > report.ResolvedAt+types.DefaultAppealDeadline {
		return nil, errorsmod.Wrap(types.ErrAppealNotAllowed, "the appeal window has closed")
	}
	// One appeal per resolution, whatever became of an earlier one.
	for _, a := range k.govActionAppealsFor(ctx, actionType, member) {
		if a.CreatedAt >= report.ResolvedAt {
			return nil, errorsmod.Wrap(types.ErrAppealAlreadyFiled, "this action was already appealed")
		}
	}
	// The restoration record is what an overturn reverses; it carries the
	// appeal once filed (which also covers an appeal filed under an address
	// the member has since migrated away from).
	rest, err := k.MemberActionRestorations.Get(ctx, member)
	if err != nil || rest.Action != actionType || rest.ResolvedAt != report.ResolvedAt {
		return nil, errorsmod.Wrapf(types.ErrAppealNotAllowed, "no appealable %s on record for %s", actionType, member)
	}
	if rest.Appealed {
		return nil, errorsmod.Wrap(types.ErrAppealAlreadyFiled, "this action was already appealed")
	}

	appealID, _, err := k.CreateGovActionAppeal(ctx, actionType, member, creatorAddr, msg.AppealReason)
	if err != nil {
		return nil, err
	}
	if err := k.markMemberActionAppealed(ctx, member, appealID); err != nil {
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

	// One pending appeal per (actionType, actionTarget), read through the
	// by-target index.
	for _, appeal := range k.govActionAppealsFor(ctx, actionType, actionTarget) {
		if appeal.Status == types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING {
			return 0, 0, errorsmod.Wrap(types.ErrAppealAlreadyFiled, "appeal already exists for this action")
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
	// TallyJuryVotes) rather than only by manual committee override. The
	// parties to the dispute are recorded on the review and never drawn: the
	// appellant, the action target, and (for moderation actions) the accused
	// sentinel. The owning module marks its record appealed before calling
	// here, so GetActionSentinel already sees it (COL-102). The jurors are
	// drawn at the end of the block (REP-R-102). Best-effort — if no jury can
	// be seated the appeal still stands and falls back to committee resolution
	// or timeout.
	excludes := []string{appellant.String(), actionTarget}
	if fk := k.appealTarget(actionType); fk != nil {
		if sentinel, sErr := fk.GetActionSentinel(ctx, actionType, actionTarget); sErr == nil && sentinel != "" {
			excludes = append(excludes, sentinel)
		}
	}
	if err := k.openAppealJury(ctx, initiativeID, excludes); err != nil {
		return 0, 0, errorsmod.Wrap(err, "failed to open appeal jury")
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

	if err := k.SetGovActionAppeal(ctx, appeal); err != nil {
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
