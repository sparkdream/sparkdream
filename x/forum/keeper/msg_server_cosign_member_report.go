package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CosignMemberReport allows another sentinel to co-sign an existing report.
func (k msgServer) CosignMemberReport(ctx context.Context, msg *types.MsgCosignMemberReport) (*types.MsgCosignMemberReportResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Load existing report
	report, err := k.MemberReport.Get(ctx, msg.Member)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrReportNotFound, fmt.Sprintf("no report found for member %s", msg.Member))
	}

	// Check report is still pending
	if report.Status != types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING {
		return nil, errorsmod.Wrap(types.ErrReportNotPending, "report is not pending")
	}

	// Verify creator is a sentinel
	if k.GetRepTier(ctx, msg.Creator) < types.DefaultMinRepTierSentinel {
		return nil, errorsmod.Wrap(types.ErrInsufficientReputation, "insufficient reputation tier to co-sign reports")
	}

	sentinelBond := k.GetSentinelBond(ctx, msg.Creator)
	if sentinelBond.LT(types.DefaultMinSentinelBond) {
		return nil, errorsmod.Wrap(types.ErrInsufficientBond, "insufficient sentinel bond to co-sign reports")
	}

	// Check not already co-signed
	for _, reporter := range report.Reporters {
		if reporter == msg.Creator {
			return nil, errorsmod.Wrap(types.ErrAlreadyCosigned, "already co-signed this report")
		}
	}

	// Check max reporters
	if uint64(len(report.Reporters)) >= types.DefaultMaxMemberReporters {
		return nil, errorsmod.Wrap(types.ErrMaxReportersReached, "maximum reporters reached")
	}

	// Transfer DREAM bond from cosigner to escrow (stub - actual transfer via x/rep)
	if err := k.TransferDREAM(ctx, msg.Creator, k.GetModuleAddress(), sentinelBond); err != nil {
		return nil, errorsmod.Wrap(err, "failed to transfer DREAM bond to escrow")
	}

	// Add cosigner
	report.Reporters = append(report.Reporters, msg.Creator)

	// Add bond to total
	totalBond, _ := math.NewIntFromString(report.TotalBond)
	newTotal := totalBond.Add(sentinelBond)
	report.TotalBond = newTotal.String()

	// Check if threshold reached for escalation
	if uint64(len(report.Reporters)) >= types.DefaultMemberReportCosignThreshold {
		report.Status = types.MemberReportStatus_MEMBER_REPORT_STATUS_ESCALATED
	}

	if err := k.MemberReport.Set(ctx, msg.Member, report); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update member report")
	}

	// Emit event
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
