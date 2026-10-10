package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/artifact/types"
)

func (q queryServer) TokensByOwner(ctx context.Context, req *types.QueryTokensByOwnerRequest) (*types.QueryTokensByOwnerResponse, error) {
	if req == nil || req.Owner == "" {
		return nil, errInvalidRequest
	}
	prefix := addrPrefix(req.Owner)
	if req.ClassId != 0 {
		prefix = addrClassPrefix(req.Owner, req.ClassId)
	}
	cache := map[uint64]types.ContentStatus{}
	views, page, err := query.CollectionPaginate(ctx, q.k.TokensByOwner, capPage(req.Pagination),
		func(key AddrTokenKey, _ collections.NoValue) (types.TokenView, error) {
			t, err := q.k.Tokens.Get(ctx, collections.Join(key.K2(), key.K3()))
			if err != nil {
				return types.TokenView{}, err
			}
			return TokenView(q.classStatus(ctx, cache, t.ClassId), t), nil
		}, prefix)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryTokensByOwnerResponse{Tokens: views, Pagination: page}, nil
}
