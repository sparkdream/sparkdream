package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ResolveMemberReport allows governance authority to resolve a member report.
func (k msgServer) ResolveMemberReport(ctx context.Context, msg *types.MsgResolveMemberReport) (*types.MsgResolveMemberReportResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Only governance, council, or operations committee can resolve
	if !k.isCouncilAuthorized(ctx, msg.Creator, "commons", "operations") {
		return nil, errorsmod.Wrap(types.ErrNotGovAuthority, "only governance, council, or operations committee can resolve member reports")
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
	// Parse total bond amount
	totalBond, _ := math.NewIntFromString(report.TotalBond)
	if report.TotalBond == "" {
		totalBond = math.ZeroInt()
	}

	if action == types.GovActionType_GOV_ACTION_TYPE_UNSPECIFIED {
		// Dismissing the report - refund bonds to reporters
		if err := k.RefundBonds(ctx, report.Reporters, totalBond); err != nil {
			return nil, errorsmod.Wrap(err, "failed to refund bonds to reporters")
		}
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

			// Warning only - slash bonds from reporters and award to warned member
			// (reporter was wrong to escalate to this level)
			if err := k.TransferDREAM(ctx, k.GetModuleAddress(), msg.Member, totalBond); err != nil {
				return nil, errorsmod.Wrap(err, "failed to award bonds to warned member")
			}

		case types.GovActionType_GOV_ACTION_TYPE_DEMOTION:
			// Demote member via x/rep (stub)
			if err := k.DemoteMember(ctx, msg.Member, msg.Reason); err != nil {
				return nil, errorsmod.Wrap(err, "failed to demote member")
			}
			// Reporters were correct - burn a portion of bond as processing fee
			// and return remainder (stub implementation burns nothing)

		case types.GovActionType_GOV_ACTION_TYPE_ZEROING:
			// Zero member via x/rep (stub)
			if err := k.ZeroMember(ctx, msg.Member, msg.Reason); err != nil {
				return nil, errorsmod.Wrap(err, "failed to zero member")
			}
			// Reporters were correct - return full bonds
			if err := k.RefundBonds(ctx, report.Reporters, totalBond); err != nil {
				return nil, errorsmod.Wrap(err, "failed to refund bonds to reporters")
			}
		}
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
