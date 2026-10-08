package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	any "github.com/cosmos/gogoproto/types/any"

	"sparkdream/x/shield/types"
)

// ResolveNullifierScope exposes resolveNullifierScope to the keeper_test package.
func (k Keeper) ResolveNullifierScope(ctx context.Context, reg types.ShieldedOpRegistration, msg *types.MsgShieldedExec) (uint64, error) {
	return k.resolveNullifierScope(ctx, reg, msg)
}

// ExecuteInnerMessage exposes executeInnerMessage to the keeper_test package.
func (k Keeper) ExecuteInnerMessage(ctx sdk.Context, params types.Params, inner *any.Any) (*any.Any, error) {
	return k.executeInnerMessage(ctx, params, inner)
}
