package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) TopPosts(ctx context.Context, req *types.QueryTopPostsRequest) (*types.QueryTopPostsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Find post with highest upvote count (simplified - in production would use proper ranking)
	var topPost *types.Post
	var maxUpvotes uint64

	err := q.k.Post.Walk(ctx, nil, func(key uint64, post types.Post) (bool, error) {
		// Only include root posts (threads)
		if post.ParentId == 0 && post.Status == types.PostStatus_POST_STATUS_ACTIVE {
			if post.UpvoteCount > maxUpvotes {
				maxUpvotes = post.UpvoteCount
				topPost = &post
			}
		}
		return false, nil
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
