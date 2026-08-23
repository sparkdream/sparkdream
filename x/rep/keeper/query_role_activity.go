package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/types"
)

// RoleActivity exposes a bonded role holder's accountability record.
//
// x/forum projects the sentinel view of this record, but reviewers, curators and
// verifiers had no way to see theirs at all — which matters because the record
// gates a role's share of its reward pool and drives the consecutive-overturn
// demotion. An accuracy score nobody can read is one nobody can contest.
func (q queryServer) RoleActivity(ctx context.Context, req *types.QueryRoleActivityRequest) (*types.QueryRoleActivityResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address cannot be empty")
	}
	activity, err := q.k.GetRoleActivity(ctx, req.RoleType, req.Address)
	if err != nil {
		return nil, status.Error(codes.NotFound, "no activity record for this role and address")
	}
	return &types.QueryRoleActivityResponse{RoleActivity: activity}, nil
}
