package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) Thread(ctx context.Context, req *types.QueryThreadRequest) (*types.QueryThreadResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Get the root post
	rootPost, err := q.k.Post.Get(ctx, req.RootId)
	if err != nil {
		return nil, status.Error(codes.NotFound, "thread not found")
	}

	// Verify it's a root post
	if rootPost.ParentId != 0 {
		return nil, status.Error(codes.InvalidArgument, "not a root post")
	}

	return &types.QueryThreadResponse{
		PostId:   rootPost.PostId,
		Author:   rootPost.Author,
		ParentId: rootPost.ParentId,
		Depth:    rootPost.Depth,
	}, nil
}
