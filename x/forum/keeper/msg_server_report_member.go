package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ReportMember allows a sentinel to report a member for forum misconduct.
func (k msgServer) ReportMember(ctx context.Context, msg *types.MsgReportMember) (*types.MsgReportMemberResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	if _, err := k.addressCodec.StringToBytes(msg.Member); err != nil {
		return nil, errorsmod.Wrap(err, "invalid member address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Cannot report self
	if msg.Creator == msg.Member {
		return nil, errorsmod.Wrap(types.ErrCannotReportSelf, "cannot report yourself")
	}

	// Verify creator is a sentinel (high tier with bond)
	if k.GetRepTier(ctx, msg.Creator) < types.DefaultMinRepTierSentinel {
		return nil, errorsmod.Wrap(types.ErrInsufficientReputation, "insufficient reputation tier to report members")
	}

	sentinelBond := k.GetSentinelBond(ctx, msg.Creator)
	if sentinelBond.LT(types.DefaultMinSentinelBond) {
		return nil, errorsmod.Wrap(types.ErrInsufficientBond, "insufficient sentinel bond to report members")
	}

	// Check if report already exists for this member
	_, err := k.MemberReport.Get(ctx, msg.Member)
	if err == nil {
		return nil, errorsmod.Wrap(types.ErrReportAlreadyExists, fmt.Sprintf("active report already exists for member %s", msg.Member))
	}

	// Validate recommended action
	action := types.GovActionType(msg.RecommendedAction)
	if action == types.GovActionType_GOV_ACTION_TYPE_UNSPECIFIED {
		action = types.GovActionType_GOV_ACTION_TYPE_WARNING
	}

	// Use a fixed reporting bond amount: the lesser of the sentinel's bond and 1000 DREAM.
	// This prevents a single report from escrow-ing the sentinel's entire bond.
	reportBondCap := math.NewInt(1000)
	reportBond := math.MinInt(sentinelBond, reportBondCap)

	// Transfer fixed reporting bond from reporter to escrow (stub - actual transfer via x/rep)
	if err := k.TransferDREAM(ctx, msg.Creator, k.GetModuleAddress(), reportBond); err != nil {
		return nil, errorsmod.Wrap(err, "failed to transfer DREAM bond to escrow")
	}

	// Create member report
	report := types.MemberReport{
		Member:            msg.Member,
		Reason:            msg.Reason,
		RecommendedAction: action,
		TotalBond:         reportBond.String(),
		CreatedAt:         now,
		Status:            types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
		Reporters:         []string{msg.Creator},
		EvidencePostIds:   []uint64{},
		DefensePostIds:    []uint64{},
	}

	if err := k.MemberReport.Set(ctx, msg.Member, report); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store member report")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"member_reported",
			sdk.NewAttribute("member", msg.Member),
			sdk.NewAttribute("reporter", msg.Creator),
			sdk.NewAttribute("reason", msg.Reason),
			sdk.NewAttribute("recommended_action", action.String()),
		),
	)

	return &types.MsgReportMemberResponse{}, nil
}
