package keeper

import (
	"context"

	"cosmossdk.io/collections"
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

	var sessions []types.Session

	// Iterate the by-type-and-granter index, scoped to SESSION_KEY grants
	// for this granter.
	rng := collections.NewSuperPrefixedTripleRange[int32, string, uint64](
		int32(types.GrantType_GRANT_TYPE_SESSION_KEY),
		req.Granter,
	)
	err := q.k.GrantsByTypeAndGranter.Walk(ctx, rng, func(key collections.Triple[int32, string, uint64]) (bool, error) {
		id := key.K3()
		grant, err := q.k.Grants.Get(ctx, id)
		if err != nil {
			return true, err
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

	return &types.QuerySessionsByGranterResponse{Sessions: sessions}, nil
}
