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

	// Collect first stake by the specified staker (proto response is singular)
	var foundStake *types.Stake
	err := q.k.Stake.Walk(ctx, nil, func(id uint64, stake types.Stake) (bool, error) {
		if stake.Staker == req.Staker {
			foundStake = &stake
			return true, nil // stop iteration
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if foundStake != nil {
		return &types.QueryStakesByStakerResponse{
			StakeId:    foundStake.Id,
			TargetType: uint64(foundStake.TargetType),
			Amount:     &foundStake.Amount,
		}, nil
	}

	return &types.QueryStakesByStakerResponse{}, nil
}
