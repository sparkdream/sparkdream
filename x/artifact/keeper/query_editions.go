package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/artifact/types"
)

func (q queryServer) Editions(ctx context.Context, req *types.QueryEditionsRequest) (*types.QueryEditionsResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	c, err := q.k.Classes.Get(ctx, req.ClassId)
	if err != nil {
		return nil, notFound(err, "class")
	}
	editions, page, err := query.CollectionPaginate(ctx, q.k.Editions, capPage(req.Pagination),
		func(_ U64Pair, e types.Edition) (types.EditionView, error) { return EditionView(c.Status, e), nil },
		query.WithCollectionPaginationPairPrefix[uint64, uint64](req.ClassId))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryEditionsResponse{Editions: editions, Pagination: page}, nil
}
