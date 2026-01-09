package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) InviteMember(ctx context.Context, msg *types.MsgInviteMember) (*types.MsgInviteMemberResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Inviter); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// TODO: Handle the message

	return &types.MsgInviteMemberResponse{}, nil
}
