package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) SubmitInterimWork(ctx context.Context, msg *types.MsgSubmitInterimWork) (*types.MsgSubmitInterimWorkResponse, error) {
	assigneeAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid assignee address")
	}

	// Submit work using the keeper method
	if err := k.Keeper.SubmitInterimWork(ctx, msg.InterimId, assigneeAddr, msg.DeliverableUri, msg.Comments); err != nil {
		return nil, errorsmod.Wrap(err, "failed to submit interim work")
	}

	return &types.MsgSubmitInterimWorkResponse{}, nil
}
