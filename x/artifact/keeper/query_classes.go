package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/artifact/types"
)

func (q queryServer) Classes(ctx context.Context, req *types.QueryClassesRequest) (*types.QueryClassesResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	views, page, err := query.CollectionPaginate(ctx, q.k.Classes, capPage(req.Pagination),
		func(_ uint64, c types.Class) (types.ClassView, error) { return ClassView(c), nil })
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryClassesResponse{Classes: views, Pagination: page}, nil
}
