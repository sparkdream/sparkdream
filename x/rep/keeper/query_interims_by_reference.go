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

	// TODO: Process the query

	return &types.QueryInterimsByReferenceResponse{}, nil
}
