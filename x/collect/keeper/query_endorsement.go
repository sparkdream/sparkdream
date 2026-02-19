package keeper

import (
	"context"

	"sparkdream/x/collect/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) Endorsement(ctx context.Context, req *types.QueryEndorsementRequest) (*types.QueryEndorsementResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	endorsement, err := q.k.Endorsement.Get(ctx, req.CollectionId)
	if err != nil {
		return nil, status.Error(codes.NotFound, "endorsement not found")
	}

	return &types.QueryEndorsementResponse{Endorsement: endorsement}, nil
}
