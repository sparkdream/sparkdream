package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PendingMigration returns the member's pending migration (REQUESTED,
// ACCEPTED or OBJECTED), or NotFound.
func (q queryServer) PendingMigration(ctx context.Context, req *types.QueryPendingMigrationRequest) (*types.QueryPendingMigrationResponse, error) {
	if req == nil || req.Member == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	m, found := q.k.GetPendingMigration(ctx, req.Member)
	if !found {
		return nil, status.Error(codes.NotFound, "no pending migration")
	}
	return &types.QueryPendingMigrationResponse{Migration: m}, nil
}
