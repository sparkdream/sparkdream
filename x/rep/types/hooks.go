package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// RepHooks lets downstream modules react to x/rep lifecycle events. All hook
// methods return error; a returned error must not be tx-halting (the hook is
// invoked after the originating state mutation is already committed). The
// canonical pattern is to log the failure and continue, so a buggy or
// uninitialized downstream module cannot brick member admission.
//
// Add new hook methods as additional lifecycle events surface; do not change
// existing signatures without coordinating with every implementer.
type RepHooks interface {
	// AfterMemberAdmitted fires after a Member record is created in x/rep via
	// MsgAcceptInvitation (the only path that admits a new member at runtime;
	// genesis bulk-load does NOT invoke this hook). Implementers may use this
	// to perform per-author bookkeeping that depends on membership status —
	// for example, x/blog enqueues the new member for ephemeral-content
	// promotion so their pre-admission posts get flipped to permanent.
	AfterMemberAdmitted(ctx context.Context, member sdk.AccAddress) error
}

// MultiRepHooks dispatches each hook call to N child implementations in order.
// Errors from individual implementations are collected; the first non-nil
// error is returned, but every implementation is invoked regardless.
type MultiRepHooks []RepHooks

// NewMultiRepHooks wraps `hooks` into a single RepHooks implementation.
func NewMultiRepHooks(hooks ...RepHooks) MultiRepHooks {
	return MultiRepHooks(hooks)
}

func (m MultiRepHooks) AfterMemberAdmitted(ctx context.Context, member sdk.AccAddress) error {
	var firstErr error
	for _, h := range m {
		if h == nil {
			continue
		}
		if err := h.AfterMemberAdmitted(ctx, member); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
