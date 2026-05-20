package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/session/types"
)

func (q queryServer) Session(ctx context.Context, req *types.QuerySessionRequest) (*types.QuerySessionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	session, err := q.k.GetSession(ctx, req.Granter, req.Grantee)
	if err != nil {
		return nil, types.ErrSessionNotFound
	}

	return &types.QuerySessionResponse{Session: session}, nil
}
