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
	addrs := q.k.allAddressesOf(ctx, req.Bidder)
	// Expired offers the EndBlocker has not refunded yet are filtered (§6).
	t := now(ctx)
	offers, page, err := query.CollectionFilteredPaginate(ctx, q.k.OffersByBidder, capPage(req.Pagination),
		func(key collections.Pair[string, uint64], _ collections.NoValue) (bool, error) {
			o, err := q.k.Offers.Get(ctx, key.K2())
			return err == nil && o.ExpiresAt > t, nil
		},
		func(key collections.Pair[string, uint64], _ collections.NoValue) (types.Offer, error) {
			return q.k.Offers.Get(ctx, key.K2())
		}, query.WithCollectionPaginationPairPrefix[string, uint64](addrs[0]))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if lastPage(page) {
		for _, a := range addrs[1:] {
			err = q.k.OffersByBidder.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](a), func(key collections.Pair[string, uint64]) (bool, error) {
				o, err := q.k.Offers.Get(ctx, key.K2())
				if err != nil {
					return true, err
				}
				if o.ExpiresAt > t {
					offers = append(offers, o)
				}
				return false, nil
			})
			if err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
		}
	}
	return &types.QueryOffersByBidderResponse{Offers: offers, Pagination: page}, nil
}
