package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) GovActionAppeals(ctx context.Context, req *types.QueryGovActionAppealsRequest) (*types.QueryGovActionAppealsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Get first gov action appeal (simplified - in production would return list with pagination)
	var firstAppeal *types.GovActionAppeal

	err := q.k.GovActionAppeal.Walk(ctx, nil, func(key uint64, appeal types.GovActionAppeal) (bool, error) {
		firstAppeal = &appeal
		return true, nil // Stop after first
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if firstAppeal != nil {
		return &types.QueryGovActionAppealsResponse{
			AppealId:   firstAppeal.Id,
			ActionType: uint64(firstAppeal.ActionType),
			Status:     uint64(firstAppeal.Status),
		}, nil
	}

	return &types.QueryGovActionAppealsResponse{}, nil
}
