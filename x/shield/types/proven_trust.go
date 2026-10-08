package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

type provenTrustLevelKey struct{}

// WithProvenTrustLevel records, for the inner message about to run, the trust
// level its ZK proof established (the exec's min_trust_level). Inner messages
// are signed by the shield module account, so this is the only way a target
// module can apply its own trust gates to an anonymous action.
func WithProvenTrustLevel(ctx sdk.Context, level uint32) sdk.Context {
	return ctx.WithValue(provenTrustLevelKey{}, level)
}

// ProvenTrustLevel returns the trust level proven for the anonymous inner
// message being executed, if any. Target modules compare it with their own
// requirements when the signer is the shield module account.
func ProvenTrustLevel(ctx context.Context) (uint32, bool) {
	level, ok := ctx.Value(provenTrustLevelKey{}).(uint32)
	return level, ok
}
