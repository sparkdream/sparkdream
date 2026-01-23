package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) InitiativesByProject(ctx context.Context, req *types.QueryInitiativesByProjectRequest) (*types.QueryInitiativesByProjectResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Collect all initiatives for the specified project
	var initiatives []*types.Initiative
	err := q.k.Initiative.Walk(ctx, nil, func(id uint64, initiative types.Initiative) (bool, error) {
		if initiative.ProjectId == req.ProjectId {
			initiativeCopy := initiative
			initiatives = append(initiatives, &initiativeCopy)
		}
		return false, nil // continue iteration
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryInitiativesByProjectResponse{
		Initiatives: initiatives,
	}, nil
}
