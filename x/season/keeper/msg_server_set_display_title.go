package keeper

import (
	"context"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) SetDisplayTitle(ctx context.Context, msg *types.MsgSetDisplayTitle) (*types.MsgSetDisplayTitleResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// TODO: Handle the message

	return &types.MsgSetDisplayTitleResponse{}, nil
}
