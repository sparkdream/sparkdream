package types

import "cosmossdk.io/collections"

const (
	// ModuleName defines the module name.
	ModuleName = "identity"

	// StoreKey defines the primary module store key.
	StoreKey = ModuleName
)

// Storage prefixes. Identity uses two distinct Items under separate prefixes
// (§5 of x-identity-spec.md): the mutable Identity served by queries, and the
// SealedGenesisIdentity used by the invariant. Independent storage locations
// guarantee the invariant comparison is never a self-comparison.
var (
	IdentityKey       = collections.NewPrefix(0x00)
	SealedIdentityKey = collections.NewPrefix(0x01)
)
