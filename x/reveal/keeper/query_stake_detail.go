package keeper

import (
	"context"

	"sparkdream/x/reveal/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) StakeDetail(ctx context.Context, req *types.QueryStakeDetailRequest) (*types.QueryStakeDetailResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	stake, err := q.k.RevealStake.Get(ctx, req.StakeId)
	if err != nil {
		return nil, status.Error(codes.NotFound, types.ErrStakeNotFound.Error())
	}

	return &types.QueryStakeDetailResponse{Stake: stake}, nil
}
