package keeper

import (
	"context"

	"sparkdream/x/commons/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetPoll returns a poll with its tallies.
func (q queryServer) GetPoll(ctx context.Context, req *types.QueryGetPollRequest) (*types.QueryGetPollResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	poll, err := q.k.Polls.Get(ctx, req.PollId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "poll %d not found", req.PollId)
	}
	return &types.QueryGetPollResponse{Poll: poll}, nil
}
