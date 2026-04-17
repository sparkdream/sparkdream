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
// DefaultTagReportBond DREAM (locked on their account); bonds are refunded or
// redistributed by ResolveTagReport.
func (k msgServer) ReportTag(ctx context.Context, msg *types.MsgReportTag) (*types.MsgReportTagResponse, error) {
	creatorBytes, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	creatorAddr := sdk.AccAddress(creatorBytes)

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	if !k.IsMember(ctx, creatorAddr) {
		return nil, errorsmod.Wrap(types.ErrNotMember, "only members can report tags")
	}

	tagFound, err := k.TagExists(ctx, msg.TagName)
	if err != nil {
		return nil, errorsmod.Wrapf(err, "failed to check tag %q", msg.TagName)
	}
	if !tagFound {
		return nil, errorsmod.Wrap(types.ErrTagNotFound, fmt.Sprintf("tag %s not found", msg.TagName))
	}

	reportBond := types.DefaultTagReportBond

	existingReport, err := k.TagReport.Get(ctx, msg.TagName)
	if err == nil {
		for _, reporter := range existingReport.Reporters {
			if reporter == msg.Creator {
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
			UnderReview:   false,
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
