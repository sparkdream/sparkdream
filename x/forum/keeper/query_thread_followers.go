package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
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

	// FORUM-S2-8: prefix-walk FollowersByThread instead of scanning all follows.
	rng := collections.NewPrefixedPairRange[uint64, string](req.ThreadId)

	var follower string
	var followedAt int64

	err := q.k.FollowersByThread.Walk(ctx, rng, func(key collections.Pair[uint64, string]) (bool, error) {
		addr := key.K2()
		followKey := fmt.Sprintf("%s:%d", addr, req.ThreadId)
		f, getErr := q.k.ThreadFollow.Get(ctx, followKey)
		if getErr != nil {
			// Stale index entry — skip.
			return false, nil
		}
		follower = f.Follower
		followedAt = f.FollowedAt
		return true, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryThreadFollowersResponse{
		Follower:   follower,
		FollowedAt: followedAt,
	}, nil
}
