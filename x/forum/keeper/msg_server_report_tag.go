package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ReportTag allows members to report a tag for review.
func (k msgServer) ReportTag(ctx context.Context, msg *types.MsgReportTag) (*types.MsgReportTagResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Verify creator is a member
	if !k.IsMember(ctx, msg.Creator) {
		return nil, errorsmod.Wrap(types.ErrNotMember, "only members can report tags")
	}

	// Check tag exists
	tagFound := false
	tagIter, err := k.Tag.Iterate(ctx, nil)
	if err == nil {
		defer tagIter.Close()
		for ; tagIter.Valid(); tagIter.Next() {
			tag, _ := tagIter.Value()
			if tag.Name == msg.TagName {
				tagFound = true
				break
			}
		}
	}

	if !tagFound {
		return nil, errorsmod.Wrap(types.ErrTagNotFound, fmt.Sprintf("tag %s not found", msg.TagName))
	}

	// Check if report already exists
	existingReport, err := k.TagReport.Get(ctx, msg.TagName)
	if err == nil {
		// Report exists, add as co-reporter
		for _, reporter := range existingReport.Reporters {
			if reporter == msg.Creator {
				return nil, errorsmod.Wrap(types.ErrReportAlreadyExists, "already reported this tag")
			}
		}

		if uint64(len(existingReport.Reporters)) >= types.DefaultMaxTagReporters {
			return nil, errorsmod.Wrap(types.ErrMaxReportersReached, "maximum reporters reached")
		}

		existingReport.Reporters = append(existingReport.Reporters, msg.Creator)
		// TODO: Add bond to total
		existingBond, _ := math.NewIntFromString(existingReport.TotalBond)
		newBond := existingBond.Add(math.NewInt(10)) // nominal bond
		existingReport.TotalBond = newBond.String()

		if err := k.TagReport.Set(ctx, msg.TagName, existingReport); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update tag report")
		}
	} else {
		// Create new report
		report := types.TagReport{
			TagName:       msg.TagName,
			TotalBond:     "10", // TODO: Use proper bond from reporter
			FirstReportAt: now,
			UnderReview:   false,
			Reporters:     []string{msg.Creator},
		}

		if err := k.TagReport.Set(ctx, msg.TagName, report); err != nil {
			return nil, errorsmod.Wrap(err, "failed to store tag report")
		}
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"tag_reported",
			sdk.NewAttribute("tag_name", msg.TagName),
			sdk.NewAttribute("reporter", msg.Creator),
			sdk.NewAttribute("reason", msg.Reason),
		),
	)

	return &types.MsgReportTagResponse{}, nil
}
