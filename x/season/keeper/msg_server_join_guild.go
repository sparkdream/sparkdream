package keeper

import (
	"context"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) JoinGuild(ctx context.Context, msg *types.MsgJoinGuild) (*types.MsgJoinGuildResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// TODO: Handle the message

	return &types.MsgJoinGuildResponse{}, nil
}
