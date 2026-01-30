package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) IsFollowingThread(ctx context.Context, req *types.QueryIsFollowingThreadRequest) (*types.QueryIsFollowingThreadResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.ThreadId == 0 {
		return nil, status.Error(codes.InvalidArgument, "thread_id required")
	}
	if req.User == "" {
		return nil, status.Error(codes.InvalidArgument, "user address required")
	}

	// Check if the follow record exists
	key := fmt.Sprintf("%d:%s", req.ThreadId, req.User)
	follow, err := q.k.ThreadFollow.Get(ctx, key)
	if err != nil {
		// Not following
		return &types.QueryIsFollowingThreadResponse{
			IsFollowing: false,
			FollowedAt:  0,
		}, nil
	}

	return &types.QueryIsFollowingThreadResponse{
		IsFollowing: true,
		FollowedAt:  follow.FollowedAt,
	}, nil
}
