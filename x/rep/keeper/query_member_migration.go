package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MemberMigration returns a migration by id.
func (q queryServer) MemberMigration(ctx context.Context, req *types.QueryMemberMigrationRequest) (*types.QueryMemberMigrationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	m, found := q.k.GetMemberMigration(ctx, req.Id)
	if !found {
		return nil, status.Error(codes.NotFound, "not found")
	}
	return &types.QueryMemberMigrationResponse{Migration: m}, nil
}
