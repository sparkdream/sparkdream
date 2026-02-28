package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ContentConviction(ctx context.Context, req *types.QueryContentConvictionRequest) (*types.QueryContentConvictionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	targetType := types.StakeTargetType(req.TargetType)
	if !types.IsContentConvictionType(targetType) {
		return nil, status.Errorf(codes.InvalidArgument, "target_type must be a content conviction type (4, 5, or 6), got %d", req.TargetType)
	}

	// Get total conviction score
	totalConviction, err := q.k.GetContentConviction(ctx, targetType, req.TargetId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Get stakes for count and total staked
	stakes, err := q.k.GetContentStakes(ctx, targetType, req.TargetId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	totalStaked := math.ZeroInt()
	for _, s := range stakes {
		totalStaked = totalStaked.Add(s.Amount)
	}

	return &types.QueryContentConvictionResponse{
		TotalConviction: totalConviction,
		StakerCount:     uint64(len(stakes)),
		TotalStaked:     totalStaked,
	}, nil
}
