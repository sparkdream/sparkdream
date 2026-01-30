package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) PinnedPosts(ctx context.Context, req *types.QueryPinnedPostsRequest) (*types.QueryPinnedPostsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Find first pinned post in category (simplified - in production would return list)
	var pinnedPost *types.Post

	err := q.k.Post.Walk(ctx, nil, func(key uint64, post types.Post) (bool, error) {
		// Only look at root posts (threads) that are pinned
		if post.ParentId == 0 && post.Pinned {
			// Filter by category if specified
			if req.CategoryId == 0 || post.CategoryId == req.CategoryId {
				pinnedPost = &post
				return true, nil // Stop after first
			}
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if pinnedPost != nil {
		return &types.QueryPinnedPostsResponse{
			PostId:   pinnedPost.PostId,
			Priority: uint64(pinnedPost.PinPriority),
			PinnedBy: pinnedPost.PinnedBy,
		}, nil
	}

	return &types.QueryPinnedPostsResponse{}, nil
}
