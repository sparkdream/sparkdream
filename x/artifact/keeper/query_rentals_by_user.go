package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/artifact/types"
)

func (q queryServer) RentalsByUser(ctx context.Context, req *types.QueryRentalsByUserRequest) (*types.QueryRentalsByUserResponse, error) {
	if req == nil || req.User == "" {
		return nil, errInvalidRequest
	}
	rentals, page, err := query.CollectionPaginate(ctx, q.k.RentalsByUser, capPage(req.Pagination),
		func(key AddrTokenKey, _ collections.NoValue) (types.Rental, error) {
			return q.k.Rentals.Get(ctx, collections.Join(key.K2(), key.K3()))
		}, addrPrefix(req.User))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryRentalsByUserResponse{Rentals: rentals, Pagination: page}, nil
}
