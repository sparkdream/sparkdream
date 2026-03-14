package ante

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	shieldtypes "sparkdream/x/shield/types"
)

// ShieldKeeper defines the interface needed by the ante handler.
type ShieldKeeper interface {
	GetShieldParams(ctx sdk.Context) (shieldtypes.Params, error)
}

// BankKeeper defines the bank interface needed by the ante handler.
type BankKeeper interface {
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
}

// ShieldGasDecorator intercepts transactions containing MsgShieldedExec
// and deducts fees from the shield module account instead of the submitter.
type ShieldGasDecorator struct {
	shieldKeeper ShieldKeeper
	bankKeeper   BankKeeper
}

// NewShieldGasDecorator creates a new ShieldGasDecorator.
func NewShieldGasDecorator(sk ShieldKeeper, bk BankKeeper) ShieldGasDecorator {
	return ShieldGasDecorator{
		shieldKeeper: sk,
		bankKeeper:   bk,
	}
}

func (d ShieldGasDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	msgs := tx.GetMsgs()

	// Check if any message is MsgShieldedExec
	hasShieldedExec := false
	for _, msg := range msgs {
		if _, ok := msg.(*shieldtypes.MsgShieldedExec); ok {
			hasShieldedExec = true
			break
		}
	}

	if !hasShieldedExec {
		return next(ctx, tx, simulate)
	}

	// REJECT multi-message transactions containing MsgShieldedExec
	if len(msgs) != 1 {
		return ctx, shieldtypes.ErrMultiMsgNotAllowed
	}

	// Check shield module is enabled
	params, err := d.shieldKeeper.GetShieldParams(ctx)
	if err != nil {
		return ctx, err
	}
	if !params.Enabled {
		return ctx, shieldtypes.ErrShieldDisabled
	}

	// Calculate fee from gas
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, sdkerrors.ErrTxDecode
	}
	fees := feeTx.GetFee()

	if fees.IsZero() {
		// No fees to pay — proceed (gas is still metered)
		ctx = ctx.WithValue(shieldtypes.ContextKeyFeePaid, true)
		return next(ctx, tx, simulate)
	}

	// Deduct fees from shield module account → fee collector
	err = d.bankKeeper.SendCoinsFromModuleToModule(
		ctx,
		shieldtypes.ModuleName,
		authtypes.FeeCollectorName,
		fees,
	)
	if err != nil {
		return ctx, shieldtypes.ErrShieldGasDepleted
	}

	// Set fee-paid flag so the standard DeductFeeDecorator skips this tx
	ctx = ctx.WithValue(shieldtypes.ContextKeyFeePaid, true)
	return next(ctx, tx, simulate)
}
