package keeper

import (
	"context"

	"sparkdream/x/blog/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ReactionCounts(ctx context.Context, req *types.QueryReactionCountsRequest) (*types.QueryReactionCountsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	counts := q.k.GetReactionCounts(ctx, req.PostId, req.ReplyId)

	return &types.QueryReactionCountsResponse{Counts: counts}, nil
}
