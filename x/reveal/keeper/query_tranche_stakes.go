package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/reveal/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) TrancheStakes(ctx context.Context, req *types.QueryTrancheStakesRequest) (*types.QueryTrancheStakesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	trancheKey := TrancheKey(req.ContributionId, req.TrancheId)

	var stakes []types.RevealStake
	err := q.k.StakesByTranche.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](trancheKey),
		func(key collections.Pair[string, uint64]) (bool, error) {
			stake, err := q.k.RevealStake.Get(ctx, key.K2())
			if err != nil {
				return false, nil // skip missing
			}
			stakes = append(stakes, stake)
			return false, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryTrancheStakesResponse{
		Stakes: stakes,
	}, nil
}
