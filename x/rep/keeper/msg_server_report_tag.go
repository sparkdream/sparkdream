package keeper

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/types"
)

// ReportTag allows members to report a tag for review. The reporter escrows
// DefaultTagReportBond DREAM (locked on their account); ResolveTagReport
// refunds the bonds when it acts on the report and burns them when it
// dismisses it. A tag is reportable when it has a Tag row or a reservation:
// a reserved tag seeded at genesis, or reserved after its Tag row expired,
// has no row, and a report is the only way to reach the restore action that
// releases it (REP-R-203).
func (k msgServer) ReportTag(ctx context.Context, msg *types.MsgReportTag) (*types.MsgReportTagResponse, error) {
	creatorBytes, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	creatorAddr := sdk.AccAddress(creatorBytes)

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	if !k.IsActiveMember(ctx, creatorAddr) {
		return nil, errorsmod.Wrap(types.ErrNotMember, "only active members can report tags")
	}

	tagFound, err := k.TagExists(ctx, msg.TagName)
	if err != nil {
		return nil, errorsmod.Wrapf(err, "failed to check tag %q", msg.TagName)
	}
	if !tagFound {
		reserved, rErr := k.IsReservedTag(ctx, msg.TagName)
		if rErr != nil {
			return nil, errorsmod.Wrapf(rErr, "failed to check reservation of %q", msg.TagName)
		}
		if !reserved {
			return nil, errorsmod.Wrap(types.ErrTagNotFound, fmt.Sprintf("tag %s not found", msg.TagName))
		}
	}

	reportBond := types.DefaultTagReportBond

	existingReport, err := k.TagReport.Get(ctx, msg.TagName)
	if err == nil {
		recent := k.RecentAddressesOf(ctx, msg.Creator)
		for _, reporter := range existingReport.Reporters {
			if AddressesMatch(recent, reporter) {
				return nil, errorsmod.Wrap(types.ErrTagReportAlreadyExists, "already reported this tag")
			}
		}
		if uint64(len(existingReport.Reporters)) >= types.DefaultMaxTagReporters {
			return nil, errorsmod.Wrap(types.ErrMaxTagReporters, "maximum reporters reached")
		}

		if err := k.LockDREAM(ctx, creatorAddr, reportBond); err != nil {
			return nil, errorsmod.Wrap(err, "failed to lock tag report bond")
		}

		existingReport.Reporters = append(existingReport.Reporters, msg.Creator)
		existingBond, _ := math.NewIntFromString(existingReport.TotalBond)
		if existingReport.TotalBond == "" {
			existingBond = math.ZeroInt()
		}
		existingReport.TotalBond = existingBond.Add(reportBond).String()

		if err := k.TagReport.Set(ctx, msg.TagName, existingReport); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update tag report")
		}
	} else {
		if err := k.LockDREAM(ctx, creatorAddr, reportBond); err != nil {
			return nil, errorsmod.Wrap(err, "failed to lock tag report bond")
		}

		report := types.TagReport{
			TagName:       msg.TagName,
			TotalBond:     reportBond.String(),
			FirstReportAt: now,
			UnderReview:   true, // open until an authority resolves it
			Reporters:     []string{msg.Creator},
		}
		if err := k.TagReport.Set(ctx, msg.TagName, report); err != nil {
			return nil, errorsmod.Wrap(err, "failed to store tag report")
		}
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"tag_reported",
		sdk.NewAttribute("tag_name", msg.TagName),
		sdk.NewAttribute("reporter", msg.Creator),
		sdk.NewAttribute("reason", msg.Reason),
	))

	return &types.MsgReportTagResponse{}, nil
}
