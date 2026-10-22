package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/artifact/types"
)

func (q queryServer) Children(ctx context.Context, req *types.QueryChildrenRequest) (*types.QueryChildrenResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	t, err := q.k.Tokens.Get(ctx, collections.Join(req.ClassId, req.TokenId))
	if err != nil {
		return nil, notFound(err, "token")
	}
	kids, err := q.k.directChildren(ctx, req.ClassId, req.TokenId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	hash, err := q.k.ContentsHash(ctx, t)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryChildrenResponse{Children: kids, ContentsHash: hash, Descendants: t.Descendants}, nil
}
