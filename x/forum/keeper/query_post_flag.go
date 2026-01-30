package keeper

import (
	"context"
	"errors"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListPostFlag(ctx context.Context, req *types.QueryAllPostFlagRequest) (*types.QueryAllPostFlagResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	postFlags, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.PostFlag,
		req.Pagination,
		func(_ uint64, value types.PostFlag) (types.PostFlag, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllPostFlagResponse{PostFlag: postFlags, Pagination: pageRes}, nil
}

func (q queryServer) GetPostFlag(ctx context.Context, req *types.QueryGetPostFlagRequest) (*types.QueryGetPostFlagResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.PostFlag.Get(ctx, req.PostId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetPostFlagResponse{PostFlag: val}, nil
}
