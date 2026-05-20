package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	sessiontypes "sparkdream/x/session/types"
)

// SessionClaimHook implements `sessiontypes.GrantClaimHook` against
// the x/commons spend-precondition surface. It is the load-bearing
// piece of the x/commons RecurringSpend migration: it lets x/session
// route every claim/pull/oneshot operation initiated by a council
// policy address through the same activation, term-expiry, and
// per-epoch rate-limit gates that today's `CheckSpendPreconditions`
// applies to `MsgSpendFromCommons` and `MsgClaimRecurringSpend`.
//
// Behaviour:
//   - **PreCheck**: if `grant.Granter` is a registered group policy,
//     runs `checkSpendGates` (activation + term + per-epoch CHECK with
//     no write). A non-nil error vetoes the disbursement. If the
//     granter is NOT a group policy (e.g. an ordinary user account
//     issuing a user-to-user RecurringPull or SpendingAllowance), the
//     hook is a no-op — council rules do not apply to ordinary user
//     grants.
//   - **PostCommit**: if `grant.Granter` is a registered group
//     policy, runs `recordEpochSpend` to debit the per-epoch bucket.
//     PostCommit errors are tx-halting by the session hook contract,
//     which closes the double-debit window documented in the migration
//     plan §3.2.
//
// Wired from app.go via `app.SessionKeeper.SetClaimHooks(
// commonskeeper.NewSessionClaimHook(app.CommonsKeeper))` (M4).
type SessionClaimHook struct {
	keeper Keeper
}

var _ sessiontypes.GrantClaimHook = (*SessionClaimHook)(nil)

// NewSessionClaimHook constructs the hook. The keeper is captured by
// value (the late-binding pointers inside it stay shared).
func NewSessionClaimHook(k Keeper) *SessionClaimHook {
	return &SessionClaimHook{keeper: k}
}

// PreCheck runs the x/commons spend gates against the granter's
// council policy if applicable. No-op for non-council grants.
func (h *SessionClaimHook) PreCheck(ctx context.Context, grant sessiontypes.Grant, amount sdk.Coins) error {
	if !h.keeper.IsGroupPolicyAddress(ctx, grant.Granter) {
		// Not a council schedule — let the session module's own
		// validation handle it.
		return nil
	}
	return h.keeper.checkSpendGates(ctx, grant.Granter, amount)
}

// PostCommit debits the council's per-epoch budget. Tx-halting on
// error (collections write failure, etc.) — the SDK rolls back the
// bank send so a debited budget always corresponds to a successful
// disbursement. See sessiontypes.GrantClaimHook for the contract.
func (h *SessionClaimHook) PostCommit(ctx context.Context, grant sessiontypes.Grant, amount sdk.Coins) error {
	if !h.keeper.IsGroupPolicyAddress(ctx, grant.Granter) {
		return nil
	}
	return h.keeper.recordEpochSpend(ctx, grant.Granter, amount)
}
