package keeper

import (
	"context"

	"sparkdream/x/artifact/types"
)

func (k msgServer) FreezeClassMetadata(ctx context.Context, msg *types.MsgFreezeClassMetadata) (*types.MsgFreezeClassMetadataResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	c, err := k.getOwnedClass(ctx, msg.ClassId, msg.Owner)
	if err != nil {
		return nil, err
	}
	if c.MetadataFrozen {
		return nil, types.ErrMetadataFrozen
	}
	c.MetadataFrozen = true
	if err := k.saveClass(ctx, c); err != nil {
		return nil, err
	}
	emit(ctx, types.EventClassMetadataFrozen, classAttr(c.Id))
	return &types.MsgFreezeClassMetadataResponse{}, nil
}
