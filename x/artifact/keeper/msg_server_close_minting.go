package keeper

import (
	"context"

	"sparkdream/x/artifact/types"
)

func (k msgServer) CloseMinting(ctx context.Context, msg *types.MsgCloseMinting) (*types.MsgCloseMintingResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	c, err := k.getOwnedClass(ctx, msg.ClassId, msg.Owner)
	if err != nil {
		return nil, err
	}
	if c.MintingClosed {
		return nil, types.ErrMintingClosed
	}
	c.MintingClosed = true
	c.MintPolicy.PublicMintEnabled = false
	if err := k.saveClass(ctx, c); err != nil {
		return nil, err
	}
	emit(ctx, types.EventMintingClosed, classAttr(c.Id))
	return &types.MsgCloseMintingResponse{}, nil
}
