package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) CompleteInitiative(ctx context.Context, msg *types.MsgCompleteInitiative) (*types.MsgCompleteInitiativeResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Complete the initiative (handles all rewards distribution)
	if err := k.Keeper.CompleteInitiative(ctx, msg.InitiativeId); err != nil {
		return nil, err
	}

	return &types.MsgCompleteInitiativeResponse{}, nil
}
