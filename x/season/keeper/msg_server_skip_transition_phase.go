package keeper

import (
	"context"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) SkipTransitionPhase(ctx context.Context, msg *types.MsgSkipTransitionPhase) (*types.MsgSkipTransitionPhaseResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// TODO: Handle the message

	return &types.MsgSkipTransitionPhaseResponse{}, nil
}
