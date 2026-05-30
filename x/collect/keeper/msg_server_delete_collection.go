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

	// Cannot self-delete a HIDDEN collection. A sentinel hide is currently
	// standing against it; allowing the owner to delete here would let them
	// dodge the endorsement-slash decision (forfeit-by-delete instead of
	// appeal). The owner's recourse is MsgAppealHide; if the appeal upholds,
	// status restores to ACTIVE and delete becomes available again.
	if coll.Status == types.CollectionStatus_COLLECTION_STATUS_HIDDEN {
		return nil, types.ErrCannotDeleteHidden
	}

	// Call deleteCollectionFull() helper
	if err := k.deleteCollectionFull(ctx, coll); err != nil {
		return nil, errorsmod.Wrap(err, "failed to delete collection")
	}

	return &types.MsgDeleteCollectionResponse{}, nil
}
