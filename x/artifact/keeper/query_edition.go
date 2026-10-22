package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/artifact/types"
)

func (q queryServer) Edition(ctx context.Context, req *types.QueryEditionRequest) (*types.QueryEditionResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	e, err := q.k.Editions.Get(ctx, collections.Join(req.ClassId, req.EditionId))
	if err != nil {
		return nil, notFound(err, "edition")
	}
	c, err := q.k.Classes.Get(ctx, req.ClassId)
	if err != nil {
		return nil, notFound(err, "class")
	}
	return &types.QueryEditionResponse{Edition: EditionView(c.Status, e)}, nil
}
