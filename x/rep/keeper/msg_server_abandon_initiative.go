package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) AbandonInitiative(ctx context.Context, msg *types.MsgAbandonInitiative) (*types.MsgAbandonInitiativeResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// TODO: Handle the message

	return &types.MsgAbandonInitiativeResponse{}, nil
}
