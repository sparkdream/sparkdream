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

func (q queryServer) ListPost(ctx context.Context, req *types.QueryAllPostRequest) (*types.QueryAllPostResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	posts, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Post,
		req.Pagination,
		func(_ uint64, value types.Post) (types.Post, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllPostResponse{Post: posts, Pagination: pageRes}, nil
}

func (q queryServer) GetPost(ctx context.Context, req *types.QueryGetPostRequest) (*types.QueryGetPostResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.Post.Get(ctx, req.PostId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetPostResponse{Post: val}, nil
}
