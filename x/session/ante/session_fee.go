package ante

import (
	"context"
	"math"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	sessiontypes "sparkdream/x/session/types"
)

// SessionKeeper defines the interface needed by the ante handler.
type SessionKeeper interface {
	GetSession(ctx context.Context, granter, grantee string) (sessiontypes.Session, error)
	UpdateSessionSpent(ctx context.Context, granter, grantee string, feeAmount sdk.Coin) error
}

// BankKeeper defines the bank interface needed by the ante handler.
type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
}

// SessionFeeDecorator intercepts transactions containing MsgExecSession
// and deducts fees from the granter's account instead of the grantee.
// Follows the same ContextKeyFeePaid pattern as ShieldGasDecorator.
type SessionFeeDecorator struct {
	sessionKeeper SessionKeeper
	bankKeeper    BankKeeper
}

// NewSessionFeeDecorator creates a new SessionFeeDecorator.
func NewSessionFeeDecorator(sk SessionKeeper, bk BankKeeper) SessionFeeDecorator {
	return SessionFeeDecorator{
		sessionKeeper: sk,
		bankKeeper:    bk,
	}
}

func (d SessionFeeDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	msgs := tx.GetMsgs()

	// 1-2. Check if any message is MsgExecSession
	hasExecSession := false
	for _, msg := range msgs {
		if _, ok := msg.(*sessiontypes.MsgExecSession); ok {
			hasExecSession = true
			break
		}
	}

	if !hasExecSession {
		return next(ctx, tx, simulate)
	}

	// 3. Reject mixed transactions
	for _, msg := range msgs {
		if _, ok := msg.(*sessiontypes.MsgExecSession); !ok {
			return ctx, sessiontypes.ErrMixedTransaction
		}
	}

	// 4. All MsgExecSession must share the same granter
	var granter string
	for _, msg := range msgs {
		execMsg := msg.(*sessiontypes.MsgExecSession)
		if granter == "" {
			granter = execMsg.Granter
		} else if granter != execMsg.Granter {
			return ctx, sessiontypes.ErrMultipleGranters
		}
	}

	// 5-6. Validate each session exists, is not expired, and has spend budget.
	// SESSION-S2-1 fix: budget is checked and debited in fee units (uspark) here
	// in the ante. The ante runs unconditionally before msg dispatch and the fee
	// is deducted regardless of inner-msg outcome, so this is also the only
	// place where Spent accounting can correctly reflect every fee-paying
	// attempt (SESSION-S2-2).
	blockTime := ctx.BlockTime()
	hasFeeDelegate := false

	feeTx, isFeeTx := tx.(sdk.FeeTx)

	for _, msg := range msgs {
		execMsg := msg.(*sessiontypes.MsgExecSession)
		// An empty exec does nothing but spend the granter's fee budget;
		// refuse it before any fee moves (the msg server refuses it too).
		if len(execMsg.Msgs) == 0 {
			return ctx, errorsmod.Wrap(sessiontypes.ErrEmptyMsgs, "exec session carries no messages")
		}
		session, err := d.sessionKeeper.GetSession(ctx, execMsg.Granter, execMsg.Grantee)
		if err != nil {
			return ctx, sessiontypes.ErrSessionNotFound
		}
		if !session.Expiration.After(blockTime) {
			return ctx, sessiontypes.ErrSessionExpired
		}
		// A used-up session must not keep landing (fee-free or granter-paid)
		// txs that only fail in the msg server.
		if session.MaxExecCount > 0 && session.ExecCount+uint64(len(execMsg.Msgs)) > session.MaxExecCount {
			return ctx, sessiontypes.ErrExecCountExceeded
		}
		if session.SpendLimit.IsPositive() {
			hasFeeDelegate = true
			// An exhausted budget pays nothing, so the tx would ride for free.
			if !session.Spent.Amount.LT(session.SpendLimit.Amount) {
				return ctx, sessiontypes.ErrSpendLimitExceeded
			}
			if isFeeTx {
				fees := feeTx.GetFee()
				for _, fee := range fees {
					// Reject fees outside the spend limit's denom scope — silently
					// ignoring them would let the granter be charged in a denom
					// they never authorized.
					if fee.Denom != session.SpendLimit.Denom {
						return ctx, sessiontypes.ErrSpendLimitExceeded
					}
					remaining := session.SpendLimit.Amount.Sub(session.Spent.Amount)
					if fee.Amount.GT(remaining) {
						return ctx, sessiontypes.ErrSpendLimitExceeded
					}
				}
			}
		}
	}

	// 7. If fee delegation active, transfer fees from granter to fee_collector
	// then debit each session's spend-limit budget by the full fee. When a tx
	// batches multiple MsgExecSessions under the same granter, we charge the
	// full fee to each participating session: defensible (overcounts slightly)
	// and simpler than apportioning, given the budget check above already
	// enforced this margin per session.
	if hasFeeDelegate {
		if !isFeeTx {
			return ctx, sdkerrors.ErrTxDecode
		}
		fees := feeTx.GetFee()

		// The standard DeductFeeDecorator, which the fee-paid flag below
		// skips, is where CheckTx enforces the node's minimum gas prices and
		// sets the tx priority. Do both here, or every session tx could ride
		// with a zero fee and any gas limit (SES-01).
		if ctx.IsCheckTx() && !simulate {
			if err := checkMinGasPrices(ctx, fees, feeTx.GetGas()); err != nil {
				return ctx, err
			}
		}
		ctx = ctx.WithPriority(txPriority(fees, feeTx.GetGas()))

		if !fees.IsZero() {
			granterAddr, err := sdk.AccAddressFromBech32(granter)
			if err != nil {
				return ctx, err
			}

			err = d.bankKeeper.SendCoinsFromAccountToModule(
				ctx,
				granterAddr,
				authtypes.FeeCollectorName,
				fees,
			)
			if err != nil {
				return ctx, err
			}

			// Persist Spent debit per session, after the fee deduction succeeded.
			for _, msg := range msgs {
				execMsg := msg.(*sessiontypes.MsgExecSession)
				session, err := d.sessionKeeper.GetSession(ctx, execMsg.Granter, execMsg.Grantee)
				if err != nil {
					return ctx, sessiontypes.ErrSessionNotFound
				}
				if !session.SpendLimit.IsPositive() {
					continue
				}
				for _, fee := range fees {
					if fee.Denom != session.SpendLimit.Denom {
						continue
					}
					if err := d.sessionKeeper.UpdateSessionSpent(ctx, execMsg.Granter, execMsg.Grantee, fee); err != nil {
						return ctx, err
					}
				}
			}
		}

		// Set fee-paid flag so SkipIfFeePaidDecorator skips the standard DeductFeeDecorator
		ctx = ctx.WithValue(sessiontypes.ContextKeySessionFeePaid, true)
	}

	return next(ctx, tx, simulate)
}

// checkMinGasPrices mirrors the SDK's CheckTx minimum-gas-price rule
// (fee >= ceil(minGasPrice * gasLimit) in at least one denom). It is
// node-local and CheckTx-only, exactly like the SDK check it replaces.
func checkMinGasPrices(ctx sdk.Context, fees sdk.Coins, gas uint64) error {
	minGasPrices := ctx.MinGasPrices()
	if minGasPrices.IsZero() {
		return nil
	}
	required := make(sdk.Coins, len(minGasPrices))
	glDec := sdkmath.LegacyNewDecFromInt(sdkmath.NewIntFromUint64(gas))
	for i, gp := range minGasPrices {
		required[i] = sdk.NewCoin(gp.Denom, gp.Amount.Mul(glDec).Ceil().RoundInt())
	}
	if !fees.IsAnyGTE(required) {
		return errorsmod.Wrapf(sdkerrors.ErrInsufficientFee, "insufficient fees; got: %s required: %s", fees, required)
	}
	return nil
}

// txPriority is the SDK's default fee priority: the smallest per-denom gas
// price of the fee, so a session tx competes on the same terms as any other.
func txPriority(fees sdk.Coins, gas uint64) int64 {
	if gas == 0 {
		return 0
	}
	var priority int64
	for _, c := range fees {
		p := int64(math.MaxInt64)
		gasPrice := c.Amount.Quo(sdkmath.NewIntFromUint64(gas))
		if gasPrice.IsInt64() {
			p = gasPrice.Int64()
		}
		if priority == 0 || p < priority {
			priority = p
		}
	}
	return priority
}
