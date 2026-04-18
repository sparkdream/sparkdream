package keeper

import (
	"context"

	"cosmossdk.io/math"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/types"
)

func (q queryServer) SentinelBondCommitment(ctx context.Context, req *types.QuerySentinelBondCommitmentRequest) (*types.QuerySentinelBondCommitmentResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address required")
	}
	sa, err := q.k.SentinelActivity.Get(ctx, req.Address)
	if err != nil {
		return nil, status.Error(codes.NotFound, "sentinel not found")
	}
	currentBond := parseIntOrZero(sa.CurrentBond)
	committedBond := parseIntOrZero(sa.TotalCommittedBond)
	available := currentBond.Sub(committedBond)
	if available.IsNegative() {
		available = math.ZeroInt()
	}
	return &types.QuerySentinelBondCommitmentResponse{
		CurrentBond:        sa.CurrentBond,
		TotalCommittedBond: sa.TotalCommittedBond,
		AvailableBond:      available.String(),
	}, nil
}
