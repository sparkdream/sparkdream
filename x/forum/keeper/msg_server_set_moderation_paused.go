package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) SetModerationPaused(ctx context.Context, msg *types.MsgSetModerationPaused) (*types.MsgSetModerationPausedResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// TODO: Handle the message

	return &types.MsgSetModerationPausedResponse{}, nil
}
