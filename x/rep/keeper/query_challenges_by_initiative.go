package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ChallengesByInitiative(ctx context.Context, req *types.QueryChallengesByInitiativeRequest) (*types.QueryChallengesByInitiativeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// TODO: Process the query

	return &types.QueryChallengesByInitiativeResponse{}, nil
}
