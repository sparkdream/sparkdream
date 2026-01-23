package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) InitiativesByAssignee(ctx context.Context, req *types.QueryInitiativesByAssigneeRequest) (*types.QueryInitiativesByAssigneeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Collect first initiative assigned to the specified assignee (proto response is singular)
	var foundInitiative *types.Initiative
	err := q.k.Initiative.Walk(ctx, nil, func(id uint64, initiative types.Initiative) (bool, error) {
		if initiative.Assignee == req.Assignee {
			foundInitiative = &initiative
			return true, nil // stop iteration
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if foundInitiative != nil {
		return &types.QueryInitiativesByAssigneeResponse{
			InitiativeId: foundInitiative.Id,
			Title:        foundInitiative.Title,
			Status:       uint64(foundInitiative.Status),
		}, nil
	}

	return &types.QueryInitiativesByAssigneeResponse{}, nil
}
