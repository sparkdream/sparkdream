package keeper

import (
	"context"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/service/types"
)

// ReportOperator files a new PENDING report against an operator. See
// x-service-spec.md §3.4.6 + §5.2. Validation gates:
//
//   - reporter trust level >= min_reporter_trust_level (via
//     RepKeeper.MeetsTrustLevel — skipped in standalone-mode tests
//     when repKeeper is nil);
//   - reason size cap;
//   - operator exists in the live store and is not SLASHED (archive
//     check is implicit because SLASHED isn't in the live store);
//   - reporter is NOT a member of the operator's controller Group
//     (CommonsKeeper.IsGroupPolicyMember — §3.4.6);
//   - reporter rate-limit (sliding window — §3.4.6);
//   - no active refile cooldown.
//
// On success: escrow `report_deposit` SPARK from reporter; create a
// PENDING Report; add to ReportsByOperator and PendingReportsQueue
// indexes; record the reporter's filing in the rate-limit ring buffer;
// return the new report_id.
func (k msgServer) ReportOperator(ctx context.Context, msg *types.MsgReportOperator) (*types.MsgReportOperatorResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	reporterBytes, err := k.addrBytes(msg.Reporter)
	if err != nil {
		return nil, types.ErrInvalidSigner.Wrap("invalid reporter address")
	}
	opBytes, err := k.addrBytes(msg.Operator)
	if err != nil {
		return nil, types.ErrOperatorNotFound.Wrap("invalid operator address")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	if uint32(len(msg.Reason)) > params.MaxReasonBytes {
		return nil, types.ErrInvalidReasonSize.Wrapf("%d > %d", len(msg.Reason), params.MaxReasonBytes)
	}

	// Reporter trust-level gate (§3.4.6). Skipped in standalone mode
	// (repKeeper nil). With repKeeper wired, the adapter resolves the
	// reporter's current trust level and rejects below
	// min_reporter_trust_level. Empty param means "no gate".
	if k.repKeeper() != nil && params.MinReporterTrustLevel != "" {
		ok, err := k.repKeeper().MeetsTrustLevel(ctx, sdk.AccAddress(reporterBytes), params.MinReporterTrustLevel)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, types.ErrReporterTrustLevelTooLow.Wrap(params.MinReporterTrustLevel)
		}
	}

	op, exists := k.GetOperator(ctx, opBytes, msg.ServiceType)
	if !exists {
		return nil, types.ErrOperatorNotFound.Wrapf("%s / %s", msg.Operator, msg.ServiceType)
	}

	// Reporter cannot be a member of the operator's controller group
	// (§3.4.6). Nil-check on commonsKeeper: skip in standalone mode.
	if k.commonsKeeper() != nil {
		isMember, err := k.commonsKeeper().IsGroupPolicyMember(ctx, op.Controller, msg.Reporter)
		if err != nil {
			return nil, err
		}
		if isMember {
			return nil, types.ErrReporterIsControllerMember.Wrapf("%s is a member of %s", msg.Reporter, op.Controller)
		}
	}

	// Refile cooldown is keyed on the operator's CURRENT controller —
	// if a prior report from the same controller was AUTO_DISMISSED
	// recently, block re-filing for `report_refile_cooldown_blocks`.
	controllerBytes, err := k.addrBytes(op.Controller)
	if err != nil {
		return nil, err
	}
	cooldownActive, err := k.isRefileCooldownActive(ctx, params, controllerBytes, opBytes, msg.ServiceType, currentHeight)
	if err != nil {
		return nil, err
	}
	if cooldownActive {
		return nil, types.ErrRefileCooldownActive
	}

	// Per-reporter sliding-window rate-limit.
	if err := k.checkReporterRateLimit(ctx, params, reporterBytes, opBytes, msg.ServiceType, currentHeight); err != nil {
		return nil, err
	}

	// Escrow the report deposit.
	if !params.ReportDepositAmount.IsPositive() {
		return nil, types.ErrInvalidParams.Wrap("report_deposit_amount must be positive")
	}
	depositCoin := sdk.NewCoin(k.BondDenom(ctx), params.ReportDepositAmount)
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, sdk.AccAddress(reporterBytes), types.ModuleName, sdk.NewCoins(depositCoin)); err != nil {
		return nil, types.ErrInsufficientReportDeposit.Wrap(err.Error())
	}

	// Allocate report_id.
	reportID, err := k.NextReportID.Next(ctx)
	if err != nil {
		return nil, err
	}
	// Sequences start at 0; report_id should be 1-indexed for human
	// audit. Bump if zero.
	if reportID == 0 {
		reportID, err = k.NextReportID.Next(ctx)
		if err != nil {
			return nil, err
		}
	}

	report := types.Report{
		ReportId:         reportID,
		OperatorAddress:  msg.Operator,
		ServiceType:      msg.ServiceType,
		Reporter:         msg.Reporter,
		Reason:           msg.Reason,
		FiledAt:          currentHeight,
		EscalatedAt:      0,
		Status:           types.ReportStatus_REPORT_STATUS_PENDING,
		ProposedSlashBps: 0,
		SlashAmount:      sdkmath.ZeroInt(),
		Deposit:          params.ReportDepositAmount,
		JuryCaseId:       0,
	}
	if err := k.Reports.Set(ctx, reportID, report); err != nil {
		return nil, err
	}
	if err := k.ReportsByOperator.Set(ctx, collections.Join3(opBytes, msg.ServiceType, reportID)); err != nil {
		return nil, err
	}
	if err := k.PendingReportsQueue.Set(ctx, collections.Join(currentHeight, reportID)); err != nil {
		return nil, err
	}

	// Update reporter's rate-limit ring buffer.
	if err := k.recordReporterFiling(ctx, params, reporterBytes, opBytes, msg.ServiceType, currentHeight, msg.Reporter, msg.Operator); err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvent(types.NewReportFiledEvent(
		reportID, msg.Reporter, msg.Operator, msg.ServiceType, depositCoin,
	))
	emitReportFiled(msg.ServiceType)

	return &types.MsgReportOperatorResponse{ReportId: reportID}, nil
}
