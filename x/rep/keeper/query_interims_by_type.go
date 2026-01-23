package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) InterimsByType(ctx context.Context, req *types.QueryInterimsByTypeRequest) (*types.QueryInterimsByTypeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Collect first interim matching the specified type (proto response is singular)
	var foundInterim *types.Interim
	err := q.k.Interim.Walk(ctx, nil, func(id uint64, interim types.Interim) (bool, error) {
		if uint64(interim.Type) == req.InterimType {
			foundInterim = &interim
			return true, nil // stop iteration
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if foundInterim != nil {
		return &types.QueryInterimsByTypeResponse{
			InterimId: foundInterim.Id,
			Status:    uint64(foundInterim.Status),
			Deadline:  foundInterim.Deadline,
		}, nil
	}

	return &types.QueryInterimsByTypeResponse{}, nil
}
