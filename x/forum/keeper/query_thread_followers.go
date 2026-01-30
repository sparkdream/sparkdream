package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ThreadFollowers(ctx context.Context, req *types.QueryThreadFollowersRequest) (*types.QueryThreadFollowersResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.ThreadId == 0 {
		return nil, status.Error(codes.InvalidArgument, "thread_id required")
	}

	// Find first follower of this thread (simplified - in production would return list)
	var threadFollow *types.ThreadFollow
	keyPrefix := fmt.Sprintf("%d:", req.ThreadId)

	err := q.k.ThreadFollow.Walk(ctx, nil, func(key string, follow types.ThreadFollow) (bool, error) {
		if follow.ThreadId == req.ThreadId {
			threadFollow = &follow
			return true, nil // Stop after first
		}
		return false, nil
	})
	_ = keyPrefix // suppress unused warning
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if threadFollow != nil {
		return &types.QueryThreadFollowersResponse{
			Follower:   threadFollow.Follower,
			FollowedAt: threadFollow.FollowedAt,
		}, nil
	}

	return &types.QueryThreadFollowersResponse{}, nil
}
