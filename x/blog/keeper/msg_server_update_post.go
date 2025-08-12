package keeper

import (
	"context"

	"sparkdream/x/blog/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) UpdatePost(ctx context.Context, msg *types.MsgUpdatePost) (*types.MsgUpdatePostResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// TODO: Handle the message

	return &types.MsgUpdatePostResponse{}, nil
}
