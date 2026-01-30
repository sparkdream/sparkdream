package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) PostFlags(ctx context.Context, req *types.QueryPostFlagsRequest) (*types.QueryPostFlagsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.PostId == 0 {
		return nil, status.Error(codes.InvalidArgument, "post_id required")
	}

	// Get the flag record for this post
	flag, err := q.k.PostFlag.Get(ctx, req.PostId)
	if err != nil {
		// No flags for this post
		return &types.QueryPostFlagsResponse{
			TotalWeight:   "0",
			InReviewQueue: false,
			FlaggerCount:  0,
		}, nil
	}

	return &types.QueryPostFlagsResponse{
		TotalWeight:   flag.TotalWeight,
		InReviewQueue: flag.InReviewQueue,
		FlaggerCount:  uint64(len(flag.Flaggers)),
	}, nil
}
