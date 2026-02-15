package keeper

import (
	"bytes"
	"context"
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
	if k.commonsKeeper == nil {
		return k.IsGovAuthority(addr)
	}
	return k.commonsKeeper.IsCouncilAuthorized(ctx, addr, council, committee)
}
