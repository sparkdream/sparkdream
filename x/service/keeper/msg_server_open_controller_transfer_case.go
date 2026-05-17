package keeper

import (
	"bytes"
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/service/types"
)

// OpenControllerTransferCase files a jury case proposing to transfer
// the operator's controller to a new x/commons Group. See
// x-service-spec.md §5.4. Validation:
//
//   - opener is either the operator themselves OR a member who meets
//     min_reporter_trust_level (via RepKeeper.MeetsTrustLevel — same
//     gate as MsgReportOperator);
//   - reason size cap;
//   - proposed_new_controller != operator.controller AND must be a
//     registered Group policy address;
//   - no other open case for this (operator, service_type).
//
// On success: escrow the opener's report_deposit; open an x/rep jury
// case via RepKeeper.CreateAppealInitiative; write the
// ControllerTransferCases row + open-by-operator index entry; return
// the new jury_case_id.
func (k msgServer) OpenControllerTransferCase(ctx context.Context, msg *types.MsgOpenControllerTransferCase) (*types.MsgOpenControllerTransferCaseResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	openerBytes, err := k.addrBytes(msg.Opener)
	if err != nil {
		return nil, types.ErrInvalidSigner.Wrap("invalid opener address")
	}
	opBytes, err := k.addrBytes(msg.Operator)
	if err != nil {
		return nil, types.ErrOperatorNotFound.Wrap("invalid operator address")
	}
	if _, err := k.addrBytes(msg.ProposedNewController); err != nil {
		return nil, types.ErrControllerNotGroup.Wrap("invalid proposed_new_controller address")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	if uint32(len(msg.Reason)) > params.MaxReasonBytes {
		return nil, types.ErrInvalidReasonSize.Wrapf("%d > %d", len(msg.Reason), params.MaxReasonBytes)
	}

	op, exists := k.GetOperator(ctx, opBytes, msg.ServiceType)
	if !exists {
		return nil, types.ErrOperatorNotFound.Wrapf("%s / %s", msg.Operator, msg.ServiceType)
	}

	if msg.ProposedNewController == op.Controller {
		return nil, types.ErrInvalidVerdict.Wrap("proposed_new_controller equals current controller")
	}

	// Eligibility: opener is operator OR member meeting
	// min_reporter_trust_level (§5.4). If opener IS the operator, skip
	// the trust-level check (they always have standing in their own
	// case). Otherwise enforce the same gate as MsgReportOperator.
	if !bytes.Equal(openerBytes, opBytes) && k.repKeeper() != nil && params.MinReporterTrustLevel != "" {
		okTrust, err := k.repKeeper().MeetsTrustLevel(ctx, sdk.AccAddress(openerBytes), params.MinReporterTrustLevel)
		if err != nil {
			return nil, err
		}
		if !okTrust {
			return nil, types.ErrReporterTrustLevelTooLow.Wrap(params.MinReporterTrustLevel)
		}
	}

	// Group eligibility check on the proposed new controller.
	if k.commonsKeeper() != nil {
		if !k.commonsKeeper().IsGroupPolicyAddress(ctx, msg.ProposedNewController) {
			return nil, types.ErrControllerNotGroup.Wrap(msg.ProposedNewController)
		}
	}

	// One open case per (operator, service_type) (§5.4).
	openIdxKey := collections.Join(opBytes, msg.ServiceType)
	if _, err := k.OpenControllerTransferByOperator.Get(ctx, openIdxKey); err == nil {
		return nil, types.ErrControllerTransferCaseAlreadyOpen.Wrapf("%s / %s", msg.Operator, msg.ServiceType)
	}

	// Escrow opener deposit (same param as report_deposit per §5.4).
	if err := k.bankKeeper.SendCoinsFromAccountToModule(
		ctx, sdk.AccAddress(openerBytes), types.ModuleName, sdk.NewCoins(params.ReportDeposit),
	); err != nil {
		return nil, types.ErrInsufficientReportDeposit.Wrap(err.Error())
	}

	// Open the jury case via x/rep's generic appeal entry-point
	// (§6.2). Payload JSON-encodes the case-specific data the jurors
	// need to deliberate; deadline = current + max_escalated_blocks.
	var juryCaseID uint64
	if k.repKeeper() != nil {
		payload := []byte(fmt.Sprintf(
			`{"operator":%q,"service_type":%q,"opener":%q,"proposed_new_controller":%q,"reason":%q}`,
			msg.Operator, msg.ServiceType, msg.Opener, msg.ProposedNewController, msg.Reason,
		))
		deadline := currentHeight + params.MaxEscalatedBlocks
		juryCaseID, err = k.repKeeper().CreateAppealInitiative(ctx, "service.controller_transfer", payload, deadline)
		if err != nil {
			return nil, err
		}
	} else {
		// Standalone dev mode (tests without x/rep wired): synthesize
		// a unique-per-block placeholder so the per-(operator,
		// service_type) "one open" index is meaningful. Production
		// wiring always takes the branch above.
		juryCaseID = uint64(currentHeight)<<16 | uint64(len(msg.Reason)&0xFFFF)
	}

	caseRow := types.ControllerTransferCase{
		JuryCaseId:            juryCaseID,
		OperatorAddress:       msg.Operator,
		ServiceType:           msg.ServiceType,
		Opener:                msg.Opener,
		ProposedNewController: msg.ProposedNewController,
		Deposit:               params.ReportDeposit,
		OpenedAt:              currentHeight,
	}
	if err := k.ControllerTransferCases.Set(ctx, juryCaseID, caseRow); err != nil {
		return nil, err
	}
	if err := k.OpenControllerTransferByOperator.Set(ctx, openIdxKey, juryCaseID); err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvent(types.NewControllerTransferCaseOpenedEvent(
		msg.Operator, msg.ServiceType, msg.Opener, msg.ProposedNewController, juryCaseID,
	))
	emitControllerTransferCaseOpened(msg.ServiceType)

	return &types.MsgOpenControllerTransferCaseResponse{JuryCaseId: juryCaseID}, nil
}
