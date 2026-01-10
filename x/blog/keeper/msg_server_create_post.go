package keeper

import (
	"context"

	"sparkdream/x/blog/types"

	errorsmod "cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k msgServer) CreatePost(ctx context.Context, msg *types.MsgCreatePost) (*types.MsgCreatePostResponse, error) {
	// Validate creator address
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Get params for validation
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Validate title
	if len(msg.Title) == 0 {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "title cannot be empty")
	}
	if uint64(len(msg.Title)) > params.MaxTitleLength {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
			"title exceeds maximum length of %d characters", params.MaxTitleLength)
	}

	// Validate body
	if len(msg.Body) == 0 {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "body cannot be empty")
	}
	if uint64(len(msg.Body)) > params.MaxBodyLength {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
			"body exceeds maximum length of %d characters", params.MaxBodyLength)
	}

	var post = types.Post{
		Creator: msg.Creator,
		Title:   msg.Title,
		Body:    msg.Body,
	}
	id := k.AppendPost(
		ctx,
		post,
	)
	return &types.MsgCreatePostResponse{
		Id: id,
	}, nil
}
