package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/service/types"
)

// UnbondOperator transitions ACTIVE | UNDERFUNDED → UNBONDING and
// starts the unbonding clock. See x-service-spec.md §3.5. MUST be
// signed by `operator` directly (not delegable via x/session or
// x/authz — the signer-check is enforced by the SDK's signer rules
// per the proto signer option).
func (k msgServer) UnbondOperator(ctx context.Context, msg *types.MsgUnbondOperator) (*types.MsgUnbondOperatorResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	opBytes, err := k.addrBytes(msg.Operator)
	if err != nil {
		return nil, types.ErrInvalidSigner.Wrap("invalid operator address")
	}

	op, exists := k.GetOperator(ctx, opBytes, msg.ServiceType)
	if !exists {
		return nil, types.ErrOperatorNotFound.Wrapf("%s / %s", msg.Operator, msg.ServiceType)
	}

	// Already UNBONDING is a no-op error — operator can't double-unbond.
	if op.Status == types.OperatorStatus_OPERATOR_STATUS_UNBONDING {
		return nil, types.ErrOperatorUnbonding
	}
	// Only ACTIVE or UNDERFUNDED can start unbonding.
	if op.Status != types.OperatorStatus_OPERATOR_STATUS_ACTIVE &&
		op.Status != types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED {
		return nil, types.ErrOperatorNotActive.Wrap(op.Status.String())
	}

	cfg, err := k.resolveServiceTypeConfig(ctx, op.ServiceType)
	if err != nil {
		return nil, err
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Settle BEFORE the status change so accrual stops at this height
	// (ACTIVE → UNBONDING is a status-exit-from-ACTIVE, §6.6).
	currentHeight := sdkCtx.BlockHeight()
	k.settleBondBlocks(&op, currentHeight)

	op.Status = types.OperatorStatus_OPERATOR_STATUS_UNBONDING
	op.UnbondCompleteAt = currentHeight + k.effectiveUnbondingPeriodBlocks(cfg, params)
	// UNDERFUNDED → UNBONDING: clear underfunded_since so PutOperator's
	// index logic drops the UnderfundedQueue entry.
	if op.UnderfundedSince > 0 {
		oldUnderfundedSince := op.UnderfundedSince
		op.UnderfundedSince = 0
		if err := k.removeUnderfundedQueueEntry(ctx, oldUnderfundedSince, opBytes, op.ServiceType); err != nil {
			return nil, err
		}
	}

	if err := k.PutOperator(ctx, op); err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvent(types.NewOperatorUnbondStartedEvent(
		msg.Operator, msg.ServiceType, op.UnbondCompleteAt, types.UnbondSourceVoluntary,
	))
	emitOperatorUnbondStarted(msg.ServiceType, types.UnbondSourceVoluntary)

	return &types.MsgUnbondOperatorResponse{}, nil
}
