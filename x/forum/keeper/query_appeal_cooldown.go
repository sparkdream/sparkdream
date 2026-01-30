package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) AppealCooldown(ctx context.Context, req *types.QueryAppealCooldownRequest) (*types.QueryAppealCooldownResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// TODO: Process the query

	return &types.QueryAppealCooldownResponse{}, nil
}
