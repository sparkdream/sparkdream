package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MemberMigrations lists migrations, paginated by id, optionally filtered by
// status (UNSPECIFIED = all) and by an address on either side.
func (q queryServer) MemberMigrations(ctx context.Context, req *types.QueryMemberMigrationsRequest) (*types.QueryMemberMigrationsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	migrations, pageRes, err := query.CollectionFilteredPaginate(
		ctx,
		q.k.MemberMigrations,
		req.Pagination,
		func(_ uint64, m types.MemberMigration) (bool, error) {
			if req.Status != types.MigrationStatus_MIGRATION_STATUS_UNSPECIFIED && m.Status != req.Status {
				return false, nil
			}
			return req.Address == "" || m.FromAddress == req.Address || m.ToAddress == req.Address, nil
		},
		func(_ uint64, m types.MemberMigration) (types.MemberMigration, error) {
			return m, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryMemberMigrationsResponse{Migrations: migrations, Pagination: pageRes}, nil
}
