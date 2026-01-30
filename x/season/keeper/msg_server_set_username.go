package keeper

import (
	"context"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) SetUsername(ctx context.Context, msg *types.MsgSetUsername) (*types.MsgSetUsernameResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// TODO: Handle the message

	return &types.MsgSetUsernameResponse{}, nil
}
