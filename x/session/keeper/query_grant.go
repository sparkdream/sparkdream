package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/session/types"
)

// Grant looks up a single grant by id.
func (q queryServer) Grant(ctx context.Context, req *types.QueryGrantRequest) (*types.QueryGrantResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	grant, err := q.k.Grants.Get(ctx, req.Id)
	if err != nil {
		return nil, status.Error(codes.NotFound, "grant not found")
	}
	return &types.QueryGrantResponse{Grant: grant}, nil
}

// GrantsByGranter lists a granter's grants, optionally filtered by type,
// one page at a time (pagination.key / next_key; at most maxQueryResults per
// page).
func (q queryServer) GrantsByGranter(ctx context.Context, req *types.QueryGrantsByGranterRequest) (*types.QueryGrantsByGranterResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	grants, page, err := paginateGrants(ctx, q.k, q.k.GrantsByGranter, req.Granter, req.Pagination,
		typeFilter(req.Type), func(g types.Grant) (types.Grant, error) { return g, nil })
	if err != nil {
		return nil, err
	}
	return &types.QueryGrantsByGranterResponse{Grants: grants, Pagination: page}, nil
}

// GrantsByGrantee lists the grants naming a grantee, optionally filtered by
// type, one page at a time. Anyone can create grants naming any grantee, so
// a client must follow next_key rather than treat one page as complete.
func (q queryServer) GrantsByGrantee(ctx context.Context, req *types.QueryGrantsByGranteeRequest) (*types.QueryGrantsByGranteeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	grants, page, err := paginateGrants(ctx, q.k, q.k.GrantsByGrantee, req.Grantee, req.Pagination,
		typeFilter(req.Type), func(g types.Grant) (types.Grant, error) { return g, nil })
	if err != nil {
		return nil, err
	}
	return &types.QueryGrantsByGranteeResponse{Grants: grants, Pagination: page}, nil
}

// typeFilter keeps grants of t, or every grant when t is UNSPECIFIED.
func typeFilter(t types.GrantType) func(types.Grant) bool {
	return func(g types.Grant) bool {
		return t == types.GrantType_GRANT_TYPE_UNSPECIFIED || g.Type == t
	}
}

// paginateGrants pages through an (address, grant id) index under one
// address, keeping the grants keep accepts and projecting each with project.
// The page size defaults to, and is capped at, maxQueryResults.
func paginateGrants[T any](
	ctx context.Context,
	k Keeper,
	index collections.KeySet[collections.Pair[string, uint64]],
	addr string,
	pageReq *query.PageRequest,
	keep func(types.Grant) bool,
	project func(types.Grant) (T, error),
) ([]T, *query.PageResponse, error) {
	if pageReq == nil {
		pageReq = &query.PageRequest{}
	} else {
		cp := *pageReq
		pageReq = &cp
	}
	if pageReq.Limit == 0 || pageReq.Limit > maxQueryResults {
		pageReq.Limit = maxQueryResults
	}
	return query.CollectionFilteredPaginate(ctx, index, pageReq,
		func(key collections.Pair[string, uint64], _ collections.NoValue) (bool, error) {
			grant, err := k.Grants.Get(ctx, key.K2())
			if err != nil {
				return false, err
			}
			return keep(grant), nil
		},
		func(key collections.Pair[string, uint64], _ collections.NoValue) (T, error) {
			grant, err := k.Grants.Get(ctx, key.K2())
			if err != nil {
				var zero T
				return zero, err
			}
			return project(grant)
		},
		query.WithCollectionPaginationPairPrefix[string, uint64](addr),
	)
}
