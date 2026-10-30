package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/session/types"
)

func (q queryServer) SessionsByGrantee(ctx context.Context, req *types.QuerySessionsByGranteeRequest) (*types.QuerySessionsByGranteeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	sessions, page, err := paginateGrants(ctx, q.k, q.k.GrantsByGrantee, req.Grantee, req.Pagination,
		typeFilter(types.GrantType_GRANT_TYPE_SESSION_KEY), projectSession)
	if err != nil {
		return nil, err
	}
	return &types.QuerySessionsByGranteeResponse{Sessions: sessions, Pagination: page}, nil
}
