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

func (q queryServer) ListThreadFollow(ctx context.Context, req *types.QueryAllThreadFollowRequest) (*types.QueryAllThreadFollowResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	threadFollows, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.ThreadFollow,
		req.Pagination,
		func(_ string, value types.ThreadFollow) (types.ThreadFollow, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllThreadFollowResponse{ThreadFollow: threadFollows, Pagination: pageRes}, nil
}

func (q queryServer) GetThreadFollow(ctx context.Context, req *types.QueryGetThreadFollowRequest) (*types.QueryGetThreadFollowResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.ThreadFollow.Get(ctx, req.Follower)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetThreadFollowResponse{ThreadFollow: val}, nil
}
