package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) InitiativeConviction(ctx context.Context, req *types.QueryInitiativeConvictionRequest) (*types.QueryInitiativeConvictionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Get the initiative
	initiative, err := q.k.Initiative.Get(ctx, req.InitiativeId)
	if err != nil {
		return nil, status.Error(codes.NotFound, "initiative not found")
	}

	// Update conviction lazily before returning
	if err := q.k.UpdateInitiativeConvictionLazy(ctx, req.InitiativeId); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Re-fetch initiative after conviction update
	initiative, err = q.k.Initiative.Get(ctx, req.InitiativeId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryInitiativeConvictionResponse{
		TotalConviction:    initiative.CurrentConviction,
		ExternalConviction: initiative.ExternalConviction,
		Threshold:          initiative.RequiredConviction,
	}, nil
}
