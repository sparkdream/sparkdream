package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/session/types"
)

// maxQueryResults is the hard cap on results returned by session list queries.
const maxQueryResults = 100

func (q queryServer) SessionsByGranter(ctx context.Context, req *types.QuerySessionsByGranterRequest) (*types.QuerySessionsByGranterResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	sessions, page, err := paginateGrants(ctx, q.k, q.k.GrantsByGranter, req.Granter, req.Pagination,
		typeFilter(types.GrantType_GRANT_TYPE_SESSION_KEY), projectSession)
	if err != nil {
		return nil, err
	}
	return &types.QuerySessionsByGranterResponse{Sessions: sessions, Pagination: page}, nil
}
