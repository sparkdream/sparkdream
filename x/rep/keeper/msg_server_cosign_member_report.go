package keeper

import (
	"context"
	"errors"
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

	member := k.Canonical(ctx, msg.Member)
	report, err := k.MemberReport.Get(ctx, member)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrReportNotFound, fmt.Sprintf("no report found for member %s", msg.Member))
	}

	if report.Status != types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING {
		return nil, errorsmod.Wrap(types.ErrReportNotPending, "report is not pending")
	}

	// A non-member has tier 0 (refused below); only a store error fails here.
	tier, getErr := k.GetReputationTier(ctx, sdk.AccAddress(creatorBytes))
	if getErr != nil && !errors.Is(getErr, types.ErrMemberNotFound) {
		return nil, getErr
	}
	if tier < types.DefaultMinRepTierSentinel {
		return nil, errorsmod.Wrap(types.ErrInsufficientReputation, "insufficient reputation tier to co-sign reports")
	}

	sentinelBond := k.getReporterBond(ctx, sdk.AccAddress(creatorBytes))
	if sentinelBond.LT(types.DefaultMinSentinelBond) {
		return nil, errorsmod.Wrap(types.ErrInsufficientSentinelBond, "insufficient sentinel bond to co-sign reports")
	}

	// A member who reported or co-signed under an earlier address has
	// already signed.
	recent := k.RecentAddressesOf(ctx, msg.Creator)
	for _, reporter := range report.Reporters {
		if AddressesMatch(recent, reporter) {
			return nil, errorsmod.Wrap(types.ErrAlreadyCosigned, "already co-signed this report")
		}
	}

	if uint64(len(report.Reporters)) >= types.DefaultMaxMemberReporters {
		return nil, errorsmod.Wrap(types.ErrMaxReportersReached, "maximum reporters reached")
	}

	if k.SameMember(ctx, msg.Creator, member) {
		return nil, errorsmod.Wrap(types.ErrCannotReportSelf, "cannot co-sign a report against yourself")
	}

	// Co-signers lock the same capped bond as the reporter.
	cosignBond := math.MinInt(sentinelBond, types.DefaultMaxMemberReportBond)
	if err := k.LockDREAM(ctx, sdk.AccAddress(creatorBytes), cosignBond); err != nil {
		return nil, errorsmod.Wrap(err, "failed to lock DREAM bond")
	}

	report.Reporters = append(report.Reporters, msg.Creator)
	report.ReporterBonds = append(report.ReporterBonds, &types.ReporterBondEntry{
		Address: msg.Creator,
		Amount:  cosignBond.String(),
	})

	totalBond, _ := math.NewIntFromString(report.TotalBond)
	newTotal := totalBond.Add(cosignBond)
	report.TotalBond = newTotal.String()

	if uint64(len(report.Reporters)) >= types.DefaultMemberReportCosignThreshold {
		report.Status = types.MemberReportStatus_MEMBER_REPORT_STATUS_ESCALATED
	}

	if err := k.MemberReport.Set(ctx, member, report); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update member report")
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"member_report_cosigned",
			sdk.NewAttribute("member", member),
			sdk.NewAttribute("cosigner", msg.Creator),
			sdk.NewAttribute("total_reporters", fmt.Sprintf("%d", len(report.Reporters))),
			sdk.NewAttribute("status", report.Status.String()),
		),
	)

	return &types.MsgCosignMemberReportResponse{}, nil
}
