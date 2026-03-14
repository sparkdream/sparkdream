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
