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
//   - 0 (dismiss): burn reporter bonds (an unfounded report costs its bond),
//     tag untouched
//   - 1 (remove):  delete the tag (and forum references), refund bonds
//   - 2 (reserve): create a ReservedTag entry, refund bonds. reserve_authority
//     is empty or a bech32 address (a council policy, the gov module
//     account, a member); anything else is refused before state changes
//   - 3 (restore): release a reserved tag back to public use, refund bonds;
//     refused for a tag that is not reserved. Works whether or not the tag
//     still has a Tag row (ReportTag accepts a bare reservation, REP-R-203)
//
// A tag that no longer exists (garbage-collected while the report was open,
// on state written before ExpireTags learned to wait for reports) does not
// block the resolution (REP-E-11): remove is then a no-op and the bonds are
// still settled.
func (k msgServer) ResolveTagReport(ctx context.Context, msg *types.MsgResolveTagReport) (*types.MsgResolveTagReportResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	if k.commonsKeeper == nil || !k.commonsKeeper.IsCouncilAuthorized(ctx, msg.Creator, "commons", "operations") {
		return nil, errorsmod.Wrap(types.ErrTagReportNotAuthorized,
			"only governance, council, or operations committee can resolve tag reports")
	}

	// Reject unknown actions before touching state: the report (and every
	// reporter's locked bond) must survive a malformed resolution.
	if msg.Action > 3 {
		return nil, errorsmod.Wrapf(types.ErrInvalidTagReportAction,
			"action %d is not one of 0 (dismiss), 1 (remove), 2 (reserve), 3 (restore)", msg.Action)
	}

	if msg.Action == 2 && msg.ReserveAuthority != "" {
		// Stored as-is and shown to clients deciding who may use the tag, so
		// a typo would reserve the tag for nobody. Refused before the report
		// is touched.
		if _, err := k.addressCodec.StringToBytes(msg.ReserveAuthority); err != nil {
			return nil, errorsmod.Wrapf(types.ErrInvalidTagReportAction,
				"reserve_authority %q is not an address: %v", msg.ReserveAuthority, err)
		}
	}

	report, err := k.TagReport.Get(ctx, msg.TagName)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrTagReportNotFound, fmt.Sprintf("no report found for tag %s", msg.TagName))
	}

	tagExists, err := k.TagExists(ctx, msg.TagName)
	if err != nil {
		return nil, errorsmod.Wrapf(err, "failed to check tag %q", msg.TagName)
	}

	totalBond, _ := math.NewIntFromString(report.TotalBond)
	if report.TotalBond == "" {
		totalBond = math.ZeroInt()
	}

	switch msg.Action {
	case 0:
		forfeitTagReportBonds(ctx, k.Keeper, msg.TagName, report.Reporters, totalBond)

	case 1:
		if tagExists {
			if err := k.RemoveTag(ctx, msg.TagName); err != nil {
				return nil, errorsmod.Wrap(err, "failed to remove tag")
			}
		}
		if k.late.forumKeeper != nil {
			// Best-effort cleanup of stale references; non-fatal.
			if err := k.late.forumKeeper.PruneTagReferences(ctx, msg.TagName); err != nil {
				return nil, err
			}
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

	case 3:
		// Restore releases a reserved tag back to public use (the report
		// said the reservation was wrong); the reporters are refunded. A
		// removed tag cannot come back this way: its references are gone.
		reserved, err := k.IsReservedTag(ctx, msg.TagName)
		if err != nil {
			return nil, errorsmod.Wrapf(err, "failed to check reservation of %q", msg.TagName)
		}
		if !reserved {
			return nil, errorsmod.Wrapf(types.ErrInvalidTagReportAction, "restore: %q is not a reserved tag", msg.TagName)
		}
		if err := k.RemoveReservedTag(ctx, msg.TagName); err != nil {
			return nil, errorsmod.Wrap(err, "failed to release reserved tag")
		}
		if err := refundTagReportBonds(ctx, k.Keeper, report.Reporters, totalBond); err != nil {
			return nil, errorsmod.Wrap(err, "failed to refund bonds to reporters")
		}

	default:
		// Unreachable (validated above); kept so a future action added to the
		// guard without a case here cannot silently drop the report.
		return nil, errorsmod.Wrapf(types.ErrInvalidTagReportAction, "action %d", msg.Action)
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

// tagReportBondShare is each reporter's share of a report's total bond.
// Integer division truncates remainders (matches forum's prior behaviour).
func tagReportBondShare(recipients []string, totalAmount math.Int) math.Int {
	if len(recipients) == 0 || totalAmount.IsNil() || !totalAmount.IsPositive() {
		return math.ZeroInt()
	}
	return totalAmount.Quo(math.NewInt(int64(len(recipients))))
}

// refundTagReportBonds unlocks the bonded DREAM evenly among reporters. A
// refund that fails (the reporter was zeroed, which burned the lock) is
// reported as an event instead of disappearing silently.
func refundTagReportBonds(ctx context.Context, k Keeper, recipients []string, totalAmount math.Int) error {
	perRecipient := tagReportBondShare(recipients, totalAmount)
	if perRecipient.IsZero() {
		return nil
	}
	for _, recipient := range recipients {
		addrBytes, err := k.addressCodec.StringToBytes(recipient)
		if err != nil {
			continue
		}
		if err := k.UnlockDREAM(ctx, sdk.AccAddress(addrBytes), perRecipient); err != nil {
			emitTagReportBondFailure(ctx, recipient, perRecipient, "refund", err)
		}
	}
	return nil
}

// forfeitTagReportBonds burns every reporter's bond on a dismissed report.
func forfeitTagReportBonds(ctx context.Context, k Keeper, tagName string, recipients []string, totalAmount math.Int) {
	perRecipient := tagReportBondShare(recipients, totalAmount)
	if perRecipient.IsZero() {
		return
	}
	for _, recipient := range recipients {
		addrBytes, err := k.addressCodec.StringToBytes(recipient)
		if err != nil {
			continue
		}
		if err := k.BurnLockedDREAM(ctx, sdk.AccAddress(addrBytes), perRecipient); err != nil {
			emitTagReportBondFailure(ctx, recipient, perRecipient, "forfeit", err)
			continue
		}
		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
			"tag_report_bond_forfeited",
			sdk.NewAttribute("tag_name", tagName),
			sdk.NewAttribute("reporter", recipient),
			sdk.NewAttribute("amount", perRecipient.String()),
		))
	}
}

func emitTagReportBondFailure(ctx context.Context, reporter string, amount math.Int, op string, err error) {
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		"tag_report_bond_settlement_failed",
		sdk.NewAttribute("reporter", reporter),
		sdk.NewAttribute("amount", amount.String()),
		sdk.NewAttribute("operation", op),
		sdk.NewAttribute("error", err.Error()),
	))
}
