package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/forum/types"
)

// IsShieldCompatible implements the x/shield ShieldAware interface.
// Returns true if the message type is compatible with shield execution.
func (k Keeper) IsShieldCompatible(_ context.Context, msg sdk.Msg) bool {
	switch msg.(type) {
	case *types.MsgCreatePost:
		return true
	case *types.MsgUpvotePost:
		return true
	case *types.MsgDownvotePost:
		return true
	default:
		return false
	}
}
