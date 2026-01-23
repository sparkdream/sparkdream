package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) TransferDream(ctx context.Context, msg *types.MsgTransferDream) (*types.MsgTransferDreamResponse, error) {
	senderAddr, err := k.addressCodec.StringToBytes(msg.Sender)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid sender address")
	}

	recipientAddr, err := sdk.AccAddressFromBech32(msg.Recipient)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid recipient address")
	}

	// Transfer DREAM
	if err := k.Keeper.TransferDREAM(
		ctx,
		senderAddr,
		recipientAddr,
		*msg.Amount,
		msg.Purpose,
	); err != nil {
		return nil, err
	}

	return &types.MsgTransferDreamResponse{}, nil
}
