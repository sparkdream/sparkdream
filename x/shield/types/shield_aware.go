package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ShieldAware is implemented by module msg servers that accept shielded operations.
// x/shield checks this interface at execution time before dispatching the inner message.
// If the target module's msg server does not implement ShieldAware, the operation is rejected.
//
// This creates a double gate:
//   - Gate 1: Governance whitelist (ShieldedOpRegistration) — controls which types are allowed
//   - Gate 2: Module interface (ShieldAware) — module must explicitly opt in
//
// Both gates must pass for a shielded operation to execute.
type ShieldAware interface {
	// IsShieldCompatible returns true if this message type is designed to accept
	// the shield module account as sender for anonymous execution.
	IsShieldCompatible(ctx context.Context, msg sdk.Msg) bool
}

// OwnershipClaim identifies the anonymous owner of some content: the domain and
// raw scope of the proof that created it, the resulting nullifier (the owner
// tag), and the owner's current sequence.
type OwnershipClaim struct {
	Domain   uint32
	Scope    uint64
	Tag      []byte
	Sequence uint64
}

// ShieldOwnershipResolver is implemented by a ShieldAware module whose
// anonymously created content can later be managed by its anonymous creator
// (NULLIFIER_MODE_OWNERSHIP operations). x/shield calls ResolveOwnership to
// learn which claim the inner message acts under, verifies a proof that
// reproduces the claim's tag over the claim's scope, then calls
// AdvanceOwnership before running the inner message.
type ShieldOwnershipResolver interface {
	// ResolveOwnership returns the ownership claim msg acts under, or an error
	// if its target is missing or not anonymously owned.
	ResolveOwnership(ctx context.Context, msg sdk.Msg) (OwnershipClaim, error)

	// AdvanceOwnership increments the claim's sequence, so the proof just
	// accepted (bound to the old sequence) can never be replayed.
	AdvanceOwnership(ctx context.Context, msg sdk.Msg) error
}
