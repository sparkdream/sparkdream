package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) AcceptInvitation(ctx context.Context, msg *types.MsgAcceptInvitation) (*types.MsgAcceptInvitationResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Invitee); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// TODO: Handle the message

	return &types.MsgAcceptInvitationResponse{}, nil
}
