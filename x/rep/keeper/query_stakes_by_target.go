package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) StakesByTarget(ctx context.Context, req *types.QueryStakesByTargetRequest) (*types.QueryStakesByTargetResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Collect all stakes matching the target type and ID
	var stakes []*types.Stake
	err := q.k.Stake.Walk(ctx, nil, func(id uint64, stake types.Stake) (bool, error) {
		if uint64(stake.TargetType) == req.TargetType && stake.TargetId == req.TargetId {
			stakeCopy := stake
			stakes = append(stakes, &stakeCopy)
		}
		return false, nil // continue iteration
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryStakesByTargetResponse{
		Stakes: stakes,
	}, nil
}
