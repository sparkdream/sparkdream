package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListPendingIdentityChallenges(ctx context.Context, req *types.QueryListPendingIdentityChallengesRequest) (*types.QueryListPendingIdentityChallengesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Filter by claimed_address using prefix range
	rng := collections.NewPrefixedPairRange[string, string](req.ClaimedAddress)
	var challenges []types.PendingIdentityChallenge
	err := q.k.PendingIdChallenges.Walk(ctx, rng, func(key collections.Pair[string, string], value types.PendingIdentityChallenge) (bool, error) {
		challenges = append(challenges, value)
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryListPendingIdentityChallengesResponse{
		Challenges: challenges,
	}, nil
}
