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
	resp := &types.QueryTokenResponse{Token: TokenView(q.classStatus(ctx, map[uint64]types.ContentStatus{}, req.ClassId), t)}
	if l, err := q.k.Listings.Get(ctx, key); err == nil {
		resp.Listing = &l
	}
	return resp, nil
}
