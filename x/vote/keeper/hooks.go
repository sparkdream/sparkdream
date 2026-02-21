package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/vote/types"
)

// OnMemberRevoked is called by x/rep when a member is removed/suspended/zeroed.
// It deactivates the member's voter registration if active.
func (k Keeper) OnMemberRevoked(ctx context.Context, member sdk.AccAddress, reason string) {
	addr, err := k.addressCodec.BytesToString(member)
	if err != nil {
		return
	}

	reg, err := k.VoterRegistration.Get(ctx, addr)
	if err != nil {
		return // not registered
	}

	if !reg.Active {
		return // already inactive
	}

	reg.Active = false
	if err := k.VoterRegistration.Set(ctx, addr, reg); err != nil {
		return
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventVoterDeactivated,
		sdk.NewAttribute(types.AttributeVoter, addr),
		sdk.NewAttribute(types.AttributeReason, reason),
	))
}
