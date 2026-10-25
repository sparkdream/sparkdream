package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/artifact/types"
)

func (q queryServer) ClassesByOwner(ctx context.Context, req *types.QueryClassesByOwnerRequest) (*types.QueryClassesByOwnerResponse, error) {
	if req == nil || req.Owner == "" {
		return nil, errInvalidRequest
	}
	addrs := q.k.allAddressesOf(ctx, req.Owner)
	view := func(key collections.Pair[string, uint64], _ collections.NoValue) (types.ClassView, error) {
		c, err := q.k.Classes.Get(ctx, key.K2())
		if err != nil {
			return types.ClassView{}, err
		}
		return ClassView(c), nil
	}
	views, page, err := query.CollectionPaginate(ctx, q.k.ClassesByOwner, capPage(req.Pagination), view,
		query.WithCollectionPaginationPairPrefix[string, uint64](addrs[0]))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if lastPage(page) {
		for _, a := range addrs[1:] {
			err = q.k.ClassesByOwner.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](a), func(key collections.Pair[string, uint64]) (bool, error) {
				v, err := view(key, collections.NoValue{})
				if err != nil {
					return true, err
				}
				views = append(views, v)
				return false, nil
			})
			if err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
		}
	}
	return &types.QueryClassesByOwnerResponse{Classes: views, Pagination: page}, nil
}
