package keeper

import (
	"context"
	"fmt"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ResolveTagReport allows governance authority to resolve a tag report.
func (k msgServer) ResolveTagReport(ctx context.Context, msg *types.MsgResolveTagReport) (*types.MsgResolveTagReportResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Only governance, council, or operations committee can resolve
	if !k.isCouncilAuthorized(ctx, msg.Creator, "commons", "operations") {
		return nil, errorsmod.Wrap(types.ErrNotGovAuthority, "only governance, council, or operations committee can resolve tag reports")
	}

	// Load report
	report, err := k.TagReport.Get(ctx, msg.TagName)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrTagReportNotFound, fmt.Sprintf("no report found for tag %s", msg.TagName))
	}

	// Find and update the tag
	var foundTag commontypes.Tag
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

	// Parse total bond amount
	totalBond, _ := math.NewIntFromString(report.TotalBond)
	if report.TotalBond == "" {
		totalBond = math.ZeroInt()
	}

	// Process based on action
	// 0 = dismiss, 1 = remove tag, 2 = reserve tag
	switch msg.Action {
	case 0:
		// Dismiss - no action on tag, refund bonds to reporters
		if err := k.RefundBonds(ctx, report.Reporters, totalBond); err != nil {
			return nil, errorsmod.Wrap(err, "failed to refund bonds to reporters")
		}

	case 1:
		// Remove tag
		if err := k.Tag.Remove(ctx, tagKey); err != nil {
			return nil, errorsmod.Wrap(err, "failed to remove tag")
		}

		// Update posts that use this tag by iterating and removing the tag reference
		postIter, iterErr := k.Post.Iterate(ctx, nil)
		if iterErr == nil {
			defer postIter.Close()
			for ; postIter.Valid(); postIter.Next() {
				post, _ := postIter.Value()
				// Check if post uses this tag
				for i, t := range post.Tags {
					if t == msg.TagName {
						// Remove tag from post
						post.Tags = append(post.Tags[:i], post.Tags[i+1:]...)
						_ = k.Post.Set(ctx, post.PostId, post)
						break
					}
				}
			}
		}

		// Note: Tag proto doesn't currently have a Creator field
		// In future, could slash bonds from tag creator here

		// Refund bonds to reporters (they were correct)
		if err := k.RefundBonds(ctx, report.Reporters, totalBond); err != nil {
			return nil, errorsmod.Wrap(err, "failed to refund bonds to reporters")
		}

	case 2:
		// Reserve tag - create a ReservedTag entry
		reservedTag := commontypes.ReservedTag{
			Name:          msg.TagName,
			Authority:     msg.ReserveAuthority,
			MembersCanUse: msg.ReserveMembersCanUse,
		}
		if err := k.ReservedTag.Set(ctx, msg.TagName, reservedTag); err != nil {
			return nil, errorsmod.Wrap(err, "failed to create reserved tag")
		}

		// Note: Tag proto doesn't have a Reserved field
		// The ReservedTag collection tracks which tags are reserved
		_ = foundTag // Tag is now managed via ReservedTag collection

		// Refund bonds to reporters
		if err := k.RefundBonds(ctx, report.Reporters, totalBond); err != nil {
			return nil, errorsmod.Wrap(err, "failed to refund bonds to reporters")
		}
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
