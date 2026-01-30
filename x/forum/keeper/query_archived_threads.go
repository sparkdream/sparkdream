package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ArchivedThreads(ctx context.Context, req *types.QueryArchivedThreadsRequest) (*types.QueryArchivedThreadsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Get first archived thread (simplified - in production would return list with pagination)
	var firstArchive *types.ArchivedThread

	err := q.k.ArchivedThread.Walk(ctx, nil, func(key uint64, archive types.ArchivedThread) (bool, error) {
		firstArchive = &archive
		return true, nil // Stop after first
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if firstArchive != nil {
		return &types.QueryArchivedThreadsResponse{
			RootId:     firstArchive.RootId,
			PostCount:  firstArchive.PostCount,
			ArchivedAt: firstArchive.ArchivedAt,
		}, nil
	}

	return &types.QueryArchivedThreadsResponse{}, nil
}
