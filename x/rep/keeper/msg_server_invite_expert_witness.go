package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) InviteExpertWitness(ctx context.Context, msg *types.MsgInviteExpertWitness) (*types.MsgInviteExpertWitnessResponse, error) {
	jurorAddr, err := k.addressCodec.StringToBytes(msg.Juror)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid juror address")
	}
	expertAddr, err := k.addressCodec.StringToBytes(msg.Expert)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid expert address")
	}

	interimID, err := k.Keeper.InviteExpertWitness(ctx, msg.JuryReviewId, jurorAddr, expertAddr)
	if err != nil {
		return nil, err
	}

	return &types.MsgInviteExpertWitnessResponse{InterimId: interimID}, nil
}
