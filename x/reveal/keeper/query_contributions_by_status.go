package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/reveal/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ContributionsByStatus(ctx context.Context, req *types.QueryContributionsByStatusRequest) (*types.QueryContributionsByStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var contributions []types.Contribution
	err := q.k.ContributionsByStatus.Walk(ctx,
		collections.NewPrefixedPairRange[int32, uint64](int32(req.Status)),
		func(key collections.Pair[int32, uint64]) (bool, error) {
			contrib, err := q.k.Contribution.Get(ctx, key.K2())
			if err != nil {
				return false, nil // skip missing
			}
			contributions = append(contributions, contrib)
			return false, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryContributionsByStatusResponse{
		Contributions: contributions,
	}, nil
}
