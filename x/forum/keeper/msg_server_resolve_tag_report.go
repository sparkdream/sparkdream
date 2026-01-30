package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ResolveTagReport allows governance authority to resolve a tag report.
func (k msgServer) ResolveTagReport(ctx context.Context, msg *types.MsgResolveTagReport) (*types.MsgResolveTagReportResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Only governance authority can resolve
	if !k.IsGovAuthority(ctx, msg.Creator) {
		return nil, errorsmod.Wrap(types.ErrNotGovAuthority, "only governance authority can resolve tag reports")
	}

	// Load report
	report, err := k.TagReport.Get(ctx, msg.TagName)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrTagReportNotFound, fmt.Sprintf("no report found for tag %s", msg.TagName))
	}

	// Find and update the tag
	var foundTag types.Tag
	var tagKey string
	tagIter, err := k.Tag.Iterate(ctx, nil)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to iterate tags")
	}
	defer tagIter.Close()

	for ; tagIter.Valid(); tagIter.Next() {
		tag, _ := tagIter.Value()
		if tag.Name == msg.TagName {
			foundTag = tag
			tagKey = tag.Name
			break
		}
	}

	if tagKey == "" {
		return nil, errorsmod.Wrap(types.ErrTagNotFound, fmt.Sprintf("tag %s not found", msg.TagName))
	}

	// Process based on action
	// 0 = dismiss, 1 = remove tag, 2 = reserve tag
	switch msg.Action {
	case 0:
		// Dismiss - no action on tag
		// TODO: Refund bonds to reporters

	case 1:
		// Remove tag
		if err := k.Tag.Remove(ctx, tagKey); err != nil {
			return nil, errorsmod.Wrap(err, "failed to remove tag")
		}
		// TODO: Update posts that use this tag
		// TODO: Slash bonds from tag creator

	case 2:
		// Reserve tag - in current schema we can't mark as reserved
		// For now, we just leave the tag as-is and note the resolution
		// TODO: Add reserved fields to Tag proto if needed
		_ = foundTag
		_ = msg.ReserveAuthority
		_ = msg.ReserveMembersCanUse
	}

	// Delete the report
	if err := k.TagReport.Remove(ctx, msg.TagName); err != nil {
		return nil, errorsmod.Wrap(err, "failed to remove tag report")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"tag_report_resolved",
			sdk.NewAttribute("tag_name", msg.TagName),
			sdk.NewAttribute("resolved_by", msg.Creator),
			sdk.NewAttribute("action", fmt.Sprintf("%d", msg.Action)),
			sdk.NewAttribute("reporters_count", fmt.Sprintf("%d", len(report.Reporters))),
		),
	)

	return &types.MsgResolveTagReportResponse{}, nil
}
