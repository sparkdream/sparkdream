package ante

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	shieldtypes "sparkdream/x/shield/types"
)

// SkipIfFeePaidDecorator wraps the standard DeductFeeDecorator and skips fee deduction
// if the ContextKeyFeePaid flag has been set by a prior decorator (ShieldGasDecorator).
type SkipIfFeePaidDecorator struct {
	inner sdk.AnteDecorator
}

// NewSkipIfFeePaidDecorator wraps an existing AnteDecorator (typically DeductFeeDecorator)
// so it is skipped when fees were already paid by the shield module.
func NewSkipIfFeePaidDecorator(inner sdk.AnteDecorator) SkipIfFeePaidDecorator {
	return SkipIfFeePaidDecorator{inner: inner}
}

func (d SkipIfFeePaidDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if feePaid, ok := ctx.Value(shieldtypes.ContextKeyFeePaid).(bool); ok && feePaid {
		// Fees already handled by ShieldGasDecorator — skip inner decorator
		return next(ctx, tx, simulate)
	}
	return d.inner.AnteHandle(ctx, tx, simulate, next)
}
