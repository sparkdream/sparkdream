package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) UserPosts(ctx context.Context, req *types.QueryUserPostsRequest) (*types.QueryUserPostsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Author == "" {
		return nil, status.Error(codes.InvalidArgument, "author address required")
	}

	// Find first post by this author (simplified - in production would return list with pagination)
	var userPost *types.Post

	err := q.k.Post.Walk(ctx, nil, func(key uint64, post types.Post) (bool, error) {
		if post.Author == req.Author {
			userPost = &post
			return true, nil // Stop after first
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if userPost != nil {
		return &types.QueryUserPostsResponse{
			PostId:     userPost.PostId,
			CategoryId: userPost.CategoryId,
			Status:     uint64(userPost.Status),
		}, nil
	}

	return &types.QueryUserPostsResponse{}, nil
}
