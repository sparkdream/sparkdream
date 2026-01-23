package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) InterimsByAssignee(ctx context.Context, req *types.QueryInterimsByAssigneeRequest) (*types.QueryInterimsByAssigneeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Collect first interim assigned to the specified assignee (proto response is singular)
	var foundInterim *types.Interim
	err := q.k.Interim.Walk(ctx, nil, func(id uint64, interim types.Interim) (bool, error) {
		// Check if the assignee is in the assignees list
		for _, assignee := range interim.Assignees {
			if assignee == req.Assignee {
				foundInterim = &interim
				return true, nil // stop iteration
			}
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if foundInterim != nil {
		return &types.QueryInterimsByAssigneeResponse{
			InterimId:   foundInterim.Id,
			InterimType: uint64(foundInterim.Type),
			Status:      uint64(foundInterim.Status),
		}, nil
	}

	return &types.QueryInterimsByAssigneeResponse{}, nil
}
