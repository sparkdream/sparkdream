package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PendingMigrationByTarget returns the pending migration (REQUESTED, ACCEPTED
// or OBJECTED) whose new address is the given address, or NotFound.
func (q queryServer) PendingMigrationByTarget(ctx context.Context, req *types.QueryPendingMigrationByTargetRequest) (*types.QueryPendingMigrationByTargetResponse, error) {
	if req == nil || req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	m, found := q.k.GetPendingMigrationByTarget(ctx, req.Address)
	if !found {
		return nil, status.Error(codes.NotFound, "no pending migration")
	}
	return &types.QueryPendingMigrationByTargetResponse{Migration: m}, nil
}
