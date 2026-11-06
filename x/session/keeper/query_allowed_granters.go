package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/session/types"
)

// AllowedGranters pages through the granters a grantee has allowed
// (SES-102), at most maxQueryResults per page.
func (q queryServer) AllowedGranters(ctx context.Context, req *types.QueryAllowedGrantersRequest) (*types.QueryAllowedGrantersResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if _, err := q.k.addressCodec.StringToBytes(req.Grantee); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid grantee address")
	}
	pageReq := &query.PageRequest{}
	if req.Pagination != nil {
		cp := *req.Pagination
		pageReq = &cp
	}
	if pageReq.Limit == 0 || pageReq.Limit > maxQueryResults {
		pageReq.Limit = maxQueryResults
	}
	granters, page, err := query.CollectionPaginate(ctx, q.k.AllowedGranters, pageReq,
		func(key collections.Pair[string, string], _ collections.NoValue) (string, error) {
			return key.K2(), nil
		},
		query.WithCollectionPaginationPairPrefix[string, string](req.Grantee),
	)
	if err != nil {
		return nil, err
	}
	return &types.QueryAllowedGrantersResponse{Granters: granters, Pagination: page}, nil
}
