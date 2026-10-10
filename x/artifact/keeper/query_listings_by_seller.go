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
	listings, page, err := query.CollectionFilteredPaginate(ctx, q.k.ListingsBySeller, capPage(req.Pagination),
		func(key AddrTokenKey, _ collections.NoValue) (bool, error) {
			l, err := q.k.Listings.Get(ctx, collections.Join(key.K2(), key.K3()))
			return err == nil && l.ExpiresAt > t, nil
		},
		func(key AddrTokenKey, _ collections.NoValue) (types.Listing, error) {
			return q.k.Listings.Get(ctx, collections.Join(key.K2(), key.K3()))
		}, addrPrefix(req.Seller))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryListingsBySellerResponse{Listings: listings, Pagination: page}, nil
}
