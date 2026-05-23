package keeper

import (
	"context"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/service/types"
)

// TopUpBond (msg-server) is the wire-format entry point. The bulk of
// the logic lives on the Keeper's public TopUpBond method so other
// modules (notably x/federation) can perform a top-up from inside a
// handler without round-tripping through the msg router.
func (k msgServer) TopUpBond(ctx context.Context, msg *types.MsgTopUpBond) (*types.MsgTopUpBondResponse, error) {
	opBytes, err := k.addrBytes(msg.Operator)
	if err != nil {
		return nil, types.ErrInvalidSigner.Wrap("invalid operator address")
	}
	if err := k.Keeper.TopUpBond(ctx, opBytes, msg.ServiceType, msg.AdditionalBondAmount); err != nil {
		return nil, err
	}
	return &types.MsgTopUpBondResponse{}, nil
}

// TopUpBond (keeper-level public API) adds SPARK to an operator's bond.
// Allowed for ACTIVE and UNDERFUNDED status; rejected for UNBONDING
// (operator is exiting) and terminal (record not in live store). See
// §3.4 disabled-type table — top-up is also allowed for operators of a
// disabled service type, so no enabled-flag check.
//
// State machine: an UNDERFUNDED operator that tops up back to ≥ min_bond
// transitions to ACTIVE and the AfterOperatorReFunded hook fires.
// settleBondBlocks runs around the bond mutation so reputation accrual
// sees the correct ACTIVE intervals.
//
// Used by x/federation's bridge-operator flow on the "Exists" branch of
// MsgRegisterBridge when the registrant already has an Operator under
// the same service_type and supplies additional stake (Phase 0 of the
// federation→service migration plan).
func (k Keeper) TopUpBond(ctx context.Context, opBytes []byte, serviceType string, additionalBondAmount sdkmath.Int) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	op, exists := k.GetOperator(ctx, opBytes, serviceType)
	if !exists {
		opStr, _ := k.addressCodec.BytesToString(opBytes)
		return types.ErrOperatorNotFound.Wrapf("%s / %s", opStr, serviceType)
	}
	if op.Status == types.OperatorStatus_OPERATOR_STATUS_UNBONDING {
		return types.ErrOperatorUnbonding
	}

	if additionalBondAmount.IsNil() || !additionalBondAmount.IsPositive() {
		return types.ErrInsufficientBond.Wrap("additional_bond_amount must be a positive Int")
	}
	bondDenom := k.BondDenom(ctx)
	additionalBond := sdk.NewCoin(bondDenom, additionalBondAmount)

	// Settle BEFORE the bond change so the ACTIVE-period accrual to date
	// captures the old bond × elapsed-blocks contribution (§6.6).
	k.settleBondBlocks(&op, sdkCtx.BlockHeight())

	// Move SPARK from operator wallet to module bond pool.
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, sdk.AccAddress(opBytes), types.ModuleName, sdk.NewCoins(additionalBond)); err != nil {
		return err
	}

	op.BondAmount = op.BondAmount.Add(additionalBondAmount)

	// If UNDERFUNDED and the new bond clears min_bond, return to ACTIVE
	// and fire the AfterOperatorReFunded hook. The hook fires AFTER
	// PutOperator so any consumer iterating live indexes sees the
	// settled state.
	transitionedToActive := false
	if op.Status == types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED {
		cfg, err := k.resolveServiceTypeConfig(ctx, op.ServiceType)
		if err != nil {
			return err
		}
		if op.BondAmount.GTE(cfg.MinBondAmount) {
			op.Status = types.OperatorStatus_OPERATOR_STATUS_ACTIVE
			// Clear underfunded_since BEFORE PutOperator so the
			// UnderfundedQueue removal in PutOperator's index logic
			// fires correctly.
			oldUnderfundedSince := op.UnderfundedSince
			op.UnderfundedSince = 0

			// PutOperator's index logic removes the queue entry for the
			// CURRENT (op.UnderfundedSince, ...) tuple — which is now 0.
			// Manually remove the old queue entry first.
			if oldUnderfundedSince > 0 {
				if err := k.UnderfundedQueue.Remove(ctx, collections.Join3(oldUnderfundedSince, opBytes, op.ServiceType)); err != nil {
					return err
				}
			}
			transitionedToActive = true
		}
	}

	if err := k.PutOperator(ctx, op); err != nil {
		return err
	}

	opStr, _ := k.addressCodec.BytesToString(opBytes)
	sdkCtx.EventManager().EmitEvent(types.NewOperatorToppedUpEvent(
		opStr, op.ServiceType, additionalBond, sdk.NewCoin(bondDenom, op.BondAmount),
	))

	// Hook fire AFTER state writes so consumers iterating live indexes
	// (e.g. federation's BindingsByOperator) see the post-transition
	// state.
	if transitionedToActive && k.hooks() != nil {
		k.hooks().AfterOperatorReFunded(ctx, sdk.AccAddress(opBytes), op.ServiceType)
	}

	return nil
}
