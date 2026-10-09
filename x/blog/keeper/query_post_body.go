package keeper

import (
	"context"

	"sparkdream/x/blog/types"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PostBody returns a post's stored body, including bodies ShowPost and the
// list queries withhold because media_flags != 0.
func (q queryServer) PostBody(ctx context.Context, req *types.QueryPostBodyRequest) (*types.QueryPostBodyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	post, found := q.k.GetPost(ctx, req.Id)
	if !found {
		return nil, sdkerrors.ErrKeyNotFound
	}

	return &types.QueryPostBodyResponse{
		Body:        post.Body,
		ContentType: post.ContentType,
		MediaFlags:  post.MediaFlags,
		BodyHash:    post.BodyHash,
	}, nil
}
