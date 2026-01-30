package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) Posts(ctx context.Context, req *types.QueryPostsRequest) (*types.QueryPostsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Iterate through posts and filter by category and status
	var posts []types.Post

	err := q.k.Post.Walk(ctx, nil, func(key uint64, post types.Post) (bool, error) {
		// Filter by category if specified
		if req.CategoryId != 0 && post.CategoryId != req.CategoryId {
			return false, nil
		}

		// Filter by status if specified
		if req.Status != 0 && uint64(post.Status) != req.Status {
			return false, nil
		}

		// Only include root posts (threads)
		if post.ParentId == 0 {
			posts = append(posts, post)
		}

		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Return first post if available (simplified response)
	if len(posts) > 0 {
		return &types.QueryPostsResponse{
			PostId: posts[0].PostId,
			Author: posts[0].Author,
			Status: uint64(posts[0].Status),
		}, nil
	}

	return &types.QueryPostsResponse{}, nil
}
