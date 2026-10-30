package keeper

import (
	"context"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ChallengesByInitiative(ctx context.Context, req *types.QueryChallengesByInitiativeRequest) (*types.QueryChallengesByInitiativeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Collect first challenge for the specified initiative (proto response is singular)
	var foundChallenge *types.Challenge
	err := q.k.Challenge.Walk(ctx, nil, func(id uint64, challenge types.Challenge) (bool, error) {
		if challenge.InitiativeId == req.InitiativeId {
			foundChallenge = &challenge
			return true, nil // stop iteration
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if foundChallenge != nil {
		return &types.QueryChallengesByInitiativeResponse{
			ChallengeId: foundChallenge.Id,
			Status:      commontypes.Saturate[uint64](foundChallenge.Status),
		}, nil
	}

	return &types.QueryChallengesByInitiativeResponse{}, nil
}
