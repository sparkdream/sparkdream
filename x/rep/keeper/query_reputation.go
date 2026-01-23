package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) Reputation(ctx context.Context, req *types.QueryReputationRequest) (*types.QueryReputationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Get member by address
	member, err := q.k.Member.Get(ctx, req.Address)
	if err != nil {
		return nil, status.Error(codes.NotFound, "member not found")
	}

	// Find reputation score for the specified tag
	score := math.LegacyZeroDec()
	lifetime := math.LegacyZeroDec()

	// ReputationScores is a map[string]string where key is tag
	if scoreStr, ok := member.ReputationScores[req.Tag]; ok {
		parsedScore, err := math.LegacyNewDecFromStr(scoreStr)
		if err == nil {
			score = parsedScore
		}
	}

	// LifetimeReputation is also a map[string]string where key is tag
	if lifetimeStr, ok := member.LifetimeReputation[req.Tag]; ok {
		parsedLifetime, err := math.LegacyNewDecFromStr(lifetimeStr)
		if err == nil {
			lifetime = parsedLifetime
		}
	}

	return &types.QueryReputationResponse{
		Score:    &score,
		Lifetime: &lifetime,
	}, nil
}
