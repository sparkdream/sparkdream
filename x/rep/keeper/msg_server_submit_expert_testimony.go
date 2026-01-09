package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) SubmitExpertTestimony(ctx context.Context, msg *types.MsgSubmitExpertTestimony) (*types.MsgSubmitExpertTestimonyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Expert); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// TODO: Handle the message

	return &types.MsgSubmitExpertTestimonyResponse{}, nil
}
