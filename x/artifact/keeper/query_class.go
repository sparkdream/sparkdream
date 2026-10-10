package keeper

import (
	"context"

	"sparkdream/x/artifact/types"
)

func (q queryServer) Class(ctx context.Context, req *types.QueryClassRequest) (*types.QueryClassResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	c, err := q.k.Classes.Get(ctx, req.ClassId)
	if err != nil {
		return nil, notFound(err, "class")
	}
	return &types.QueryClassResponse{Class: ClassView(c)}, nil
}
