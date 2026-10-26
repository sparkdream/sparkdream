package keeper

import (
	"context"

	"sparkdream/x/artifact/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// AuctionsBySeller lists the open auctions started by seller. Auctions started
// under a predecessor address are appended on the last page.
func (q queryServer) AuctionsBySeller(ctx context.Context, req *types.QueryAuctionsBySellerRequest) (*types.QueryAuctionsBySellerResponse, error) {
	if req == nil || req.Seller == "" {
		return nil, errInvalidRequest
	}
	addrs := q.k.allAddressesOf(ctx, req.Seller)
	auctions, page, err := query.CollectionPaginate(ctx, q.k.AuctionsBySeller, capPage(req.Pagination),
		func(key collections.Pair[string, uint64], _ collections.NoValue) (types.Auction, error) {
			return q.k.Auctions.Get(ctx, key.K2())
		}, query.WithCollectionPaginationPairPrefix[string, uint64](addrs[0]))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if lastPage(page) {
		for _, a := range addrs[1:] {
			err = q.k.AuctionsBySeller.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](a), func(key collections.Pair[string, uint64]) (bool, error) {
				au, err := q.k.Auctions.Get(ctx, key.K2())
				if err != nil {
					return true, err
				}
				auctions = append(auctions, au)
				return false, nil
			})
			if err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
		}
	}
	return &types.QueryAuctionsBySellerResponse{Auctions: auctions, Pagination: page}, nil
}
