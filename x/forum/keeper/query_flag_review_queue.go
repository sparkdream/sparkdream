package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) FlagReviewQueue(ctx context.Context, req *types.QueryFlagReviewQueueRequest) (*types.QueryFlagReviewQueueResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// TODO: Process the query

	return &types.QueryFlagReviewQueueResponse{}, nil
}
