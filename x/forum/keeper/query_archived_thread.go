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

func (q queryServer) ListArchivedThread(ctx context.Context, req *types.QueryAllArchivedThreadRequest) (*types.QueryAllArchivedThreadResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	archivedThreads, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.ArchivedThread,
		req.Pagination,
		func(_ uint64, value types.ArchivedThread) (types.ArchivedThread, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAllArchivedThreadResponse{ArchivedThread: archivedThreads, Pagination: pageRes}, nil
}

func (q queryServer) GetArchivedThread(ctx context.Context, req *types.QueryGetArchivedThreadRequest) (*types.QueryGetArchivedThreadResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	val, err := q.k.ArchivedThread.Get(ctx, req.RootId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetArchivedThreadResponse{ArchivedThread: val}, nil
}
