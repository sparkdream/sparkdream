package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/artifact/types"
)

func (k msgServer) DeleteClass(ctx context.Context, msg *types.MsgDeleteClass) (*types.MsgDeleteClassResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	c, err := k.getOwnedClass(ctx, msg.ClassId, msg.Owner)
	if err != nil {
		return nil, err
	}
	if c.Supply != 0 || c.ReservedSupply != 0 {
		return nil, types.ErrClassNotEmpty
	}
	if has, _ := k.HideByTarget.Has(ctx, targetKey(c.Id, 0)); has {
		return nil, types.ErrContentHidden
	}
	if err := k.clearPendingOwner(ctx, c.Id); err != nil {
		return nil, err
	}
	if err := k.PublicMintCount.Clear(ctx, collections.NewPrefixedPairRange[uint64, string](c.Id)); err != nil {
		return nil, err
	}
	if err := k.ClassCancelQueue.Remove(ctx, c.Id); err != nil {
		return nil, err
	}
	if err := k.ClassScrubQueue.Remove(ctx, c.Id); err != nil {
		return nil, err
	}
	if err := k.ClassesByOwner.Remove(ctx, collections.Join(c.Owner, c.Id)); err != nil {
		return nil, err
	}
	if err := k.Classes.Remove(ctx, c.Id); err != nil {
		return nil, err
	}
	if err := k.adjustCreatorCount(ctx, c.Creator, -1); err != nil {
		return nil, err
	}
	emit(ctx, types.EventClassDeleted, classAttr(c.Id))
	return &types.MsgDeleteClassResponse{}, nil
}
