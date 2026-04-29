package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) TopPosts(ctx context.Context, req *types.QueryTopPostsRequest) (*types.QueryTopPostsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// FORUM-S2-8: walk PostsByUpvotes in descending order so the first ACTIVE
	// root post we hit is the highest-ranked one. Avoids a full Post scan.
	rng := new(collections.Range[collections.Pair[uint64, uint64]]).Descending()

	var topPost *types.Post

	err := q.k.PostsByUpvotes.Walk(ctx, rng, func(key collections.Pair[uint64, uint64]) (bool, error) {
		postID := key.K2()
		p, getErr := q.k.Post.Get(ctx, postID)
		if getErr != nil {
			// Stale index — skip.
			return false, nil
		}
		if p.ParentId != 0 || p.Status != types.PostStatus_POST_STATUS_ACTIVE {
			return false, nil
		}
		topPost = &p
		return true, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if topPost != nil {
		return &types.QueryTopPostsResponse{
			PostId:      topPost.PostId,
			UpvoteCount: topPost.UpvoteCount,
		}, nil
	}

	return &types.QueryTopPostsResponse{}, nil
}
