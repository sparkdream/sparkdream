package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"cosmossdk.io/math"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) SentinelBondCommitment(ctx context.Context, req *types.QuerySentinelBondCommitmentRequest) (*types.QuerySentinelBondCommitmentResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address required")
	}

	// Get sentinel activity
	sentinelActivity, err := q.k.SentinelActivity.Get(ctx, req.Address)
	if err != nil {
		return nil, status.Error(codes.NotFound, "sentinel not found")
	}

	// Calculate available bond
	currentBond, _ := math.NewIntFromString(sentinelActivity.CurrentBond)
	if sentinelActivity.CurrentBond == "" {
		currentBond = math.ZeroInt()
	}
	committedBond, _ := math.NewIntFromString(sentinelActivity.TotalCommittedBond)
	if sentinelActivity.TotalCommittedBond == "" {
		committedBond = math.ZeroInt()
	}
	availableBond := currentBond.Sub(committedBond)

	return &types.QuerySentinelBondCommitmentResponse{
		CurrentBond:        sentinelActivity.CurrentBond,
		TotalCommittedBond: sentinelActivity.TotalCommittedBond,
		AvailableBond:      availableBond.String(),
	}, nil
}
