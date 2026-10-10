package keeper

import (
	"context"

	"sparkdream/x/artifact/types"
)

func (k msgServer) HideContent(ctx context.Context, msg *types.MsgHideContent) (*types.MsgHideContentResponse, error) {
	id, err := k.openHide(ctx, msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgHideContentResponse{HideId: id}, nil
}
