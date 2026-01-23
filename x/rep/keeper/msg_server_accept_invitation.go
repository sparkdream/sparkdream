package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) AcceptInvitation(ctx context.Context, msg *types.MsgAcceptInvitation) (*types.MsgAcceptInvitationResponse, error) {
	inviteeAddr, err := k.addressCodec.StringToBytes(msg.Invitee)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid invitee address")
	}

	// Accept invitation
	if err := k.Keeper.AcceptInvitation(ctx, msg.InvitationId, inviteeAddr); err != nil {
		return nil, err
	}

	return &types.MsgAcceptInvitationResponse{}, nil
}
