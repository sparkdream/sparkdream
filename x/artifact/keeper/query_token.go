package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/artifact/types"
)

func (q queryServer) Token(ctx context.Context, req *types.QueryTokenRequest) (*types.QueryTokenResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	key := collections.Join(req.ClassId, req.TokenId)
	t, err := q.k.Tokens.Get(ctx, key)
	if err != nil {
		return nil, notFound(err, "token")
	}
	c, err := q.k.Classes.Get(ctx, req.ClassId)
	if err != nil {
		return nil, notFound(err, "class")
	}
	resp := &types.QueryTokenResponse{Token: q.k.tokenView(ctx, c, t, true)}
	if l, err := q.k.Listings.Get(ctx, key); err == nil {
		resp.Listing = &l
	}
	return resp, nil
}
