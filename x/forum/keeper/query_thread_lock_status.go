package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ThreadLockStatus(ctx context.Context, req *types.QueryThreadLockStatusRequest) (*types.QueryThreadLockStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.RootId == 0 {
		return nil, status.Error(codes.InvalidArgument, "root_id required")
	}

	// Get the post
	post, err := q.k.Post.Get(ctx, req.RootId)
	if err != nil {
		return nil, status.Error(codes.NotFound, "thread not found")
	}

	// Check if it's a root post
	if post.ParentId != 0 {
		return nil, status.Error(codes.InvalidArgument, "not a root post")
	}

	// Check if there's a sentinel lock record
	isSentinelLock := false
	_, err = q.k.ThreadLockRecord.Get(ctx, req.RootId)
	if err == nil {
		isSentinelLock = true
	}

	return &types.QueryThreadLockStatusResponse{
		Locked:         post.Locked,
		LockedBy:       post.LockedBy,
		Reason:         post.LockReason,
		IsSentinelLock: isSentinelLock,
	}, nil
}
