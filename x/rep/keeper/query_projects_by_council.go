package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ProjectsByCouncil(ctx context.Context, req *types.QueryProjectsByCouncilRequest) (*types.QueryProjectsByCouncilResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Collect first project matching the council (proto response is singular)
	var foundProject *types.Project
	err := q.k.Project.Walk(ctx, nil, func(id uint64, project types.Project) (bool, error) {
		if project.Council == req.Council {
			foundProject = &project
			return true, nil // stop iteration
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if foundProject != nil {
		return &types.QueryProjectsByCouncilResponse{
			ProjectId: foundProject.Id,
			Name:      foundProject.Name,
			Status:    uint64(foundProject.Status),
		}, nil
	}

	return &types.QueryProjectsByCouncilResponse{}, nil
}
