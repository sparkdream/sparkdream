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
	// Records stay indexed under the address that wrote them; a migrated
	// member's are gathered across every alias (unpaginated, as before).
	for _, contributor := range q.k.allAddressesOf(ctx, req.Contributor) {
		err := q.k.ContributionsByContributor.Walk(ctx,
			collections.NewPrefixedPairRange[string, uint64](contributor),
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
	}

	return &types.QueryContributionsByContributorResponse{
		Contributions: contributions,
	}, nil
}
