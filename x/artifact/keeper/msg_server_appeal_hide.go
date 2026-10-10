package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"

	"sparkdream/x/artifact/types"
)

// AppealHide opens an x/rep moderation appeal (GOV_ACTION_TYPE_ARTIFACT_HIDE)
// for a pending hide. x/rep charges its standard appeal bond, seats a jury,
// and applies the verdict through the callbacks in appeal_target.go. The
// appellant is the token holder (token hide) or the class owner (class hide).
func (k msgServer) AppealHide(ctx context.Context, msg *types.MsgAppealHide) (*types.MsgAppealHideResponse, error) {
	appellant, err := k.decodeAddr("appellant", msg.Appellant)
	if err != nil {
		return nil, err
	}
	if err := types.ValidateText("reason", msg.Reason, types.HideReasonMaxLength, true); err != nil {
		return nil, err
	}
	hr, err := k.HideRecords.Get(ctx, msg.HideId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrHideNotFound, "hide %d", msg.HideId)
	}
	if hr.Outcome != types.HideOutcome_HIDE_OUTCOME_PENDING || hr.Appealed || height(ctx) >= hr.AppealDeadline {
		return nil, types.ErrAppealWindowClosed
	}
	switch hr.TargetKind {
	case types.HideTargetKind_HIDE_TARGET_KIND_CLASS:
		c, err := k.getClass(ctx, hr.ClassId)
		if err != nil || c.Owner != msg.Appellant {
			return nil, types.ErrNotAppellant
		}
	default:
		t, err := k.getToken(ctx, hr.ClassId, hr.TokenId)
		if err != nil || t.Owner != msg.Appellant {
			return nil, types.ErrNotAppellant
		}
	}
	if k.late.rep == nil {
		return nil, errorsmod.Wrap(types.ErrAppealWindowClosed, "rep keeper not wired")
	}

	reason := msg.Reason
	if reason == "" {
		reason = "artifact hide appeal: " + hr.ReasonText
	}
	appealID, _, err := k.late.rep.CreateGovActionAppeal(ctx, reptypes.GovActionType_GOV_ACTION_TYPE_ARTIFACT_HIDE,
		u64(hr.Id), appellant, reason)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to open appeal")
	}

	// x/rep now owns the deadline: drop the hide-expiry entry so the
	// EndBlocker never scrubs content that is under appeal.
	if err := k.HideExpiry.Remove(ctx, collections.Join(hr.AppealDeadline, hr.Id)); err != nil {
		return nil, err
	}
	hr.Appealed = true
	hr.Appellant = msg.Appellant
	hr.AppealId = appealID
	if err := k.HideRecords.Set(ctx, hr.Id, hr); err != nil {
		return nil, err
	}
	if hr.Sentinel != "" {
		_ = k.late.rep.RecordRoleAction(ctx, sentinelRole, hr.Sentinel, reptypes.ActionKindArtifactAppealFiled)
	}
	emit(ctx, types.EventHideAppealed, sdk.NewAttribute("hide_id", u64(hr.Id)),
		sdk.NewAttribute("appellant", msg.Appellant), sdk.NewAttribute("appeal_id", u64(appealID)))
	return &types.MsgAppealHideResponse{AppealId: appealID}, nil
}
