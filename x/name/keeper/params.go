package keeper

import (
	"sparkdream/x/name/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SetParams sets the module's parameters.
func (k Keeper) SetParams(ctx sdk.Context, params types.Params) error {
	return k.Params.Set(ctx, params)
}
