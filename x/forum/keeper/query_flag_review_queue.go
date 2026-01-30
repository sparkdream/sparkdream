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

	// Find first post in review queue (simplified - in production would return list)
	var flaggedPost *types.PostFlag

	err := q.k.PostFlag.Walk(ctx, nil, func(key uint64, flag types.PostFlag) (bool, error) {
		if flag.InReviewQueue {
			flaggedPost = &flag
			return true, nil // Stop after first
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if flaggedPost != nil {
		return &types.QueryFlagReviewQueueResponse{
			PostId:      flaggedPost.PostId,
			TotalWeight: flaggedPost.TotalWeight,
		}, nil
	}

	return &types.QueryFlagReviewQueueResponse{}, nil
}
