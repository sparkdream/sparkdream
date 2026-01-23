package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) InterimsByReference(ctx context.Context, req *types.QueryInterimsByReferenceRequest) (*types.QueryInterimsByReferenceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Collect first interim matching the reference type and ID (proto response is singular)
	var foundInterim *types.Interim
	err := q.k.Interim.Walk(ctx, nil, func(id uint64, interim types.Interim) (bool, error) {
		if interim.ReferenceType == req.ReferenceType && interim.ReferenceId == req.ReferenceId {
			foundInterim = &interim
			return true, nil // stop iteration
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if foundInterim != nil {
		return &types.QueryInterimsByReferenceResponse{
			InterimId:   foundInterim.Id,
			InterimType: uint64(foundInterim.Type),
			Status:      uint64(foundInterim.Status),
		}, nil
	}

	return &types.QueryInterimsByReferenceResponse{}, nil
}
