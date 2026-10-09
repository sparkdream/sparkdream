package keeper

import (
	"context"

	"sparkdream/x/service/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Checkpoint returns the latest checkpoint for (operator, service_type), or
// NotFound if the operator has never checkpointed under that type.
func (q queryServer) Checkpoint(ctx context.Context, req *types.QueryCheckpointRequest) (*types.QueryCheckpointResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	addrBytes, err := q.k.addrBytes(req.Operator)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid address")
	}

	cp, ok := q.k.GetCheckpoint(ctx, addrBytes, req.ServiceType)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "no checkpoint for %s / %s", req.Operator, req.ServiceType)
	}

	return &types.QueryCheckpointResponse{Checkpoint: cp}, nil
}
