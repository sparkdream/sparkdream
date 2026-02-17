package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/reveal/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ContributionsByContributor(ctx context.Context, req *types.QueryContributionsByContributorRequest) (*types.QueryContributionsByContributorResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var contributions []types.Contribution
	err := q.k.ContributionsByContributor.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](req.Contributor),
		func(key collections.Pair[string, uint64]) (bool, error) {
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

	return &types.QueryContributionsByContributorResponse{
		Contributions: contributions,
	}, nil
}
