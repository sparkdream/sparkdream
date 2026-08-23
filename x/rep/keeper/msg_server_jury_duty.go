package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) AcceptJuryDuty(ctx context.Context, msg *types.MsgAcceptJuryDuty) (*types.MsgAcceptJuryDutyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Juror); err != nil {
		return nil, errorsmod.Wrap(err, "invalid juror address")
	}
	if err := k.Keeper.AcceptJuryDuty(ctx, msg.JuryReviewId, msg.Juror); err != nil {
		return nil, err
	}
	return &types.MsgAcceptJuryDutyResponse{}, nil
}

func (k msgServer) DeclineJuryDuty(ctx context.Context, msg *types.MsgDeclineJuryDuty) (*types.MsgDeclineJuryDutyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Juror); err != nil {
		return nil, errorsmod.Wrap(err, "invalid juror address")
	}
	if err := k.Keeper.DeclineJuryDuty(ctx, msg.JuryReviewId, msg.Juror); err != nil {
		return nil, err
	}
	return &types.MsgDeclineJuryDutyResponse{}, nil
}
