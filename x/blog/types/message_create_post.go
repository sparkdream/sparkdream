package types

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var _ sdk.Msg = &MsgCreatePost{}

func NewMsgCreatePost(creator string, title string, body string) *MsgCreatePost {
	return &MsgCreatePost{
		Creator: creator,
		Title:   title,
		Body:    body,
	}
}

// ValidateBasic performs basic stateless validation.
// Note: Full validation including length constraints happens in the keeper using params.
func (msg *MsgCreatePost) ValidateBasic() error {
	if len(msg.Creator) == 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "creator address cannot be empty")
	}

	// Basic non-empty checks - length validation happens in keeper with params
	if len(msg.Title) == 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "title cannot be empty")
	}

	if len(msg.Body) == 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "body cannot be empty")
	}

	return nil
}
