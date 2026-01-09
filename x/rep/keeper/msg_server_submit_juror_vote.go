package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) SubmitJurorVote(ctx context.Context, msg *types.MsgSubmitJurorVote) (*types.MsgSubmitJurorVoteResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Juror); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// TODO: Handle the message

	return &types.MsgSubmitJurorVoteResponse{}, nil
}
