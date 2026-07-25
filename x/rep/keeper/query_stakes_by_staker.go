package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) StakesByStaker(ctx context.Context, req *types.QueryStakesByStakerRequest) (*types.QueryStakesByStakerResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Collect all stakes owned by the specified staker.
	var stakes []*types.Stake
	err := q.k.Stake.Walk(ctx, nil, func(id uint64, stake types.Stake) (bool, error) {
		if stake.Staker == req.Staker {
			stakeCopy := stake
			stakes = append(stakes, &stakeCopy)
		}
		return false, nil // continue iteration
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryStakesByStakerResponse{
		Stakes: stakes,
	}, nil
}
