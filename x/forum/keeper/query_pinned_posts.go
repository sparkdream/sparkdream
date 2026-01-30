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

	// TODO: Process the query

	return &types.QueryPinnedPostsResponse{}, nil
}
