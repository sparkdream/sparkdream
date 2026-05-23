package keeper

import (
	"context"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/service/types"
)

// FinalizeControllerTransfer applies the jury verdict to a
// controller-transfer case. See x-service-spec.md §5.4. Signer MUST be
// a member of the Commons Council Operations Committee — same pattern
// as `MsgResolveGovActionAppeal` and forum's appeal-resolve handlers.
//
// The resolver's verdict is cross-checked against the underlying
// x/rep JuryReview's tallied verdict (see §6.2): a resolver cannot
// forge a verdict the jurors didn't actually reach.
//
// ACCEPT path: re-check Group eligibility at apply time (the target
// Group may have been dissolved or emptied during the jury case); if
// the re-check fails, treat as REJECT (deposit forfeited) rather than
// leaving the case dangling. On pass: update operator.controller,
// refund opener deposit, delete case + index.
//
// REJECT path: forfeit opener deposit to community pool, delete case +
// index.
//
// Note: msg.JuryAuthority is the resolver's bech32 address (the proto
// field name is retained for backward compatibility; conceptually it
// means "the council member relaying the jury verdict").
func (k msgServer) FinalizeControllerTransfer(ctx context.Context, msg *types.MsgFinalizeControllerTransfer) (*types.MsgFinalizeControllerTransferResponse, error) {
	// Authority gate: Commons Operations Committee (§5.4).
	if err := k.assertCouncilResolver(ctx, msg.JuryAuthority); err != nil {
		return nil, err
	}

	caseRow, err := k.ControllerTransferCases.Get(ctx, msg.JuryCaseId)
	if err != nil {
		return nil, types.ErrControllerTransferCaseNotFound.Wrap(err.Error())
	}

	// Verdict cross-check against the underlying JuryReview (§6.2).
	if err := k.crossCheckTransferVerdict(ctx, caseRow.JuryCaseId, msg.Verdict); err != nil {
		return nil, err
	}

	switch msg.Verdict {
	case types.TransferVerdict_TRANSFER_VERDICT_ACCEPT:
		return k.applyControllerTransferAccept(ctx, caseRow, msg.NewController)
	case types.TransferVerdict_TRANSFER_VERDICT_REJECT:
		return k.applyControllerTransferReject(ctx, caseRow)
	default:
		return nil, types.ErrInvalidVerdict.Wrap(msg.Verdict.String())
	}
}

// crossCheckTransferVerdict verifies that the resolver's submitted
// controller-transfer verdict is consistent with the underlying
// JuryReview's tallied verdict (§6.2 mapping table). For binary
// transfer verdicts:
//
//   - ACCEPT requires JuryReview verdict == UPHOLD_CHALLENGE
//   - REJECT requires JuryReview verdict ∈ {REJECT_CHALLENGE, INCONCLUSIVE}
//
// Skipped in standalone mode (repKeeper not wired).
func (k Keeper) crossCheckTransferVerdict(ctx context.Context, juryCaseID uint64, submittedVerdict types.TransferVerdict) error {
	if k.repKeeper() == nil || juryCaseID == 0 {
		return nil
	}
	verdictName, err := k.repKeeper().GetJuryVerdictName(ctx, juryCaseID)
	if err != nil {
		return types.ErrJuryVerdictMismatch.Wrapf("could not fetch JuryReview %d: %v", juryCaseID, err)
	}
	switch submittedVerdict {
	case types.TransferVerdict_TRANSFER_VERDICT_ACCEPT:
		if verdictName != types.JuryVerdictUpholdChallenge {
			return types.ErrJuryVerdictMismatch.Wrapf(
				"resolver submitted ACCEPT but jury voted %s", verdictName,
			)
		}
	case types.TransferVerdict_TRANSFER_VERDICT_REJECT:
		if verdictName != types.JuryVerdictRejectChallenge && verdictName != types.JuryVerdictInconclusive {
			return types.ErrJuryVerdictMismatch.Wrapf(
				"resolver submitted REJECT but jury voted %s", verdictName,
			)
		}
	}
	return nil
}

// applyControllerTransferAccept performs the ACCEPT path: re-check
// Group eligibility, mutate operator.controller, refund opener
// deposit, delete case.
//
// If the re-check fails (Group dissolved / empty), this routes through
// the REJECT path instead (deposit forfeited) so a stale jury verdict
// can't strand the operator in a bad state.
func (k msgServer) applyControllerTransferAccept(
	ctx context.Context,
	caseRow types.ControllerTransferCase,
	newController string,
) (*types.MsgFinalizeControllerTransferResponse, error) {
	// new_controller from the message must match the case payload
	// (defensive: x/rep should pass through the same value).
	if newController != caseRow.ProposedNewController {
		return nil, types.ErrInvalidVerdict.Wrap("new_controller mismatch with opened case")
	}

	// Apply-time re-check: Group must still be registered AND have at
	// least one active member (§5.4). On failure, fall through to
	// REJECT semantics so the case isn't left dangling.
	if k.commonsKeeper() != nil {
		if !k.commonsKeeper().IsGroupPolicyAddress(ctx, newController) {
			return k.applyControllerTransferReject(ctx, caseRow)
		}
		count, err := k.commonsKeeper().GroupPolicyMemberCount(ctx, newController)
		if err != nil {
			return nil, err
		}
		if count < 1 {
			return k.applyControllerTransferReject(ctx, caseRow)
		}
	}

	// Locate the live operator record and update its controller. This
	// also moves the OperatorsByController index entry (the old
	// controller key drops, the new one is added) — PutOperator
	// rebuilds the indexes for the current op.Controller value, but
	// the OLD entry is keyed on the previous controller and must be
	// dropped manually before swap.
	opBytes, err := k.addrBytes(caseRow.OperatorAddress)
	if err != nil {
		return nil, err
	}
	op, exists := k.GetOperator(ctx, opBytes, caseRow.ServiceType)
	if !exists {
		// Operator archived since the case was opened. Treat as REJECT
		// so the deposit doesn't sit forever (the case is now moot).
		return k.applyControllerTransferReject(ctx, caseRow)
	}

	// Remove the OLD controller's index entry.
	oldControllerBytes, err := k.addrBytes(op.Controller)
	if err == nil {
		_ = k.OperatorsByController.Remove(ctx, collections.Join3(oldControllerBytes, opBytes, op.ServiceType))
	}

	op.Controller = newController
	if err := k.PutOperator(ctx, op); err != nil {
		return nil, err
	}

	// Refund opener deposit.
	if !caseRow.Deposit.IsZero() {
		openerBytes, err := k.addrBytes(caseRow.Opener)
		if err != nil {
			return nil, err
		}
		depositCoin := sdk.NewCoin(k.BondDenom(ctx), caseRow.Deposit)
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(
			ctx, types.ModuleName, sdk.AccAddress(openerBytes), sdk.NewCoins(depositCoin),
		); err != nil {
			return nil, err
		}
	}

	// Delete case + open-by-operator index entry.
	if err := k.ControllerTransferCases.Remove(ctx, caseRow.JuryCaseId); err != nil {
		return nil, err
	}
	_ = k.OpenControllerTransferByOperator.Remove(ctx, collections.Join(opBytes, caseRow.ServiceType))

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvents(sdk.Events{
		types.NewControllerTransferredEvent(caseRow.OperatorAddress, caseRow.ServiceType, newController, caseRow.JuryCaseId),
		types.NewControllerTransferCaseFinalizedEvent(caseRow.JuryCaseId, "ACCEPT", types.CtfDepositOpenerRefunded),
	})
	emitControllerTransferCaseFinalized(caseRow.ServiceType, "ACCEPT")

	return &types.MsgFinalizeControllerTransferResponse{}, nil
}

// applyControllerTransferReject forfeits opener deposit to community
// pool and deletes the case.
func (k msgServer) applyControllerTransferReject(
	ctx context.Context,
	caseRow types.ControllerTransferCase,
) (*types.MsgFinalizeControllerTransferResponse, error) {
	if err := k.forfeitDepositToCommunityPool(ctx, caseRow.Deposit); err != nil {
		return nil, err
	}

	if err := k.ControllerTransferCases.Remove(ctx, caseRow.JuryCaseId); err != nil {
		return nil, err
	}

	opBytes, err := k.addrBytes(caseRow.OperatorAddress)
	if err == nil {
		_ = k.OpenControllerTransferByOperator.Remove(ctx, collections.Join(opBytes, caseRow.ServiceType))
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		types.NewControllerTransferCaseFinalizedEvent(caseRow.JuryCaseId, "REJECT", types.CtfDepositCommunityPool),
	)
	emitControllerTransferCaseFinalized(caseRow.ServiceType, "REJECT")

	return &types.MsgFinalizeControllerTransferResponse{}, nil
}
