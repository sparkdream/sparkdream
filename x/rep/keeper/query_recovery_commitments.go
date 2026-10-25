package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RecoveryCommitments returns a member's recovery commitment set with due
// removals applied. Commitments are hashes: listing them reveals nothing.
func (q queryServer) RecoveryCommitments(ctx context.Context, req *types.QueryRecoveryCommitmentsRequest) (*types.QueryRecoveryCommitmentsResponse, error) {
	if req == nil || req.Member == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	set, found := q.k.GetRecoveryCommitments(ctx, req.Member)
	if !found {
		return nil, status.Error(codes.NotFound, "no recovery commitments")
	}
	return &types.QueryRecoveryCommitmentsResponse{Set: set}, nil
}
