package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/artifact/types"
)

func (q queryServer) PendingClassOwner(ctx context.Context, req *types.QueryPendingClassOwnerRequest) (*types.QueryPendingClassOwnerResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	po, err := q.k.PendingClassOwners.Get(ctx, req.ClassId)
	if err != nil {
		return nil, notFound(err, "pending class owner")
	}
	if po.ExpiresAt <= now(ctx) {
		return nil, notFound(collections.ErrNotFound, "pending class owner")
	}
	return &types.QueryPendingClassOwnerResponse{Pending: po}, nil
}
