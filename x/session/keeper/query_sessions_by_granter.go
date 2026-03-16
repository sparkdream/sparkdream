package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/session/types"
)

func (q queryServer) SessionsByGranter(ctx context.Context, req *types.QuerySessionsByGranterRequest) (*types.QuerySessionsByGranterResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var sessions []types.Session

	rng := collections.NewPrefixedPairRange[string, string](req.Granter)
	err := q.k.SessionsByGranter.Walk(ctx, rng, func(key collections.Pair[string, string]) (bool, error) {
		granter := key.K1()
		grantee := key.K2()
		session, err := q.k.Sessions.Get(ctx, collections.Join(granter, grantee))
		if err != nil {
			return true, err
		}
		sessions = append(sessions, session)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return &types.QuerySessionsByGranterResponse{Sessions: sessions}, nil
}
