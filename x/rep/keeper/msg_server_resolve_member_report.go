package keeper

import (
	"context"
	"fmt"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ResolveMemberReport resolves a member report. The signer must be the
// Commons Operations Committee (or Commons Council) policy address or x/gov.
func (k msgServer) ResolveMemberReport(ctx context.Context, msg *types.MsgResolveMemberReport) (*types.MsgResolveMemberReportResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// A resolution can zero a member, so it takes an executed proposal of the
	// Commons Operations Committee (or its council, or x/gov), never a single
	// committee member.
	if !k.isCouncilOrCommitteePolicy(ctx, msg.Creator, "commons", "operations") {
		return nil, errorsmod.Wrap(types.ErrNotGovAuthority, "only governance, the Commons Council or the Commons Operations Committee policy can resolve member reports")
	}

	member := k.Canonical(ctx, msg.Member)
	report, err := k.MemberReport.Get(ctx, member)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrReportNotFound, fmt.Sprintf("no report found for member %s", member))
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

	action := commontypes.Saturate[types.GovActionType](msg.Action)

	memberBytes, _ := k.addressCodec.StringToBytes(member)

	// What an OVERTURNED appeal of the action needs to undo it
	// (member_action_appeal.go). Written below for every applied action.
	var restoration *types.MemberActionRestoration
	if types.MemberActionAppealTypes[action] {
		rest, err := k.newMemberActionRestoration(ctx, member, action)
		if err != nil {
			return nil, err
		}
		restoration = &rest
	}

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

			// Escalation sees the member's full history (AllAddressesOf:
			// gas slack here).
			memberAddrs := k.AllAddressesOf(ctx, member)
			var warningCount uint64
			warningIter, err := k.MemberWarning.Iterate(ctx, nil)
			if err == nil {
				defer warningIter.Close()
				for ; warningIter.Valid(); warningIter.Next() {
					warning, _ := warningIter.Value()
					if AddressesMatch(memberAddrs, warning.Member) {
						warningCount++
					}
				}
			}

			warning := types.MemberWarning{
				Id:              warningID,
				Member:          member,
				Reason:          msg.Reason,
				IssuedAt:        now,
				IssuedBy:        msg.Creator,
				WarningNumber:   warningCount + 1,
				EvidencePostIds: report.EvidencePostIds,
			}

			if err := k.MemberWarning.Set(ctx, warningID, warning); err != nil {
				return nil, errorsmod.Wrap(err, "failed to store warning")
			}
			restoration.WarningId = warningID

			if err := k.refundReportBonds(ctx, report); err != nil {
				return nil, errorsmod.Wrap(err, "failed to refund bonds to reporters")
			}

		case types.GovActionType_GOV_ACTION_TYPE_DEMOTION:
			before, err := k.Member.Get(ctx, member)
			if err != nil {
				return nil, types.ErrMemberNotFound
			}
			if err := k.DemoteMember(ctx, sdk.AccAddress(memberBytes), msg.Reason); err != nil {
				return nil, errorsmod.Wrap(err, "failed to demote member")
			}
			after, err := k.Member.Get(ctx, member)
			if err != nil {
				return nil, types.ErrMemberNotFound
			}
			restoration.Reputation = reputationSlashed(before.ReputationScores, after.ReputationScores)
			// An upheld report returns the reporters' bonds, whichever action
			// the resolver picked. This branch omitted the refund, so a report
			// that succeeded on its merits still stranded every reporter's
			// locked DREAM permanently.
			if err := k.refundReportBonds(ctx, report); err != nil {
				return nil, errorsmod.Wrap(err, "failed to refund bonds to reporters")
			}

		case types.GovActionType_GOV_ACTION_TYPE_ZEROING:
			before, err := k.Member.Get(ctx, member)
			if err != nil {
				return nil, types.ErrMemberNotFound
			}
			burned, err := k.zeroMember(ctx, sdk.AccAddress(memberBytes), msg.Reason)
			if err != nil {
				return nil, errorsmod.Wrap(err, "failed to zero member")
			}
			after, err := k.Member.Get(ctx, member)
			if err != nil {
				return nil, types.ErrMemberNotFound
			}
			restoration.PriorStatus = before.Status
			restoration.PriorTrustLevel = before.TrustLevel
			restoration.PriorInvitationCredits = before.InvitationCredits
			restoration.Reputation = copyStringMap(before.ReputationScores)
			restoration.BurnedDream = burned.String()
			restoration.ZeroedCount = after.ZeroedCount
			if err := k.refundReportBonds(ctx, report); err != nil {
				return nil, errorsmod.Wrap(err, "failed to refund bonds to reporters")
			}

		default:
			// The switch had no default, so every other action type fell
			// through applying nothing, refunding nothing, and still marking
			// the report RESOLVED below — the reporters' bonds vanished for an
			// action the resolver was allowed to pick. Reject the resolution
			// instead of half-applying it.
			return nil, errorsmod.Wrapf(types.ErrUnauthorized,
				"action %s is not a valid resolution for a member report", action)
		}
	}

	report.Status = types.MemberReportStatus_MEMBER_REPORT_STATUS_RESOLVED
	// Recorded for the member's MsgAppealGovAction filing window.
	report.ResolvedAction = action
	report.ResolvedAt = now

	if err := k.MemberReport.Set(ctx, member, report); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update member report")
	}
	if restoration != nil {
		if err := k.setMemberActionRestoration(ctx, *restoration); err != nil {
			return nil, errorsmod.Wrap(err, "failed to record member-action restoration")
		}
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"member_report_resolved",
			sdk.NewAttribute("member", member),
			sdk.NewAttribute("resolved_by", msg.Creator),
			sdk.NewAttribute("action", action.String()),
			sdk.NewAttribute("reason", msg.Reason),
		),
	)

	return &types.MsgResolveMemberReportResponse{}, nil
}
