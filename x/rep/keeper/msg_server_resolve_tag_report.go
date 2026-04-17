package keeper

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/types"
)

// ResolveTagReport processes a tag report. Authorization: governance, the
// Commons Council policy, or the Commons Operations Committee. Actions:
//   - 0 (dismiss): refund reporter bonds, tag untouched
//   - 1 (remove):  delete the tag (and forum references), refund bonds
//   - 2 (reserve): create a ReservedTag entry, refund bonds
func (k msgServer) ResolveTagReport(ctx context.Context, msg *types.MsgResolveTagReport) (*types.MsgResolveTagReportResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	if k.commonsKeeper == nil || !k.commonsKeeper.IsCouncilAuthorized(ctx, msg.Creator, "commons", "operations") {
		return nil, errorsmod.Wrap(types.ErrTagReportNotAuthorized,
			"only governance, council, or operations committee can resolve tag reports")
	}

	report, err := k.TagReport.Get(ctx, msg.TagName)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrTagReportNotFound, fmt.Sprintf("no report found for tag %s", msg.TagName))
	}

	foundTag, err := k.GetTag(ctx, msg.TagName)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrTagNotFound, fmt.Sprintf("tag %s not found", msg.TagName))
	}

	totalBond, _ := math.NewIntFromString(report.TotalBond)
	if report.TotalBond == "" {
		totalBond = math.ZeroInt()
	}

	switch msg.Action {
	case 0:
		if err := refundTagReportBonds(ctx, k.Keeper, report.Reporters, totalBond); err != nil {
			return nil, errorsmod.Wrap(err, "failed to refund bonds to reporters")
		}

	case 1:
		if err := k.RemoveTag(ctx, foundTag.Name); err != nil {
			return nil, errorsmod.Wrap(err, "failed to remove tag")
		}
		if k.late.forumKeeper != nil {
			// Best-effort cleanup of stale references; non-fatal.
			_ = k.late.forumKeeper.PruneTagReferences(ctx, msg.TagName)
		}
		if err := refundTagReportBonds(ctx, k.Keeper, report.Reporters, totalBond); err != nil {
			return nil, errorsmod.Wrap(err, "failed to refund bonds to reporters")
		}

	case 2:
		reservedTag := types.ReservedTag{
			Name:          msg.TagName,
			Authority:     msg.ReserveAuthority,
			MembersCanUse: msg.ReserveMembersCanUse,
		}
		if err := k.SetReservedTag(ctx, reservedTag); err != nil {
			return nil, errorsmod.Wrap(err, "failed to create reserved tag")
		}
		if err := refundTagReportBonds(ctx, k.Keeper, report.Reporters, totalBond); err != nil {
			return nil, errorsmod.Wrap(err, "failed to refund bonds to reporters")
		}
	}

	if err := k.TagReport.Remove(ctx, msg.TagName); err != nil {
		return nil, errorsmod.Wrap(err, "failed to remove tag report")
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"tag_report_resolved",
		sdk.NewAttribute("tag_name", msg.TagName),
		sdk.NewAttribute("resolved_by", msg.Creator),
		sdk.NewAttribute("action", fmt.Sprintf("%d", msg.Action)),
		sdk.NewAttribute("reporters_count", fmt.Sprintf("%d", len(report.Reporters))),
	))

	return &types.MsgResolveTagReportResponse{}, nil
}

// refundTagReportBonds unlocks the bonded DREAM evenly among reporters.
// Integer division truncates remainders (matches forum's prior behaviour).
func refundTagReportBonds(ctx context.Context, k Keeper, recipients []string, totalAmount math.Int) error {
	if len(recipients) == 0 || totalAmount.IsZero() {
		return nil
	}
	perRecipient := totalAmount.Quo(math.NewInt(int64(len(recipients))))
	if perRecipient.IsZero() {
		return nil
	}
	for _, recipient := range recipients {
		addrBytes, err := k.addressCodec.StringToBytes(recipient)
		if err != nil {
			continue
		}
		if err := k.UnlockDREAM(ctx, sdk.AccAddress(addrBytes), perRecipient); err != nil {
			continue
		}
	}
	return nil
}
