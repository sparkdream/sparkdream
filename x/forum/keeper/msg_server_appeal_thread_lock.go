package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) AppealThreadLock(ctx context.Context, msg *types.MsgAppealThreadLock) (*types.MsgAppealThreadLockResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// TODO: Handle the message

	return &types.MsgAppealThreadLockResponse{}, nil
}
