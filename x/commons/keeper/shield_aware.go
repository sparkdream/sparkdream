package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/commons/types"
)

// IsShieldCompatible implements the x/shield ShieldAware interface.
// Returns true if the message type is designed for anonymous execution
// through the shield module.
func (k Keeper) IsShieldCompatible(_ context.Context, msg sdk.Msg) bool {
	switch msg.(type) {
	case *types.MsgSubmitAnonymousProposal:
		return true
	case *types.MsgAnonymousVoteProposal:
		return true
	default:
		return false
	}
}
