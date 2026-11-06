package types

// feePaidCtxKey is the private type of ContextKeyFeePaid. A typed key cannot
// collide with any other package's context value, and x/shield and x/session
// share this one value instead of two string constants that merely agree.
type feePaidCtxKey struct{}

// ContextKeyFeePaid is the context key an ante decorator sets (to true) once
// it has paid the tx fee on someone's behalf: ShieldGasDecorator (fee paid by
// the shield module) and SessionFeeDecorator (fee paid by the session
// granter). SkipIfFeePaidDecorator reads it and skips the standard
// DeductFeeDecorator. A decorator that sets it must also enforce the node's
// CheckTx minimum gas prices itself, because the skipped decorator is where
// the SDK does that.
var ContextKeyFeePaid = feePaidCtxKey{}

// sessionExecCtxKey is the private type of ContextKeySessionExec.
type sessionExecCtxKey struct{}

// ContextKeySessionExec is set (to the granter's address string) on the
// context x/session's MsgExecSession hands each inner message: the message
// was signed by a delegated session key, not by the granter. Handlers may
// branch on it only to tighten what a delegated key can do (x/artifact
// records the granter as the payer of a mint deposit so the refund goes
// back to it, not to whoever burns the token), never to relax a check.
var ContextKeySessionExec = sessionExecCtxKey{}

// SessionExecGranter returns the granter whose session key signed the
// message being handled, or "" when it was not dispatched by
// MsgExecSession.
func SessionExecGranter(ctx interface{ Value(key any) any }) string {
	if g, ok := ctx.Value(ContextKeySessionExec).(string); ok {
		return g
	}
	return ""
}

// executingProposalCtxKey is the private type of ContextKeyExecutingProposal.
type executingProposalCtxKey struct{}

// ContextKeyExecutingProposal is set (to the proposal id, a uint64) on the
// context x/commons hands each message an executed council or committee
// proposal carries. A handler whose action is the body's decision can read
// it to attribute that decision to the members who voted on it (x/rep pays a
// project's review interim only to the committee members who voted on the
// proposal that approved the project). It is never an authorization input.
var ContextKeyExecutingProposal = executingProposalCtxKey{}

// ExecutingProposalID returns the id of the x/commons proposal whose
// execution dispatched the message being handled, or (0, false) when the
// message was not dispatched by MsgExecuteProposal.
func ExecutingProposalID(ctx interface{ Value(key any) any }) (uint64, bool) {
	id, ok := ctx.Value(ContextKeyExecutingProposal).(uint64)
	return id, ok && id != 0
}
