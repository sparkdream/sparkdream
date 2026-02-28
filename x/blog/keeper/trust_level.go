package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

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
func (k Keeper) isActiveMember(ctx context.Context, addr sdk.AccAddress) bool {
	if k.repKeeper == nil {
		return false
	}
	return k.repKeeper.IsActiveMember(ctx, addr)
}
