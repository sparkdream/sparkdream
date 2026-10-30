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

// isCouncilAuthorized checks if the address is the governance authority or
// the council / committee policy address (an executed proposal). A single
// committee member's signature is never enough: the blog operational
// params include the media gate and rate limits.
// Delegates to x/commons IsCouncilOrCommitteePolicy when available.
// Falls back to IsGovAuthority when x/commons is not wired.
func (k Keeper) isCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool {
	if k.late.commonsKeeper == nil {
		return k.IsGovAuthority(addr)
	}
	return k.late.commonsKeeper.IsCouncilOrCommitteePolicy(ctx, addr, council, committee)
}
