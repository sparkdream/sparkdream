package keeper

import (
	"context"
	shieldtypes "sparkdream/x/shield/types"

	"sparkdream/x/blog/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// shieldModuleAddress is the deterministic address for the shield module account.
// Computed once: SHA256("shield")[:20].
//
// SECURITY NOTE (CROSS-1): This address bypasses the membership check when it appears
// as the message signer, and trust gates compare against the level its ZK proof
// established (shieldtypes.ProvenTrustLevel; no proven level means refused). This is a
// single point of failure — if the shield module account is compromised or if a bug
// allows arbitrary messages to be routed through x/shield without proper ZK proof
// verification, membership gating in this module is bypassed. Per-address bookkeeping
// (rate limits, per-creator reaction records, the thread-author exemption) does not
// apply to it, since every anonymous member shares it; content validation still does.
// No per-action SPARK charge (storage fee, edit delta fee, reaction fee) applies
// either: the shield account's balance is the communal gas reserve, not the member's.
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
//	 0 = any active member (NEW or above)
//	 1 = PROVISIONAL or above
//	 2 = ESTABLISHED or above
//	 3 = TRUSTED or above
//	 4 = CORE
func (k Keeper) meetsReplyTrustLevel(ctx context.Context, addr sdk.AccAddress, minLevel int32) bool {
	if minLevel == -1 {
		return true // open to all
	}
	// Shield module address: compare with the level the ZK proof established.
	// No proven level means the message didn't come through a shield exec, so
	// nothing about the actual signer is known: refuse.
	if isShieldModuleAddress(addr) {
		proven, ok := shieldtypes.ProvenTrustLevel(ctx)
		return ok && int32(proven) >= minLevel
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

// trustLevelError returns the appropriate error for an addr that failed
// meetsReplyTrustLevel against minLevel. When the bar only requires
// membership (minLevel <= 0), or the caller isn't a member at all, "not a
// member" is the actionable signal; only an active member whose trust is
// below a raised bar gets ErrInsufficientTrustLevel. Callers pass a subject
// phrase (e.g. "replies on this post", "pinning posts") that completes
// "does not meet minimum trust level for {subject}".
func (k Keeper) trustLevelError(ctx context.Context, addr sdk.AccAddress, minLevel int32, subject string) error {
	if minLevel <= 0 || !k.isActiveMember(ctx, addr) {
		return errorsmod.Wrap(types.ErrNotMember, addr.String())
	}
	return errorsmod.Wrapf(types.ErrInsufficientTrustLevel,
		"does not meet minimum trust level for %s", subject)
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
