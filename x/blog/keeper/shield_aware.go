package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/blog/types"
)

// IsShieldCompatible implements the x/shield ShieldAware interface.
// Returns true if the message type is designed for anonymous execution
// through the shield module.
func (k Keeper) IsShieldCompatible(_ context.Context, msg sdk.Msg) bool {
	switch msg.(type) {
	case *types.MsgCreatePost:
		return true
	case *types.MsgCreateReply:
		return true
	case *types.MsgReact:
		return true
	default:
		return false
	}
}
