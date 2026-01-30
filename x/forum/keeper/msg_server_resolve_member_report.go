package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

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

	// Only governance authority can resolve
	if !k.IsGovAuthority(ctx, msg.Creator) {
		return nil, errorsmod.Wrap(types.ErrNotGovAuthority, "only governance authority can resolve member reports")
	}

	// Load report
	report, err := k.MemberReport.Get(ctx, msg.Member)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrReportNotFound, fmt.Sprintf("no report found for member %s", msg.Member))
	}

	// Check report is not already resolved
	if report.Status == types.MemberReportStatus_MEMBER_REPORT_STATUS_RESOLVED {
		return nil, errorsmod.Wrap(types.ErrReportNotPending, "report is already resolved")
	}

	// If defense was submitted, check wait period
	if report.Defense != "" {
		waitPeriod := report.DefenseSubmittedAt + types.DefaultMinDefenseWait
		if now < waitPeriod {
			return nil, errorsmod.Wrap(types.ErrDefenseWaitPeriod, "must wait after defense before resolution")
		}
	}

	// Validate action
	action := types.GovActionType(msg.Action)
	if action == types.GovActionType_GOV_ACTION_TYPE_UNSPECIFIED {
		// Dismissing the report
		// TODO: Refund bonds to reporters
	} else {
		// Taking action
		switch action {
		case types.GovActionType_GOV_ACTION_TYPE_WARNING:
			// Issue warning
			warningID, err := k.MemberWarningSeq.Next(ctx)
			if err != nil {
				return nil, errorsmod.Wrap(err, "failed to generate warning ID")
			}

			// Count existing warnings
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

			// TODO: Slash bonds from reporters and award to warned member

		case types.GovActionType_GOV_ACTION_TYPE_DEMOTION:
			// TODO: Demote member via x/rep
		case types.GovActionType_GOV_ACTION_TYPE_ZEROING:
			// TODO: Zero member via x/rep
		}

		// TODO: Slash bonds and distribute to treasury/rewards
	}

	// Mark as resolved
	report.Status = types.MemberReportStatus_MEMBER_REPORT_STATUS_RESOLVED

	if err := k.MemberReport.Set(ctx, msg.Member, report); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update member report")
	}

	// Emit event
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
