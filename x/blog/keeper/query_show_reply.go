package keeper

import (
	"context"

	"sparkdream/x/blog/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ShowReply(ctx context.Context, req *types.QueryShowReplyRequest) (*types.QueryShowReplyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	reply, found := q.k.GetReply(ctx, req.Id)
	if !found {
		return nil, status.Error(codes.NotFound, "reply not found")
	}

	return &types.QueryShowReplyResponse{Reply: reply}, nil
}
