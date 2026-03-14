package keeper

import (
	"bytes"
	"context"

	"sparkdream/x/futarchy/types"
)

// IsGovAuthority checks if the given address is the governance authority.
func (k Keeper) IsGovAuthority(addr string) bool {
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return false
	}
	return bytes.Equal(k.authority, addrBytes)
}

// IsCouncilAuthorized checks if the address is authorized via governance authority,
// council policy address, or committee membership.
// Delegates to x/commons IsCouncilAuthorized when available.
// Falls back to IsGovAuthority when x/commons is not wired.
func (k Keeper) isCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool {
	if k.late.commonsKeeper == nil {
		return k.IsGovAuthority(addr)
	}
	return k.late.commonsKeeper.IsCouncilAuthorized(ctx, addr, council, committee)
}

// SetCommonsKeeper sets the commons keeper dependency.
// This is wired after depinject initialization to break the cyclic dependency
// between x/commons (which depends on futarchy) and x/futarchy.
// Uses the shared lateKeepers so all value copies see the update.
func (k Keeper) SetCommonsKeeper(ck types.CommonsKeeper) {
	k.late.commonsKeeper = ck
}

// SetHooks sets the hooks for the futarchy module.
// Note: It must be a pointer receiver to update the struct.
func (k *Keeper) SetHooks(hooks types.FutarchyHooks) {
	if k.Hooks != nil {
		panic("FutarchyHooks already set")
	}
	k.Hooks = hooks
}
