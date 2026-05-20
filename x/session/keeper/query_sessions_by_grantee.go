package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/session/types"
)

func (q queryServer) SessionsByGrantee(ctx context.Context, req *types.QuerySessionsByGranteeRequest) (*types.QuerySessionsByGranteeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var sessions []types.Session

	// GrantsByGrantee is keyed by (grantee, id); iterate prefix=grantee and
	// filter for SESSION_KEY grants.
	rng := collections.NewPrefixedPairRange[string, uint64](req.Grantee)
	err := q.k.GrantsByGrantee.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
		id := key.K2()
		grant, err := q.k.Grants.Get(ctx, id)
		if err != nil {
			return true, err
		}
		if grant.Type != types.GrantType_GRANT_TYPE_SESSION_KEY {
			return false, nil
		}
		session, err := projectSession(grant)
		if err != nil {
			return true, err
		}
		sessions = append(sessions, session)
		if len(sessions) >= maxQueryResults {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return &types.QuerySessionsByGranteeResponse{Sessions: sessions}, nil
}
