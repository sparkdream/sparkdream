package ante

import (
	"context"

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
		session, err := d.sessionKeeper.GetSession(ctx, execMsg.Granter, execMsg.Grantee)
		if err != nil {
			return ctx, sessiontypes.ErrSessionNotFound
		}
		if !session.Expiration.After(blockTime) {
			return ctx, sessiontypes.ErrSessionExpired
		}
		if session.SpendLimit.IsPositive() {
			hasFeeDelegate = true
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
