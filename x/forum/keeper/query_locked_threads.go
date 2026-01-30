package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) LockedThreads(ctx context.Context, req *types.QueryLockedThreadsRequest) (*types.QueryLockedThreadsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Find first locked thread (simplified - in production would return list)
	var lockedPost *types.Post

	err := q.k.Post.Walk(ctx, nil, func(key uint64, post types.Post) (bool, error) {
		// Only look at root posts (threads) that are locked
		if post.ParentId == 0 && post.Locked {
			lockedPost = &post
			return true, nil // Stop after first
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if lockedPost != nil {
		return &types.QueryLockedThreadsResponse{
			RootId:   lockedPost.PostId,
			LockedBy: lockedPost.LockedBy,
			LockedAt: lockedPost.LockedAt,
		}, nil
	}

	return &types.QueryLockedThreadsResponse{}, nil
}
