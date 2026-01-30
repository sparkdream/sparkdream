package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) AssignBountyToReply(ctx context.Context, msg *types.MsgAssignBountyToReply) (*types.MsgAssignBountyToReplyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// TODO: Handle the message

	return &types.MsgAssignBountyToReplyResponse{}, nil
}
