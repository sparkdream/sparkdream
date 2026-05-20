package keeper

import (
	"context"

	"cosmossdk.io/collections"
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

// GrantsByGranter lists all grants for a granter, optionally filtered by type.
func (q queryServer) GrantsByGranter(ctx context.Context, req *types.QueryGrantsByGranterRequest) (*types.QueryGrantsByGranterResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var grants []types.Grant

	// When a type filter is supplied we use the more-selective
	// GrantsByTypeAndGranter index; otherwise we walk GrantsByGranter.
	if req.Type != types.GrantType_GRANT_TYPE_UNSPECIFIED {
		rng := collections.NewSuperPrefixedTripleRange[int32, string, uint64](
			int32(req.Type),
			req.Granter,
		)
		err := q.k.GrantsByTypeAndGranter.Walk(ctx, rng, func(key collections.Triple[int32, string, uint64]) (bool, error) {
			grant, err := q.k.Grants.Get(ctx, key.K3())
			if err != nil {
				return true, err
			}
			grants = append(grants, grant)
			if len(grants) >= maxQueryResults {
				return true, nil
			}
			return false, nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		rng := collections.NewPrefixedPairRange[string, uint64](req.Granter)
		err := q.k.GrantsByGranter.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
			grant, err := q.k.Grants.Get(ctx, key.K2())
			if err != nil {
				return true, err
			}
			grants = append(grants, grant)
			if len(grants) >= maxQueryResults {
				return true, nil
			}
			return false, nil
		})
		if err != nil {
			return nil, err
		}
	}

	return &types.QueryGrantsByGranterResponse{Grants: grants}, nil
}

// GrantsByGrantee lists all grants for a grantee, optionally filtered by type.
func (q queryServer) GrantsByGrantee(ctx context.Context, req *types.QueryGrantsByGranteeRequest) (*types.QueryGrantsByGranteeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var grants []types.Grant
	filterType := req.Type != types.GrantType_GRANT_TYPE_UNSPECIFIED

	rng := collections.NewPrefixedPairRange[string, uint64](req.Grantee)
	err := q.k.GrantsByGrantee.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
		grant, err := q.k.Grants.Get(ctx, key.K2())
		if err != nil {
			return true, err
		}
		if filterType && grant.Type != req.Type {
			return false, nil
		}
		grants = append(grants, grant)
		if len(grants) >= maxQueryResults {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return &types.QueryGrantsByGranteeResponse{Grants: grants}, nil
}
