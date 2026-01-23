package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) SubmitInitiativeWork(ctx context.Context, msg *types.MsgSubmitInitiativeWork) (*types.MsgSubmitInitiativeWorkResponse, error) {
	creatorAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Submit work for initiative (creator is the assignee)
	err = k.Keeper.SubmitInitiativeWork(ctx, msg.InitiativeId, creatorAddr, msg.DeliverableUri)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to submit initiative work")
	}

	return &types.MsgSubmitInitiativeWorkResponse{}, nil
}
