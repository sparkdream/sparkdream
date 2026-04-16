package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// shieldModuleAddress is the deterministic address for the shield module account.
// Computed once: SHA256("shield")[:20].
//
// SECURITY NOTE (CROSS-1): This address bypasses ALL membership and trust level checks
// when it appears as the message signer. This is a single point of failure — if the shield
// module account is compromised or if a bug allows arbitrary messages to be routed through
// x/shield without proper ZK proof verification, all access controls in this module are
// bypassed. The bypass is intentional because x/shield verifies membership and trust level
// via ZK proofs before routing, but this creates a hard dependency on x/shield's correctness.
// The bypass is narrowly scoped to membership/trust-level checks only — other validations
// (content length, rate limits, etc.) still apply.
var shieldModuleAddress = authtypes.NewModuleAddress("shield")

// isShieldModuleAddress returns true if addr is the shield module account.
// When the shield module routes a message, the ZK proof has already verified
// the user's membership and trust level, so the target module should bypass
// its own membership checks.
func isShieldModuleAddress(addr sdk.AccAddress) bool {
	return addr.Equals(shieldModuleAddress)
}

// meetsReplyTrustLevel checks if addr meets the post's min_reply_trust_level.
//
// Trust levels per spec §7.6:
//
//	-1 = open to all (no membership required)
//	 0 = any active member
//	 1 = NEWCOMER or above
//	 2 = ESTABLISHED or above
//	 3 = TRUSTED or above
//	 4 = PILLAR
func (k Keeper) meetsReplyTrustLevel(ctx context.Context, addr sdk.AccAddress, minLevel int32) bool {
	if minLevel == -1 {
		return true // open to all
	}
	// Shield module address: ZK proof already verified trust level
	if isShieldModuleAddress(addr) {
		return true
	}
	if !k.isActiveMember(ctx, addr) {
		return false
	}
	if minLevel <= 0 {
		return true // any active member suffices
	}
	// Granular trust level check: require GetTrustLevel >= minLevel
	trustLevel, err := k.repKeeper.GetTrustLevel(ctx, addr)
	if err != nil {
		return false
	}
	return int32(trustLevel) >= minLevel
}

// isActiveMember checks if addr is an active member via RepKeeper.
// The shield module address is always considered an active member because
// the ZK proof verified membership before routing the message.
func (k Keeper) isActiveMember(ctx context.Context, addr sdk.AccAddress) bool {
	if isShieldModuleAddress(addr) {
		return true
	}
	if k.repKeeper == nil {
		return false
	}
	return k.repKeeper.IsActiveMember(ctx, addr)
}
