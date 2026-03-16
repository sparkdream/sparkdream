package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/session/types"
)

func (q queryServer) Session(ctx context.Context, req *types.QuerySessionRequest) (*types.QuerySessionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	key := collections.Join(req.Granter, req.Grantee)
	session, err := q.k.Sessions.Get(ctx, key)
	if err != nil {
		return nil, types.ErrSessionNotFound
	}

	return &types.QuerySessionResponse{Session: session}, nil
}
