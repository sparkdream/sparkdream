package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefendMemberReport allows the reported member to submit a defense.
func (k msgServer) DefendMemberReport(ctx context.Context, msg *types.MsgDefendMemberReport) (*types.MsgDefendMemberReportResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	report, err := k.MemberReport.Get(ctx, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrReportNotFound, "no report found for your account")
	}

	if report.Status != types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING &&
		report.Status != types.MemberReportStatus_MEMBER_REPORT_STATUS_ESCALATED {
		return nil, errorsmod.Wrap(types.ErrReportNotPending, "report is not in a defendable state")
	}

	if report.Defense != "" {
		return nil, errorsmod.Wrap(types.ErrDefenseAlreadySubmitted, "defense already submitted")
	}

	report.Defense = msg.Defense
	report.DefenseSubmittedAt = now

	if err := k.MemberReport.Set(ctx, msg.Creator, report); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update member report")
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"member_report_defended",
			sdk.NewAttribute("member", msg.Creator),
			sdk.NewAttribute("defense_submitted_at", sdkCtx.BlockTime().String()),
		),
	)

	return &types.MsgDefendMemberReportResponse{}, nil
}
