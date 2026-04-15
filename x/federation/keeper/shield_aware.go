package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// IsShieldCompatible implements x/shield's ShieldAware interface.
// Returns true for MsgSubmitArbiterHash, which is the only message
// that anonymous members can submit via x/shield for quorum-based
// challenge resolution.
func (k Keeper) IsShieldCompatible(_ context.Context, msg sdk.Msg) bool {
	switch msg.(type) {
	case *types.MsgSubmitArbiterHash:
		return true
	default:
		return false
	}
}
