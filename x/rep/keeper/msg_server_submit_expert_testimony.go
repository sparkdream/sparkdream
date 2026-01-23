package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) SubmitExpertTestimony(ctx context.Context, msg *types.MsgSubmitExpertTestimony) (*types.MsgSubmitExpertTestimonyResponse, error) {
	expertAddr, err := k.addressCodec.StringToBytes(msg.Expert)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid expert address")
	}

	// Submit the expert testimony
	if err := k.Keeper.SubmitExpertTestimony(
		ctx,
		msg.JuryReviewId,
		expertAddr,
		msg.Opinion,
		msg.Reasoning,
	); err != nil {
		return nil, err
	}

	return &types.MsgSubmitExpertTestimonyResponse{}, nil
}
