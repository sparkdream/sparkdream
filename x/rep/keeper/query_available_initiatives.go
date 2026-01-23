package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) AvailableInitiatives(ctx context.Context, req *types.QueryAvailableInitiativesRequest) (*types.QueryAvailableInitiativesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Collect all initiatives with status OPEN (not yet assigned)
	var initiatives []types.Initiative
	err := q.k.Initiative.Walk(ctx, nil, func(id uint64, initiative types.Initiative) (bool, error) {
		if initiative.Status == types.InitiativeStatus_INITIATIVE_STATUS_OPEN {
			initiatives = append(initiatives, initiative)
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// For now return the first available initiative (proto response is singular)
	// This matches the proto definition which has singular fields, not repeated
	if len(initiatives) > 0 {
		first := initiatives[0]
		return &types.QueryAvailableInitiativesResponse{
			InitiativeId: first.Id,
			Title:        first.Title,
			Tier:         uint64(first.Tier),
			Budget:       first.Budget,
		}, nil
	}

	return &types.QueryAvailableInitiativesResponse{}, nil
}
