package types

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
)

// RegisterInterfaces is a no-op: x/identity has no Msg types (genesis-only
// immutable, see §3.4 of docs/x-identity-spec.md). Kept for symmetry with
// other modules and to allow future addition of MsgUpdateNonCanonicalMetadata
// (§18) without rewiring the module-registration plumbing.
func RegisterInterfaces(_ codectypes.InterfaceRegistry) {}
