package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ArchivedThreadMeta(ctx context.Context, req *types.QueryArchivedThreadMetaRequest) (*types.QueryArchivedThreadMetaResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.RootId == 0 {
		return nil, status.Error(codes.InvalidArgument, "root_id required")
	}

	// Get the archived thread
	archive, err := q.k.ArchivedThread.Get(ctx, req.RootId)
	if err != nil {
		return nil, status.Error(codes.NotFound, "archived thread not found")
	}

	return &types.QueryArchivedThreadMetaResponse{
		RootId:     archive.RootId,
		PostCount:  archive.PostCount,
		ArchivedAt: archive.ArchivedAt,
	}, nil
}
