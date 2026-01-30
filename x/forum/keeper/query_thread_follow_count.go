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

func (q queryServer) ListThreadFollowCount(ctx context.Context, req *types.QueryAllThreadFollowCountRequest) (*types.QueryAllThreadFollowCountResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	threadFollowCounts, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.ThreadFollowCount,
		req.Pagination,
		func(_ uint64, value types.ThreadFollowCount) (types.ThreadFollowCount, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllThreadFollowCountResponse{ThreadFollowCount: threadFollowCounts, Pagination: pageRes}, nil
}

func (q queryServer) GetThreadFollowCount(ctx context.Context, req *types.QueryGetThreadFollowCountRequest) (*types.QueryGetThreadFollowCountResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.ThreadFollowCount.Get(ctx, req.ThreadId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetThreadFollowCountResponse{ThreadFollowCount: val}, nil
}
