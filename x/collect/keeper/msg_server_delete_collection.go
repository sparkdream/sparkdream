package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/collect/types"
)

func (k msgServer) DeleteCollection(ctx context.Context, msg *types.MsgDeleteCollection) (*types.MsgDeleteCollectionResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Get collection
	coll, err := k.Collection.Get(ctx, msg.Id)
	if err != nil {
		return nil, types.ErrCollectionNotFound
	}

	// Must be owner
	if coll.Owner != msg.Creator {
		return nil, types.ErrUnauthorized
	}

	// Call deleteCollectionFull() helper
	if err := k.deleteCollectionFull(ctx, coll); err != nil {
		return nil, errorsmod.Wrap(err, "failed to delete collection")
	}

	return &types.MsgDeleteCollectionResponse{}, nil
}
