package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/artifact/types"
)

func (q queryServer) Tokens(ctx context.Context, req *types.QueryTokensRequest) (*types.QueryTokensResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	cache := map[uint64]types.ContentStatus{}
	views, page, err := query.CollectionPaginate(ctx, q.k.Tokens, capPage(req.Pagination),
		func(_ U64Pair, t types.Token) (types.TokenView, error) {
			return TokenView(q.classStatus(ctx, cache, t.ClassId), t), nil
		},
		query.WithCollectionPaginationPairPrefix[uint64, uint64](req.ClassId))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryTokensResponse{Tokens: views, Pagination: page}, nil
}
