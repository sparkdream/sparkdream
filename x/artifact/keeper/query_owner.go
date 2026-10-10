package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/artifact/types"
)

func (q queryServer) Owner(ctx context.Context, req *types.QueryOwnerRequest) (*types.QueryOwnerResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	t, err := q.k.Tokens.Get(ctx, collections.Join(req.ClassId, req.TokenId))
	if err != nil {
		return nil, notFound(err, "token")
	}
	return &types.QueryOwnerResponse{Owner: t.Owner}, nil
}
