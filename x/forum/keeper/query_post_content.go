package keeper

import (
	"context"
	"errors"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PostContent returns a post's stored content, including content GetPost and
// the list queries withhold because media_flags != 0.
func (q queryServer) PostContent(ctx context.Context, req *types.QueryPostContentRequest) (*types.QueryPostContentResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	post, err := q.k.Post.Get(ctx, req.PostId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryPostContentResponse{
		Content:     post.Content,
		ContentType: post.ContentType,
		MediaFlags:  post.MediaFlags,
		BodyHash:    post.BodyHash,
	}, nil
}
