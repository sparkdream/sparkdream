package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) PinnedPosts(ctx context.Context, req *types.QueryPinnedPostsRequest) (*types.QueryPinnedPostsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// FORUM-S2-8: prefix-walk PostsByPinned instead of scanning every post.
	// When CategoryId == 0, walk the entire index; otherwise prefix by category.
	walk := func(rng collections.Ranger[collections.Pair[uint64, uint64]]) (*types.Post, error) {
		var found *types.Post
		err := q.k.PostsByPinned.Walk(ctx, rng, func(key collections.Pair[uint64, uint64]) (bool, error) {
			postID := key.K2()
			p, getErr := q.k.Post.Get(ctx, postID)
			if getErr != nil {
				return false, nil
			}
			if !p.Pinned {
				return false, nil
			}
			found = &p
			return true, nil
		})
		return found, err
	}

	var pinned *types.Post
	var err error
	if req.CategoryId == 0 {
		pinned, err = walk(nil)
	} else {
		rng := collections.NewPrefixedPairRange[uint64, uint64](req.CategoryId)
		pinned, err = walk(rng)
	}
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if pinned != nil {
		return &types.QueryPinnedPostsResponse{
			PostId:   pinned.PostId,
			Priority: uint64(pinned.PinPriority),
			PinnedBy: pinned.PinnedBy,
		}, nil
	}

	return &types.QueryPinnedPostsResponse{}, nil
}
