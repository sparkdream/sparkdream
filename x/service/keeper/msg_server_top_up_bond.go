package keeper

import (
	"context"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/service/types"
)

// TopUpBond adds SPARK to an operator's bond. Allowed for ACTIVE and
// UNDERFUNDED status; rejected for UNBONDING (operator is exiting) and
// terminal (record not in live store). See §3.4 disabled-type table —
// top-up is also allowed for operators of a disabled service type, so
// no enabled-flag check.
//
// State machine: an UNDERFUNDED operator that tops up back to ≥ min_bond
// transitions to ACTIVE; settleBondBlocks runs around the bond mutation
// so reputation accrual sees the correct ACTIVE intervals.
func (k msgServer) TopUpBond(ctx context.Context, msg *types.MsgTopUpBond) (*types.MsgTopUpBondResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	opBytes, err := k.addrBytes(msg.Operator)
	if err != nil {
		return nil, types.ErrInvalidSigner.Wrap("invalid operator address")
	}

	op, exists := k.GetOperator(ctx, opBytes, msg.ServiceType)
	if !exists {
		return nil, types.ErrOperatorNotFound.Wrapf("%s / %s", msg.Operator, msg.ServiceType)
	}
	if op.Status == types.OperatorStatus_OPERATOR_STATUS_UNBONDING {
		return nil, types.ErrOperatorUnbonding
	}

	if msg.AdditionalBond.Denom != types.BondDenom {
		return nil, types.ErrBondDenomMismatch.Wrapf("expected %s, got %s", types.BondDenom, msg.AdditionalBond.Denom)
	}

	// Settle BEFORE the bond change so the ACTIVE-period accrual to date
	// captures the old bond × elapsed-blocks contribution (§6.6).
	k.settleBondBlocks(&op, sdkCtx.BlockHeight())

	// Move SPARK from operator wallet to module bond pool.
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, sdk.AccAddress(opBytes), types.ModuleName, sdk.NewCoins(msg.AdditionalBond)); err != nil {
		return nil, err
	}

	op.Bond = op.Bond.Add(msg.AdditionalBond)

	// If UNDERFUNDED and the new bond clears min_bond, return to ACTIVE.
	if op.Status == types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED {
		cfg, err := k.resolveServiceTypeConfig(ctx, op.ServiceType)
		if err != nil {
			return nil, err
		}
		if op.Bond.Amount.GTE(cfg.MinBond.Amount) {
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
					return nil, err
				}
			}
		}
	}

	if err := k.PutOperator(ctx, op); err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvent(types.NewOperatorToppedUpEvent(
		msg.Operator, msg.ServiceType, msg.AdditionalBond, op.Bond,
	))

	return &types.MsgTopUpBondResponse{}, nil
}
