package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MemberMigrations lists every migration, paginated by id.
func (q queryServer) MemberMigrations(ctx context.Context, req *types.QueryMemberMigrationsRequest) (*types.QueryMemberMigrationsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	migrations, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.MemberMigrations,
		req.Pagination,
		func(_ uint64, m types.MemberMigration) (types.MemberMigration, error) {
			return m, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryMemberMigrationsResponse{Migrations: migrations, Pagination: pageRes}, nil
}
