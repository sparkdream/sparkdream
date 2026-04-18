package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CosignMemberReport allows another sentinel to co-sign an existing report.
func (k msgServer) CosignMemberReport(ctx context.Context, msg *types.MsgCosignMemberReport) (*types.MsgCosignMemberReportResponse, error) {
	creatorBytes, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	report, err := k.MemberReport.Get(ctx, msg.Member)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrReportNotFound, fmt.Sprintf("no report found for member %s", msg.Member))
	}

	if report.Status != types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING {
		return nil, errorsmod.Wrap(types.ErrReportNotPending, "report is not pending")
	}

	tier, _ := k.GetReputationTier(ctx, sdk.AccAddress(creatorBytes))
	if tier < types.DefaultMinRepTierSentinel {
		return nil, errorsmod.Wrap(types.ErrInsufficientReputation, "insufficient reputation tier to co-sign reports")
	}

	sentinelBond := k.getReporterBond(ctx, sdk.AccAddress(creatorBytes))
	if sentinelBond.LT(types.DefaultMinSentinelBond) {
		return nil, errorsmod.Wrap(types.ErrInsufficientSentinelBond, "insufficient sentinel bond to co-sign reports")
	}

	for _, reporter := range report.Reporters {
		if reporter == msg.Creator {
			return nil, errorsmod.Wrap(types.ErrAlreadyCosigned, "already co-signed this report")
		}
	}

	if uint64(len(report.Reporters)) >= types.DefaultMaxMemberReporters {
		return nil, errorsmod.Wrap(types.ErrMaxReportersReached, "maximum reporters reached")
	}

	if err := k.LockDREAM(ctx, sdk.AccAddress(creatorBytes), sentinelBond); err != nil {
		return nil, errorsmod.Wrap(err, "failed to lock DREAM bond")
	}

	report.Reporters = append(report.Reporters, msg.Creator)

	totalBond, _ := math.NewIntFromString(report.TotalBond)
	newTotal := totalBond.Add(sentinelBond)
	report.TotalBond = newTotal.String()

	if uint64(len(report.Reporters)) >= types.DefaultMemberReportCosignThreshold {
		report.Status = types.MemberReportStatus_MEMBER_REPORT_STATUS_ESCALATED
	}

	if err := k.MemberReport.Set(ctx, msg.Member, report); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update member report")
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"member_report_cosigned",
			sdk.NewAttribute("member", msg.Member),
			sdk.NewAttribute("cosigner", msg.Creator),
			sdk.NewAttribute("total_reporters", fmt.Sprintf("%d", len(report.Reporters))),
			sdk.NewAttribute("status", report.Status.String()),
		),
	)

	return &types.MsgCosignMemberReportResponse{}, nil
}
