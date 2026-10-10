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

// addrPrefix paginates an (address, class, token) index under one address.
func addrPrefix(addr string) func(o *query.CollectionsPaginateOptions[AddrTokenKey]) {
	return func(o *query.CollectionsPaginateOptions[AddrTokenKey]) {
		pfx := collections.TriplePrefix[string, uint64, uint64](addr)
		o.Prefix = &pfx
	}
}

// addrClassPrefix paginates an (address, class, token) index under one
// address and class.
func addrClassPrefix(addr string, classID uint64) func(o *query.CollectionsPaginateOptions[AddrTokenKey]) {
	return func(o *query.CollectionsPaginateOptions[AddrTokenKey]) {
		pfx := collections.TripleSuperPrefix[string, uint64, uint64](addr, classID)
		o.Prefix = &pfx
	}
}
