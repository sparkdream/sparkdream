package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/artifact/types"
)

func (q queryServer) OffersByBidder(ctx context.Context, req *types.QueryOffersByBidderRequest) (*types.QueryOffersByBidderResponse, error) {
	if req == nil || req.Bidder == "" {
		return nil, errInvalidRequest
	}
	offers, page, err := query.CollectionPaginate(ctx, q.k.OffersByBidder, capPage(req.Pagination),
		func(key collections.Pair[string, uint64], _ collections.NoValue) (types.Offer, error) {
			return q.k.Offers.Get(ctx, key.K2())
		}, query.WithCollectionPaginationPairPrefix[string, uint64](req.Bidder))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryOffersByBidderResponse{Offers: offers, Pagination: page}, nil
}
