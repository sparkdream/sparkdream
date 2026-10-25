package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/artifact/types"
)

func (q queryServer) ListingsBySeller(ctx context.Context, req *types.QueryListingsBySellerRequest) (*types.QueryListingsBySellerResponse, error) {
	if req == nil || req.Seller == "" {
		return nil, errInvalidRequest
	}
	t := now(ctx)
	addrs := q.k.allAddressesOf(ctx, req.Seller)
	live := func(key AddrTokenKey, _ collections.NoValue) (bool, error) {
		l, err := q.k.Listings.Get(ctx, collections.Join(key.K2(), key.K3()))
		return err == nil && l.ExpiresAt > t, nil
	}
	listings, page, err := query.CollectionFilteredPaginate(ctx, q.k.ListingsBySeller, capPage(req.Pagination), live,
		func(key AddrTokenKey, _ collections.NoValue) (types.Listing, error) {
			return q.k.Listings.Get(ctx, collections.Join(key.K2(), key.K3()))
		}, addrPrefix(addrs[0]))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if lastPage(page) {
		for _, a := range addrs[1:] {
			err = q.k.ListingsBySeller.Walk(ctx, addrRange(a, 0), func(key AddrTokenKey) (bool, error) {
				if ok, _ := live(key, collections.NoValue{}); ok {
					l, _ := q.k.Listings.Get(ctx, collections.Join(key.K2(), key.K3()))
					listings = append(listings, l)
				}
				return false, nil
			})
			if err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
		}
	}
	return &types.QueryListingsBySellerResponse{Listings: listings, Pagination: page}, nil
}
