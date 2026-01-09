package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) SubmitInitiativeWork(ctx context.Context, msg *types.MsgSubmitInitiativeWork) (*types.MsgSubmitInitiativeWorkResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// TODO: Handle the message

	return &types.MsgSubmitInitiativeWorkResponse{}, nil
}
