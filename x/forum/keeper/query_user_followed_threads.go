package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
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

	// FORUM-S2-8: prefix-walk ThreadsByFollower instead of scanning all follows.
	rng := collections.NewPrefixedPairRange[string, uint64](req.User)

	var threadID uint64
	var followedAt int64

	err := q.k.ThreadsByFollower.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
		tid := key.K2()
		followKey := fmt.Sprintf("%s:%d", req.User, tid)
		f, getErr := q.k.ThreadFollow.Get(ctx, followKey)
		if getErr != nil {
			return false, nil
		}
		threadID = f.ThreadId
		followedAt = f.FollowedAt
		return true, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryUserFollowedThreadsResponse{
		ThreadId:   threadID,
		FollowedAt: followedAt,
	}, nil
}
