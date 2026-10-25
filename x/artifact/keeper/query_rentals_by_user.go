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
	addrs := q.k.allAddressesOf(ctx, req.User)
	rentals, page, err := query.CollectionPaginate(ctx, q.k.RentalsByUser, capPage(req.Pagination),
		func(key AddrTokenKey, _ collections.NoValue) (types.Rental, error) {
			return q.k.Rentals.Get(ctx, collections.Join(key.K2(), key.K3()))
		}, addrPrefix(addrs[0]))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if lastPage(page) {
		for _, a := range addrs[1:] {
			err = q.k.RentalsByUser.Walk(ctx, addrRange(a, 0), func(key AddrTokenKey) (bool, error) {
				r, err := q.k.Rentals.Get(ctx, collections.Join(key.K2(), key.K3()))
				if err != nil {
					return true, err
				}
				rentals = append(rentals, r)
				return false, nil
			})
			if err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
		}
	}
	return &types.QueryRentalsByUserResponse{Rentals: rentals, Pagination: page}, nil
}
