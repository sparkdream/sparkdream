package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ExecNullifier is the consumed nullifier a shielded exec ran under, with the
// domain and raw scope it was proven for. A module that lets the anonymous
// creator manage content later stores these as the content's owner claim.
type ExecNullifier struct {
	Domain    uint32
	Scope     uint64
	Nullifier []byte
}

type execNullifierKey struct{}

// WithExecNullifier records the consumed nullifier for the inner message about
// to run.
func WithExecNullifier(ctx sdk.Context, n ExecNullifier) sdk.Context {
	return ctx.WithValue(execNullifierKey{}, n)
}

// GetExecNullifier returns the consumed nullifier of the anonymous inner
// message being executed, if any.
func GetExecNullifier(ctx context.Context) (ExecNullifier, bool) {
	n, ok := ctx.Value(execNullifierKey{}).(ExecNullifier)
	return n, ok
}

type ownershipTagKey struct{}

// WithOwnershipTag records that the inner message about to run was authorized
// by an ownership proof for this owner tag.
func WithOwnershipTag(ctx sdk.Context, tag []byte) sdk.Context {
	return ctx.WithValue(ownershipTagKey{}, append([]byte(nil), tag...))
}

// GetOwnershipTag returns the owner tag the anonymous inner message being
// executed proved ownership of, if any. Target modules compare it with the
// stored tag before letting the shield address manage content.
func GetOwnershipTag(ctx context.Context) ([]byte, bool) {
	tag, ok := ctx.Value(ownershipTagKey{}).([]byte)
	return tag, ok && len(tag) > 0
}
