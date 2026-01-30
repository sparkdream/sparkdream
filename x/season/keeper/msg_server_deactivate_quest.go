package keeper

import (
	"context"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) DeactivateQuest(ctx context.Context, msg *types.MsgDeactivateQuest) (*types.MsgDeactivateQuestResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// TODO: Handle the message

	return &types.MsgDeactivateQuestResponse{}, nil
}
