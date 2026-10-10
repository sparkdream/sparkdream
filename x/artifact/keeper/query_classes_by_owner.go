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
	views, page, err := query.CollectionPaginate(ctx, q.k.ClassesByOwner, capPage(req.Pagination),
		func(key collections.Pair[string, uint64], _ collections.NoValue) (types.ClassView, error) {
			c, err := q.k.Classes.Get(ctx, key.K2())
			if err != nil {
				return types.ClassView{}, err
			}
			return ClassView(c), nil
		},
		query.WithCollectionPaginationPairPrefix[string, uint64](req.Owner))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryClassesByOwnerResponse{Classes: views, Pagination: page}, nil
}
