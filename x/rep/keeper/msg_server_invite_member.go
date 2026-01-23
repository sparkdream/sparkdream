package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) InviteMember(ctx context.Context, msg *types.MsgInviteMember) (*types.MsgInviteMemberResponse, error) {
	inviterAddr, err := k.addressCodec.StringToBytes(msg.Inviter)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid inviter address")
	}

	inviteeAddr, err := sdk.AccAddressFromBech32(msg.InviteeAddress)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid invitee address")
	}

	// Create invitation
	_, err = k.Keeper.CreateInvitation(
		ctx,
		inviterAddr,
		inviteeAddr,
		*msg.StakedDream,
		msg.VouchedTags,
	)
	if err != nil {
		return nil, err
	}

	return &types.MsgInviteMemberResponse{}, nil
}
