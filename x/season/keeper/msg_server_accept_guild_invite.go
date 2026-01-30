package keeper

import (
	"context"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) AcceptGuildInvite(ctx context.Context, msg *types.MsgAcceptGuildInvite) (*types.MsgAcceptGuildInviteResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// TODO: Handle the message

	return &types.MsgAcceptGuildInviteResponse{}, nil
}
