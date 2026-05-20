package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GrantClaimHook is the callback contract for downstream modules that
// need to gate on-the-wire claim/pull/fire operations against
// module-owned preconditions.
//
// Primary use case: the x/commons RecurringSpend migration. Council
// policy addresses are valid Cosmos bech32 addresses but are not
// arbitrary user accounts — claims against grants whose granter is a
// council policy must additionally satisfy the council's activation
// gate, term-expiry gate, and per-epoch cumulative rate limit. That
// logic lives in x/commons; x/session invokes it via this hook so the
// registry stays a leaf module.
//
// The interface is two-method:
//
//   - PreCheck runs before bank send and is the veto surface. A
//     non-nil error aborts the claim and returns to the caller; no
//     state mutation should happen in PreCheck (it may run twice on
//     pause/resume retries).
//
//   - PostCommit runs after a successful bankKeeper.SendCoins, before
//     the tx returns to the caller. State-mutating side effects (e.g.
//     an x/commons per-epoch budget debit) belong here, so that a
//     bank-send failure doesn't leave the side effect committed. A
//     non-nil error from PostCommit HALTS the tx — the SDK rolls back
//     the bank send, the grant update, and any state touched by
//     PreCheck or PostCommit. The retry runs from the original
//     pre-tx state.
//
// The PostCommit halting contract is security-critical: a write
// failure (collections backing error, OOM, concurrent map write) must
// not leave a successful disbursement uncounted against the caller's
// own rate limits. Callers that only need notification (no atomic
// side effect) can embed NoOpPostCommitHook to opt out.
//
// `grant` carries every field the hook might branch on (granter,
// grantee, type, payload). `amount` is the to-be-disbursed coin(s)
// for the current op — bank.SendCoins-shaped so hooks can implement
// rate limits in standard SDK units without translation.
//
// Invoked from:
//   - MsgClaimRecurringPull (PreCheck before bank send; PostCommit
//     after epoch-bucket debit succeeds).
//   - MsgPullAllowance (PreCheck before bank send; PostCommit after
//     period-clock + spent_in_current_period commit).
//   - fireScheduledOneshot (Transfer variant), inside the
//     CacheContext: PreCheck before bank send, PostCommit after bank
//     send. Exec variant is not hooked — OneshotExec's audit contract
//     is the allowed_msg_types allowlist; arbitrary inner-msg
//     dispatch already runs the destination handler's own validation.
//
// Hook implementations should be:
//   - **Idempotent** on PreCheck: it may be invoked twice for the
//     same (grant, amount) on retry/pause/resume edge cases.
//   - **Bounded gas**: hooks run synchronously inside the claim tx
//     and contribute to its gas cost.
//   - **No recursion into x/session**: hooks must not call
//     ClaimRecurringPull / PullAllowance / CreateGrantOnBehalfOf
//     from inside themselves. The session module makes no
//     re-entrance guarantee.
type GrantClaimHook interface {
	PreCheck(ctx context.Context, grant Grant, amount sdk.Coins) error
	PostCommit(ctx context.Context, grant Grant, amount sdk.Coins) error
}

// NoOpPostCommitHook can be embedded by hook implementations that only
// need PreCheck semantics — its PostCommit is a no-op that returns
// nil, so the halting-on-error contract is harmless.
//
//	type MyHook struct {
//	    NoOpPostCommitHook
//	}
//	func (MyHook) PreCheck(...) error { ... }
type NoOpPostCommitHook struct{}

// PostCommit implements GrantClaimHook with a no-op.
func (NoOpPostCommitHook) PostCommit(_ context.Context, _ Grant, _ sdk.Coins) error {
	return nil
}

// GrantClaimMultiHook composes multiple hooks into one. Both PreCheck
// and PostCommit fan out in registration order and **short-circuit on
// the first error in either pass**. Returns the first non-nil error
// verbatim (no wrapping) so callers can identify which hook rejected.
type GrantClaimMultiHook []GrantClaimHook

// PreCheck implements GrantClaimHook by fanning out PreCheck to each
// registered hook in order.
func (h GrantClaimMultiHook) PreCheck(ctx context.Context, grant Grant, amount sdk.Coins) error {
	for _, hook := range h {
		if hook == nil {
			continue
		}
		if err := hook.PreCheck(ctx, grant, amount); err != nil {
			return err
		}
	}
	return nil
}

// PostCommit implements GrantClaimHook by fanning out PostCommit to
// each registered hook in order. Errors halt the surrounding tx — see
// the GrantClaimHook docstring.
func (h GrantClaimMultiHook) PostCommit(ctx context.Context, grant Grant, amount sdk.Coins) error {
	for _, hook := range h {
		if hook == nil {
			continue
		}
		if err := hook.PostCommit(ctx, grant, amount); err != nil {
			return err
		}
	}
	return nil
}
