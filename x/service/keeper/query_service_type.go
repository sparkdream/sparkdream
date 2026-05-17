package keeper

import (
	"context"

	"sparkdream/x/service/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ServiceType returns the single registry entry for the given key.
func (q queryServer) ServiceType(ctx context.Context, req *types.QueryServiceTypeRequest) (*types.QueryServiceTypeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	cfg, err := q.k.ServiceTypes.Get(ctx, req.ServiceType)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "service type %s not found", req.ServiceType)
	}

	return &types.QueryServiceTypeResponse{Config: cfg}, nil
}
