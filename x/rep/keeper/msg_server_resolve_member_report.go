package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ResolveMemberReport allows governance authority to resolve a member report.
func (k msgServer) ResolveMemberReport(ctx context.Context, msg *types.MsgResolveMemberReport) (*types.MsgResolveMemberReportResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	if !k.isCouncilAuthorized(ctx, msg.Creator, "commons", "operations") {
		return nil, errorsmod.Wrap(types.ErrNotGovAuthority, "only governance, council, or operations committee can resolve member reports")
	}

	report, err := k.MemberReport.Get(ctx, msg.Member)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrReportNotFound, fmt.Sprintf("no report found for member %s", msg.Member))
	}

	if report.Status == types.MemberReportStatus_MEMBER_REPORT_STATUS_RESOLVED {
		return nil, errorsmod.Wrap(types.ErrReportNotPending, "report is already resolved")
	}

	if report.Defense != "" {
		waitPeriod := report.DefenseSubmittedAt + types.DefaultMinDefenseWait
		if now < waitPeriod {
			return nil, errorsmod.Wrap(types.ErrDefenseWaitPeriod, "must wait after defense before resolution")
		}
	}

	action := types.GovActionType(msg.Action)

	memberBytes, _ := k.addressCodec.StringToBytes(msg.Member)

	if action == types.GovActionType_GOV_ACTION_TYPE_UNSPECIFIED {
		// Dismiss: refund bonds, no slashing.
		if err := k.refundReportBonds(ctx, report); err != nil {
			return nil, errorsmod.Wrap(err, "failed to refund bonds to reporters")
		}
	} else {
		switch action {
		case types.GovActionType_GOV_ACTION_TYPE_WARNING:
			warningID, err := k.MemberWarningSeq.Next(ctx)
			if err != nil {
				return nil, errorsmod.Wrap(err, "failed to generate warning ID")
			}

			var warningCount uint64
			warningIter, err := k.MemberWarning.Iterate(ctx, nil)
			if err == nil {
				defer warningIter.Close()
				for ; warningIter.Valid(); warningIter.Next() {
					warning, _ := warningIter.Value()
					if warning.Member == msg.Member {
						warningCount++
					}
				}
			}

			warning := types.MemberWarning{
				Id:              warningID,
				Member:          msg.Member,
				Reason:          msg.Reason,
				IssuedAt:        now,
				IssuedBy:        msg.Creator,
				WarningNumber:   warningCount + 1,
				EvidencePostIds: report.EvidencePostIds,
			}

			if err := k.MemberWarning.Set(ctx, warningID, warning); err != nil {
				return nil, errorsmod.Wrap(err, "failed to store warning")
			}

			if err := k.refundReportBonds(ctx, report); err != nil {
				return nil, errorsmod.Wrap(err, "failed to refund bonds to reporters")
			}

		case types.GovActionType_GOV_ACTION_TYPE_DEMOTION:
			if err := k.DemoteMember(ctx, sdk.AccAddress(memberBytes), msg.Reason); err != nil {
				return nil, errorsmod.Wrap(err, "failed to demote member")
			}

		case types.GovActionType_GOV_ACTION_TYPE_ZEROING:
			if err := k.ZeroMember(ctx, sdk.AccAddress(memberBytes), msg.Reason); err != nil {
				return nil, errorsmod.Wrap(err, "failed to zero member")
			}
			if err := k.refundReportBonds(ctx, report); err != nil {
				return nil, errorsmod.Wrap(err, "failed to refund bonds to reporters")
			}
		}
	}

	report.Status = types.MemberReportStatus_MEMBER_REPORT_STATUS_RESOLVED

	if err := k.MemberReport.Set(ctx, msg.Member, report); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update member report")
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"member_report_resolved",
			sdk.NewAttribute("member", msg.Member),
			sdk.NewAttribute("resolved_by", msg.Creator),
			sdk.NewAttribute("action", action.String()),
			sdk.NewAttribute("reason", msg.Reason),
		),
	)

	return &types.MsgResolveMemberReportResponse{}, nil
}
