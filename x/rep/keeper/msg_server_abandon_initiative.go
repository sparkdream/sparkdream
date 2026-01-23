package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) AbandonInitiative(ctx context.Context, msg *types.MsgAbandonInitiative) (*types.MsgAbandonInitiativeResponse, error) {
	creatorAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Abandon initiative (creator must be the assignee)
	err = k.Keeper.AbandonInitiative(ctx, msg.InitiativeId, creatorAddr, msg.Reason)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to abandon initiative")
	}

	return &types.MsgAbandonInitiativeResponse{}, nil
}
