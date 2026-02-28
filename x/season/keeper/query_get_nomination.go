package keeper

import (
	"context"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetNomination returns a nomination by ID.
func (q queryServer) GetNomination(ctx context.Context, req *types.QueryGetNominationRequest) (*types.QueryGetNominationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	nomination, err := q.k.Nomination.Get(ctx, req.Id)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "nomination %d not found", req.Id)
	}

	return &types.QueryGetNominationResponse{Nomination: nomination}, nil
}
