package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/artifact/types"
)

// Offers lists the open offers on a token, or (token_id 0) a class's
// collection offers. Expired offers the EndBlocker has not refunded are
// filtered.
func (q queryServer) Offers(ctx context.Context, req *types.QueryOffersRequest) (*types.QueryOffersResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	t := now(ctx)
	offers, page, err := query.CollectionFilteredPaginate(ctx, q.k.OffersByToken, capPage(req.Pagination),
		func(key collections.Triple[uint64, uint64, uint64], _ collections.NoValue) (bool, error) {
			o, err := q.k.Offers.Get(ctx, key.K3())
			return err == nil && o.ExpiresAt > t, nil
		},
		func(key collections.Triple[uint64, uint64, uint64], _ collections.NoValue) (types.Offer, error) {
			return q.k.Offers.Get(ctx, key.K3())
		}, editionPrefix[uint64](req.ClassId, req.TokenId))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryOffersResponse{Offers: offers, Pagination: page}, nil
}
