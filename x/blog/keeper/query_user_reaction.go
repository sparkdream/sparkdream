package keeper

import (
	"context"

	"sparkdream/x/blog/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) UserReaction(ctx context.Context, req *types.QueryUserReactionRequest) (*types.QueryUserReactionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	reaction, found := q.k.GetReaction(ctx, req.PostId, req.ReplyId, req.Creator)
	if !found {
		return &types.QueryUserReactionResponse{Reaction: nil}, nil
	}

	return &types.QueryUserReactionResponse{Reaction: &reaction}, nil
}
