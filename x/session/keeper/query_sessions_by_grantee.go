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

	// Index key is (grantee, granter); session primary key is (granter, grantee)
	rng := collections.NewPrefixedPairRange[string, string](req.Grantee)
	err := q.k.SessionsByGrantee.Walk(ctx, rng, func(key collections.Pair[string, string]) (bool, error) {
		granter := key.K2() // K1 = grantee (prefix), K2 = granter
		session, err := q.k.Sessions.Get(ctx, collections.Join(granter, req.Grantee))
		if err != nil {
			return true, err
		}
		sessions = append(sessions, session)
		// SESSION-6 fix: hard cap to prevent unbounded iteration
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
