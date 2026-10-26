package keeper

import (
	"context"

	"sparkdream/x/artifact/types"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RentalOffers lists unexpired rental offers, optionally only those addressed
// to user or one of its predecessor addresses.
func (q queryServer) RentalOffers(ctx context.Context, req *types.QueryRentalOffersRequest) (*types.QueryRentalOffersResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	var users map[string]bool
	if req.User != "" {
		users = map[string]bool{}
		for _, a := range q.k.allAddressesOf(ctx, req.User) {
			users[a] = true
		}
	}
	t := now(ctx)
	offers, page, err := query.CollectionFilteredPaginate(ctx, q.k.RentalOffers, capPage(req.Pagination),
		func(_ U64Pair, o types.RentalOffer) (bool, error) {
			return o.ExpiresAt > t && (users == nil || users[o.User]), nil
		},
		func(_ U64Pair, o types.RentalOffer) (types.RentalOffer, error) { return o, nil })
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryRentalOffersResponse{Offers: offers, Pagination: page}, nil
}
