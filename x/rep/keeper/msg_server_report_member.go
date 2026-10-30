package keeper

import (
	"context"
	"errors"
	"fmt"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ReportMember allows a sentinel to report a member for misconduct.
func (k msgServer) ReportMember(ctx context.Context, msg *types.MsgReportMember) (*types.MsgReportMemberResponse, error) {
	creatorBytes, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	if _, err := k.addressCodec.StringToBytes(msg.Member); err != nil {
		return nil, errorsmod.Wrap(err, "invalid member address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	if k.SameMember(ctx, msg.Creator, msg.Member) {
		return nil, errorsmod.Wrap(types.ErrCannotReportSelf, "cannot report yourself")
	}

	// A non-member has tier 0 (refused below); only a store error fails here.
	tier, getErr := k.GetReputationTier(ctx, sdk.AccAddress(creatorBytes))
	if getErr != nil && !errors.Is(getErr, types.ErrMemberNotFound) {
		return nil, getErr
	}
	if tier < types.DefaultMinRepTierSentinel {
		return nil, errorsmod.Wrap(types.ErrInsufficientReputation, "insufficient reputation tier to report members")
	}

	sentinelBond := k.getReporterBond(ctx, sdk.AccAddress(creatorBytes))
	if sentinelBond.LT(types.DefaultMinSentinelBond) {
		return nil, errorsmod.Wrap(types.ErrInsufficientSentinelBond, "insufficient sentinel bond to report members")
	}

	// Reports are keyed by the member's canonical address (REP-R-09): a
	// report against a tombstone lands on the identity now at its successor
	// (one report per member; HasOpenMemberReport then refuses the key path
	// and the successor can defend it), and only an existing, non-zeroed
	// member can be reported, so reporter bonds cannot be parked against
	// arbitrary addresses.
	member := k.Canonical(ctx, msg.Member)
	target, err := k.Member.Get(ctx, member)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrMemberNotFound, "%s is not a member", msg.Member)
	}
	if target.Status == types.MemberStatus_MEMBER_STATUS_ZEROED {
		return nil, errorsmod.Wrapf(types.ErrMemberNotActive, "%s is zeroed", member)
	}

	// One open report per member. A resolved report is history and gives way
	// to a new one (it used to block every later report for good).
	if k.HasOpenMemberReport(ctx, member) {
		return nil, errorsmod.Wrap(types.ErrReportAlreadyExists, fmt.Sprintf("active report already exists for member %s", member))
	}
	// ...but not while the member can still appeal the last resolution, whose
	// record the new report would overwrite.
	if prev, err := k.MemberReport.Get(ctx, member); err == nil &&
		prev.ResolvedAction != types.GovActionType_GOV_ACTION_TYPE_UNSPECIFIED &&
		now <= prev.ResolvedAt+types.DefaultAppealDeadline {
		return nil, errorsmod.Wrap(types.ErrReportAlreadyExists, "the previous report on this member is still within its appeal window")
	}
	// ...nor while an appeal of that resolution is pending: its outcome
	// settles the restoration record the new resolution would replace.
	if has, _ := k.MemberActionRestorations.Has(ctx, member); has {
		return nil, errorsmod.Wrap(types.ErrReportAlreadyExists, "the last action on this member is still under appeal")
	}

	action := commontypes.Saturate[types.GovActionType](msg.RecommendedAction)
	if action == types.GovActionType_GOV_ACTION_TYPE_UNSPECIFIED {
		action = types.GovActionType_GOV_ACTION_TYPE_WARNING
	}

	// The reporting bond is the lesser of the sentinel's staked DREAM and
	// DefaultMaxMemberReportBond (co-signers lock the same).
	reportBond := math.MinInt(sentinelBond, types.DefaultMaxMemberReportBond)

	if err := k.LockDREAM(ctx, sdk.AccAddress(creatorBytes), reportBond); err != nil {
		return nil, errorsmod.Wrap(err, "failed to lock DREAM bond")
	}

	report := types.MemberReport{
		Member:            member,
		Reason:            msg.Reason,
		RecommendedAction: action,
		TotalBond:         reportBond.String(),
		CreatedAt:         now,
		Status:            types.MemberReportStatus_MEMBER_REPORT_STATUS_PENDING,
		Reporters:         []string{msg.Creator},
		EvidencePostIds:   []uint64{},
		DefensePostIds:    []uint64{},
		ReporterBonds: []*types.ReporterBondEntry{
			{Address: msg.Creator, Amount: reportBond.String()},
		},
	}

	if err := k.MemberReport.Set(ctx, member, report); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store member report")
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"member_reported",
			sdk.NewAttribute("member", member),
			sdk.NewAttribute("reporter", msg.Creator),
			sdk.NewAttribute("reason", msg.Reason),
			sdk.NewAttribute("recommended_action", action.String()),
		),
	)

	return &types.MsgReportMemberResponse{}, nil
}

// getReporterBond returns the DREAM bond available to back a member report.
// Uses the staked DREAM balance on the member record.
func (k Keeper) getReporterBond(ctx context.Context, addr sdk.AccAddress) math.Int {
	addrStr, err := k.addressCodec.BytesToString(addr)
	if err != nil {
		return math.ZeroInt()
	}
	member, err := k.Member.Get(ctx, addrStr)
	if err != nil {
		return math.ZeroInt()
	}
	if member.StakedDream == nil {
		return math.ZeroInt()
	}
	return *member.StakedDream
}

// refundReportBonds refunds DREAM bonds to each reporter using the per-signer
// locked amount recorded on the report. Falls back to legacy averaged refund
// only when reporter_bonds is empty (defense-in-depth: should not happen for
// reports created after this change).
func (k Keeper) refundReportBonds(ctx context.Context, report types.MemberReport) error {
	if len(report.ReporterBonds) > 0 {
		for _, entry := range report.ReporterBonds {
			if entry == nil {
				continue
			}
			amt, ok := math.NewIntFromString(entry.Amount)
			if !ok || amt.IsZero() {
				continue
			}
			recipientBytes, err := k.addressCodec.StringToBytes(entry.Address)
			if err != nil {
				continue
			}
			if err := k.UnlockDREAM(ctx, sdk.AccAddress(recipientBytes), amt); err != nil {
				return err
			}
		}
		return nil
	}

	// Fallback for any pre-existing reports written before reporter_bonds
	// was added. Should be unreachable on a fresh chain.
	if len(report.Reporters) == 0 {
		return nil
	}
	totalAmount, ok := math.NewIntFromString(report.TotalBond)
	if !ok || totalAmount.IsZero() {
		return nil
	}
	amountPerRecipient := totalAmount.Quo(math.NewInt(int64(len(report.Reporters))))
	if amountPerRecipient.IsZero() {
		return nil
	}
	for _, recipient := range report.Reporters {
		recipientBytes, err := k.addressCodec.StringToBytes(recipient)
		if err != nil {
			continue
		}
		if err := k.UnlockDREAM(ctx, sdk.AccAddress(recipientBytes), amountPerRecipient); err != nil {
			return err
		}
	}
	return nil
}
