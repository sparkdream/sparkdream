package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

// IsShieldCompatible implements the x/shield ShieldAware interface.
// Returns true if the message type is designed for anonymous execution
// through the shield module.
func (k Keeper) IsShieldCompatible(_ context.Context, msg sdk.Msg) bool {
	switch msg.(type) {
	case *types.MsgCreateCollection:
		return true
	case *types.MsgUpvoteContent:
		return true
	case *types.MsgDownvoteContent:
		return true
	default:
		return false
	}
}
