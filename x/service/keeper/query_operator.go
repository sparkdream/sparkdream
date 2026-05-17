package keeper

import (
	"context"

	"sparkdream/x/service/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Operator returns the live operator record for (address, service_type),
// or NotFound if missing (terminal records live in ArchivedOperators
// and aren't returned by this RPC).
func (q queryServer) Operator(ctx context.Context, req *types.QueryOperatorRequest) (*types.QueryOperatorResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	addrBytes, err := q.k.addrBytes(req.Address)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid address")
	}

	op, exists := q.k.GetOperator(ctx, addrBytes, req.ServiceType)
	if !exists {
		return nil, status.Errorf(codes.NotFound, "operator %s / %s not found", req.Address, req.ServiceType)
	}

	return &types.QueryOperatorResponse{Operator: op}, nil
}
