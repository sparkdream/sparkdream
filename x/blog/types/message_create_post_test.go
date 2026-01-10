package types

import (
	"testing"

	"sparkdream/testutil/sample"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
)

func TestMsgCreatePost_ValidateBasic(t *testing.T) {
	tests := []struct {
		name string
		msg  MsgCreatePost
		err  error
	}{
		{
			name: "empty creator address",
			msg: MsgCreatePost{
				Creator: "",
				Title:   "Valid Title",
				Body:    "Valid body",
			},
			err: sdkerrors.ErrInvalidAddress,
		},
		{
			name: "empty title",
			msg: MsgCreatePost{
				Creator: sample.AccAddress(),
				Title:   "",
				Body:    "Valid body",
			},
			err: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "empty body",
			msg: MsgCreatePost{
				Creator: sample.AccAddress(),
				Title:   "Valid Title",
				Body:    "",
			},
			err: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "valid message with long title",
			msg: MsgCreatePost{
				Creator: sample.AccAddress(),
				Title:   string(make([]byte, 500)), // Length validation happens in keeper
				Body:    "Valid body",
			},
		},
		{
			name: "valid message with long body",
			msg: MsgCreatePost{
				Creator: sample.AccAddress(),
				Title:   "Valid Title",
				Body:    string(make([]byte, 20000)), // Length validation happens in keeper
			},
		},
		{
			name: "valid message",
			msg: MsgCreatePost{
				Creator: sample.AccAddress(),
				Title:   "Valid Title",
				Body:    "Valid body",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.ValidateBasic()
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
				return
			}
			require.NoError(t, err)
		})
	}
}
