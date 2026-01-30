package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) UserFollowedThreads(ctx context.Context, req *types.QueryUserFollowedThreadsRequest) (*types.QueryUserFollowedThreadsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.User == "" {
		return nil, status.Error(codes.InvalidArgument, "user address required")
	}

	// Find first thread followed by this user (simplified - in production would return list)
	var threadFollow *types.ThreadFollow

	err := q.k.ThreadFollow.Walk(ctx, nil, func(key string, follow types.ThreadFollow) (bool, error) {
		if follow.Follower == req.User {
			threadFollow = &follow
			return true, nil // Stop after first
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if threadFollow != nil {
		return &types.QueryUserFollowedThreadsResponse{
			ThreadId:   threadFollow.ThreadId,
			FollowedAt: threadFollow.FollowedAt,
		}, nil
	}

	return &types.QueryUserFollowedThreadsResponse{}, nil
}
