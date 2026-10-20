package keeper

import (
	"context"

	"sparkdream/x/artifact/types"
)

// LockRoyalty freezes royalty_bps and royalty_recipients for the life of the
// class (docs/x-artifact-spec.md §5.1.12). One-way, like metadata_frozen: a
// collaborator's share no longer depends on the owner's word.
func (k msgServer) LockRoyalty(ctx context.Context, msg *types.MsgLockRoyalty) (*types.MsgLockRoyaltyResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	c, err := k.getOwnedClass(ctx, msg.ClassId, msg.Owner)
	if err != nil {
		return nil, err
	}
	if c.RoyaltyLocked {
		return nil, types.ErrRoyaltyLocked
	}
	c.RoyaltyLocked = true
	if err := k.saveClass(ctx, c); err != nil {
		return nil, err
	}
	emit(ctx, types.EventRoyaltyLocked, classAttr(c.Id))
	return &types.MsgLockRoyaltyResponse{}, nil
}
