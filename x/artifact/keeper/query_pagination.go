package keeper

import (
	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
)

// capPage clamps a page request to maxPageLimit.
func capPage(p *query.PageRequest) *query.PageRequest {
	if p == nil {
		return &query.PageRequest{Limit: maxPageLimit}
	}
	cp := *p
	if cp.Limit == 0 || cp.Limit > maxPageLimit {
		cp.Limit = maxPageLimit
	}
	return &cp
}

// lastPage reports whether a page response has no further pages.
//
// By-address queries union over every address of a member (member key
// migration): the canonical address is paginated as before and the other
// aliases' full lists are appended once, on the last page, so page keys
// stay meaningful without a second cursor.
func lastPage(p *query.PageResponse) bool {
	return p == nil || len(p.NextKey) == 0
}

// addrPrefix paginates an (address, class, token) index under one address.
func addrPrefix(addr string) func(o *query.CollectionsPaginateOptions[AddrTokenKey]) {
	return func(o *query.CollectionsPaginateOptions[AddrTokenKey]) {
		pfx := collections.TriplePrefix[string, uint64, uint64](addr)
		o.Prefix = &pfx
	}
}

// addrRange is the walk range matching addrPrefix / addrClassPrefix
// (classID 0 means every class).
func addrRange(addr string, classID uint64) collections.Ranger[AddrTokenKey] {
	if classID != 0 {
		return collections.NewSuperPrefixedTripleRange[string, uint64, uint64](addr, classID)
	}
	return collections.NewPrefixedTripleRange[string, uint64, uint64](addr)
}

// addrClassPrefix paginates an (address, class, token) index under one
// address and class.
func addrClassPrefix(addr string, classID uint64) func(o *query.CollectionsPaginateOptions[AddrTokenKey]) {
	return func(o *query.CollectionsPaginateOptions[AddrTokenKey]) {
		pfx := collections.TripleSuperPrefix[string, uint64, uint64](addr, classID)
		o.Prefix = &pfx
	}
}
