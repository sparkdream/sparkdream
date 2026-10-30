package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

type proofVerifiedKey struct{}

// WithProofVerified records that the ante handler verified msg's ZK proof
// against the same block state the msg server is about to run in, so the msg
// server can skip verifying it a second time.
func WithProofVerified(ctx sdk.Context, msg *MsgShieldedExec) sdk.Context {
	return ctx.WithValue(proofVerifiedKey{}, msg)
}

// ProofVerified reports whether the ante handler verified this exact message.
// It compares pointers, so a MsgShieldedExec reaching the msg server by any
// other route (or a different message in the same tx) is verified again.
func ProofVerified(ctx context.Context, msg *MsgShieldedExec) bool {
	verified, ok := ctx.Value(proofVerifiedKey{}).(*MsgShieldedExec)
	return ok && msg != nil && verified == msg
}

type execConsumedKey struct{}

// WithExecConsumed records that the ante handler validated msg and spent it
// (recorded its nullifier or advanced its owner sequence, and counted it
// against the identity rate limit) in the ante state, which commits with the
// fee payment even if the inner message then fails. The msg server must then
// execute it without re-checking freshness.
func WithExecConsumed(ctx sdk.Context, msg *MsgShieldedExec) sdk.Context {
	return ctx.WithValue(execConsumedKey{}, msg)
}

// ExecConsumed reports whether the ante handler spent this exact message. Like
// ProofVerified it compares pointers.
func ExecConsumed(ctx context.Context, msg *MsgShieldedExec) bool {
	consumed, ok := ctx.Value(execConsumedKey{}).(*MsgShieldedExec)
	return ok && msg != nil && consumed == msg
}
